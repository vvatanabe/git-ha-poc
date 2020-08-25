package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	dbconn "github.com/go-jwdk/db-connector"
	"github.com/go-jwdk/jobworker"
	"github.com/go-jwdk/mysql-connector"

	"github.com/spf13/viper"

	"github.com/spf13/cobra"

	"github.com/vvatanabe/git-ha-poc/internal/grpc-proxy/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	cmdName = "grpc-proxyd"

	flagNameConfig   = "config"
	flagNameVerbose  = "verbose"
	flagNamePort     = "port"
	flagNameCertFile = "cert_file"
	flagNameKeyFile  = "key_file"
	flagNameDSN      = "dsn"
)

var (
	envPrefix = "GRPC_PROXYD"

	config Config
)

func main() {

	rootCmd := &cobra.Command{
		Use: cmdName,
		Run: run,
	}

	flags := rootCmd.PersistentFlags()

	flags.StringP(flagNameConfig, "c", "config.yml", "config file path")
	flags.BoolP(flagNameVerbose, "v", false, fmt.Sprintf("debug mode [%s_%s]", envPrefix, strings.ToUpper(flagNameVerbose)))
	flags.IntP(flagNamePort, "p", 50051, fmt.Sprintf("listen port [%s_%s]", envPrefix, strings.ToUpper(flagNamePort)))
	flags.String(flagNameCertFile, "", fmt.Sprintf("cert file path [%s_%s]", envPrefix, strings.ToUpper(flagNameCertFile)))
	flags.String(flagNameKeyFile, "", fmt.Sprintf("key file path [%s_%s]", envPrefix, strings.ToUpper(flagNameKeyFile)))
	flags.String(flagNameDSN, "", "data source name for replication queue")

	_ = viper.BindPFlag(flagNameVerbose, flags.Lookup(flagNameVerbose))
	_ = viper.BindPFlag(flagNamePort, flags.Lookup(flagNamePort))
	_ = viper.BindPFlag(flagNameCertFile, flags.Lookup(flagNameCertFile))
	_ = viper.BindPFlag(flagNameKeyFile, flags.Lookup(flagNameKeyFile))
	_ = viper.BindPFlag(flagNameDSN, flags.Lookup(flagNameDSN))

	cobra.OnInitialize(func() {
		configFile, err := flags.GetString(flagNameConfig)
		if err != nil {
			printFatal("failed to parse flag of config path", err)
		}
		viper.SetConfigFile(configFile)
		viper.SetConfigType("yaml")
		viper.SetEnvPrefix(envPrefix)
		viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
		viper.AutomaticEnv()
		if err := viper.ReadInConfig(); err != nil {
			printFatal("failed to read config", err)
		}
		if err := viper.Unmarshal(&config); err != nil {
			printFatal("failed to unmarshal config", err)
		}
	})

	if err := rootCmd.Execute(); err != nil {
		printFatal(err)
	}
}

const replicationQueueName = "replication"

func run(cmd *cobra.Command, args []string) {

	// debug config
	if config.Verbose {
		printDebug(fmt.Sprintf("config: %#v", config))
	}

	db, err := sql.Open("mysql", config.DSN)
	if err != nil {
		printFatal("failed to open conn of mysql", err)
	}
	store := &Store{db: db}

	// init replication worker
	jwConn, err := mysql.Open(&mysql.Config{
		DSN: config.DSN,
	})
	if err != nil {
		printFatal("failed to open conn of job queue", err)
	}
	jwConn.SetLoggerFunc(printInfo)

	_, err = jwConn.CreateQueue(context.Background(), &dbconn.CreateQueueInput{
		Name:              replicationQueueName,
		DelaySeconds:      2,
		VisibilityTimeout: 60,
		MaxReceiveCount:   10,
		DeadLetterTarget:  "",
	})
	if err != nil {
		printFatal("failed to create queue", err)
	}

	publisher, err := jobworker.New(&jobworker.Setting{
		Primary:                    jwConn,
		DeadConnectorRetryInterval: 3,
		LoggerFunc:                 printInfo,
	})
	if err != nil {
		printFatal("failed to init worker", err)
	}
	defer publisher.Shutdown(context.Background())

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.Port))

	if err != nil {
		printFatal("failed to listen", err)
	}

	printInfo("proxy running at %s", lis.Addr())

	server := newServer(config, publisher, store)

	go func() {
		if err := server.Serve(lis); err != nil {
			printFatal("failed to serve: %v", err)
		}
	}()

	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)

	<-sigint

	printInfo("received a signal of graceful shutdown")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	go func() {
		<-ctx.Done()
		printError("failed to graceful shutdown", err)
		server.Stop()
	}()
	server.GracefulStop()
	printInfo("completed graceful shutdown")
}

func newServer(config Config, publisher *jobworker.JobWorker, store *Store) *grpc.Server {
	var opts []grpc.ServerOption

	opts = append(opts, grpc.CustomCodec(proxy.NewCodec()),
		grpc.UnknownServiceHandler(proxy.TransparentHandler(getDirector(config, publisher, store))))

	if config.CertFile != "" && config.KeyFile != "" {
		creds, err := credentials.NewServerTLSFromFile(config.CertFile, config.KeyFile)
		if err != nil {
			printFatal("Failed to generate credentials", err)
		}
		opts = append(opts, grpc.Creds(creds))
	}

	return grpc.NewServer(opts...)
}

func printDebug(args ...interface{}) {
	args = append([]interface{}{cmdName + ":", "[DEBUG]"}, args...)
	log.Println(args...)
}

func printInfo(args ...interface{}) {
	args = append([]interface{}{cmdName + ":", "[INFO]"}, args...)
	log.Println(args...)
}

func printError(args ...interface{}) {
	args = append([]interface{}{cmdName + ":", "[ERROR]"}, args...)
	log.Println(args...)
}

func printFatal(args ...interface{}) {
	args = append([]interface{}{cmdName + ":", "[Fatal]"}, args...)
	log.Fatalln(args...)
}
