package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/pires/go-proxyproto"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	pbSSH "github.com/vvatanabe/git-ha-poc/proto/ssh"
	"github.com/vvatanabe/git-ha-poc/shared/interceptor"
	"github.com/vvatanabe/git-ha-poc/shared/metadata"
	"github.com/vvatanabe/git-ssh-test-server/gitssh"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc"
)

const (
	cmdName       = "git-ssh"
	envPrefix     = "git_ssh"
	dotSSHDirName = ".ssh"

	defaultPort               = 22
	defaultShutdownTimeout    = 30 * time.Second
	defaultHostPrivateKeyName = "id_rsa"

	flagNamePort               = "port"
	flagNameTCPHealthCheckPort = "tcp_health_check_port"
	flagNameDSN                = "dsn"
	flagNameHostPrivateKeyPath = "host_private_key_path"
	flagNameShutdownTimeout    = "shutdown_timeout"
	flagNameProxyProtocol      = "proxy_protocol"
	flagNameGitRPCAddr         = "git_rpc_addr"
)

type Config struct {
	Port               int           `mapstructure:"port"`
	TCPHealthCheckPort int           `mapstructure:"tcp_health_check_port"`
	HostPrivateKeyPath string        `mapstructure:"host_private_key_path"`
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

	homeDir, err := os.UserHomeDir()
	if err != nil {
		sugar.Fatal("failed to get user home dir:", err)
	}
	defaultPrivateKey := path.Join(homeDir, dotSSHDirName, defaultHostPrivateKeyName)

	root := &cobra.Command{
		Use: cmdName,
		Run: run,
	}

	flags := root.PersistentFlags()
	flags.Int(flagNamePort, defaultPort, fmt.Sprintf("port number for SSH [%s]", getEnvVarName(flagNamePort)))
	flags.Int(flagNameTCPHealthCheckPort, 0, fmt.Sprintf("port number for TCP Health Check [%s] (listen only when you specify port)", getEnvVarName(flagNameTCPHealthCheckPort)))
	flags.String(flagNameDSN, "", fmt.Sprintf("database source name [%s]", getEnvVarName(flagNameDSN)))
	flags.DurationP(flagNameShutdownTimeout, "", defaultShutdownTimeout, fmt.Sprintf("process shutdown timeout [%s]", getEnvVarName(flagNameShutdownTimeout)))
	flags.Bool(flagNameProxyProtocol, false, fmt.Sprintf("use proxy protocol [%s]", getEnvVarName(flagNameProxyProtocol)))
	flags.Bool(flagNameGitRPCAddr, false, fmt.Sprintf("git rpc addr [%s]", getEnvVarName(flagNameGitRPCAddr)))
	flags.StringP(flagNameHostPrivateKeyPath, "", defaultPrivateKey, fmt.Sprintf("host's private key path [%s]", getEnvVarName(flagNameHostPrivateKeyPath)))

	_ = viper.BindPFlag(flagNamePort, flags.Lookup(flagNamePort))
	_ = viper.BindPFlag(flagNameTCPHealthCheckPort, flags.Lookup(flagNameTCPHealthCheckPort))
	_ = viper.BindPFlag(flagNameDSN, flags.Lookup(flagNameDSN))
	_ = viper.BindPFlag(flagNameShutdownTimeout, flags.Lookup(flagNameShutdownTimeout))
	_ = viper.BindPFlag(flagNameProxyProtocol, flags.Lookup(flagNameProxyProtocol))
	_ = viper.BindPFlag(flagNameGitRPCAddr, flags.Lookup(flagNameGitRPCAddr))
	_ = viper.BindPFlag(flagNameHostPrivateKeyPath, flags.Lookup(flagNameHostPrivateKeyPath))

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
				sugar.Fatalf("failed to listen tcp: %v", err)
			}
			sugar.Infof("start to serve on tcp port: %d", config.TCPHealthCheckPort)
			for {
				conn, err := tcpLis.Accept()
				if err != nil {
					sugar.Errorf("failed to accept tcp: %v", err)
					continue
				}
				conn.Close()
			}
		}()
	}

	hostPrivateKey, err := ioutil.ReadFile(config.HostPrivateKeyPath)
	if err != nil {
		sugar.Fatalf("failed to read private key: %s %v", config.HostPrivateKeyPath, err)
	}
	hostPrivateKeySigner, err := ssh.ParsePrivateKey(hostPrivateKey)
	if err != nil {
		sugar.Fatalf("failed to parse private key: %s %v", config.HostPrivateKeyPath, err)
	}

	store, err := newStore(config.DSN)
	if err != nil {
		sugar.Fatal("failed to create store:", err)
	}

	conn, err := newGRPCConn(config.GitRPCAddr)
	if err != nil {
		sugar.Fatal("failed to dial: %s", err)
	}

	gitSSH := &GitSSHRPCClient{
		client: pbSSH.NewSSHProtocolServiceClient(conn),
	}

	s := gitssh.Server{
		GitRequestTransfer: newGitRequestTransfer(gitSSH),
		PublicKeyCallback:  newPublicKeyCallback(store),
		Signer:             hostPrivateKeySigner,
		Logger: &GitSSHLogger{
			sugar: sugar,
		},
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.Port))
	if err != nil {
		sugar.Fatalf("failed to listen ssh: %v", err)
	}

	if config.ProxyProtocol {
		lis = &proxyproto.Listener{Listener: lis}
	}

	go func() {
		sugar.Infof("start to serve on ssh port: %d", config.Port)
		ready <- struct{}{}
		if err := s.Serve(lis); err != gitssh.ErrServerClosed && err != nil {
			sugar.Errorf("failed to serve: %v", err)
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	<-done

	sugar.Info("received a signal")

	ctx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		sugar.Infof("failed to shutdown: %v", err)
	}

	sugar.Info("completed shutdown")
}

