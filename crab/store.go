package main

import (
	"database/sql"
)

type Store struct {
	db *sql.DB
}

func newStore(dsn string) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) GetEncryptedPassword(username string) ([]byte, error) {
	row := s.db.QueryRow(`
select encrypted_password from user where name = ?;
`, username)
	var out []byte
	err := row.Scan(&out)
	if err != nil {
		return nil, err
	}
	return out, nil
}
