package main

import (
	"context"

	"google.golang.org/grpc/metadata"

	"google.golang.org/grpc"
)

func XGitUserStreamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	user := GetUserFromContext(ctx)
	ctx = AddUserToMetadata(ctx, user)
	stream, err := streamer(ctx, desc, cc, method, opts...)
	return stream, err
}

func XGitRepoStreamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	user := GetRepoFromContext(ctx)
	ctx = AddRepoToMetadata(ctx, user)
	stream, err := streamer(ctx, desc, cc, method, opts...)
	return stream, err
}

func XGitUserUnaryInterceptor(ctx context.Context,
	method string,
	req, reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption) error {
	user := GetUserFromContext(ctx)
	ctx = AddUserToMetadata(ctx, user)
	return invoker(ctx, method, req, reply, cc, opts...)
}

func XGitRepoUnaryInterceptor(ctx context.Context,
	method string,
	req, reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption) error {
	user := GetRepoFromContext(ctx)
	ctx = AddRepoToMetadata(ctx, user)
	return invoker(ctx, method, req, reply, cc, opts...)
}

type contextKeyUser struct{}
type contextKeyRepo struct{}

func GetUserFromContext(ctx context.Context) string {
	u := ctx.Value(contextKeyUser{})
	user, ok := u.(string)
	if !ok {
		return ""
	}
	return user
}

func GetRepoFromContext(ctx context.Context) string {
	u := ctx.Value(contextKeyRepo{})
	repo, ok := u.(string)
	if !ok {
		return ""
	}
	return repo
}

func AddUserToContext(ctx context.Context,
	user string) context.Context {
	return context.WithValue(ctx, contextKeyUser{}, user)
}

func AddRepoToContext(ctx context.Context,
	repo string) context.Context {
	return context.WithValue(ctx, contextKeyRepo{}, repo)
}

const (
	metadataKeyUser string = "x-git-user"
	metadataKeyRepo string = "x-git-repo"
)

func AddUserToMetadata(ctx context.Context, user string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, metadataKeyUser, user)
}

func AddRepoToMetadata(ctx context.Context, repo string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, metadataKeyRepo, repo)
}
