package main

type Config struct {
	Port     int    `mapstructure:"port"`
	Verbose  bool   `mapstructure:"verbose"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
	DSN      string `mapstructure:"dsn"`
}

type Cluster struct {
	Name  string
	Nodes []Node
}

type Node struct {
	Name        string
	ClusterName string
	Addr        string
	Writable    bool
	Active      bool
	CertFile    string
}

type User struct {
	Name        string
	ClusterName string
}
