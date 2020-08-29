package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	pbReplication "github.com/vvatanabe/git-ha-poc/proto/replication"
	pbRepository "github.com/vvatanabe/git-ha-poc/proto/repository"
	pbSmart "github.com/vvatanabe/git-ha-poc/proto/smart"
	pbSSH "github.com/vvatanabe/git-ha-poc/proto/ssh"
	"github.com/vvatanabe/git-ha-poc/shared/interceptor"
	"github.com/vvatanabe/git-ssh-test-server/gitssh"
	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	pbHealth "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	appName   = "tadpole"
	grpcPort  = 50051
	sshPort   = 2020
	keyPath   = "/root/host_key"
	repoDir   = "/root/git/repositories"
	binPath   = "/usr/bin/git"
	shellPath = "/usr/bin/git-shell"
)

func init() {
	log.SetFlags(0)
	log.SetPrefix(fmt.Sprintf("[%s] ", appName))
}

func main() {

	hostPrivateKey, err := ioutil.ReadFile(keyPath)
	if err != nil {
		log.Fatalf("failed to read host %s %v\n", keyPath, err)
	}

	hostPrivateKeySigner, err := ssh.ParsePrivateKey(hostPrivateKey)
	if err != nil {
		log.Fatalf("failed to parse private key %s %v\n", keyPath, err)
	}

	go func() {
		grpcLis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
		if err != nil {
			log.Fatalf("failed to listen: %v\n", err)
		}
		s := grpc.NewServer(grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			interceptor.LoggingUnaryServerInterceptor(),
		)), grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
			interceptor.LoggingStreamServerInterceptor()),
		))

		pbHealth.RegisterHealthServer(s, health.NewServer())

		pbSmart.RegisterSmartProtocolServiceServer(s, &SmartProtocolService{
			RootPath: repoDir,
			BinPath:  binPath,
		})
		pbSSH.RegisterSSHProtocolServiceServer(s, &SSHProtocolService{
			RootPath:  repoDir,
			ShellPath: shellPath,
		})
		pbRepository.RegisterRepositoryServiceServer(s, &RepositoryService{
			RootPath: repoDir,
			BinPath:  binPath,
		})
		pbReplication.RegisterReplicationServiceServer(s, &ReplicationService{
			RootPath: repoDir,
			BinPath:  binPath,
		})
		log.Printf("start grpc server on port %d\n", grpcPort)
		if err := s.Serve(grpcLis); err != nil {
			log.Fatalf("failed to serve: %v\n", err)
		}
	}()

	sshLis, err := net.Listen("tcp", fmt.Sprintf(":%d", sshPort))
	if err != nil {
		log.Fatalf("failed to listen: %v\n", err)
	}
	s := gitssh.Server{
		RepoDir:   repoDir,
		ShellPath: shellPath,
		PublicKeyCallback: func(metadata ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			log.Println(metadata.RemoteAddr(), "authenticate with", key.Type())
			return &ssh.Permissions{
				Extensions: make(map[string]string),
			}, nil
		},
		Signer: hostPrivateKeySigner,
	}
	log.Printf("start internal ssh server on port %d\n", sshPort)
	if err := s.Serve(sshLis); err != nil {
		log.Fatalf("failed to serve: %v\n", err)
	}
}
