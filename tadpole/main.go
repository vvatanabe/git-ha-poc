package main

import (
	"context"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"syscall"

	"google.golang.org/grpc"

	pbSmart "github.com/vvatanabe/git-ha-poc/proto/smart"
)

const port = ":50051"

func main() {
	log.SetFlags(0)
	log.SetPrefix("[tadpole] ")
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v\n", err)
	}
	s := grpc.NewServer()

	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get user home dir: %v\n", err)
	}
	rootPath := path.Join(home, "git", "repositories")
	err = os.MkdirAll(rootPath, 0644)
	if err != nil {
		log.Fatalf("failed to mkdir: %s %v\n", rootPath, err)
	}

	pbSmart.RegisterSmartProtocolServiceServer(s, &SmartProtocolService{
		RootPath: rootPath,
		BinPath:  "/usr/bin/git",
	})
	log.Printf("start server on port%s\n", port)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v\n", err)
	}
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

	buf := make([]byte, 1000*1024)
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
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}

	repo := c.GetRepository()
	firstData := c.GetData()

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

	if _, err := stdin.Write(firstData); err != nil {
		log.Println("failed to write the request body to standard input. ", err.Error())
		// TODO define error
		return err
	}

	for {
		c, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		data := c.GetData()
		stdin.Write(data)
	}

	out, err := cmd.Output()
	if err != nil {
		log.Println("failed to starts the specified command. ", err.Error())
		// TODO define error
		return err
	}

	err = stream.SendAndClose(&pbSmart.ReceivePackResponse{
		Data: out,
	})

	return err
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
