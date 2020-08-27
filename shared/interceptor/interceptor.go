package interceptor

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc/status"

	"github.com/vvatanabe/git-ha-poc/shared/metadata"
	"google.golang.org/grpc"
)

func XGitUserStreamClientInterceptor(ctx context.Context,
	desc *grpc.StreamDesc,
	cc *grpc.ClientConn,
	method string,
	streamer grpc.Streamer,
	opts ...grpc.CallOption) (grpc.ClientStream, error) {
	user := metadata.UserFromContext(ctx)
	ctx = metadata.AppendUserToOutgoingContext(ctx, user)
	stream, err := streamer(ctx, desc, cc, method, opts...)
	return stream, err
}

func XGitUserUnaryClientInterceptor(ctx context.Context,
	method string,
	req, reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption) error {
	user := metadata.UserFromContext(ctx)
	ctx = metadata.AppendUserToOutgoingContext(ctx, user)
	return invoker(ctx, method, req, reply, cc, opts...)
}

func XGitRepoStreamClientInterceptor(ctx context.Context,
	desc *grpc.StreamDesc,
	cc *grpc.ClientConn,
	method string,
	streamer grpc.Streamer,
	opts ...grpc.CallOption) (grpc.ClientStream, error) {
	repo := metadata.RepoFromContext(ctx)
	ctx = metadata.AppendRepoToOutgoingContext(ctx, repo)
	stream, err := streamer(ctx, desc, cc, method, opts...)
	return stream, err
}

func XGitRepoUnaryClientInterceptor(ctx context.Context,
	method string,
	req, reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption) error {
	repo := metadata.RepoFromContext(ctx)
	ctx = metadata.AppendRepoToOutgoingContext(ctx, repo)
	return invoker(ctx, method, req, reply, cc, opts...)
}

const loggingFmt = "FullMethod:%s\tElapsedTime:%s\tStatusCode:%s\tError:%s\n"

func LoggingUnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (interface{}, error) {

		start := time.Now()
		h, err := handler(ctx, req)
		var errMsg string
		if err != nil {
			errMsg = err.Error()
		}
		log.Printf(loggingFmt,
			info.FullMethod,
			time.Since(start),
			status.Code(err), errMsg)
		return h, err
	}
}

func LoggingStreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {

		start := time.Now()
		err = handler(srv, ss)
		var errMsg string
		if err != nil {
			errMsg = err.Error()
		}
		log.Printf(loggingFmt,
			info.FullMethod,
			time.Since(start),
			status.Code(err), errMsg)
		return err
	}

}
