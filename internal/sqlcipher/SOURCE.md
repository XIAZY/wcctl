# Vendored SQLCipher source

`sqlite3.c` and `sqlite3.h` are the generated amalgamation from the official
SQLCipher `v4.16.0` tag at commit
`e2a6040f2ae5cfff2b3e08eb3320007d93cdf3fc`.

Generation command:

```sh
./configure --with-tempstore=yes --disable-shared \
  CFLAGS='-O2 -DSQLITE_HAS_CODEC -DSQLITE_EXTRA_INIT=sqlcipher_extra_init -DSQLITE_EXTRA_SHUTDOWN=sqlcipher_extra_shutdown -DSQLCIPHER_CRYPTO_CC' \
  LDFLAGS='-framework Security -framework CoreFoundation'
make sqlite3.c sqlite3.h
```

SHA-256 checksums:

```text
0c8371853e124f20bb7728368559fe743ac3d1f0b97d317ba68b70bfe802bcb2  sqlite3.c
126a6cdd1a2b2b3f47ec1952defc8e2434ebecfa239f917463e11fdffcfe16dd  sqlite3.h
```

The build flags used by the Go package are kept in `sqlcipher.go`; SQLCipher is
compiled into `wcctl` with Apple's CommonCrypto provider.

`testdata/fixture.db` is a synthetic SQLCipher database containing one sample
row. Its key is intentionally public in `sqlcipher_test.go`; it contains no
user data.
