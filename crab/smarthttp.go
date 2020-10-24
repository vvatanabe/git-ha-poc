package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	pbSmart "github.com/vvatanabe/git-ha-poc/proto/smart"
	"github.com/vvatanabe/git-ha-poc/shared/metadata"
)

const (
	uploadPack  = "upload-pack"
	receivePack = "receive-pack"
)

type GitSmartHTTPClient struct {
	client pbSmart.SmartProtocolServiceClient
}

func (t *GitSmartHTTPClient) GitUploadPack(rw http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	user := metadata.UserFromContext(ctx)
	repo := metadata.RepoFromContext(ctx)

	var body io.ReadCloser
	var err error

	if r.Header.Get("Content-Encoding") == "gzip" {
		body, err = gzip.NewReader(r.Body)
		if err != nil {
			sugar.Info("failed to create a reader reading the given reader.", err)
			renderInternalServerError(rw)
			return
		}
	} else {
		body = r.Body
	}
	defer body.Close()

	buf := new(bytes.Buffer)
	io.Copy(buf, body)

	stream, err := t.client.PostUploadPack(ctx, &pbSmart.UploadPackRequest{
		Repository: &pbSmart.Repository{
			User: user,
			Repo: repo,
		},
		Data: buf.Bytes(),
	})
	if err != nil {
		renderInternalServerError(rw)
		return
	}

	rw.Header().Set("Content-Operation", fmt.Sprintf("application/x-git-%s-result", uploadPack))
	rw.WriteHeader(http.StatusOK)

	for {
		c, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			renderInternalServerError(rw)
			return
		}
		rw.Write(c.Data)
	}
}

func (t *GitSmartHTTPClient) GitReceivePack(rw http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	user := metadata.UserFromContext(ctx)
	repo := metadata.RepoFromContext(ctx)

	var body io.ReadCloser
	var err error

	if r.Header.Get("Content-Encoding") == "gzip" {
		body, err = gzip.NewReader(r.Body)
		if err != nil {
			sugar.Info("failed to create a reader reading the given reader.", err)
			renderInternalServerError(rw)
			return
		}
	} else {
		body = r.Body
	}
	defer body.Close()

	stream, err := t.client.PostReceivePack(ctx)
	if err != nil {
		renderInternalServerError(rw)
		return
	}
	defer func() {
		resp, err := stream.CloseAndRecv()
		if err != nil {
			sugar.Info("failed to close and recv.", err)
			return
		}

		rw.Write(resp.Data)
	}()

	err = stream.Send(&pbSmart.ReceivePackRequest{
		Repository: &pbSmart.Repository{
			User: user,
			Repo: repo,
		},
	})
	if err != nil {
		renderInternalServerError(rw)
		return
	}

	buf := make([]byte, 32*1024)

	for {
		n, err := body.Read(buf)
		if n > 0 {
			err = stream.Send(&pbSmart.ReceivePackRequest{
				Repository: &pbSmart.Repository{
					User: user,
					Repo: repo,
				},
				Data: buf[:n],
			})
			if err != nil {
				renderInternalServerError(rw)
				return
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			renderInternalServerError(rw)
			return
		}
	}

	rw.Header().Set("Content-Operation", fmt.Sprintf("application/x-git-%s-result", receivePack))
	rw.WriteHeader(http.StatusOK)
}

func (t *GitSmartHTTPClient) GetInfoRefs(rw http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	user := metadata.UserFromContext(ctx)
	repo := metadata.RepoFromContext(ctx)

	serviceName := getServiceType(r)
	var service pbSmart.Service
	switch serviceName {
	case uploadPack:
		service = pbSmart.Service_UPLOAD_PACK
	case receivePack:
		service = pbSmart.Service_RECEIVE_PACK
	}

	infoRefs, err := t.client.GetInfoRefs(ctx, &pbSmart.InfoRefsRequest{
		Repository: &pbSmart.Repository{
			User: user,
			Repo: repo,
		},
		Service: service,
	})

	if err != nil {
		renderNotFound(rw)
		return
	}

	rw.Header().Set("Expires", "Fri, 01 Jan 1980 00:00:00 GMT")
	rw.Header().Set("Pragma", "no-cache")
	rw.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
	rw.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-advertisement", serviceName))
	rw.WriteHeader(http.StatusOK)

	str := "# service=git-" + serviceName + "\n"
	s := fmt.Sprintf("%04s", strconv.FormatInt(int64(len(str)+4), 16))
	rw.Write([]byte(s + str))
	rw.Write([]byte("0000"))

	rw.Write(infoRefs.Data)
}

func getServiceType(req *http.Request) string {
	serviceType := req.FormValue("service")
	if has := strings.HasPrefix(serviceType, "git-"); !has {
		return ""
	}
	return strings.Replace(serviceType, "git-", "", 1)
}
