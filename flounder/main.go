package main

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strings"

	pbSSH "github.com/vvatanabe/git-ha-poc/proto/ssh"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"google.golang.org/grpc"

	"github.com/vvatanabe/git-ha-poc/internal/gitssh"

	"golang.org/x/crypto/ssh"
)

const (
	sshPort = ":2222"
)

var (
	hostPrivateKeySigner ssh.Signer
	gitSSHTransfer       *GitSSHTransfer
)

func init() {
	keyPath := "/root/host_key"

	hostPrivateKey, err := ioutil.ReadFile(keyPath)
	if err != nil {
		panic(err)
	}

	hostPrivateKeySigner, err = ssh.ParsePrivateKey(hostPrivateKey)
	if err != nil {
		panic(err)
	}

	streamChain := grpc_middleware.ChainStreamClient(XGitUserStreamInterceptor, XGitRepoStreamInterceptor)
	conn, err := grpc.Dial(os.Getenv("SWORDFISH_ADDR"), grpc.WithStreamInterceptor(streamChain), grpc.WithInsecure())
	if err != nil {
		panic("failed to dial: " + err.Error())
	}

	gitSSHTransfer = &GitSSHTransfer{
		client: pbSSH.NewSSHProtocolServiceClient(conn),
	}
}

func main() {

	log.SetFlags(0)
	log.SetPrefix("[flounder] ")

	sshLis, err := net.Listen("tcp", sshPort)
	if err != nil {
		log.Fatalf("failed to listen: %v\n", err)
	}
	s := gitssh.Server{
		Handler: handler,
		Signer:  hostPrivateKeySigner,
	}
	log.Printf("start ssh server on port%s\n", sshPort)
	if err := s.Serve(sshLis); err != nil {
		log.Fatalf("failed to serve: %v\n", err)
	}
}

func handler(ch ssh.Channel, req *ssh.Request, perms *ssh.Permissions) {
	var err error
	defer func() {
		if err != nil {
			sendExit(ch, 1, "failed")
		} else {
			sendExit(ch, 0, "")
		}
		ch.Close()
	}()

	subCmd, user, repo := extractPayload(req.Payload)
	if subCmd == "" || user == "" || repo == "" {
		return
	}

	ctx := context.Background()
	ctx = AddUserToContext(ctx, user)
	ctx = AddRepoToContext(ctx, repo)

	_ = req.Reply(true, nil)

	if subCmd == "git-receive-pack" {
		err = gitSSHTransfer.GitReceivePack(ctx, ch)
	} else if subCmd == "git-upload-pack" {
		err = gitSSHTransfer.GitUploadPack(ctx, ch, req)
	} else {
		err = errors.New("unknown operation " + subCmd)
	}
}

func extractPayload(payload []byte) (subCmd, user, repo string) {

	payloadStr := string(payload)

	i := strings.Index(payloadStr, "git")
	if i == -1 {
		return
	}

	cmdArgs := strings.Split(payloadStr[i:], " ")

	if len(cmdArgs) != 2 {
		return
	}

	cmd := cmdArgs[0]
	if !(cmd == "git-receive-pack" || cmd == "git-upload-pack") {
		return
	}

	path := cmdArgs[1]
	path = strings.Trim(path, "'")

	splitPath := strings.Split(path, "/")
	if len(splitPath) != 3 {
		return
	}

	subCmd = cmd
	user = splitPath[1]
	repo = strings.TrimSuffix(splitPath[2], ".git")
	return
}

func sendExit(ch ssh.Channel, code uint8, msg string) {
	if code == 0 && msg != "" {
		ch.Write([]byte(msg + "\r\n"))
	} else {
		ch.Stderr().Write([]byte(msg + "\r\n"))
	}
	_, err := ch.SendRequest("exit-status", false, []byte{0, 0, 0, code})
	if err != nil {
		log.Println(err)
	}
}

type GitSSHTransfer struct {
	client pbSSH.SSHProtocolServiceClient
}

func (t *GitSSHTransfer) GitUploadPack(ctx context.Context, ch ssh.Channel, req *ssh.Request) error {

	user := GetUserFromContext(ctx)
	repo := GetRepoFromContext(ctx)

	stream, err := t.client.PostUploadPack(ctx)
	if err != nil {
		return err
	}
	defer func() {
		err := stream.CloseSend()
		if err != nil {
			log.Println("failed to close send ", err)
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
			log.Println("failed to send first ", err)
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
					log.Println("failed to send request ", err)
					return
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Println("failed to read channel ", err)
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

func (t *GitSSHTransfer) GitReceivePack(ctx context.Context, ch ssh.Channel) error {

	user := GetUserFromContext(ctx)
	repo := GetRepoFromContext(ctx)

	stream, err := t.client.PostReceivePack(ctx)
	if err != nil {
		return err
	}
	defer func() {
		err := stream.CloseSend()
		if err != nil {
			log.Println("failed to close send ", err)
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
			log.Println("failed to send first ", err)
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
					log.Println("failed to send request ", err)
					return
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Println("failed to read channel ", err)
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
