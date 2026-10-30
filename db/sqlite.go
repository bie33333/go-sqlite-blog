package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	// _ "github.com/mattn/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

func ConnectDB(driver string, source string) (*sql.DB, error) {
	return sql.Open(driver, source)
}

func CloseDB(db *sql.DB) error {
	return db.Close()
}

func CreateTables(logger *slog.Logger, db *sql.DB, initFile string) error {
	cleanPath := filepath.Clean(initFile)
	if strings.Contains(cleanPath, "..") {
		logger.Error("CreateTables: Invalid file path", "path", cleanPath)
		return fmt.Errorf("invalid file path")
	}

	// read tables.sql
	content, err := os.ReadFile(initFile)
	if err != nil {
		logger.Error("CreateTables: Error reading init file", "error", err)
		return err
	}

	// execute the content
	_, err = db.Exec(string(content))
	if err != nil {
		logger.Error("CreateTables: Error executing init file", "error", err)
		return err
	}

	return nil
}

func Init(logger *slog.Logger, driver string, source string, initFile string) (*sql.DB, error) {
	db, err := ConnectDB(driver, source)
	if err != nil {
		return nil, err
	}

	err = CreateTables(logger, db, initFile)
	if err != nil {
		return nil, err
	}

	if err := migrateArticleApproval(db); err != nil {
		return nil, fmt.Errorf("Init: migrate article approval: %w", err)
	}

	return db, nil
}

func migrateArticleApproval(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(articles)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "approved" {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE articles ADD COLUMN approved BOOLEAN NOT NULL DEFAULT 1`)
	return err
}
