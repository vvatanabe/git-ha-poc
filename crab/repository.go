package main

import (
	"fmt"
	"net/http"

	pbRepository "github.com/vvatanabe/git-ha-poc/proto/repository"
	"github.com/vvatanabe/git-ha-poc/shared/metadata"
)

type GitRepositoryClient struct {
	client pbRepository.RepositoryServiceClient
}

func (gr *GitRepositoryClient) Create(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := metadata.UserFromContext(ctx)
	repo := metadata.RepoFromContext(ctx)

	_, err := gr.client.CreateRepository(ctx, &pbRepository.CreateRepositoryRequest{
		User: user,
		Repo: repo,
	})
	if err != nil {
		sugar.Info("failed to create a repository.", err)
		renderInternalServerError(rw)
		return
	}
	rw.WriteHeader(http.StatusOK)
	rw.Write([]byte(fmt.Sprintf("create success %s/%s.git", user, repo)))
}
