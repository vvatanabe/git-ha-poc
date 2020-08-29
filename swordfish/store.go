package main

import (
	"context"
	"database/sql"
)

type Store struct {
	db *sql.DB
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
