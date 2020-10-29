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

	"github.com/pires/go-proxyproto"

	"github.com/vvatanabe/git-ssh-test-server/gitssh"
	"golang.org/x/crypto/ssh"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	pbReplication "github.com/vvatanabe/git-ha-poc/proto/replication"
	pbRepository "github.com/vvatanabe/git-ha-poc/proto/repository"
	pbSmart "github.com/vvatanabe/git-ha-poc/proto/smart"
	pbSSH "github.com/vvatanabe/git-ha-poc/proto/ssh"
	"github.com/vvatanabe/git-ha-poc/shared/interceptor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	pbHealth "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	cmdName       = "git-rpc"
	envPrefix     = "git_rpc"
	dotSSHDirName = ".ssh"

	defaultGRPCPort           = 50051
	defaultSSHPort            = 2020
	defaultShutdownTimeout    = 30 * time.Second
	defaultRepoDirPath        = "/root/git/repositories"
	defaultGitBinPath         = "/usr/bin/git"
	defaultGitShellPath       = "/usr/bin/git-shell"
	defaultHostPrivateKeyName = "id_rsa"
	defaultAuthorizedKeysName = "authorized_keys"

	flagNameGRPCPort              = "grpc_port"
	flagNameSSHPort               = "ssh_port"
	flagNameTCPHealthCheckPort    = "tcp_health_check_port"
	flagNameHostPrivateKeyPath    = "host_private_key_path"
	flagNameShutdownTimeout       = "shutdown_timeout"
	flagNameProxyProtocol         = "proxy_protocol"
	flagNameInternalSSHUploadPack = "internal_ssh_upload_pack"
	flagNameRepoDirPath           = "repo_dir_path"
	flagNameGitBinPath            = "git_bin_path"
	flagNameGitShellPath          = "git_shell_path"
	flagNameAuthorizedKeysPath    = "authorized_keys_path"
)

