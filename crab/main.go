package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	pbSmart "github.com/vvatanabe/git-ha-poc/proto/smart"
	"google.golang.org/grpc"
)

const (
	port = ":8080"

	uploadPack  = "upload-pack"
	receivePack = "receive-pack"
)

func main() {

	conn, err := grpc.Dial(os.Getenv("CRAB_SERVICE_ADDR"), grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed to dial: %s", err)
	}
	transfer := &GitHttpTransfer{
		client: pbSmart.NewSmartProtocolServiceClient(conn),
	}

	r := mux.NewRouter()
	r.Path("/{user}/{repo}.git/git-upload-pack").
		Methods(http.MethodPost).
		HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			vars := mux.Vars(r)
			user := vars["user"]
			repo := vars["repo"]
			transfer.GitUploadPack(context.Background(), user, repo, rw, r)
		})

	r.Path("/{user}/{repo}.git/git-receive-pack").Methods(http.MethodPost).
		HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			vars := mux.Vars(r)
			user := vars["user"]
			repo := vars["repo"]
			transfer.GitReceivePack(context.Background(), user, repo, rw, r)
		})

	r.Path("/{user}/{repo}.git/info/refs").Methods(http.MethodPost).
		HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {

		})

	log.Println("start server on port", port)

	err = http.ListenAndServe(port, r)
	if err != nil {
		log.Println("failed to exit serve: ", err)
	}
}

type GitHttpTransfer struct {
	client pbSmart.SmartProtocolServiceClient
}

func (t *GitHttpTransfer) GitUploadPack(ctx context.Context, user, repo string, rw http.ResponseWriter, r *http.Request) {

	var body io.ReadCloser
	var err error

	if r.Header.Get("Content-Encoding") == "gzip" {
		body, err = gzip.NewReader(r.Body)
		if err != nil {
			log.Println("failed to create a reader reading the given reader. ", err.Error())
			RenderInternalServerError(rw)
			return
		}
	} else {
		body = r.Body
	}
	defer body.Close()

	buf := new(bytes.Buffer)
	io.Copy(buf, body)

	stream, err := t.client.PostUploadPack(context.Background(), &pbSmart.UploadPackRequest{
		Repository: &pbSmart.Repository{
			User: user,
			Repo: repo,
		},
		Data: buf.Bytes(),
	})
	if err != nil {
		RenderInternalServerError(rw)
		return
	}

	rw.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-result", uploadPack))
	rw.WriteHeader(http.StatusOK)

	for {
		c, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			RenderInternalServerError(rw)
			return
		}
		rw.Write(c.Data)
	}
}

func (t *GitHttpTransfer) GitReceivePack(ctx context.Context, user, repo string, rw http.ResponseWriter, r *http.Request) {

	var body io.ReadCloser
	var err error

	if r.Header.Get("Content-Encoding") == "gzip" {
		body, err = gzip.NewReader(r.Body)
		if err != nil {
			log.Println("failed to create a reader reading the given reader. ", err.Error())
			RenderInternalServerError(rw)
			return
		}
	} else {
		body = r.Body
	}
	defer body.Close()

	stream, err := t.client.PostReceivePack(context.Background())
	if err != nil {
		RenderInternalServerError(rw)
		return
	}
	defer stream.CloseAndRecv()

	buf := make([]byte, 1000*1024)
	for {
		n, err := body.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			RenderInternalServerError(rw)
			return
		}
		err = stream.Send(&pbSmart.ReceivePackRequest{
			Repository: &pbSmart.Repository{
				User: user,
				Repo: repo,
			},
			Data: buf[:n],
		})
		if err != nil {
			RenderInternalServerError(rw)
			return
		}
	}

	rw.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-result", receivePack))
	rw.WriteHeader(http.StatusOK)
}

func RenderMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	if r.Proto == "HTTP/1.1" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(http.StatusText(http.StatusMethodNotAllowed)))
	} else {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(http.StatusText(http.StatusBadRequest)))
	}
}

func RenderNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(http.StatusText(http.StatusNotFound)))
}

func RenderNoAccess(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(http.StatusText(http.StatusForbidden)))
}

func RenderInternalServerError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(http.StatusText(http.StatusInternalServerError)))
}
