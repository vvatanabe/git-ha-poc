package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path"
	"syscall"
	"time"

	"github.com/vvatanabe/git-ha-poc/internal/gitssh"

	pbReplication "github.com/vvatanabe/git-ha-poc/proto/replication"
	pbRepository "github.com/vvatanabe/git-ha-poc/proto/repository"
	pbSmart "github.com/vvatanabe/git-ha-poc/proto/smart"
	"google.golang.org/grpc"
)

const (
	grpcPort = ":50051"
	sshPort  = ":2020"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("[tadpole] ")

	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get user home dir: %v\n", err)
	}
	rootPath := path.Join(home, "git", "repositories")
	binPath := "/usr/bin/git"

	go func() {
		grpcLis, err := net.Listen("tcp", grpcPort)
		if err != nil {
			log.Fatalf("failed to listen: %v\n", err)
		}
		s := grpc.NewServer()

		pbSmart.RegisterSmartProtocolServiceServer(s, &SmartProtocolService{
			RootPath: rootPath,
			BinPath:  binPath,
		})
		pbRepository.RegisterRepositoryServiceServer(s, &RepositoryService{
			RootPath: rootPath,
			BinPath:  binPath,
		})
		pbReplication.RegisterReplicationServiceServer(s, &ReplicationService{
			RootPath: rootPath,
			BinPath:  binPath,
		})
		log.Printf("start grpc server on port%s\n", grpcPort)
		if err := s.Serve(grpcLis); err != nil {
			log.Fatalf("failed to serve: %v\n", err)
		}
	}()

	sshLis, err := net.Listen("tcp", sshPort)
	if err != nil {
		log.Fatalf("failed to listen: %v\n", err)
	}
	s := gitssh.Server{
		RootPath: rootPath,
		BinPath:  binPath,
	}
	log.Printf("start ssh server on port%s\n", sshPort)
	if err := s.Serve(sshLis); err != nil {
		log.Fatalf("failed to serve: %v\n", err)
	}
}

type ReplicationService struct {
	RootPath string
	BinPath  string
}

func (r *ReplicationService) ReplicateRepository(ctx context.Context, request *pbReplication.ReplicateRepositoryRequest) (response *pbReplication.ReplicateRepositoryResponse, err error) {

	remoteName := fmt.Sprintf("internal-%s", RandomString(10))
	remoteURL := fmt.Sprintf("tadpole@%s/%s/%s.git",
		request.RemoteAddr+sshPort, request.Repository.User, request.Repository.User)
	repoPath := path.Join(r.RootPath, request.Repository.User, request.Repository.Repo+".git")
	err = addRemote(r.BinPath, remoteName, remoteURL, repoPath)
	if err != nil {
		return nil, err
	}

	defer func() {
		e := removeRemote(r.BinPath, remoteName, repoPath)
		if e != nil {
			err = e
		}
	}()

	err = fetchInternal(r.BinPath, remoteName, repoPath)
	if err != nil {
		return nil, err
	}

	return &pbReplication.ReplicateRepositoryResponse{}, nil
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

type RepositoryService struct {
	RootPath string
	BinPath  string
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

type SmartProtocolService struct {
	RootPath string
	BinPath  string
}

func (s *SmartProtocolService) getAbsolutePath(repoPath string) string {
	return path.Join(s.RootPath, repoPath)
}

func (s *SmartProtocolService) PostUploadPack(request *pbSmart.UploadPackRequest, stream pbSmart.SmartProtocolService_PostUploadPackServer) error {

	args := []string{"upload-pack", "--stateless-rpc", "."}
	cmd := exec.Command(s.BinPath, args...)
	cmd.Dir = s.getAbsolutePath(path.Join(request.Repository.User, request.Repository.Repo+".git"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Println("failed to get pipe that will be connected to the command's standard input. ", err.Error())
		// TODO define error
		return err
	}
	defer stdin.Close()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Println("failed to get pipe that will be connected to the command's standard output. ", err.Error())
		// TODO define error
		return err
	}
	defer stdout.Close()

	err = cmd.Start()
	if err != nil {
		log.Println("failed to starts the specified command. ", err.Error())
		// TODO define error
		return err
	}
	defer cmd.Wait()

	if _, err := stdin.Write(request.Data); err != nil {
		log.Println("failed to write the request body to standard input. ", err.Error())
		// TODO define error
		return err
	}

	// "git-upload-pack" waits for the remaining input and it hangs,
	// so must close it after completing the copy request body to standard input.
	stdin.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := stdout.Read(buf)
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}
		err = stream.Send(&pbSmart.UploadPackResponse{
			Data: buf[:n],
		})
		if err != nil {
			// TODO define error
			return err
		}
	}

	return nil
}

