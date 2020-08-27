package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/go-jwdk/jobworker"
	"github.com/vvatanabe/git-ha-poc/internal/grpc-proxy/proxy"
	"github.com/vvatanabe/git-ha-poc/shared/metadata"
	"github.com/vvatanabe/git-ha-poc/shared/replication"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

type Node struct {
	Name        string
	ClusterName string
	Addr        string
	Writable    bool
	Active      bool
	CertFile    string
}

func getDirector(publisher *jobworker.JobWorker, store *Store) func(context.Context, string) (context.Context, *grpc.ClientConn, func(), error) {

	credentialsCache := make(map[string]credentials.TransportCredentials)

	return func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, func(), error) {

		userName := metadata.GetUserFromIncomingContext(ctx)
		if len(userName) < 1 {
			return ctx, nil, nil, errors.New("no user name in incoming context")
		}

		repoName := metadata.GetRepoFromIncomingContext(ctx)
		if len(repoName) < 1 {
			return ctx, nil, nil, errors.New("no repo name in incoming context")
		}

		clusterName, err := store.GetClusterNameByUserName(userName)
		if err != nil {
			return ctx, nil, nil, fmt.Errorf("failed to get cluster name: %w", err)
		}

		nodes, err := store.GetNodesByClusterName(ctx, clusterName)
		if err != nil {
			return ctx, nil, nil, fmt.Errorf("failed to get nodes: %w", err)
		}
		if len(nodes) == 0 {
			return ctx, nil, nil, errors.New("no nodes")
		}

		ope := getOperation(fullMethodName)
		if ope == Unknown {
			return ctx, nil, nil, errors.New("unknown operation: " + fullMethodName)
		}

		node := selectNode(nodes, ope)
		if node == nil {
			return ctx, nil, nil, errors.New("could not select node")
		}

		finishedFunc := getFinishedFunc(publisher, nodes, fullMethodName, clusterName, userName, repoName)

		log.Printf("found rpc: %s > %s\n", fullMethodName, node.Addr)

		if node.CertFile == "" {
			conn, err := grpc.DialContext(ctx, node.Addr, grpc.WithDefaultCallOptions(grpc.ForceCodec(proxy.NewCodec())),
				grpc.WithInsecure())
			return ctx, conn, finishedFunc, err
		}
		creds, err := getCredentials(credentialsCache, node)
		if err != nil {
			log.Printf("failed to create TLS credentials: %v\n", err)
			return ctx, nil, nil, status.Errorf(codes.FailedPrecondition, "addr TLS is not configured properly")
		}
		conn, err := grpc.DialContext(ctx, node.Addr, grpc.WithDefaultCallOptions(grpc.ForceCodec(proxy.NewCodec())),
			grpc.WithTransportCredentials(creds))
		return ctx, conn, finishedFunc, err
	}
}

type Operation string

const (
	Unknown Operation = "unknown"
	Write   Operation = "write"
	Read    Operation = "read"
)

func getOperation(fullMethodName string) Operation {
	if strings.HasPrefix(fullMethodName, "/repository.RepositoryService") {
		return Write
	}
	if strings.HasSuffix(fullMethodName, "/PostReceivePack") {
		return Write
	}
	if strings.HasSuffix(fullMethodName, "/PostUploadPack") {
		return Read
	}
	if strings.HasPrefix(fullMethodName, "/smart.SmartProtocolService/GetInfoRefs") { // TODO
		return Read
	}
	return Unknown
}

func selectNode(nodes []*Node, ope Operation) *Node {
	shuffle(nodes)
	switch ope {
	case Write:
		for _, node := range nodes {
			if node.Active && node.Writable {
				return node
			}
		}
	case Read:
		for _, node := range nodes {
			if node.Active {
				return node
			}
		}
	}
	return nil
}

func shuffle(nodes []*Node) {
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(nodes), func(i, j int) {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	})
}

func getFinishedFunc(publisher *jobworker.JobWorker, nodes []*Node, fullMethodName, cluster, user, repo string) func() {
	return func() {

		t := getReplicationOperation(fullMethodName)
		if t == "" {
			log.Printf("not found replication operation: %s\n", fullMethodName)
			return
		}

		var writerNode *Node
		for _, node := range nodes {
			if node.Writable {
				writerNode = node
				break
			}
		}
		if writerNode == nil {
			log.Println("not found writer node")
			return
		}

		var entries []*jobworker.EnqueueBatchEntry
		for i, node := range nodes {
			if node.Writable {
				continue
			}
			content, err := json.Marshal(&replication.Log{
				Operation:  t,
				User:       user,
				Repo:       repo,
				TargetNode: node.Addr,
				RemoteNode: writerNode.Addr,
				Cluster:    cluster,
			})
			if err != nil {
				log.Println("failed to marshal replication log", err)
				return
			}
			entries = append(entries, &jobworker.EnqueueBatchEntry{
				ID:      fmt.Sprintf("id-%d", i),
				Content: string(content),
			})
		}

		out, err := publisher.EnqueueBatch(context.Background(), &jobworker.EnqueueBatchInput{
			Queue:   replication.LogQueueName,
			Entries: entries,
		})
		if err != nil {
			if out != nil {
				log.Printf("failed to enqueue replication log: %s, %s\n", err, out.Failed)
			} else {
				log.Printf("failed to enqueue replication log: %s\n", err)
			}
		}
	}
}

func getReplicationOperation(fullMethodName string) replication.Operation {
	if strings.HasPrefix(fullMethodName, "/repository.RepositoryService/CreateRepository") {
		return replication.CreateRepo
	}
	if strings.HasSuffix(fullMethodName, "/PostReceivePack") {
		return replication.UpdateRepo
	}
	return ""
}

func getCredentials(cache map[string]credentials.TransportCredentials, backend *Node) (credentials.TransportCredentials, error) {
	if v := cache[backend.Addr]; v != nil {
		return v, nil
	}
	creds, err := credentials.NewClientTLSFromFile(backend.CertFile, backend.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS credentials: %w", err)
	}
	cache[backend.Addr] = creds
	return creds, nil
}
