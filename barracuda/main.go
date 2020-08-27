package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	dbconn "github.com/go-jwdk/db-connector"
	"github.com/go-jwdk/jobworker"
	mysqlConn "github.com/go-jwdk/mysql-connector"
	pbReplication "github.com/vvatanabe/git-ha-poc/proto/replication"
	"github.com/vvatanabe/git-ha-poc/shared/replication"
	"google.golang.org/grpc"
)

const (
	appName = "barracuda"
)

var (
	dsn string
)

func init() {
	log.SetFlags(0)
	log.SetPrefix(fmt.Sprintf("[%s] ", appName))

	dsn = os.Getenv("DSN")
}

func main() {
	jwConn, err := mysqlConn.Open(&mysqlConn.Config{
		DSN: dsn,
	})
	if err != nil {
		log.Fatalln("failed to open conn", err)
	}
	jwConn.SetLoggerFunc(log.Println)

	_, err = jwConn.CreateQueue(context.Background(), &dbconn.CreateQueueInput{
		Name:              replication.LogQueueName,
		DelaySeconds:      2,
		VisibilityTimeout: 60,
		MaxReceiveCount:   10,
	})
	if err != nil {
		log.Fatalln("failed to create replication log queue", err)
	}

	worker, err := jobworker.New(&jobworker.Setting{
		Primary:                    jwConn,
		DeadConnectorRetryInterval: 3,
		LoggerFunc:                 log.Println,
	})
	if err != nil {
		log.Fatalln("failed to init worker", err)
	}

	worker.RegisterFunc(replication.LogQueueName, replicate)

	go func() {
		log.Println("start replication worker")
		err := worker.Work(&jobworker.WorkSetting{
			WorkerConcurrency: 5,
		})
		if err != nil {
			log.Println("failed to start replication worker", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	<-quit

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()

	if err := worker.Shutdown(ctx); err != nil {
		log.Println("failed to shutdown:", err)
	}
	log.Println("completed shutdown")

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
		log.Println("failed to replication ", err)
		return err
	}

	return nil
}