type Config struct {
	GRPCPort              int           `mapstructure:"grpc_port"`
	SSHPort               int           `mapstructure:"ssh_port"`
	TCPHealthCheckPort    int           `mapstructure:"tcp_health_check_port"`
	HostPrivateKeyPath    string        `mapstructure:"host_private_key_path"`
	ShutdownTimeout       time.Duration `mapstructure:"shutdown_timeout"`
	ProxyProtocol         bool          `mapstructure:"proxy_protocol"`
	InternalSSHUploadPack bool          `mapstructure:"internal_ssh_upload_pack"`
	RepoDirPath           string        `mapstructure:"repo_dir_path"`
	GitBinPath            string        `mapstructure:"git_bin_path"`
	GitShellPath          string        `mapstructure:"git_shell_path"`
	AuthorizedKeysPath    string        `mapstructure:"authorized_keys_path"`
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
	defaultAuthorizedKeysPath := path.Join(homeDir, dotSSHDirName, defaultAuthorizedKeysName)

	root := &cobra.Command{
		Use: cmdName,
		Run: run,
	}

	flags := root.PersistentFlags()
	flags.Int(flagNameGRPCPort, defaultGRPCPort, fmt.Sprintf("port number for gRPC [%s]", getEnvVarName(flagNameGRPCPort)))
	flags.Int(flagNameSSHPort, defaultSSHPort, fmt.Sprintf("port number for SSH [%s]", getEnvVarName(flagNameSSHPort)))
	flags.Int(flagNameTCPHealthCheckPort, 0, fmt.Sprintf("port number for TCP Health Check [%s] (listen only when you specify port)", getEnvVarName(flagNameTCPHealthCheckPort)))
	flags.String(flagNameHostPrivateKeyPath, defaultPrivateKey, fmt.Sprintf("host's private key path for SSH [%s]", getEnvVarName(flagNameHostPrivateKeyPath)))
	flags.Duration(flagNameShutdownTimeout, defaultShutdownTimeout, fmt.Sprintf("process shutdown timeout [%s]", getEnvVarName(flagNameShutdownTimeout)))
	flags.Bool(flagNameProxyProtocol, false, fmt.Sprintf("use proxy protocol [%s]", getEnvVarName(flagNameProxyProtocol)))
	flags.Bool(flagNameInternalSSHUploadPack, false, fmt.Sprintf("use internal ssh for upload pack [%s]", getEnvVarName(flagNameInternalSSHUploadPack)))
	flags.String(flagNameRepoDirPath, defaultRepoDirPath, fmt.Sprintf("root dir path for git repository [%s]", getEnvVarName(flagNameRepoDirPath)))
	flags.String(flagNameGitBinPath, defaultGitBinPath, fmt.Sprintf("git bin path [%s]", getEnvVarName(flagNameGitBinPath)))
	flags.String(flagNameGitShellPath, defaultGitShellPath, fmt.Sprintf("git shell path [%s]", getEnvVarName(flagNameGitShellPath)))
	flags.StringP(flagNameAuthorizedKeysPath, "", defaultAuthorizedKeysPath, fmt.Sprintf("authorized keys path [%s]", getEnvVarName(flagNameAuthorizedKeysPath)))

	_ = viper.BindPFlag(flagNameGRPCPort, flags.Lookup(flagNameGRPCPort))
	_ = viper.BindPFlag(flagNameSSHPort, flags.Lookup(flagNameSSHPort))
	_ = viper.BindPFlag(flagNameTCPHealthCheckPort, flags.Lookup(flagNameTCPHealthCheckPort))
	_ = viper.BindPFlag(flagNameHostPrivateKeyPath, flags.Lookup(flagNameHostPrivateKeyPath))
	_ = viper.BindPFlag(flagNameShutdownTimeout, flags.Lookup(flagNameShutdownTimeout))
	_ = viper.BindPFlag(flagNameProxyProtocol, flags.Lookup(flagNameProxyProtocol))
	_ = viper.BindPFlag(flagNameInternalSSHUploadPack, flags.Lookup(flagNameInternalSSHUploadPack))
	_ = viper.BindPFlag(flagNameRepoDirPath, flags.Lookup(flagNameRepoDirPath))
	_ = viper.BindPFlag(flagNameGitBinPath, flags.Lookup(flagNameGitBinPath))
	_ = viper.BindPFlag(flagNameGitShellPath, flags.Lookup(flagNameGitShellPath))
	_ = viper.BindPFlag(flagNameAuthorizedKeysPath, flags.Lookup(flagNameAuthorizedKeysPath))

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

	grpcSrv := newGRPCServer(&config)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.GRPCPort))
	if err != nil {
		sugar.Fatal("failed to listen grpc:", err)
	}

	if config.ProxyProtocol {
		lis = &proxyproto.Listener{Listener: lis}
	}

	go func() {
		sugar.Info("start to serve on port:", config.GRPCPort)
		ready <- struct{}{}
		if err := grpcSrv.Serve(lis); err != grpc.ErrServerStopped && err != nil {
			sugar.Error("failed to serve:", err)
		}
	}()

	if config.InternalSSHUploadPack {
		go func() {
			lis, err := net.Listen("tcp", fmt.Sprintf(":%d", defaultSSHPort))
			if err != nil {
				sugar.Fatalf("failed to listen: %v\n", err)
			}
			s := newSSHServer(&config)
			sugar.Infof("start internal ssh server on port %d\n", defaultSSHPort)
			if err := s.Serve(lis); err != nil {
				sugar.Fatalf("failed to serve: %v\n", err)
			}
		}()
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	<-done

	sugar.Info("received a signal")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	go func() {
		<-ctx.Done()
		sugar.Info("timeout shutdown", err)
		grpcSrv.Stop()
	}()
	grpcSrv.GracefulStop()

	sugar.Info("completed shutdown")
}

func newGRPCServer(cfg *Config) *grpc.Server {

	srv := grpc.NewServer(grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
		interceptor.LoggingUnaryServerInterceptor(),
	)), grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
		interceptor.LoggingStreamServerInterceptor()),
	))

	pbHealth.RegisterHealthServer(srv, health.NewServer())

	pbSmart.RegisterSmartProtocolServiceServer(srv, &SmartProtocolService{
		RootPath: cfg.RepoDirPath,
		BinPath:  cfg.GitBinPath,
	})
	pbSSH.RegisterSSHProtocolServiceServer(srv, &SSHProtocolService{
		RootPath:  cfg.RepoDirPath,
		ShellPath: cfg.GitShellPath,
	})
	pbRepository.RegisterRepositoryServiceServer(srv, &RepositoryService{
		RootPath: cfg.RepoDirPath,
		BinPath:  cfg.GitBinPath,
	})
	pbReplication.RegisterReplicationServiceServer(srv, &ReplicationService{
		RootPath: cfg.RepoDirPath,
		BinPath:  cfg.GitBinPath,
		SSHPort:  cfg.SSHPort,
	})

	return srv
}

