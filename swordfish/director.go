package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/connectivity"

	"github.com/go-jwdk/jobworker"
	"github.com/vvatanabe/expiremap"
	"github.com/vvatanabe/git-ha-poc/internal/grpc-proxy/proxy"
	"github.com/vvatanabe/git-ha-poc/shared/metadata"
	"github.com/vvatanabe/git-ha-poc/shared/replication"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	pbHealth "google.golang.org/grpc/health/grpc_health_v1"
)

type Node struct {
	Name        string
	ClusterName string
	Addr        string
	Writable    bool
	Active      bool
	CertFile    string
}

func getDirector(publisher *jobworker.JobWorker, store *Store) func(context.Context, string) (context.Context, *grpc.ClientConn, func(), error) {

	connMgr := ClientConnManager{
		ConnMaxLifetime:    connMaxLifetime,
		ConnInactiveExpire: connInactiveExpire,
	}

	return func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, func(), error) {

		ope := getOperation(fullMethodName)
		if ope == Unknown {
			return ctx, nil, nil, errors.New("unknown operation: " + fullMethodName)
		}

		userName := metadata.GetUserFromIncomingContext(ctx)
		if len(userName) < 1 {
			return ctx, nil, nil, errors.New("no user name in incoming context")
		}

		repoName := metadata.GetRepoFromIncomingContext(ctx)
		if len(repoName) < 1 {
			return ctx, nil, nil, errors.New("no repo name in incoming context")
		}

		clusterName, err := store.GetClusterNameByUserName(userName)
		if err != nil {
			return ctx, nil, nil, fmt.Errorf("failed to get cluster name: %w", err)
		}

		nodes, err := store.GetNodesByClusterName(ctx, clusterName)
		if err != nil {
			return ctx, nil, nil, fmt.Errorf("failed to get nodes: %w", err)
		}
		if len(nodes) == 0 {
			return ctx, nil, nil, errors.New("no nodes")
		}

		shuffle(nodes)

		switch ope {
		case Read:
			var activeConn *grpc.ClientConn
			for _, node := range nodes {

				conn, err := connMgr.GetConn(node.Addr)
				if err != nil {
					log.Println("failed to get conn:", err)
					continue
				}

				hasLog, err := store.ExistsReplicationLog(replication.NewGroupID(node.Addr, userName, repoName))
				if err != nil {
					log.Println("failed to exists replication log:", err)
					continue
				}
				if hasLog {
					log.Println("node is currently replicating:", node.Addr)
					continue
				}

				activeConn = conn
			}
			if activeConn == nil {
				return ctx, nil, nil, errors.New("could not select node")
			}
			log.Printf("found reader node: %s > %s\n", fullMethodName, activeConn.Target())
			return ctx, activeConn, func() {}, err
		case Write:
			var (
				primaryNode    *Node
				secondaryNodes []*Node
			)
			for _, node := range nodes {
				if node.Writable {
					primaryNode = node
				} else {
					secondaryNodes = append(secondaryNodes, node)
				}
			}
			if primaryNode == nil {
				return ctx, nil, nil, errors.New("could not select writer node")
			}
			conn, err := connMgr.GetConn(primaryNode.Addr)
			if err != nil {
				return ctx, nil, nil, fmt.Errorf("could not select writer node: %w", err)
			}
			log.Printf("found writer node: %s > %s\n", fullMethodName, conn.Target())
			finishedFunc := getFinishedFunc(publisher, primaryNode, secondaryNodes, fullMethodName, clusterName, userName, repoName)
			return ctx, conn, finishedFunc, err
		}

		return ctx, nil, func() {}, errors.New("not found active node")
	}
}

type NodeSelector struct {
	store   *Store
	connMgr *ClientConnManager
}

func (s *NodeSelector) GetWritableNode(ctx context.Context, userName, repoName string) (*grpc.ClientConn, error) {

	clusterName, err := s.store.GetClusterNameByUserName(userName)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster name: %w", err)
	}

	nodes, err := s.store.GetNodesByClusterName(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes: %w", err)
	}
	if len(nodes) == 0 {
		return nil, errors.New("no nodes")
	}

	shuffle(nodes)

	var (
		primaryNode    *Node
		secondaryNodes []*Node
	)
	for _, node := range nodes {
		if node.Writable {
			primaryNode = node
		} else {
			secondaryNodes = append(secondaryNodes, node)
		}
	}
	if primaryNode == nil {
		return nil, errors.New("could not select writer node")
	}
	conn, err := s.connMgr.GetConn(primaryNode.Addr)
	if err != nil {
		return nil, fmt.Errorf("could not select writer node: %w", err)
	}
	return conn, err
}

func (s *NodeSelector) GetReadableNode(ctx context.Context, userName, repoName string) (*grpc.ClientConn, error) {

	clusterName, err := s.store.GetClusterNameByUserName(userName)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster name: %w", err)
	}

	nodes, err := s.store.GetNodesByClusterName(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes: %w", err)
	}
	if len(nodes) == 0 {
		return nil, errors.New("no nodes")
	}

	shuffle(nodes)

	var activeConn *grpc.ClientConn
	for _, node := range nodes {

		conn, err := s.connMgr.GetConn(node.Addr)
		if err != nil {
			log.Println("failed to get conn:", err)
			continue
		}

		hasLog, err := s.store.ExistsReplicationLog(replication.NewGroupID(node.Addr, userName, repoName))
		if err != nil {
			log.Println("failed to exists replication log:", err)
			continue
		}
		if hasLog {
			log.Println("node is currently replicating:", node.Addr)
			continue
		}

		activeConn = conn
	}
	if activeConn == nil {
		return nil, errors.New("could not select node")
	}

	return activeConn, nil

}

