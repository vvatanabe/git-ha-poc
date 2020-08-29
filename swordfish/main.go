package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	dbconn "github.com/go-jwdk/db-connector"
	"github.com/go-jwdk/jobworker"
	"github.com/go-jwdk/mysql-connector"
	"github.com/vvatanabe/git-ha-poc/internal/grpc-proxy/proxy"
	"github.com/vvatanabe/git-ha-poc/shared/replication"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	appName            = "swordfish"
	port               = 50051
	connMaxLifetime    = 10 * time.Second
	connInactiveExpire = 3 * time.Second
)

var (
	dsn      string
	certFile string
	keyFile  string
)

func init() {

	log.SetFlags(0)
	log.SetPrefix(fmt.Sprintf("[%s] ", appName))

	dsn = os.Getenv("DSN")
	certFile = os.Getenv("CERT_FILE")
	keyFile = os.Getenv("KEY_FILE")
}

func main() {

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalln("failed to open conn of mysql", err)
	}
	store := &Store{db: db}

	jwConn, err := mysql.Open(&mysql.Config{
		DSN: dsn,
	})
	if err != nil {
		log.Fatalln("failed to open conn of job queue", err)
	}
	jwConn.SetLoggerFunc(log.Println)

	_, err = jwConn.CreateQueue(context.Background(), &dbconn.CreateQueueInput{
		Name:              replication.LogQueueName,
		DelaySeconds:      2,
		VisibilityTimeout: 60,
		MaxReceiveCount:   10,
		DeadLetterTarget:  "",
	})
	if err != nil {
		log.Fatalln("failed to create replication log queue", err)
	}

	publisher, err := jobworker.New(&jobworker.Setting{
		Primary:                    jwConn,
		DeadConnectorRetryInterval: 3,
		LoggerFunc:                 log.Println,
	})
	if err != nil {
		log.Fatalln("failed to init replication worker", err)
	}
	defer publisher.Shutdown(context.Background())

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalln("failed to listen", err)
	}

	server := newServer(publisher, store)

	go func() {
		if err := server.Serve(lis); err != nil {
			log.Println("failed to serve", err)
		}
	}()

	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)

	<-sigint

	log.Println("received a signal")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	go func() {
		<-ctx.Done()
		log.Println("timeout shutdown", err)
		server.Stop()
	}()
	server.GracefulStop()
	log.Println("completed shutdown")
}

func newServer(publisher *jobworker.JobWorker, store *Store) *grpc.Server {
	var opts []grpc.ServerOption

	opts = append(opts, grpc.CustomCodec(proxy.NewCodec()),
		grpc.UnknownServiceHandler(proxy.TransparentHandler(getDirector(publisher, store))))

	if certFile != "" && keyFile != "" {
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			log.Fatalf("Failed to generate credentials: %v\n", err)
		}
		opts = append(opts, grpc.Creds(creds))
	}

	return grpc.NewServer(opts...)
}
