package metadata

import (
	"context"

	"google.golang.org/grpc/metadata"
)

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