func (s *SmartProtocolService) PostReceivePack(stream pbSmart.SmartProtocolService_PostReceivePackServer) error {

	c, err := stream.Recv()
	if err != nil {
		log.Println("failed to recv stream first", err)
		return err
	}

	repo := c.GetRepository()

	args := []string{"receive-pack", "--stateless-rpc", "."}
	cmd := exec.Command(s.BinPath, args...)

	cmd.Dir = s.getAbsolutePath(path.Join(repo.User, repo.Repo+".git"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Println("failed to get pipe that will be connected to the command's standard input. ", err.Error())
		// TODO define error
		return err
	}
	defer stdin.Close()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Println("failed to get pipe that will be connected to the command's standard output. ", err.Error())
		// TODO define error
		return err
	}
	defer stdout.Close()

	err = cmd.Start()
	if err != nil {
		log.Println("failed to starts the specified command. ", err.Error())
		// TODO define error
		return err
	}
	defer cmd.Wait()

	sr := StreamReader{
		ReadFunc: func() ([]byte, error) {
			r, err := stream.Recv()
			if r != nil {
				return r.Data, err
			}
			return nil, err
		},
	}

	_, err = io.Copy(stdin, sr)
	if err != nil {
		log.Println("failed to write stdin ", err)
		return err
	}

	// "git-upload-pack" waits for the remaining input and it hangs,
	// so must close it after completing the copy request body to standard input.
	stdin.Close()

	var buf []byte
	sw := &StreamWriter{
		WriteFunc: func(p []byte) error {
			buf = append(buf, p...)
			return nil
		},
	}

	_, err = io.Copy(sw, stdout)
	if err != nil {
		return err
	}
	err = stream.SendAndClose(&pbSmart.ReceivePackResponse{
		Data: buf,
	})
	return err
}

type StreamWriter struct {
	WriteFunc func(p []byte) error
}

func (s *StreamWriter) Write(p []byte) (n int, err error) {
	err = s.WriteFunc(p)
	n = len(p)
	return
}

type StreamReader struct {
	ReadFunc func() ([]byte, error)
}

func (s StreamReader) Read(p []byte) (n int, err error) {
	data, err := s.ReadFunc()
	n = copy(p, data)
	data = data[n:]
	if len(data) == 0 {
		return n, err
	}
	return
}

func (s *SmartProtocolService) GetInfoRefs(ctx context.Context, request *pbSmart.InfoRefsRequest) (*pbSmart.InfoRefsResponse, error) {
	var serviceName string
	switch request.Service {
	case pbSmart.Service_UPLOAD_PACK:
		serviceName = "upload-pack"
	case pbSmart.Service_RECEIVE_PACK:
		serviceName = "receive-pack"
	}
	args := []string{serviceName, "--stateless-rpc", "--advertise-refs", "."}
	cmd := exec.CommandContext(ctx, s.BinPath, args...)
	cmd.Dir = s.getAbsolutePath(path.Join(request.Repository.User, request.Repository.Repo+".git"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	refs, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return &pbSmart.InfoRefsResponse{
		Data: refs,
	}, nil
}
