package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-jwdk/aws-sqs-connector"
	_ "github.com/go-jwdk/db-connector"
	"github.com/go-jwdk/jobworker"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	pbReplication "github.com/vvatanabe/git-ha-poc/proto/replication"
	"github.com/vvatanabe/git-ha-poc/shared/replication"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
)

const (
	cmdName   = "git-replication-worker"
	envPrefix = "git_replication_worker"

	defaultShutdownTimeout = 30 * time.Second

	flagNameTCPHealthCheckPort        = "tcp_health_check_port"
	flagNameDSN                       = "dsn"
	flagNameShutdownTimeout           = "shutdown_timeout"
	flagNameReplicationQueueSource    = "replication_queue_source"
	flagNameReplicationQueueAWSRegion = "replication_queue_aws_region"
	flagNameReplicationQueueDSN       = "replication_queue_dsn"
)

type Config struct {
	TCPHealthCheckPort        int           `mapstructure:"tcp_health_check_port"`
	DSN                       string        `mapstructure:"dsn"`
	ShutdownTimeout           time.Duration `mapstructure:"shutdown_timeout"`
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
	flags.Int(flagNameTCPHealthCheckPort, 0, fmt.Sprintf("port number for TCP Health Check [%s] (listen only when you specify port)", getEnvVarName(flagNameTCPHealthCheckPort)))
	flags.String(flagNameDSN, "", fmt.Sprintf("database source name [%s]", getEnvVarName(flagNameDSN)))
	flags.DurationP(flagNameShutdownTimeout, "", defaultShutdownTimeout, fmt.Sprintf("process shutdown timeout [%s]", getEnvVarName(flagNameShutdownTimeout)))
	flags.String(flagNameReplicationQueueSource, "", fmt.Sprintf("replication queue source name value: sqs or mysql [%s]", getEnvVarName(flagNameReplicationQueueSource)))
	flags.String(flagNameReplicationQueueAWSRegion, "", fmt.Sprintf("replication queue aws region  [%s]", getEnvVarName(flagNameReplicationQueueAWSRegion)))
	flags.String(flagNameReplicationQueueDSN, "", fmt.Sprintf("replication queue dsn  [%s]", getEnvVarName(flagNameReplicationQueueDSN)))

	_ = viper.BindPFlag(flagNameTCPHealthCheckPort, flags.Lookup(flagNameTCPHealthCheckPort))
	_ = viper.BindPFlag(flagNameDSN, flags.Lookup(flagNameDSN))
	_ = viper.BindPFlag(flagNameShutdownTimeout, flags.Lookup(flagNameShutdownTimeout))
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

	jwConn, err := jobworker.Open(config.ReplicationQueueSource, map[string]interface{}{
		"Region": config.ReplicationQueueAWSRegion,
		"DSN":    config.ReplicationQueueDSN,
	})
	if err != nil {
		sugar.Fatal("failed to open queue source connector:", err)
	}
	jwConn.SetLoggerFunc(sugar.Info)

	worker, err := jobworker.New(&jobworker.Setting{
		Primary:                    jwConn,
		DeadConnectorRetryInterval: 3,
		LoggerFunc:                 sugar.Info,
	})
	if err != nil {
		sugar.Fatal("failed to init replication worker", err)
	}
	defer worker.Shutdown(context.Background())

	worker.RegisterFunc(replication.LogQueueName, replicate)

	go func() {
		sugar.Info("start replication worker")
		err := worker.Work(&jobworker.WorkSetting{
			WorkerConcurrency: 5,
		})
		if err != nil {
			sugar.Info("failed to start replication worker", err)
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	<-done

	sugar.Info("received a signal")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := worker.Shutdown(ctx); err != nil {
		sugar.Info("failed to shutdown:", err)
	}
	sugar.Info("completed shutdown")
}

func replicate(job *jobworker.Job) error {

	var rLog replication.Log
	err := json.Unmarshal([]byte(job.Content), &rLog)
	if err != nil {
		return fmt.Errorf("failed to unmarshal replication log: %w", err)
	}

	conn, err := grpc.Dial(rLog.TargetNode, grpc.WithInsecure())
	if err != nil {
		return fmt.Errorf("failed to dial replication target node: %w", err)
	}
	defer conn.Close()

	client := pbReplication.NewReplicationServiceClient(conn)
	ctx := context.Background()
	repo := &pbReplication.Repository{
		User: rLog.User,
		Repo: rLog.Repo,
	}
	remoteAddr := strings.Split(rLog.RemoteNode, ":")[0]

	switch rLog.Operation {
	case replication.CreateRepo:
		_, err = client.CreateRepository(ctx, &pbReplication.CreateRepositoryRequest{
			Repository: repo,
			RemoteAddr: remoteAddr,
		})
	case replication.UpdateRepo:
		_, err = client.SyncRepository(context.Background(), &pbReplication.SyncRepositoryRequest{
			Repository: repo,
			RemoteAddr: remoteAddr,
		})
	default:
		return errors.New("unknown type operation: " + string(rLog.Operation))
	}
	if err != nil {
		sugar.Info("failed to replication ", err)
		return err
	}

	return nil
}
