package dbstore

import (
	"database/sql"
	"strings"

	_ "modernc.org/sqlite"
)

func Init_db() (*sql.DB, error) {
	db, err := sql.Open("sqlite", "zip.sqlite")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS paths (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            value TEXT NOT NULL
        );
    `)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func InsertStrings(db *sql.DB, values []string) error {
	const batchSize = 500 // reste sous la limite de paramètres SQLite

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op si Commit a réussi

	for i := 0; i < len(values); i += batchSize {
		end := min(i+batchSize, len(values))
		batch := values[i:end]

		placeholders := strings.Repeat("(?),", len(batch))
		placeholders = placeholders[:len(placeholders)-1] // retire la dernière virgule

		args := make([]any, len(batch))
		for j, v := range batch {
			args[j] = v
		}

		query := "INSERT INTO paths(value) VALUES " + placeholders
		if _, err := tx.Exec(query, args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func GetAllValues(db *sql.DB) ([]string, error) {
	// get number of rows to allocate the right amount in result
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM paths").Scan(&count); err != nil {
		return nil, err
	}

	rows, err := db.Query("SELECT value FROM paths")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]string, 0, count)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}

	return result, rows.Err()
}