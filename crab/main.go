package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/pires/go-proxyproto"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	pbRepository "github.com/vvatanabe/git-ha-poc/proto/repository"
	pbSmart "github.com/vvatanabe/git-ha-poc/proto/smart"
	"github.com/vvatanabe/git-ha-poc/shared/interceptor"
	"github.com/vvatanabe/git-ha-poc/shared/metadata"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
)

const (
	cmdName   = "git-smart-http"
	envPrefix = "git_smart_http"

	defaultPort            = 8080
	defaultShutdownTimeout = 30 * time.Second

	flagNamePort               = "port"
	flagNameTCPHealthCheckPort = "tcp_health_check_port"
	flagNameDSN                = "dsn"
	flagNameShutdownTimeout    = "shutdown_timeout"
	flagNameProxyProtocol      = "proxy_protocol"
	flagNameGitRPCAddr         = "git_rpc_addr"
)

type Config struct {
	Port               int           `mapstructure:"port"`
	TCPHealthCheckPort int           `mapstructure:"tcp_health_check_port"`
	DSN                string        `mapstructure:"dsn"`
	ShutdownTimeout    time.Duration `mapstructure:"shutdown_timeout"`
	ProxyProtocol      bool          `mapstructure:"proxy_protocol"`
	GitRPCAddr         string        `mapstructure:"git_rpc_addr"`
}

var (
	config Config
	sugar  *zap.SugaredLogger
)

func init() {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		zap.NewAtomicLevel(),
	))
	sugar = logger.Sugar()
}

func main() {
	defer sugar.Sync() // flushes buffer, if any

	root := &cobra.Command{
		Use: cmdName,
		Run: run,
	}

	flags := root.PersistentFlags()
	flags.Int(flagNamePort, defaultPort, fmt.Sprintf("port number for HTTP [%s]", getEnvVarName(flagNamePort)))
	flags.Int(flagNameTCPHealthCheckPort, 0, fmt.Sprintf("port number for TCP Health Check [%s] (listen only when you specify port)", getEnvVarName(flagNameTCPHealthCheckPort)))
	flags.String(flagNameDSN, "", fmt.Sprintf("database source name [%s]", getEnvVarName(flagNameDSN)))
	flags.DurationP(flagNameShutdownTimeout, "", defaultShutdownTimeout, fmt.Sprintf("process shutdown timeout [%s]", getEnvVarName(flagNameShutdownTimeout)))
	flags.Bool(flagNameProxyProtocol, false, fmt.Sprintf("use proxy protocol [%s]", getEnvVarName(flagNameProxyProtocol)))
	flags.Bool(flagNameGitRPCAddr, false, fmt.Sprintf("git rpc addr [%s]", getEnvVarName(flagNameGitRPCAddr)))

	_ = viper.BindPFlag(flagNamePort, flags.Lookup(flagNamePort))
	_ = viper.BindPFlag(flagNameTCPHealthCheckPort, flags.Lookup(flagNameTCPHealthCheckPort))
	_ = viper.BindPFlag(flagNameDSN, flags.Lookup(flagNameDSN))
	_ = viper.BindPFlag(flagNameShutdownTimeout, flags.Lookup(flagNameShutdownTimeout))
	_ = viper.BindPFlag(flagNameProxyProtocol, flags.Lookup(flagNameProxyProtocol))
	_ = viper.BindPFlag(flagNameGitRPCAddr, flags.Lookup(flagNameGitRPCAddr))

	cobra.OnInitialize(func() {
		viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
		viper.SetEnvPrefix(envPrefix)
		viper.AutomaticEnv()
		if err := viper.Unmarshal(&config); err != nil {
			sugar.Fatal("failed to unmarshal config:", err)
		}
	})

	if err := root.Execute(); err != nil {
		sugar.Fatal("failed to run cmd:", err)
	}
}

func getEnvVarName(s string) string {
	return strings.ToUpper(envPrefix + "_" + s)
}

