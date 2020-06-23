package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"google.golang.org/grpc/metadata"

	"github.com/go-jwdk/mysql-connector"

	"github.com/go-jwdk/jobworker"

	"github.com/vvatanabe/git-ha-poc/internal/grpc-proxy/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

func getDirector(config Config) func(context.Context, string) (context.Context, *grpc.ClientConn, func(), error) {

	credentialsCache := make(map[string]credentials.TransportCredentials)

	conn, err := mysql.Open(&mysql.Config{
		DSN: "", // TODO
	})
	if err != nil {
		// TODO
		log.Fatalln("failed to open conn", err)
	}
	jw, err := jobworker.New(&jobworker.Setting{
		Primary:                    conn, // TODO
		DeadConnectorRetryInterval: 3,
		LoggerFunc:                 log.Println,
	})
	if err != nil {
		// TODO
		log.Fatalln("failed to init worker", err)
	}

	return func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, func(), error) {

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return ctx, nil, nil, errors.New("no incoming context") // TODO
		}
		valuesUser := md.Get("x-git-user")
		if len(valuesUser) < 1 {
			return ctx, nil, nil, errors.New("no user name in incoming context") // TODO
		}
		userName := valuesUser[0]

		valuesRepo := md.Get("x-git-repo")
		if len(valuesRepo) < 1 {
			return ctx, nil, nil, errors.New("no repo name in incoming context") // TODO
		}
		repoName := valuesRepo[0]

		var zoneName string
		for _, user := range config.Users {
			if userName == user.Name {
				zoneName = user.Zone
				break
			}
		}
		if zoneName == "" {
			return ctx, nil, nil, errors.New("no zone") // TODO
		}

		getOperation := func(fullMethodName string) Operation {
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

		ope := getOperation(fullMethodName)
		if ope == Unknown {
			return ctx, nil, nil, errors.New("unknown operation: " + fullMethodName)
		}

		getNode := func(zoneName string, ope Operation) *Node {
			var nodes map[*Node]struct{}
			for _, zone := range config.Zones {
				if zone.Name == zoneName {
					for _, node := range zone.Nodes {
						nodes[&node] = struct{}{}
					}
					break
				}
			}
			switch ope {
			case Write:
				for node := range nodes {
					if node.Writable {
						return node
					}
				}
			case Read:
				for node := range nodes {
					return node
				}
			}
			return nil
		}

		backend := getNode(zoneName, ope)
		if backend == nil {
			if config.Verbose {
				printInfo(fmt.Sprintf("Not found: %s", fullMethodName))
			}
			return ctx, nil, nil, status.Errorf(codes.Unimplemented, "Unknown method")
		}

		finishedFunc := func() {

			var entries []*jobworker.EnqueueBatchEntry
			for _, zone := range config.Zones {
				if zone.Name == zoneName {
					for i, node := range zone.Nodes {
						if !node.Writable {
							var content = &ReplicationContent{
								Ope:        ope,
								Zone:       zoneName,
								TargetNode: node.Addr,
								User:       userName,
								Repo:       repoName,
								RemoteNode: backend.Addr,
							}
							b, err := json.Marshal(content)
							if err != nil {
								printError(err)
								return
							}

							entries = append(entries, &jobworker.EnqueueBatchEntry{
								ID:      fmt.Sprintf("id-%d", i),
								Content: string(b),
							})
						}
					}
					break
				}
			}

			out, err := jw.EnqueueBatch(context.Background(), &jobworker.EnqueueBatchInput{
				Queue:   "replication",
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

		if config.Verbose {
			printInfo(fmt.Sprintf("Found: %s > %s", fullMethodName, backend.Addr))
		}
		if backend.CertFile == "" {
			conn, err := grpc.DialContext(ctx, backend.Addr, grpc.WithDefaultCallOptions(grpc.ForceCodec(proxy.NewCodec())),
				grpc.WithInsecure())
			return ctx, conn, finishedFunc, err
		}
		creds := getCredentials(credentialsCache, backend)
		if creds != nil {
			conn, err := grpc.DialContext(ctx, backend.Addr, grpc.WithDefaultCallOptions(grpc.ForceCodec(proxy.NewCodec())),
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
