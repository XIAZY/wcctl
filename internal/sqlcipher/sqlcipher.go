// Package sqlcipher provides the read-only SQLCipher access used by wcctl.
package sqlcipher

/*
#cgo CFLAGS: -O2 -DSQLITE_HAS_CODEC -DSQLITE_TEMP_STORE=2 -DSQLITE_THREADSAFE=1
#cgo CFLAGS: -DSQLITE_EXTRA_INIT=sqlcipher_extra_init -DSQLITE_EXTRA_SHUTDOWN=sqlcipher_extra_shutdown
#cgo CFLAGS: -DSQLCIPHER_CRYPTO_CC -DSQLITE_OMIT_LOAD_EXTENSION
#cgo darwin LDFLAGS: -framework Security -framework CoreFoundation

#include <stdlib.h>
#include "sqlite3.h"
*/
import "C"

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"unsafe"
)

const setupSQL = `
PRAGMA cipher_page_size=4096;
PRAGMA cipher_hmac_algorithm=HMAC_SHA512;
PRAGMA cipher_kdf_algorithm=PBKDF2_HMAC_SHA512;
PRAGMA temp_store=MEMORY;
PRAGMA query_only=ON;
`

// Query creates a disposable copy-on-write clone of a SQLCipher database and
// returns all rows from a single read-only statement. aesKey is WeChat's
// 32-byte final database key. SQLite never opens the live database.
func Query(path string, aesKey []byte, statement string) (rows []map[string]any, resultErr error) {
	if len(aesKey) != 32 {
		return nil, fmt.Errorf("database key must be 32 bytes")
	}
	copy, err := makeWorkingCopy(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, copy.Close())
	}()

	salt, err := readSalt(copy.path)
	if err != nil {
		return nil, err
	}
	return queryTarget(copy.path, appendRawKey(aesKey, salt), statement)
}

func readSalt(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	salt := make([]byte, 16)
	_, readErr := io.ReadFull(file, salt)
	closeErr := file.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read database salt: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close database: %w", closeErr)
	}
	return salt, nil
}

func appendRawKey(aesKey, salt []byte) string {
	buffer := make([]byte, 0, 3+2*(len(aesKey)+len(salt)))
	buffer = append(buffer, 'x', '\'')
	encoded := make([]byte, hex.EncodedLen(len(aesKey)+len(salt)))
	material := make([]byte, 0, len(aesKey)+len(salt))
	material = append(material, aesKey...)
	material = append(material, salt...)
	hex.Encode(encoded, material)
	buffer = append(buffer, encoded...)
	buffer = append(buffer, '\'')
	return string(buffer)
}

func queryTarget(target, keySpec, statement string) ([]map[string]any, error) {
	cTarget := C.CString(target)
	defer C.free(unsafe.Pointer(cTarget))

	var database *C.sqlite3
	// The working copy is private and disposable, so allow SQLite to create a
	// new SHM file or recover a rollback journal there. query_only and the
	// statement check below still prohibit caller-requested writes.
	flags := C.int(C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_URI | C.SQLITE_OPEN_NOMUTEX | C.SQLITE_OPEN_EXRESCODE)
	if rc := C.sqlite3_open_v2(cTarget, &database, flags, nil); rc != C.SQLITE_OK {
		err := sqliteError(database, "open database", rc)
		if database != nil {
			C.sqlite3_close_v2(database)
		}
		return nil, err
	}
	defer C.sqlite3_close_v2(database)
	C.sqlite3_busy_timeout(database, 1000)

	key := C.CBytes([]byte(keySpec))
	defer C.free(key)
	if rc := C.sqlite3_key(database, key, C.int(len(keySpec))); rc != C.SQLITE_OK {
		return nil, sqliteError(database, "set database key", rc)
	}
	if err := execute(database, setupSQL); err != nil {
		return nil, err
	}
	return selectRows(database, statement)
}

func execute(database *C.sqlite3, statement string) error {
	cStatement := C.CString(statement)
	defer C.free(unsafe.Pointer(cStatement))
	var errorMessage *C.char
	if rc := C.sqlite3_exec(database, cStatement, nil, nil, &errorMessage); rc != C.SQLITE_OK {
		if errorMessage != nil {
			message := C.GoString(errorMessage)
			C.sqlite3_free(unsafe.Pointer(errorMessage))
			return fmt.Errorf("configure database: %s (sqlite code %d)", message, int(rc))
		}
		return sqliteError(database, "configure database", rc)
	}
	return nil
}

func selectRows(database *C.sqlite3, statement string) ([]map[string]any, error) {
	cStatement := C.CString(statement)
	defer C.free(unsafe.Pointer(cStatement))

	var prepared *C.sqlite3_stmt
	if rc := C.sqlite3_prepare_v2(database, cStatement, -1, &prepared, nil); rc != C.SQLITE_OK {
		return nil, sqliteError(database, "prepare query", rc)
	}
	defer C.sqlite3_finalize(prepared)
	if C.sqlite3_stmt_readonly(prepared) == 0 {
		return nil, fmt.Errorf("refusing to execute a statement that may modify the database")
	}

	columnCount := int(C.sqlite3_column_count(prepared))
	columnNames := make([]string, columnCount)
	for column := range columnCount {
		columnNames[column] = C.GoString(C.sqlite3_column_name(prepared, C.int(column)))
	}

	rows := make([]map[string]any, 0)
	for {
		rc := C.sqlite3_step(prepared)
		switch rc {
		case C.SQLITE_DONE:
			return rows, nil
		case C.SQLITE_ROW:
			row := make(map[string]any, columnCount)
			for column, name := range columnNames {
				row[name] = columnValue(prepared, C.int(column))
			}
			rows = append(rows, row)
		default:
			return nil, sqliteError(database, "execute query", rc)
		}
	}
}

func columnValue(statement *C.sqlite3_stmt, column C.int) any {
	switch C.sqlite3_column_type(statement, column) {
	case C.SQLITE_INTEGER:
		return int64(C.sqlite3_column_int64(statement, column))
	case C.SQLITE_FLOAT:
		return float64(C.sqlite3_column_double(statement, column))
	case C.SQLITE_TEXT:
		length := C.sqlite3_column_bytes(statement, column)
		value := C.sqlite3_column_text(statement, column)
		return C.GoStringN((*C.char)(unsafe.Pointer(value)), length)
	case C.SQLITE_BLOB:
		length := C.sqlite3_column_bytes(statement, column)
		if length == 0 {
			return []byte{}
		}
		return C.GoBytes(C.sqlite3_column_blob(statement, column), length)
	default:
		return nil
	}
}

func sqliteError(database *C.sqlite3, operation string, code C.int) error {
	message := C.GoString(C.sqlite3_errstr(code))
	if database != nil {
		message = C.GoString(C.sqlite3_errmsg(database))
	}
	return fmt.Errorf("%s: %s (sqlite code %d)", operation, message, int(code))
}
