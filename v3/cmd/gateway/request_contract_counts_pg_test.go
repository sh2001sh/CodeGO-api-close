//go:build pgintegration

package main

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

type contractContextKey struct{}

const contractBackground = "background"

// All calls made during a serialized request are counted, including detached
// lease release and billing finalize contexts. Only buildProd's explicitly
// marked background loops are excluded, never unmarked contexts.
type contractCounts struct {
	mu       sync.Mutex
	active   bool
	postgres []string
	redis    []string
	detached int
}

type contractResult struct {
	postgres, redis []string
	detached        int
}

func (c *contractCounts) start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active, c.postgres, c.redis, c.detached = true, nil, nil, 0
}

func (c *contractCounts) stop() contractResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active = false
	return contractResult{append([]string(nil), c.postgres...), append([]string(nil), c.redis...), c.detached}
}

func (c *contractCounts) record(ctx context.Context, command string, postgres bool) {
	if ctx.Value(contractContextKey{}) == contractBackground {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return
	}
	if postgres {
		c.postgres = append(c.postgres, command)
	} else {
		c.redis = append(c.redis, command)
		if ctx.Value(contractContextKey{}) == nil {
			c.detached++
		}
	}
}

func (c *contractCounts) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.record(ctx, data.SQL, true)
	return ctx
}

func (*contractCounts) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (c *contractCounts) TraceBatchStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	for _, query := range data.Batch.QueuedQueries {
		c.record(ctx, query.SQL, true)
	}
	return ctx
}

func (*contractCounts) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {}
func (*contractCounts) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData)     {}

func (*contractCounts) DialHook(next redis.DialHook) redis.DialHook { return next }

func (c *contractCounts) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		c.record(ctx, cmd.Name(), false)
		return next(ctx, cmd)
	}
}

func (c *contractCounts) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		// A pipeline is one socket round trip, even when it contains many commands.
		command := "pipeline"
		for _, cmd := range cmds {
			command += " " + cmd.Name()
		}
		c.record(ctx, command, false)
		return next(ctx, cmds)
	}
}

var _ pgx.QueryTracer = (*contractCounts)(nil)
var _ pgx.BatchTracer = (*contractCounts)(nil)
var _ redis.Hook = (*contractCounts)(nil)