func newPublicKeyCallback(store *Store) gitssh.PublicKeyCallback {
	return func(connMetadata ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		startTime := time.Now()
		session := hex.EncodeToString(connMetadata.SessionID())
		var err error
		defer func() {
			finishTime := time.Now()
			sugar.Infow("PUBLIC_KEY_AUTH",
				"session", session,
				"user", connMetadata.User(),
				"remote_addr", getRemoteAddr(connMetadata.RemoteAddr()),
				"client_version", string(connMetadata.ClientVersion()),
				"key_type", key.Type(),
				"elapsed", finishTime.Sub(startTime),
				"error", err)
		}()

		user, err := store.GetUserBySSHKey(string(key.Marshal()))
		if err != nil {
			return nil, errors.New("failed to authorize")
		}
		return &ssh.Permissions{
			CriticalOptions: map[string]string{},
			Extensions: map[string]string{
				"SESSION":  session,
				"USERNAME": user,
			},
		}, nil
	}
}

func newGitRequestTransfer(rpc *GitSSHRPCClient) gitssh.GitRequestTransfer {
	return func(ch ssh.Channel, req *ssh.Request, perms *ssh.Permissions, gitCmd, repoPath string) error {
		startTime := time.Now()
		chs := &ChannelWithSize{
			Channel: ch,
		}
		var err error
		defer func() {
			finishTime := time.Now()

			payload := string(req.Payload)
			i := strings.Index(payload, "git")
			if i > -1 {
				payload = payload[i:]
			}

			sugar.Infow("GIT_SSH_REQUEST",
				"session", perms.Extensions["SESSION"],
				"type", req.Type,
				"payload", payload,
				"size", chs.Size(),
				"elapsed", finishTime.Sub(startTime),
				"error", err)
		}()

		user, repo := splitRepoPath(repoPath)
		ctx := context.Background()
		ctx = metadata.ContextWithUser(ctx, user)
		ctx = metadata.ContextWithRepo(ctx, repo)

		if perms.Extensions["USERNAME"] != user {
			err = errors.New("could not access")
			return err
		}

		_ = req.Reply(true, nil)

		switch gitCmd {
		case "git-receive-pack":
			err = rpc.GitReceivePack(ctx, chs)
			return err
		case "git-upload-pack":
			err = rpc.GitUploadPack(ctx, chs, req)
			return err
		default:
			err = errors.New("unknown operation " + gitCmd)
			return err
		}
	}
}

func newGRPCConn(addr string) (*grpc.ClientConn, error) {
	streamChain := grpc_middleware.ChainStreamClient(
		interceptor.XGitUserStreamClientInterceptor,
		interceptor.XGitRepoStreamClientInterceptor)
	return grpc.Dial(addr, grpc.WithInsecure(),
		grpc.WithStreamInterceptor(streamChain))
}

func splitRepoPath(repoPath string) (user, repo string) {
	splitPath := strings.Split(repoPath, "/")
	if len(splitPath) != 3 {
		return
	}
	user = splitPath[1]
	repo = strings.TrimSuffix(splitPath[2], ".git")
	return
}

func getRemoteAddr(addr net.Addr) string {
	s := addr.String()
	if strings.ContainsRune(s, ':') {
		host, _, _ := net.SplitHostPort(s)
		return host
	}
	return s
}
