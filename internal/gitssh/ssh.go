package gitssh

import (
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/crypto/ssh"
)

type Server struct {
	RootPath string
	BinPath  string
	Handler  func(ch ssh.Channel, req *ssh.Request, perms *ssh.Permissions)
	Signer   ssh.Signer
}

func (srv *Server) Serve(listener net.Listener) error {

	if srv.Handler == nil {
		h := &DefaultHandler{
			RootPath: srv.RootPath,
			BinPath:  srv.BinPath,
		}
		srv.Handler = h.Handler
	}

	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			log.Println(conn.RemoteAddr(), "authenticate with", key.Type())
			return nil, nil
		},
	}
	cfg.AddHostKey(srv.Signer)

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go srv.handleConn(conn, cfg)
	}
}

func (srv *Server) handleConn(conn net.Conn, cfg *ssh.ServerConfig) {
	defer conn.Close()

	sConn, reqs, globalReqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}

	go ssh.DiscardRequests(globalReqs)

	for newChan := range reqs {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		srv.handleChannel(newChan, sConn.Permissions)
	}
}

func (srv *Server) handleChannel(newChan ssh.NewChannel, perms *ssh.Permissions) {
	ch, reqs, err := newChan.Accept()
	if err != nil {
		return
	}
	defer ch.Close()

	var wg sync.WaitGroup
	for req := range reqs {
		if req.Type != "exec" {
			req.Reply(false, nil)
			continue
		}
		wg.Add(1)
		go func(req *ssh.Request) {
			defer wg.Done()
			srv.Handler(ch, req, perms)
		}(req)
	}
	wg.Wait()
}

type DefaultHandler struct {
	RootPath string
	BinPath  string
}

func (h *DefaultHandler) Handler(ch ssh.Channel, req *ssh.Request, perms *ssh.Permissions) {

	var err error
	defer func() {
		if err != nil {
			h.sendExit(ch, 1, "failed")
		} else {
			h.sendExit(ch, 0, "")
		}
		ch.Close()
	}()

	subCmd, repoPath := h.extractPayload(req.Payload)
	if subCmd == "" || repoPath == "" {
		return
	}

	cmd := exec.Command(h.BinPath, "-c", fmt.Sprintf("%s '%s'", subCmd, repoPath))
	cmd.Dir = repoPath
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return
	}

	if err := cmd.Start(); err != nil {
		return
	}
	defer cmd.Wait()

	_ = req.Reply(true, nil)

	go func() {
		_, err = io.Copy(stdin, ch)
		_ = stdin.Close()
	}()

	_, err = io.Copy(ch, stdout)
	_ = stdout.Close()

	_, err = io.Copy(ch.Stderr(), stderr)
	_ = stderr.Close()

}

func (h *DefaultHandler) extractPayload(payload []byte) (subCmd, repoPath string) {

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

	if len(strings.Split(path, "/")) != 3 {
		return
	}

	subCmd = cmd
	repoPath = filepath.Join(h.RootPath, path)
	return
}

func (h *DefaultHandler) sendExit(ch ssh.Channel, code uint8, msg string) {
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