func run(c *cobra.Command, args []string) {

	sugar.Infof("config: %#v", config)

	ready := make(chan struct{}, 1)
	if config.TCPHealthCheckPort > 0 {
		go func() {
			<-ready
			tcpLis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.TCPHealthCheckPort))
			if err != nil {
				sugar.Fatal("failed to listen tcp:", err)
			}
			sugar.Info("start to serve on tcp port:", config.TCPHealthCheckPort)
			for {
				conn, err := tcpLis.Accept()
				if err != nil {
					sugar.Error("failed to accept tcp:", err)
					continue
				}
				conn.Close()
			}
		}()
	}

	store, err := newStore(config.DSN)
	if err != nil {
		sugar.Fatal("failed to create store:", err)
	}

	conn, err := newGRPCConn(config.GitRPCAddr)
	if err != nil {
		sugar.Fatal("failed to dial: %s", err)
	}

	smartHttp := &GitSmartHTTPClient{
		client: pbSmart.NewSmartProtocolServiceClient(conn),
	}
	repository := &GitRepositoryClient{
		client: pbRepository.NewRepositoryServiceClient(conn),
	}

	router := mux.NewRouter()
	router.Use(withMetadata)
	router.Use(basicAuth(store))
	router.HandleFunc("/{user}/{repo}.git/git-upload-pack", smartHttp.GitUploadPack).Methods(http.MethodPost)
	router.HandleFunc("/{user}/{repo}.git/git-receive-pack", smartHttp.GitReceivePack).Methods(http.MethodPost)
	router.HandleFunc("/{user}/{repo}.git/info/refs", smartHttp.GetInfoRefs).Methods(http.MethodGet)
	router.HandleFunc("/{user}/{repo}.git", repository.Create).Methods(http.MethodPost)

	srv := &http.Server{
		Handler: router,
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.Port))
	if err != nil {
		sugar.Fatal("failed to listen http:", err)
	}

	if config.ProxyProtocol {
		lis = &proxyproto.Listener{Listener: lis}
	}

	go func() {
		sugar.Info("start to serve on h port:", config.Port)
		ready <- struct{}{}
		if err := srv.Serve(lis); err != http.ErrServerClosed && err != nil {
			sugar.Error("failed to serve:", err)
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	<-done

	sugar.Info("received a signal")

	ctx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		sugar.Infof("failed to shutdown: %v", err)
	}

	sugar.Info("completed shutdown")
}

func newGRPCConn(addr string) (*grpc.ClientConn, error) {
	unaryChain := grpc_middleware.ChainUnaryClient(
		interceptor.XGitUserUnaryClientInterceptor,
		interceptor.XGitRepoUnaryClientInterceptor)
	streamChain := grpc_middleware.ChainStreamClient(
		interceptor.XGitUserStreamClientInterceptor,
		interceptor.XGitRepoStreamClientInterceptor)
	return grpc.Dial(addr, grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(unaryChain),
		grpc.WithStreamInterceptor(streamChain))
}

func withMetadata(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		ctx := r.Context()
		ctx = metadata.ContextWithUser(ctx, vars["user"])
		ctx = metadata.ContextWithRepo(ctx, vars["repo"])
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func basicAuth(store *Store) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username, password, ok := r.BasicAuth()
			if !ok {
				renderUnauthorized(w)
				return
			}
			encrypted, err := store.GetEncryptedPassword(username)
			if err != nil {
				renderUnauthorized(w)
				return
			}
			err = bcrypt.CompareHashAndPassword(encrypted, []byte(password))
			if err != nil {
				renderUnauthorized(w)
				return
			}
			ctx := r.Context()
			resource := metadata.UserFromContext(ctx)
			if username != resource {
				renderNoAccess(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func renderUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Please enter your username and password."`)
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(http.StatusText(http.StatusUnauthorized)))
	w.Header().Set("Content-Type", "text/plain")
}

func renderNoAccess(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(http.StatusText(http.StatusForbidden)))
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
