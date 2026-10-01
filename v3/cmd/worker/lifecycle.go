package main

import (
	"context"
	"fmt"
)

// A stopped critical loop must cancel its siblings even when it returned nil.
// Otherwise the process keeps advertising metrics while no longer doing work.
func runService(ctx context.Context, name string, run func(context.Context) error) error {
	err := run(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("%s stopped unexpectedly", name)
}
