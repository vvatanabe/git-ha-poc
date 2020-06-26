package main

type Config struct {
	Port     int    `mapstructure:"port"`
	Verbose  bool   `mapstructure:"verbose"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
	Zones    []Zone `mapstructure:"zones"`
	Users    []User `mapstructure:"users"`
	DSN      string `mapstructure:"dsn"`
}

func (c *Config) GetZoneNameByUserName(name string) string {
	for _, user := range config.Users {
		if name == user.Name {
			return user.Zone
		}
	}
	return ""
}

func (c *Config) GetNodesByZoneName(name string) []Node {
	for _, zone := range config.Zones {
		if zone.Name == name {
			return zone.Nodes
		}
	}
	return nil
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
