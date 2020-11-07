package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"syscall"

	pbSmart "github.com/vvatanabe/git-ha-poc/proto/smart"
)

type SmartProtocolService struct {
	RootPath string
	BinPath  string
	pbSmart.UnimplementedSmartProtocolServiceServer
}

func (s *SmartProtocolService) PostUploadPack(request *pbSmart.UploadPackRequest, stream pbSmart.SmartProtocolService_PostUploadPackServer) error {

	args := []string{"upload-pack", "--stateless-rpc", "."}
	cmd := exec.Command(s.BinPath, args...)
	cmd.Dir = s.getAbsolutePath(path.Join(request.Repository.User, request.Repository.Repo+".git"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		sugar.Info("failed to get pipe that will be connected to the command's standard input. ", err.Error())
		// TODO define error
		return err
	}
	defer stdin.Close()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sugar.Info("failed to get pipe that will be connected to the command's standard output. ", err.Error())
		// TODO define error
		return err
	}
	defer stdout.Close()

	err = cmd.Start()
	if err != nil {
		sugar.Info("failed to starts the specified command. ", err.Error())
		// TODO define error
		return err
	}
	defer cmd.Wait()

	if _, err := stdin.Write(request.Data); err != nil {
		sugar.Info("failed to write the request body to standard input. ", err.Error())
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
		sugar.Info("failed to recv stream first", err)
		return err
	}

	repo := c.GetRepository()

	args := []string{"receive-pack", "--stateless-rpc", "."}
	cmd := exec.Command(s.BinPath, args...)

	cmd.Dir = s.getAbsolutePath(path.Join(repo.User, repo.Repo+".git"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		sugar.Info("failed to get pipe that will be connected to the command's standard input. ", err.Error())
		// TODO define error
		return err
	}
	defer stdin.Close()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sugar.Info("failed to get pipe that will be connected to the command's standard output. ", err.Error())
		// TODO define error
		return err
	}
	defer stdout.Close()

	err = cmd.Start()
	if err != nil {
		sugar.Info("failed to starts the specified command. ", err.Error())
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
		sugar.Info("failed to write stdin ", err)
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

func (s *SmartProtocolService) GetInfoRefs(ctx context.Context, request *pbSmart.InfoRefsRequest) (*pbSmart.InfoRefsResponse, error) {

	var serviceName string
	switch request.Service {
	case pbSmart.Service_UPLOAD_PACK:
		serviceName = "upload-pack"
	case pbSmart.Service_RECEIVE_PACK:
		serviceName = "receive-pack"
	}

	repoPath := s.getAbsolutePath(path.Join(request.Repository.User, request.Repository.Repo+".git"))

	info, err := os.Stat(repoPath)
	if err != nil {
		return nil, fmt.Errorf("not exisits %s: %w", repoPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not dir %s: %w", repoPath, err)
	}

	args := []string{serviceName, "--stateless-rpc", "--advertise-refs", "."}
	cmd := exec.Command(s.BinPath, args...)
	cmd.Dir = repoPath
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	refs, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to exec service %s %s: %w", serviceName, repoPath, err)
	}
	return &pbSmart.InfoRefsResponse{
		Data: refs,
	}, nil
}

func (s *SmartProtocolService) getAbsolutePath(repoPath string) string {
	return path.Join(s.RootPath, repoPath)
}
