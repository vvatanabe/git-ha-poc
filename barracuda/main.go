package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	dbconn "github.com/go-jwdk/db-connector"
	"github.com/go-jwdk/jobworker"
	"github.com/go-jwdk/mysql-connector"
	pbReplication "github.com/vvatanabe/git-ha-poc/proto/replication"
	"google.golang.org/grpc"
)

const replicationQueueName = "replication"

func main() {
	log.SetFlags(0)
	log.SetPrefix("[barracuda] ")

	jwConn, err := mysql.Open(&mysql.Config{
		DSN: os.Getenv("DSN"),
	})
	if err != nil {
		// TODO
		log.Fatalln("failed to open conn", err)
	}
	jwConn.SetLoggerFunc(log.Println)

	_, err = jwConn.CreateQueue(context.Background(), &dbconn.CreateQueueInput{
		Name:              replicationQueueName,
		DelaySeconds:      2,
		VisibilityTimeout: 60,
		MaxReceiveCount:   10,
		DeadLetterTarget:  "",
	})
	if err != nil {
		log.Fatalln("failed to create queue", err)
	}

	worker, err := jobworker.New(&jobworker.Setting{
		Primary:                    jwConn,
		DeadConnectorRetryInterval: 3,
		LoggerFunc:                 log.Println,
	})
	if err != nil {
		// TODO
		log.Fatalln("failed to init worker", err)
	}

	worker.Register(replicationQueueName, &ReplicationWorker{})

	go func() {
		log.Println("Start work")
		err := worker.Work(&jobworker.WorkSetting{
			WorkerConcurrency: 5,
		})
		if err != nil {
			log.Println("failed to work", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()
	log.Println("Received a signal of graceful shutdown")
	if err := worker.Shutdown(ctx); err != nil {
		log.Println("Failed to graceful shutdown:", err)
	}
	log.Println("Completed graceful shutdown")

}

type ReplicationContent struct {
	Type       ReplicationType `json:"type"`
	User       string          `json:"use"`
	Repo       string          `json:"repo"`
	TargetNode string          `json:"target_node"`
	RemoteNode string          `json:"remote_node"`
	Zone       string          `json:"zone"`
}

type ReplicationType string

const (
	CreateRepo ReplicationType = "CreateRepo"
	UpdateRepo ReplicationType = "UpdateRepo"
)

type ReplicationWorker struct {
}

func (r *ReplicationWorker) Work(job *jobworker.Job) error {
	var content ReplicationContent
	err := json.Unmarshal([]byte(job.Content), &job)
	if err != nil {
		return err
	}
	conn, err := grpc.Dial(content.TargetNode, grpc.WithInsecure())
	if err != nil {
		return err
	}
	defer conn.Close()
	client := pbReplication.NewReplicationServiceClient(conn)
	if content.Type == CreateRepo {
		_, err = client.CreateRepository(context.Background(), &pbReplication.CreateRepositoryRequest{
			Repository: &pbReplication.Repository{
				User: content.User,
				Repo: content.Repo,
			},
			RemoteAddr: strings.Split(content.RemoteNode, ":")[0git
		})
	}
	if content.Type == UpdateRepo {
		_, err = client.SyncRepository(context.Background(), &pbReplication.SyncRepositoryRequest{
			Repository: &pbReplication.Repository{
				User: content.User,
				Repo: content.Repo,
			},
			RemoteAddr: strings.Split(content.RemoteNode, ":")[0],
		})
	}
	if err != nil {
		return err
	}
	return nil
}
