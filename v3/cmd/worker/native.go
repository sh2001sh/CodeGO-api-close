package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/credentials"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/limits"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers"
	"github.com/sh2001sh/new-api/v3/internal/gateway/routing"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

type nativeJobs struct {
	workflow *workflow.Handler
	live     *live.Handler
	close    func()
	requests *audit.RequestRecorder
}

// nativeDeps are the shared pieces both reconcilers use.
type nativeDeps struct {
	deps     *boot.Deps
	settler  *billing.Settler
	clients  gateway.ClientProvider
	resolve  func(ctx context.Context, channel, credential int64) (gateway.Target, error)
	registry map[string]gateway.Provider
	log      *slog.Logger
	requests *audit.RequestRecorder
}

func newNativeJobs(deps *boot.Deps, settler *billing.Settler, current func() *catalog.Snapshot, log *slog.Logger) (*nativeJobs, error) {
	transports := httpx.NewPool(httpx.TransportConfig{})
	credentialTransports := credentials.NewTransportPool(credentials.TransportConfig{})
	closeTransports := func() {
		credentialTransports.CloseIdle()
		transports.CloseIdle()
	}
	clients := boot.TargetClients(credentialTransports, transports)
	requests := audit.NewRequestRecorder(context.Background(), deps.PG.Pool, log)
	succeeded := false
	defer func() {
		if !succeeded {
			requests.Close()
		}
	}()
	n := nativeDeps{deps: deps, settler: settler, clients: clients, log: log,
		requests: requests,
		registry: providers.Registry(clients),
		resolve: func(ctx context.Context, channel, credential int64) (gateway.Target, error) {
			return deps.ResolveTarget(ctx, channel, credential)
		}}

	workflowJobs, err := workflow.NewReconciler(workflow.Config{
		Settler: billing.NewWorkflowSettler(settler), Repository: &workflow.PostgresRepository{Pool: deps.PG.Pool},
		ResolveTarget: n.resolve, Clients: clients, Logger: log, Requests: requests,
	})
	if err != nil {
		closeTransports()
		return nil, fmt.Errorf("workflow reconciler: %w", err)
	}
	background, files, err := n.backgroundReconciler(current)
	if err != nil {
		closeTransports()
		return nil, err
	}
	succeeded = true
	return &nativeJobs{workflow: workflowJobs, live: background, requests: requests, close: func() {
		requests.Close() // caller joins reconcilers before closing native services
		closeTransports()
		if err := files.Close(); err != nil {
			log.Error("close background file storage", "err", err)
		}
	}}, nil
}

// backgroundReconciler builds the live handler that resumes Responses,
// Realtime and media jobs. Reconciliation uses persisted targets and frozen
// pricing; it never plans a replacement channel or admits a new generation.
func (n nativeDeps) backgroundReconciler(current func() *catalog.Snapshot) (*live.Handler, *live.DiskFileStore, error) {
	locators, err := live.NewRedisRepository(n.deps.Redis, "")
	if err != nil {
		return nil, nil, err
	}
	jobs, err := live.NewRedisBackgroundRepository(n.deps.Redis, "", n.deps.Crypto.DeriveKey("background-jobs"))
	if err != nil {
		return nil, nil, err
	}
	files, err := openFiles()
	if err != nil {
		return nil, nil, err
	}
	background, err := live.New(live.Config{
		Auth:    identity.New(n.deps.PG.Pool, n.deps.Redis, identity.Config{}, n.log),
		Planner: routing.New(func() *catalog.Snapshot { return nil }, routing.Config{}),
		Settler: n.settler, Limits: limits.New(n.deps.Redis, limits.Config{}), Providers: n.registry, Requests: n.requests,
		Resolve: n.resolve, Repository: locators, BackgroundJobs: jobs, Clients: n.clients,
		BackgroundBilling: billing.NewBackgroundSettler(n.settler, jobs),
		TargetPolicy:      sensitiveWordPolicy(current, n.log),
		ResolvePrincipal: func(ctx context.Context, user, key int64) (gateway.Principal, error) {
			return identity.LoadPrincipal(ctx, n.deps.PG.Pool, user, key)
		},
		Files: files, DeliveryKey: n.deps.Crypto.DeriveKey("file-delivery"), Logger: n.log,
	})
	if err != nil {
		if closeErr := files.Close(); closeErr != nil {
			n.log.Error("close background files after setup failure", "err", closeErr)
		}
		return nil, nil, fmt.Errorf("background reconciler: %w", err)
	}
	for id, adapter := range n.registry {
		n.registry[id] = background.TrackingProvider(adapter)
	}
	return background, files, nil
}

func openFiles() (*live.DiskFileStore, error) {
	directory := os.Getenv("V3_FILES_DIR")
	if directory == "" {
		directory = "files"
	}
	files, err := live.NewDiskFileStore(directory)
	if err != nil {
		return nil, fmt.Errorf("background files: %w", err)
	}
	return files, nil
}

func sensitiveWordPolicy(current func() *catalog.Snapshot, log *slog.Logger) gateway.TargetPolicy {
	return gateway.NewSensitiveWordPolicy(func() map[string]json.RawMessage {
		if snapshot := current(); snapshot != nil {
			return snapshot.Settings
		}
		return nil
	}, log)
}
