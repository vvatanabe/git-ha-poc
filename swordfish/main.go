package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-jwdk/aws-sqs-connector"
	_ "github.com/go-jwdk/db-connector"
	"github.com/go-jwdk/jobworker"
	"github.com/pires/go-proxyproto"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/vvatanabe/git-ha-poc/internal/grpc-proxy/proxy"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

const (
	cmdName   = "git-proxy"
	envPrefix = "git_proxy"

	defaultPort            = 50051
	defaultShutdownTimeout = 30 * time.Second

	flagNamePort               = "port"
	flagNameTCPHealthCheckPort = "tcp_health_check_port"
	flagNameDSN                = "dsn"
	flagNameShutdownTimeout    = "shutdown_timeout"
	flagNameProxyProtocol      = "proxy_protocol"

	flagNameReplicationQueueSource    = "replication_queue_source"
	flagNameReplicationQueueAWSRegion = "replication_queue_aws_region"
	flagNameReplicationQueueDSN       = "replication_queue_dsn"

	connMaxLifetime    = 10 * time.Second
	connInactiveExpire = 3 * time.Second
)

type Config struct {
	Port                      int           `mapstructure:"port"`
	TCPHealthCheckPort        int           `mapstructure:"tcp_health_check_port"`
	DSN                       string        `mapstructure:"dsn"`
	ShutdownTimeout           time.Duration `mapstructure:"shutdown_timeout"`
	ProxyProtocol             bool          `mapstructure:"proxy_protocol"`
	ReplicationQueueSource    string        `mapstructure:"replication_queue_source"`
	ReplicationQueueAWSRegion string        `mapstructure:"replication_queue_aws_region"`
	ReplicationQueueDSN       string        `mapstructure:"replication_queue_dsn"`
}

var (
	config Config
	sugar  *zap.SugaredLogger
)

func init() {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		zap.NewAtomicLevel(),
	))
	sugar = logger.Sugar()
}

func main() {
	defer sugar.Sync() // flushes buffer, if any

	root := &cobra.Command{
		Use: cmdName,
		Run: run,
	}

	flags := root.PersistentFlags()
	flags.Int(flagNamePort, defaultPort, fmt.Sprintf("port number for HTTP [%s]", getEnvVarName(flagNamePort)))
	flags.Int(flagNameTCPHealthCheckPort, 0, fmt.Sprintf("port number for TCP Health Check [%s] (listen only when you specify port)", getEnvVarName(flagNameTCPHealthCheckPort)))
	flags.String(flagNameDSN, "", fmt.Sprintf("database source name [%s]", getEnvVarName(flagNameDSN)))
	flags.DurationP(flagNameShutdownTimeout, "", defaultShutdownTimeout, fmt.Sprintf("process shutdown timeout [%s]", getEnvVarName(flagNameShutdownTimeout)))
	flags.Bool(flagNameProxyProtocol, false, fmt.Sprintf("use proxy protocol [%s]", getEnvVarName(flagNameProxyProtocol)))
	flags.String(flagNameReplicationQueueSource, "", fmt.Sprintf("replication queue source name value: sqs or mysql [%s]", getEnvVarName(flagNameReplicationQueueSource)))
	flags.String(flagNameReplicationQueueAWSRegion, "", fmt.Sprintf("replication queue aws region  [%s]", getEnvVarName(flagNameReplicationQueueAWSRegion)))
	flags.String(flagNameReplicationQueueDSN, "", fmt.Sprintf("replication queue dsn  [%s]", getEnvVarName(flagNameReplicationQueueDSN)))

	_ = viper.BindPFlag(flagNamePort, flags.Lookup(flagNamePort))
	_ = viper.BindPFlag(flagNameTCPHealthCheckPort, flags.Lookup(flagNameTCPHealthCheckPort))
	_ = viper.BindPFlag(flagNameDSN, flags.Lookup(flagNameDSN))
	_ = viper.BindPFlag(flagNameShutdownTimeout, flags.Lookup(flagNameShutdownTimeout))
	_ = viper.BindPFlag(flagNameProxyProtocol, flags.Lookup(flagNameProxyProtocol))
	_ = viper.BindPFlag(flagNameReplicationQueueSource, flags.Lookup(flagNameReplicationQueueSource))
	_ = viper.BindPFlag(flagNameReplicationQueueAWSRegion, flags.Lookup(flagNameReplicationQueueAWSRegion))
	_ = viper.BindPFlag(flagNameReplicationQueueDSN, flags.Lookup(flagNameReplicationQueueDSN))

	cobra.OnInitialize(func() {
		viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
		viper.SetEnvPrefix(envPrefix)
		viper.AutomaticEnv()
		if err := viper.Unmarshal(&config); err != nil {
			sugar.Fatal("failed to unmarshal config:", err)
		}
	})

	if err := root.Execute(); err != nil {
		sugar.Fatal("failed to run cmd:", err)
	}
}

func getEnvVarName(s string) string {
	return strings.ToUpper(envPrefix + "_" + s)
}

func run(c *cobra.Command, args []string) {

	sugar.Infof("config: %#v", config)

	ready := make(chan struct{}, 1)
	if config.TCPHealthCheckPort > 0 {
		go func() {
			<-ready
			tcpLis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.TCPHealthCheckPort))
			if err != nil {
				sugar.Fatal("failed to listen tcp:", err)
			}
			sugar.Info("start to serve on tcp port:", config.TCPHealthCheckPort)
			for {
				conn, err := tcpLis.Accept()
				if err != nil {
					sugar.Error("failed to accept tcp:", err)
					continue
				}
				conn.Close()
			}
		}()
	}

	store, err := newStore(config.DSN)
	if err != nil {
		sugar.Fatal("failed to create store:", err)
	}

	jwConn, err := jobworker.Open(config.ReplicationQueueSource, map[string]interface{}{
		"Region": config.ReplicationQueueAWSRegion,
		"DSN":    config.ReplicationQueueDSN,
	})
	if err != nil {
		sugar.Fatal("failed to open queue source connector:", err)
	}
	jwConn.SetLoggerFunc(sugar.Info)

	publisher, err := jobworker.New(&jobworker.Setting{
		Primary:                    jwConn,
		DeadConnectorRetryInterval: 3,
		LoggerFunc:                 sugar.Info,
	})
	if err != nil {
		log.Fatalln("failed to init replication worker", err)
	}
	defer publisher.Shutdown(context.Background())

	srv := newServer(publisher, store)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.Port))
	if err != nil {
		sugar.Fatal("failed to listen grpc:", err)
	}

	if config.ProxyProtocol {
		lis = &proxyproto.Listener{Listener: lis}
	}

	go func() {
		sugar.Info("start to serve on port:", config.Port)
		ready <- struct{}{}
		if err := srv.Serve(lis); err != grpc.ErrServerStopped && err != nil {
			sugar.Error("failed to serve:", err)
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	<-done

	sugar.Info("received a signal")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	go func() {
		<-ctx.Done()
		sugar.Info("timeout shutdown", err)
		srv.Stop()
	}()
	srv.GracefulStop()

	sugar.Info("completed shutdown")
}

func newServer(publisher *jobworker.JobWorker, store *Store) *grpc.Server {
	encoding.RegisterCodec(proxy.NewCodec())
	director := getDirector(publisher, store)
	transparent := proxy.TransparentHandler(director)
	unknown := grpc.UnknownServiceHandler(transparent)
	return grpc.NewServer(unknown)
}
