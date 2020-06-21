package main

type Config struct {
	Port     int    `mapstructure:"port"`
	Verbose  bool   `mapstructure:"verbose"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
	Zones    []Zone `mapstructure:"zones"`
	Users    []User `mapstructure:"repos"`
}

type Zone struct {
	Name  string `mapstructure:"name"`
	Nodes []Node `mapstructure:"nodes"`
}

type Node struct {
	Addr       string `mapstructure:"addr"`
	Writable   bool   `mapstructure:"writable"`
	CertFile   string `mapstructure:"cert_file"`
	ServerName string `mapstructure:"server_name"`
}

type User struct {
	Name string `mapstructure:"name"`
	Zone string `mapstructure:"zone"`
}
