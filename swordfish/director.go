package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/metadata"

	"github.com/go-jwdk/jobworker"

	"github.com/vvatanabe/git-ha-poc/internal/grpc-proxy/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

type Store struct {
	db *sql.DB
}

func (s *Store) GetZoneNameByUserName(name string) (string, error) {
	row := s.db.QueryRow(`select zone_name from user where name=?`, name)
	var zoneName string
	err := row.Scan(zoneName)
	if err != nil {
		return "", err
	}
	return zoneName, nil
}

func getDirector(config Config, publisher *jobworker.JobWorker, store *Store) func(context.Context, string) (context.Context, *grpc.ClientConn, func(), error) {

	credentialsCache := make(map[string]credentials.TransportCredentials)

	return func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, func(), error) {

		userName := GetUserFromContext(ctx)
		if len(userName) < 1 {
			return ctx, nil, nil, errors.New("no user name in incoming context") // TODO
		}

		repoName := GetRepoFromContext(ctx)
		if len(repoName) < 1 {
			return ctx, nil, nil, errors.New("no repo name in incoming context") // TODO
		}

		zoneName, err := store.GetZoneNameByUserName(userName)
		if err != nil {
			return ctx, nil, nil, errors.New("no zone")
		}

		nodes := config.GetNodesByZoneName(zoneName)
		if len(nodes) == 0 {
			return ctx, nil, nil, errors.New("no nodes")
		}

		ope := getOperation(fullMethodName)
		if ope == Unknown {
			return ctx, nil, nil, errors.New("unknown operation: " + fullMethodName)
		}

		node := selectNode(nodes, ope)
		if node == nil {
			if config.Verbose {
				printInfo(fmt.Sprintf("Not found: %s", fullMethodName))
			}
			return ctx, nil, nil, status.Errorf(codes.Unimplemented, "Unknown method")
		}

		finishedFunc := getFinishedFunc(publisher, nodes, ope, zoneName, userName, repoName)

		if config.Verbose {
			printInfo(fmt.Sprintf("Found: %s > %s", fullMethodName, node.Addr))
		}
		if node.CertFile == "" {
			conn, err := grpc.DialContext(ctx, node.Addr, grpc.WithDefaultCallOptions(grpc.ForceCodec(proxy.NewCodec())),
				grpc.WithInsecure())
			return ctx, conn, finishedFunc, err
		}
		creds := getCredentials(credentialsCache, node)
		if creds != nil {
			conn, err := grpc.DialContext(ctx, node.Addr, grpc.WithDefaultCallOptions(grpc.ForceCodec(proxy.NewCodec())),
				grpc.WithTransportCredentials(creds))
			return ctx, conn, finishedFunc, err
		}
		printFatal("failed to create TLS credentials")
		return ctx, nil, nil, status.Errorf(codes.FailedPrecondition, "Addr TLS is not configured properly in grpc-proxy")
	}
}

type Operation string

const (
	Unknown = "unknown"
	Write   = "Write"
	Read    = "read"
)

func getOperation(fullMethodName string) Operation {
	if strings.HasPrefix(fullMethodName, "/repository.RepositoryService") {
		return Write
	}
	if strings.HasPrefix(fullMethodName, "/smart.SmartProtocolService/PostReceivePack") {
		return Write
	}
	if strings.HasPrefix(fullMethodName, "/smart.SmartProtocolService/PostUploadPack") {
		return Read
	}
	if strings.HasPrefix(fullMethodName, "/smart.SmartProtocolService/GetInfoRefs") { // TODO
		return Read
	}
	return Unknown
}

func selectNode(nodes []Node, ope Operation) *Node {
	shuffled := make(map[Node]struct{})
	for _, node := range nodes {
		shuffled[node] = struct{}{}
	}
	switch ope {
	case Write:
		for node := range shuffled {
			if node.Writable {
				return &node
			}
		}
	case Read:
		for node := range shuffled {
			return &node
		}
	}
	return nil
}

func getFinishedFunc(publisher *jobworker.JobWorker, nodes []Node, ope Operation, zone, user, repo string) func() {
	return func() {
		var entries []*jobworker.EnqueueBatchEntry
		for i, node := range nodes {
			if node.Writable {
				continue
			}
			content, err := json.Marshal(&ReplicationContent{
				Ope:        ope,
				Zone:       zone,
				TargetNode: node.Addr,
				User:       user,
				Repo:       repo,
				RemoteNode: node.Addr,
			})
			if err != nil {
				printError(err)
				return
			}
			entries = append(entries, &jobworker.EnqueueBatchEntry{
				ID:      fmt.Sprintf("id-%d", i),
				Content: string(content),
			})
		}

		out, err := publisher.EnqueueBatch(context.Background(), &jobworker.EnqueueBatchInput{
			Queue:   replicationQueueName,
			Entries: entries,
		})
		if err != nil {
			if out != nil {
				printError(err, out.Failed)
			} else {
				printError(err)
			}
		}
	}
}

type ReplicationContent struct {
	Ope        Operation `json:"ope"`
	User       string    `json:"use"`
	Repo       string    `json:"repo"`
	TargetNode string    `json:"target_node"`
	RemoteNode string    `json:"remote_node"`
	Zone       string    `json:"zone"`
}

func getCredentials(cache map[string]credentials.TransportCredentials, backend *Node) credentials.TransportCredentials {
	if cache[backend.Addr] != nil {
		return cache[backend.Addr]
	}
	creds, err := credentials.NewClientTLSFromFile(backend.CertFile, backend.ServerName)
	if err != nil {
		printFatal(fmt.Sprintf("Failed to create TLS credentials %v", err))
		return nil
	}
	cache[backend.Addr] = creds
	return creds
}

const (
	metadataKeyUser string = "x-git-user"
	metadataKeyRepo string = "x-git-repo"
)

func GetUserFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(metadataKeyUser)
	if len(values) < 1 {
		return ""
	}
	return values[0]
}

func GetRepoFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(metadataKeyRepo)
	if len(values) < 1 {
		return ""
	}
	return values[0]
}
