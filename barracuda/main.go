package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/go-jwdk/jobworker"
	"github.com/go-jwdk/mysql-connector"
	pbReplication "github.com/vvatanabe/git-ha-poc/proto/replication"
	"google.golang.org/grpc"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("[barracuda] ")

	jwConn, err := mysql.Open(&mysql.Config{
		DSN: "", // TODO
	})
	if err != nil {
		// TODO
		log.Fatalln("failed to open conn", err)
	}
	jw, err := jobworker.New(&jobworker.Setting{
		Primary:                    jwConn, // TODO
		DeadConnectorRetryInterval: 3,
		LoggerFunc:                 log.Println,
	})
	if err != nil {
		// TODO
		log.Fatalln("failed to init worker", err)
	}

	jw.RegisterFunc("replication", func(job *jobworker.Job) error {
		var content ReplicationJob
		json.Unmarshal([]byte(job.Content), &job)

		conn, err := grpc.Dial(content.TargetNode, grpc.WithInsecure())
		if err != nil {
			log.Fatalf("failed to dial: %s", err)
		}
		defer conn.Close()
		client := pbReplication.NewReplicationServiceClient(conn)
		_, err = client.ReplicateRepository(context.Background(), &pbReplication.ReplicateRepositoryRequest{
			Repository: &pbReplication.Repository{
				User: content.User,
				Repo: content.Repo,
			},
			RemoteAddr: content.RemoteNode,
		})
		if err != nil {
			return err
		}
		return nil
	})

	jw.Work(&jobworker.WorkSetting{
		WorkerConcurrency: 10,
	})

}

type ReplicationJob struct {
	Ope        string `json:"ope"`
	User       string `json:"use"`
	Repo       string `json:"repo"`
	TargetNode string `json:"target_node"`
	RemoteNode string `json:"remote_node"`
	Zone       string `json:"zone"`
}
