package main

import (
	"context"
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

func getDirector(config Config) func(context.Context, string) (context.Context, *grpc.ClientConn, error) {

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

	return func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, error) {

		finishedFunc := func() {

		}

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return ctx, nil, errors.New("no incoming context") // TODO
		}
		values := md.Get("x-git-user")
		if len(values) < 1 {
			return ctx, nil, errors.New("no user name in incoming context") // TODO
		}
		userName := values[0]

		var zoneName string
		for _, user := range config.Users {
			if userName == user.Name {
				zoneName = user.Zone
				break
			}
		}
		if zoneName == "" {
			return ctx, nil, errors.New("no zone") // TODO
		}

		var nodes []Node
		for _, zone := range config.Zones {
			if zone.Name == zoneName {
				nodes = zone.Nodes
			}
		}

		type Operation int
		const (
			Unknown = iota
			Write
			Read
			Replicate
		)

		getOperation := func(fullMethodName string) Operation {

			//- match:
			//addr: tadpole:50051
			//- match: /smart.SmartProtocolService
			//addr: tadpole:50051
			if strings.HasPrefix(fullMethodName, "/repository.RepositoryService") {
				return Read
			}
			if s
		}

		//if IsReadOnlyRPC(fullMethodName) {
		//	nodes, err := GetReadableNodesByGroup(group)
		//	if err != nil {
		//		return
		//	}
		//} else {
		//	nodes, err := GetWritableNodeByGroup(group)
		//	if err != nil {
		//		return
		//	}
		//}

		for _, backend := range config.Routes {
			if strings.HasPrefix(fullMethodName, backend.Match) {
				if config.Verbose {
					printInfo(fmt.Sprintf("Found: %s > %s", fullMethodName, backend.Addr))
				}
				if backend.CertFile == "" {
					conn, err := grpc.DialContext(ctx, backend.Addr, grpc.WithDefaultCallOptions(grpc.ForceCodec(proxy.NewCodec())),
						grpc.WithInsecure())
					return ctx, conn, err
				}
				creds := getCredentials(credentialsCache, backend)
				if creds != nil {
					conn, err := grpc.DialContext(ctx, backend.Addr, grpc.WithDefaultCallOptions(grpc.ForceCodec(proxy.NewCodec())),
						grpc.WithTransportCredentials(creds))
					return ctx, conn, err
				}
				printFatal("failed to create TLS credentials")
				return ctx, nil, status.Errorf(codes.FailedPrecondition, "Addr TLS is not configured properly in grpc-proxy")
			}
		}

		if config.Verbose {
			printInfo(fmt.Sprintf("Not found: %s", fullMethodName))
		}
		return ctx, nil, status.Errorf(codes.Unimplemented, "Unknown method")
	}
}

func getCredentials(cache map[string]credentials.TransportCredentials, backend Route) credentials.TransportCredentials {
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
