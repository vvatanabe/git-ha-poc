package main

import (
	"context"
	"os"
	"os/exec"
	"path"

	pbRepository "github.com/vvatanabe/git-ha-poc/proto/repository"
)

type RepositoryService struct {
	RootPath string
	BinPath  string
	pbRepository.UnimplementedRepositoryServiceServer
}

func (r *RepositoryService) CreateRepository(ctx context.Context, request *pbRepository.CreateRepositoryRequest) (*pbRepository.CreateRepositoryResponse, error) {
	repoPath := path.Join(r.RootPath, request.User, request.Repo+".git")
	if err := mkDirIfNotExist(repoPath); err != nil {
		return nil, err
	}
	initArgs := []string{"init", "--bare", "--shared"}
	initCmd := exec.Command(r.BinPath, initArgs...)
	initCmd.Dir = repoPath
	if err := initCmd.Run(); err != nil {
		return nil, err
	}
	cfgPath := path.Join(repoPath, "config")
	cfgArgs := []string{"config", "-f", cfgPath, "--unset", "receive.denyNonFastForwards"}
	cfgCmd := exec.Command(r.BinPath, cfgArgs...)
	if err := cfgCmd.Run(); err != nil {
		return nil, err
	}
	return &pbRepository.CreateRepositoryResponse{}, nil
}

func mkDirIfNotExist(path string) error {
	if IsExistDir(path) {
		return nil
	}
	return os.MkdirAll(path, 0755)
}

func IsExistDir(path string) bool {
	i, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return i.IsDir()
}
