package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	pbRepository "github.com/vvatanabe/git-ha-poc/proto/repository"
	pbSmart "github.com/vvatanabe/git-ha-poc/proto/smart"
	"github.com/vvatanabe/git-ha-poc/shared/interceptor"
	"github.com/vvatanabe/git-ha-poc/shared/metadata"
	"google.golang.org/grpc"
)

const (
	appName     = "barracuda"
	port        = ":8080"
	uploadPack  = "upload-pack"
	receivePack = "receive-pack"
)

var (
	dsn           string
	swordfishAddr string
)

func init() {
	log.SetFlags(0)
	log.SetPrefix(fmt.Sprintf("[%s] ", appName))

	dsn = os.Getenv("DSN")
	swordfishAddr = os.Getenv("SWORDFISH_ADDR")
}

func main() {

	unaryChain := grpc_middleware.ChainUnaryClient(interceptor.XGitUserUnaryClientInterceptor, interceptor.XGitRepoUnaryClientInterceptor)
	streamChain := grpc_middleware.ChainStreamClient(interceptor.XGitUserStreamClientInterceptor, interceptor.XGitRepoStreamClientInterceptor)
	conn, err := grpc.Dial(swordfishAddr, grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(unaryChain), grpc.WithStreamInterceptor(streamChain))
	if err != nil {
		log.Fatalf("failed to dial: %s", err)
	}

	transfer := &GitHttpTransfer{
		client: pbSmart.NewSmartProtocolServiceClient(conn),
	}
	repository := &GitRepository{
		client: pbRepository.NewRepositoryServiceClient(conn),
	}

	r := mux.NewRouter()

	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			vars := mux.Vars(r)
			ctx := r.Context()
			ctx = metadata.ContextWithUser(ctx, vars["user"])
			ctx = metadata.ContextWithRepo(ctx, vars["repo"])
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	r.Path("/{user}/{repo}.git/git-upload-pack").
		Methods(http.MethodPost).
		HandlerFunc(transfer.GitUploadPack)

	r.Path("/{user}/{repo}.git/git-receive-pack").Methods(http.MethodPost).
		HandlerFunc(transfer.GitReceivePack)

	r.Path("/{user}/{repo}.git/info/refs").Methods(http.MethodGet).
		HandlerFunc(transfer.GetInfoRefs)

	r.Path("/{user}/{repo}.git").
		Methods(http.MethodPost).
		HandlerFunc(repository.Create)

	log.Println("start server on port", port)

	err = http.ListenAndServe(port, r)
	if err != nil {
		log.Println("failed to exit serve: ", err)
	}
}

type GitRepository struct {
	client pbRepository.RepositoryServiceClient
}

func (gr *GitRepository) Create(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := metadata.UserFromContext(ctx)
	repo := metadata.RepoFromContext(ctx)

	_, err := gr.client.CreateRepository(ctx, &pbRepository.CreateRepositoryRequest{
		User: user,
		Repo: repo,
	})
	if err != nil {
		log.Println("failed to create a repository. ", err.Error())
		renderInternalServerError(rw)
		return
	}
	rw.WriteHeader(http.StatusOK)
	rw.Write([]byte(fmt.Sprintf("remote url: http://localhost:8080/%s/%s.git", user, repo)))
}

type GitHttpTransfer struct {
	client pbSmart.SmartProtocolServiceClient
}

func (t *GitHttpTransfer) GitUploadPack(rw http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	user := metadata.UserFromContext(ctx)
	repo := metadata.RepoFromContext(ctx)

	var body io.ReadCloser
	var err error

	if r.Header.Get("Content-Encoding") == "gzip" {
		body, err = gzip.NewReader(r.Body)
		if err != nil {
			log.Println("failed to create a reader reading the given reader. ", err)
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

func (t *GitHttpTransfer) GitReceivePack(rw http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	user := metadata.UserFromContext(ctx)
	repo := metadata.RepoFromContext(ctx)

	var body io.ReadCloser
	var err error

	if r.Header.Get("Content-Encoding") == "gzip" {
		body, err = gzip.NewReader(r.Body)
		if err != nil {
			log.Println("failed to create a reader reading the given reader. ", err)
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
			log.Println("failed to close and recv ", err)
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

func (t *GitHttpTransfer) GetInfoRefs(rw http.ResponseWriter, r *http.Request) {

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

func renderNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Operation", "text/plain")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(http.StatusText(http.StatusNotFound)))
}

func renderInternalServerError(w http.ResponseWriter) {
	w.Header().Set("Content-Operation", "text/plain")
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(http.StatusText(http.StatusInternalServerError)))
}
