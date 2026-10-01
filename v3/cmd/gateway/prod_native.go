package main

import (
	"context"
	"log/slog"
	"net/netip"
	"os"

	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/auxiliary"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

type nativeHandlers struct {
	live      *live.Handler
	auxiliary *auxiliary.Handler
	workflow  *workflow.Handler
	close     func()
}

func assembleNative(deps *boot.Deps, auth gateway.Authorizer, planner gateway.Planner, settler *billing.Settler,
	leases gateway.LeaseController, failures gateway.AuthFailureController, registry map[string]gateway.Provider,
	transports *httpx.Pool, clients gateway.ClientProvider, policy gateway.TargetPolicy, guard gateway.RequestGuard,
	trusted []netip.Prefix, log *slog.Logger) (result nativeHandlers, err error) {
	resolve := deps.ResolveTarget
	contentAuthorizer, err := newContentAuthorizer(deps, log)
	if err != nil {
		return result, err
	}
	jobs, locators, files, err := newNativeStores(deps)
	if err != nil {
		return result, err
	}
	result.close = func() {
		if err := files.Close(); err != nil {
			log.Error("close file storage failed", "error", err)
		}
	}
	defer func() {
		if err != nil {
			result.close()
		}
	}()
	result.live, err = live.New(live.Config{
		Auth: auth, Planner: planner, Settler: settler, Limits: leases, AuthFailures: failures,
		Providers: registry, Resolve: resolve, Repository: locators, Clients: clients, TargetPolicy: policy, RequestGuard: guard,
		BackgroundJobs: jobs, BackgroundBilling: billing.NewBackgroundSettler(settler, jobs),
		ResolvePrincipal: func(ctx context.Context, userID, keyID int64) (gateway.Principal, error) {
			return identity.LoadPrincipal(ctx, deps.PG.Pool, userID, keyID)
		},
		TrustedProxies: trusted, Files: files, DeliveryKey: deps.Crypto.DeriveKey("file-delivery"), Logger: log,
	})
	if err != nil {
		return result, err
	}
	for name, provider := range registry {
		registry[name] = result.live.TrackingProvider(provider)
	}
	result.auxiliary, err = auxiliary.New(auxiliary.Config{
		Authorizer: auth, Planner: result.live, Settler: settler, Limits: leases,
		TargetPolicy: policy, RequestGuard: guard,
		AuthFailures: failures, TrustedProxies: trusted, Transports: transports, Clients: clients, Logger: log,
	})
	if err != nil {
		return result, err
	}
	result.workflow, err = workflow.New(workflow.Config{
		Authorizer: auth, Planner: result.live, Settler: billing.NewWorkflowSettler(settler),
		Repository: &workflow.PostgresRepository{Pool: deps.PG.Pool}, ResolveTarget: resolve,
		Limits: leases, TrustedProxies: trusted, Clients: clients, ContentAuthorizer: contentAuthorizer, Logger: log,
	})
	return result, err
}

// newNativeStores builds the Redis-backed background job and locator
// repositories and the on-disk file store the live handler depends on.
func newNativeStores(deps *boot.Deps) (jobs live.BackgroundJobRepository, locators live.Repository, files *live.DiskFileStore, err error) {
	jobs, err = live.NewRedisBackgroundRepository(deps.Redis, "", deps.Crypto.DeriveKey("background-jobs"))
	if err != nil {
		return nil, nil, nil, err
	}
	locators, err = live.NewRedisRepository(deps.Redis, "")
	if err != nil {
		return nil, nil, nil, err
	}
	directory := os.Getenv("V3_FILES_DIR")
	if directory == "" {
		directory = "files"
	}
	files, err = live.NewDiskFileStore(directory)
	if err != nil {
		return nil, nil, nil, err
	}
	return jobs, locators, files, nil
}
