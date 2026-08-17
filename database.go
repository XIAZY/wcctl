package main

import (
	"encoding/json"
	"fmt"

	"wcctl/internal/sqlcipher"
)

func queryDatabaseJSON(path string, aesKey []byte, statement string, destination any) error {
	return queryDatabaseJSONTarget(path, aesKey, statement, destination)
}

var querySQLCipher = sqlcipher.Query

func queryDatabaseJSONTarget(path string, aesKey []byte, statement string, destination any) error {
	rows, err := querySQLCipher(path, aesKey, statement)
	if err != nil {
		return fmt.Errorf("query database: %w", err)
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("encode query result: %w", err)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("decode query result: %w", err)
	}
	return nil
}
