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

func (s *Store) GetUserBySSHKey(key string) (string, error) {
	row := s.db.QueryRow(`
select name from user where ssh_key = ?;
`, key)
	var out string
	err := row.Scan(&out)
	if err != nil {
		return "", err
	}
	return out, nil
}
