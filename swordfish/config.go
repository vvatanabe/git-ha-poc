package main

type Config struct {
	Port     int     `mapstructure:"port"`
	Verbose  bool    `mapstructure:"verbose"`
	CertFile string  `mapstructure:"cert_file"`
	KeyFile  string  `mapstructure:"key_file"`
	Routes   []Route `mapstructure:"routes"`
}

type Route struct {
	Match      string `mapstructure:"match"`
	Addr       string `mapstructure:"addr"`
	CertFile   string `mapstructure:"cert_file"`
	ServerName string `mapstructure:"server_name"`
}
