package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
)

// Loops outlive the signal context until HTTP requests have drained. Any early
// loop exit permanently refuses new admission; cleanup joins all loops before
// their PostgreSQL/Redis clients and billing WAL are closed.
type prodRuntime struct {
	ctx            context.Context
	cancel         context.CancelFunc
	requests       context.Context
	cancelRequests context.CancelFunc
	failed         atomic.Bool
	ready          atomic.Bool
	wg             sync.WaitGroup
	active         sync.WaitGroup
	admission      sync.Mutex
	closing        bool
	once           sync.Once
	close          []func()
	log            *slog.Logger
}

func newProdRuntime(parent context.Context, log *slog.Logger) *prodRuntime {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	requests, cancelRequests := context.WithCancel(context.WithoutCancel(parent))
	return &prodRuntime{ctx: ctx, cancel: cancel, requests: requests, cancelRequests: cancelRequests, log: log}
}

func (p *prodRuntime) start(name string, run func(context.Context) error) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		err := run(p.ctx)
		if p.ctx.Err() == nil {
			p.failed.Store(true)
			p.log.Error("background loop exited", "loop", name, "err", err)
			p.cancel()
		}
	}()
}

func (p *prodRuntime) Close() {
	p.once.Do(func() {
		p.admission.Lock()
		p.closing = true
		p.ready.Store(false)
		p.admission.Unlock()
		p.cancelRequests() // includes hijacked WebSocket handlers, which Server.Shutdown skips
		p.active.Wait()
		p.cancel()
		p.wg.Wait()
		for i := len(p.close) - 1; i >= 0; i-- {
			p.close[i]()
		}
	})
}

func (p *prodRuntime) healthy() bool {
	return p.ready.Load() && !p.failed.Load() && p.ctx.Err() == nil
}

func (p *prodRuntime) Wrap(parent context.Context, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.admission.Lock()
		if p.closing || (r.URL.Path != "/metrics" && (!p.healthy() || parent.Err() != nil)) {
			p.admission.Unlock()
			http.Error(w, "gateway is not ready", http.StatusServiceUnavailable)
			return
		}
		p.active.Add(1)
		p.admission.Unlock()
		defer p.active.Done()
		ctx, cancel := context.WithCancel(r.Context())
		stop := context.AfterFunc(p.requests, cancel)
		defer func() { stop(); cancel() }()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
