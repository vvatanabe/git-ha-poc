package main

import (
	"context"
	"io"
	"log"
	"os/exec"
	"path"
	"syscall"

	pbSmart "github.com/vvatanabe/git-ha-poc/proto/smart"
)

func main() {

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

func (s *SmartProtocolService) PostReceivePack(server pbSmart.SmartProtocolService_PostReceivePackServer) error {
	panic("implement me")
}

func (s *SmartProtocolService) GetInfoRefs(ctx context.Context, request *pbSmart.InfoRefsRequest) (*pbSmart.InfoRefsResponse, error) {
	panic("implement me")
}
