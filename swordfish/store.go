package main

import (
	"context"
	"database/sql"

	"github.com/vvatanabe/git-ha-poc/shared/replication"
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

func (s *Store) ExistsReplicationLog(id replication.GroupID) (bool, error) {
	row := s.db.QueryRow(`
select sec_id from jwdk_replication_log where group_id = ? limit 1;
`, id)
	var tmp int64
	err := row.Scan(&tmp)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) GetClusterNameByUserName(name string) (string, error) {
	row := s.db.QueryRow(`select cluster_name from user where name=?`, name)
	var clusterName string
	err := row.Scan(&clusterName)
	if err != nil {
		return "", err
	}
	return clusterName, nil
}

func (s *Store) GetNodesByClusterName(ctx context.Context, name string) ([]*Node, error) {
	q := `
select
	n.*
from
	cluster c join node n on c.name = n.cluster_name
where c.name=? and n.active = 1
`
	rows, err := s.db.QueryContext(ctx, q, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []*Node
	for rows.Next() {
		var (
			node     Node
			active   int
			writable int
		)
		err := rows.Scan(&node.Name, &node.ClusterName, &node.Addr, &writable, &active)
		if err != nil {
			return nil, err
		}
		node.Active = active > 0
		node.Writable = writable > 0
		nodes = append(nodes, &node)
	}
	return nodes, nil
}
