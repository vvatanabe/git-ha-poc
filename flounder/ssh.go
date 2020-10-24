package main

import (
	"context"
	"io"

	pbSSH "github.com/vvatanabe/git-ha-poc/proto/ssh"
	"github.com/vvatanabe/git-ha-poc/shared/metadata"
	"golang.org/x/crypto/ssh"
)

type GitSSHRPCClient struct {
	client pbSSH.SSHProtocolServiceClient
}

func (t *GitSSHRPCClient) GitUploadPack(ctx context.Context, ch ssh.Channel, req *ssh.Request) error {

	user := metadata.UserFromContext(ctx)
	repo := metadata.RepoFromContext(ctx)

	stream, err := t.client.PostUploadPack(ctx)
	if err != nil {
		return err
	}
	defer func() {
		err := stream.CloseSend()
		if err != nil {
			sugar.Info("failed to close send ", err)
		}
	}()

	go func() {
		err = stream.Send(&pbSSH.UploadPackRequest{
			Repository: &pbSSH.Repository{
				User: user,
				Repo: repo,
			},
		})
		if err != nil {
			sugar.Info("failed to send first ", err)
		}

		buf := make([]byte, 32*1024)

		for {
			n, err := ch.Read(buf)
			if n > 0 {
				err = stream.Send(&pbSSH.UploadPackRequest{
					Repository: &pbSSH.Repository{
						User: user,
						Repo: repo,
					},
					Data: buf[:n],
				})
				if err != nil {
					sugar.Info("failed to send request ", err)
					return
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				sugar.Info("failed to read channel ", err)
				return
			}
		}
	}()

	for {
		c, err := stream.Recv()
		if c != nil {
			if len(c.Data) > 0 {
				ch.Write(c.Data)
			}
			if len(c.Err) > 0 {
				ch.Stderr().Write(c.Data)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *GitSSHRPCClient) GitReceivePack(ctx context.Context, ch ssh.Channel) error {

	user := metadata.UserFromContext(ctx)
	repo := metadata.RepoFromContext(ctx)

	stream, err := t.client.PostReceivePack(ctx)
	if err != nil {
		return err
	}
	defer func() {
		err := stream.CloseSend()
		if err != nil {
			sugar.Info("failed to close send ", err)
		}
	}()

	go func() {
		err = stream.Send(&pbSSH.ReceivePackRequest{
			Repository: &pbSSH.Repository{
				User: user,
				Repo: repo,
			},
		})
		if err != nil {
			sugar.Info("failed to send first ", err)
		}

		buf := make([]byte, 32*1024)

		for {
			n, err := ch.Read(buf)
			if n > 0 {
				err = stream.Send(&pbSSH.ReceivePackRequest{
					Repository: &pbSSH.Repository{
						User: user,
						Repo: repo,
					},
					Data: buf[:n],
				})
				if err != nil {
					sugar.Info("failed to send request ", err)
					return
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				sugar.Info("failed to read channel ", err)
				return
			}
		}
	}()

	for {
		c, err := stream.Recv()
		if c != nil {
			if len(c.Data) > 0 {
				ch.Write(c.Data)
			}
			if len(c.Err) > 0 {
				ch.Stderr().Write(c.Data)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}
