package runtimeapi

import (
	"fmt"
	"sync"

	"github.com/ClickHouse/clickhouse-go/v2"
	sentinelaccess "mcp-runtime/pkg/access"
	chpkg "mcp-runtime/pkg/clickhouse"
	"mcp-runtime/pkg/controlplane"
	"mcp-runtime/pkg/k8sclient"
	"mcp-runtime/pkg/sentinel"
)

type RuntimeServer struct {
	db          *chpkg.Client
	clickhouse  clickhouse.Conn
	dbName      string
	apiKeys     map[string]struct{}
	platform    *platformStore
	k8sClients  *k8sclient.Clients
	control     *controlplane.Manager
	accessMgr   *sentinelaccess.Manager
	sentinelMgr *sentinel.Manager
	audit       auditWriter

	liveInventoryOnce  sync.Once
	liveInventoryCache *liveInventoryCache
	liveInventoryProbe liveInventoryProber
}

// NewRuntimeServer creates a runtime server with Kubernetes access.
func NewRuntimeServer(db clickhouse.Conn, dbName string, apiKeys map[string]struct{}, platform *platformStore) (*RuntimeServer, error) {
	// Create ClickHouse client wrapper
	chClient := &chpkg.Client{
		Conn:   db,
		DBName: dbName,
	}

	// Initialize Kubernetes clients (in-cluster or kubeconfig)
	k8sClients, err := k8sclient.New()
	if err != nil {
		// Log warning but don't fail - some endpoints will be unavailable
		fmt.Printf("[WARN] Kubernetes client initialization failed: %v\n", err)
		k8sClients = nil
	}

	var accessMgr *sentinelaccess.Manager
	var sentinelMgr *sentinel.Manager
	var control *controlplane.Manager

	if k8sClients != nil {
		control = controlplane.New(k8sClients)
		accessMgr = sentinelaccess.NewManager(k8sClients.Dynamic, k8sClients.Clientset)
		sentinelMgr = sentinel.NewManager(k8sClients.Clientset)
	}

	return &RuntimeServer{
		db:          chClient,
		clickhouse:  db,
		dbName:      dbName,
		apiKeys:     apiKeys,
		platform:    platform,
		k8sClients:  k8sClients,
		control:     control,
		accessMgr:   accessMgr,
		sentinelMgr: sentinelMgr,
	}, nil
}

func (s *RuntimeServer) controlPlane() *controlplane.Manager {
	if s.control != nil {
		return s.control
	}
	if s.k8sClients == nil {
		return nil
	}
	return controlplane.New(s.k8sClients)
}

func (s *RuntimeServer) KubernetesAvailable() bool {
	return s != nil && s.k8sClients != nil
}
