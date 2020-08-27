package metadata

import (
	"context"

	"google.golang.org/grpc/metadata"
)

type contextKeyUser struct{}
type contextKeyRepo struct{}

func UserFromContext(ctx context.Context) string {
	u := ctx.Value(contextKeyUser{})
	user, ok := u.(string)
	if !ok {
		return ""
	}
	return user
}

func RepoFromContext(ctx context.Context) string {
	u := ctx.Value(contextKeyRepo{})
	repo, ok := u.(string)
	if !ok {
		return ""
	}
	return repo
}

func ContextWithUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, contextKeyUser{}, user)
}

func ContextWithRepo(ctx context.Context, repo string) context.Context {
	return context.WithValue(ctx, contextKeyRepo{}, repo)
}

const (
	metadataKeyUser string = "x-git-user"
	metadataKeyRepo string = "x-git-repo"
)

func AppendUserToOutgoingContext(ctx context.Context, user string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, metadataKeyUser, user)
}

func AppendRepoToOutgoingContext(ctx context.Context, repo string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, metadataKeyRepo, repo)
}

func GetUserFromIncomingContext(ctx context.Context) string {
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

func GetRepoFromIncomingContext(ctx context.Context) string {
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
