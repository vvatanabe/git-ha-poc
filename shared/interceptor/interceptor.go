package interceptor

import (
	"context"

	"github.com/vvatanabe/git-ha-poc/shared/metadata"
	"google.golang.org/grpc"
)

func XGitUserStreamInterceptor(ctx context.Context,
	desc *grpc.StreamDesc,
	cc *grpc.ClientConn,
	method string,
	streamer grpc.Streamer,
	opts ...grpc.CallOption) (grpc.ClientStream, error) {
	user := metadata.GetUserFromContext(ctx)
	ctx = metadata.AddUserToMetadata(ctx, user)
	stream, err := streamer(ctx, desc, cc, method, opts...)
	return stream, err
}

func XGitRepoStreamInterceptor(ctx context.Context,
	desc *grpc.StreamDesc,
	cc *grpc.ClientConn,
	method string,
	streamer grpc.Streamer,
	opts ...grpc.CallOption) (grpc.ClientStream, error) {
	user := metadata.GetRepoFromContext(ctx)
	ctx = metadata.AddRepoToMetadata(ctx, user)
	stream, err := streamer(ctx, desc, cc, method, opts...)
	return stream, err
}

func XGitUserUnaryInterceptor(ctx context.Context,
	method string,
	req, reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption) error {
	user := metadata.GetUserFromContext(ctx)
	ctx = metadata.AddUserToMetadata(ctx, user)
	return invoker(ctx, method, req, reply, cc, opts...)
}

func XGitRepoUnaryInterceptor(ctx context.Context,
	method string,
	req, reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption) error {
	user := metadata.GetRepoFromContext(ctx)
	ctx = metadata.AddRepoToMetadata(ctx, user)
	return invoker(ctx, method, req, reply, cc, opts...)
}
