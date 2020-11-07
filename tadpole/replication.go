package main

import (
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"path"
	"syscall"
	"time"

	pbReplication "github.com/vvatanabe/git-ha-poc/proto/replication"
)

type ReplicationService struct {
	RootPath string
	BinPath  string
	SSHPort  int
	pbReplication.UnimplementedReplicationServiceServer
}

func (r *ReplicationService) CreateRepository(ctx context.Context, request *pbReplication.CreateRepositoryRequest) (*pbReplication.CreateRepositoryResponse, error) {
	repoPath := path.Join(r.RootPath, request.Repository.User, request.Repository.Repo+".git")
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
	return &pbReplication.CreateRepositoryResponse{}, nil
}

func (r *ReplicationService) SyncRepository(ctx context.Context, request *pbReplication.SyncRepositoryRequest) (response *pbReplication.SyncRepositoryResponse, err error) {
	remoteName := fmt.Sprintf("internal-%s", RandomString(10))
	remoteURL := fmt.Sprintf("ssh://git@%s:%d/%s/%s.git",
		request.RemoteAddr, r.SSHPort, request.Repository.User, request.Repository.Repo)
	repoPath := path.Join(r.RootPath, request.Repository.User, request.Repository.Repo+".git")
	err = addRemote(r.BinPath, remoteName, remoteURL, repoPath)
	if err != nil {
		sugar.Info("failed to add remote ", err)
		return nil, err
	}

	defer func() {
		e := removeRemote(r.BinPath, remoteName, repoPath)
		if e != nil {
			sugar.Info("failed to remove remote ", err)
			err = e
		}
	}()

	err = fetchInternal(r.BinPath, remoteName, repoPath)
	if err != nil {
		sugar.Info("failed to fetch internal ", err)
		return nil, err
	}

	return &pbReplication.SyncRepositoryResponse{}, nil
}

const charset = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var seededRand *rand.Rand = rand.New(
	rand.NewSource(time.Now().UnixNano()))

func RandomString(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

// git remote add <name> ssh://git@<host>:2020/<user>/<repo>.git
func addRemote(bin, name, url, repoPath string) error {
	args := []string{"remote", "add", name, url}
	cmd := exec.Command(bin, args...)
	cmd.Dir = repoPath
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func removeRemote(bin, name, repoPath string) error {
	args := []string{"remote", "remove", name}
	cmd := exec.Command(bin, args...)
	cmd.Dir = repoPath
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

// git fetch --prune <name> "+refs/*:refs/*"
func fetchInternal(bin, name, repoPath string) error {
	args := []string{"fetch", "--prune", name, "+refs/*:refs/*"}
	cmd := exec.Command(bin, args...)
	cmd.Dir = repoPath
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}