func newSSHServer(cfg *Config) *gitssh.Server {
	hostPrivateKey, err := ioutil.ReadFile(cfg.HostPrivateKeyPath)
	if err != nil {
		sugar.Fatalf("failed to read private key: %s %v", cfg.HostPrivateKeyPath, err)
	}
	hostPrivateKeySigner, err := ssh.ParsePrivateKey(hostPrivateKey)
	if err != nil {
		sugar.Fatalf("failed to parse private key: %s %v", cfg.HostPrivateKeyPath, err)
	}

	authorizedKeysBytes, err := ioutil.ReadFile(cfg.AuthorizedKeysPath)
	if err != nil {
		sugar.Fatalf("failed to load authorized_keys: %s %v", cfg.AuthorizedKeysPath, err)
	}
	authorizedKeys := make(map[string]struct{})
	for len(authorizedKeysBytes) > 0 {
		pubKey, _, _, rest, err := ssh.ParseAuthorizedKey(authorizedKeysBytes)
		if err != nil {
			sugar.Fatalf("failed to parse authorized_keys, err: %v", err)
		}
		authorizedKeys[string(pubKey.Marshal())] = struct{}{}
		authorizedKeysBytes = rest
	}

	sshLis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.SSHPort))
	if err != nil {
		sugar.Fatalf("failed to listen ssh: %v", err)
	}

	if config.ProxyProtocol {
		sshLis = &proxyproto.Listener{Listener: sshLis}
	}

	srv := &gitssh.Server{
		RepoDir:            cfg.RepoDirPath,
		Signer:             hostPrivateKeySigner,
		PublicKeyCallback:  loggingPublicKeyCallback(authorizedKeys),
		GitRequestTransfer: loggingGitRequestTransfer(cfg.GitShellPath),
		Logger:             sugar,
	}
	return srv
}

func loggingPublicKeyCallback(authorizedKeys map[string]struct{}) gitssh.PublicKeyCallback {
	return func(metadata ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		startTime := time.Now()
		session := hex.EncodeToString(metadata.SessionID())
		var err error
		defer func() {
			finishTime := time.Now()
			sugar.Infow("PUBLIC_KEY_AUTH",
				"session", session,
				"user", metadata.User(),
				"remote_addr", getRemoteAddr(metadata.RemoteAddr()),
				"client_version", string(metadata.ClientVersion()),
				"key_type", key.Type(),
				"elapsed", finishTime.Sub(startTime),
				"error", err)
		}()
		if _, ok := authorizedKeys[string(key.Marshal())]; !ok {
			err = errors.New("failed to authorize")
			return nil, err
		}
		return &ssh.Permissions{
			CriticalOptions: map[string]string{},
			Extensions: map[string]string{
				"SESSION": session,
			},
		}, nil
	}
}
func loggingGitRequestTransfer(shellPath string) gitssh.GitRequestTransfer {
	t := gitssh.LocalGitRequestTransfer(shellPath)
	return func(ctx context.Context, ch ssh.Channel, req *ssh.Request, perms *ssh.Permissions, packCmd, repoPath string) error {
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
		err = t(ctx, chs, req, perms, packCmd, repoPath)
		return err
	}
}

type ChannelWithSize struct {
	ssh.Channel
	size int64
}

func (ch *ChannelWithSize) Size() int64 {
	return ch.size
}

func (ch *ChannelWithSize) Write(data []byte) (int, error) {
	written, err := ch.Channel.Write(data)
	ch.size += int64(written)
	return written, err
}

func getRemoteAddr(addr net.Addr) string {
	s := addr.String()
	if strings.ContainsRune(s, ':') {
		host, _, _ := net.SplitHostPort(s)
		return host
	}
	return s
}
