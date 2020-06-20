package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/vvatanabe/git-ha-poc/internal/grpc-proxy/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

func getDirector(config Config) func(context.Context, string) (context.Context, *grpc.ClientConn, error) {

	credentialsCache := make(map[string]credentials.TransportCredentials)

	return func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, error) {

		//md, ok := metadata.FromIncomingContext(ctx)
		//
		//user := md.Get("x-git-user")
		//
		//group, err := GetGroupByUser(user)
		//if err != nil {
		//	return
		//}
		//
		//if IsReadOnlyRPC(fullMethodName) {
		//	nodes, err := GetReadableNodesByGroup(group)
		//	if err != nil {
		//		return
		//	}
		//} else {
		//	nodes, err := GetWritebleNodeByGroup(group)
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
