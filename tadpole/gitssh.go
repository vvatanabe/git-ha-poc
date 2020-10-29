package main

import (
	"log"
	"path/filepath"

	pbSSH "github.com/vvatanabe/git-ha-poc/proto/ssh"
	"github.com/vvatanabe/git-ssh-test-server/gitssh"
)

type SSHProtocolService struct {
	RootPath  string
	ShellPath string
}

func (s *SSHProtocolService) PostUploadPack(stream pbSSH.SSHProtocolService_PostUploadPackServer) error {

	c, err := stream.Recv()
	if err != nil {
		log.Println("failed to recv stream first", err)
		return err
	}

	repo := c.GetRepository()

	repoPath := filepath.Join(s.RootPath, repo.User, repo.Repo+".git")

	rw := &StreamReadWriter{
		WriteFunc: func(p []byte) error {
			return stream.Send(&pbSSH.UploadPackResponse{
				Data: p,
			})
		},
		ReadFunc: func() ([]byte, error) {
			r, err := stream.Recv()
			if r != nil {
				return r.Data, err
			}
			return nil, err
		},
	}
	rwe := &StreamReadWriter{
		WriteFunc: func(p []byte) error {
			return stream.Send(&pbSSH.UploadPackResponse{
				Err: p,
			})
		},
	}

	err = gitssh.GitUploadPack(stream.Context(), s.ShellPath, repoPath, rw, rwe)
	if err != nil {
		log.Println("failed to GitUploadPack", err)
		return err
	}

	return nil
}

func (s *SSHProtocolService) PostReceivePack(stream pbSSH.SSHProtocolService_PostReceivePackServer) error {

	c, err := stream.Recv()
	if err != nil {
		log.Println("failed to recv stream first", err)
		return err
	}

	repo := c.GetRepository()

	repoPath := filepath.Join(s.RootPath, repo.User, repo.Repo+".git")

	rw := &StreamReadWriter{
		WriteFunc: func(p []byte) error {
			return stream.Send(&pbSSH.ReceivePackResponse{
				Data: p,
			})
		},
		ReadFunc: func() ([]byte, error) {
			r, err := stream.Recv()
			if r != nil {
				return r.Data, err
			}
			return nil, err
		},
	}
	rwe := &StreamReadWriter{
		WriteFunc: func(p []byte) error {
			return stream.Send(&pbSSH.ReceivePackResponse{
				Err: p,
			})
		},
	}

	err = gitssh.GitReceivePack(stream.Context(), s.ShellPath, repoPath, rw, rwe)
	if err != nil {
		log.Println("failed to GitReceivePack", err)
		return err
	}
	return nil
}