type ClientConnManager struct {
	ConnMaxLifetime    time.Duration
	ConnInactiveExpire time.Duration

	addr2conn     expiremap.Map
	addr2inactive expiremap.Map
}

var ErrNodeInactive = errors.New("node inactive")

type getter func() (*grpc.ClientConn, error)

func newGetter(addr string) getter {

	var (
		once sync.Once
		conn *grpc.ClientConn
		err  error
	)

	return func() (*grpc.ClientConn, error) {
		once.Do(func() {
			conn, err = grpc.DialContext(context.Background(), addr, grpc.WithDefaultCallOptions(grpc.ForceCodec(proxy.NewCodec())),
				grpc.WithInsecure())
		})
		return conn, err
	}
}

func (m *ClientConnManager) GetConn(addr string) (*grpc.ClientConn, error) {

	if _, inactive := m.addr2inactive.Load(addr); inactive {
		return nil, ErrNodeInactive
	}

	var conn *grpc.ClientConn

	actual, _ := m.addr2conn.LoadOrStore(addr,
		newGetter(addr),
		expiremap.Expire(m.ConnMaxLifetime),
		expiremap.ExpiredFunc(func() {
			if conn != nil {
				return
			}
			if conn.GetState() == connectivity.Connecting {
				conn.WaitForStateChange(context.Background(), connectivity.Connecting)
			}
			err := conn.Close()
			if err != nil {
				log.Println("failed to close gRPC conn:", err)
			}
		}))

	if actual != nil {
		getter := actual.(getter)
		newConn, err := getter()
		if err != nil {
			return nil, err
		}
		conn = newConn
	}

	health := pbHealth.NewHealthClient(conn)

	resp, err := health.Check(context.Background(), &pbHealth.HealthCheckRequest{Service: ""})

	if err != nil || resp.Status != pbHealth.HealthCheckResponse_SERVING {
		m.addr2inactive.Store(addr, struct{}{}, expiremap.Expire(m.ConnInactiveExpire))
		return nil, ErrNodeInactive
	}

	return conn, nil
}

type Operation string

const (
	Unknown Operation = "unknown"
	Write   Operation = "write"
	Read    Operation = "read"
)

func getOperation(fullMethodName string) Operation {
	if strings.HasPrefix(fullMethodName, "/repository.RepositoryService") {
		return Write
	}
	if strings.HasSuffix(fullMethodName, "/PostReceivePack") {
		return Write
	}
	if strings.HasSuffix(fullMethodName, "/PostUploadPack") {
		return Read
	}
	if strings.HasPrefix(fullMethodName, "/smart.SmartProtocolService/GetInfoRefs") { // TODO
		return Read
	}
	return Unknown
}

func selectNode(nodes []*Node, ope Operation) *Node {
	shuffle(nodes)
	switch ope {
	case Write:
		for _, node := range nodes {
			if node.Active && node.Writable {
				return node
			}
		}
	case Read:
		for _, node := range nodes {
			if node.Active {
				return node
			}
		}
	}
	return nil
}

func shuffle(nodes []*Node) {
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(nodes), func(i, j int) {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	})
}

func getFinishedFunc(publisher *jobworker.JobWorker, src *Node, dests []*Node, fullMethodName, cluster, user, repo string) func() {
	return func() {

		t := getReplicationOperation(fullMethodName)
		if t == "" {
			log.Printf("not found replication operation: %s\n", fullMethodName)
			return
		}

		var entries []*jobworker.EnqueueBatchEntry
		for i, dest := range dests {
			if dest.Writable {
				continue
			}
			content, err := json.Marshal(&replication.Log{
				Operation:  t,
				User:       user,
				Repo:       repo,
				TargetNode: dest.Addr,
				RemoteNode: src.Addr,
				Cluster:    cluster,
			})
			if err != nil {
				log.Println("failed to marshal replication log", err)
				return
			}
			//groupID :=
			entries = append(entries, &jobworker.EnqueueBatchEntry{
				ID: fmt.Sprintf("id-%d", i),
				Metadata: map[string]string{
					"GroupID": string(replication.NewGroupID(dest.Addr, user, repo)),
				},
				Content: string(content),
			})
		}

		out, err := publisher.EnqueueBatch(context.Background(), &jobworker.EnqueueBatchInput{
			Queue:   replication.LogQueueName,
			Entries: entries,
		})
		if err != nil {
			if out != nil {
				log.Printf("failed to enqueue replication log: %s, %s\n", err, out.Failed)
			} else {
				log.Printf("failed to enqueue replication log: %s\n", err)
			}
		}
	}
}

func getReplicationOperation(fullMethodName string) replication.Operation {
	if strings.HasPrefix(fullMethodName, "/repository.RepositoryService/CreateRepository") {
		return replication.CreateRepo
	}
	if strings.HasSuffix(fullMethodName, "/PostReceivePack") {
		return replication.UpdateRepo
	}
	return ""
}

func getCredentials(cache map[string]credentials.TransportCredentials, backend *Node) (credentials.TransportCredentials, error) {
	if v := cache[backend.Addr]; v != nil {
		return v, nil
	}
	creds, err := credentials.NewClientTLSFromFile(backend.CertFile, backend.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS credentials: %w", err)
	}
	cache[backend.Addr] = creds
	return creds, nil
}
