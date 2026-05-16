package db

import (
	"database/sql"
	"fmt"
	"runtime"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	Write *sql.DB
	Read  *sql.DB
}

func (d *DB) Close() error {
	werr := d.Write.Close()
	rerr := d.Read.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

func Open(path string) (*DB, error) {
	if path == "" {
		path = "/data/app.db"
	}
	dsn := fmt.Sprintf(
		"file:%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_synchronous=NORMAL",
		path,
	)

	writeDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	if err := writeDB.Ping(); err != nil {
		return nil, err
	}

	readDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		writeDB.Close()
		return nil, err
	}
	n := max(4, runtime.NumCPU())
	readDB.SetMaxOpenConns(n)
	readDB.SetMaxIdleConns(n)
	if err := readDB.Ping(); err != nil {
		writeDB.Close()
		return nil, err
	}

	return &DB{Write: writeDB, Read: readDB}, nil
}
