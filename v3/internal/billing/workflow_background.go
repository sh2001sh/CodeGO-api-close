package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
)

// BackgroundSettler uses the same durable frozen hold as native workflows.
// Construct it before serving requests or starting any worker. Supplying the
// repository enables verified orphan cleanup; without it the sweeper retains
// background holds whenever task existence cannot be established.
type BackgroundSettler struct{ workflow *WorkflowSettler }

func NewBackgroundSettler(settler *Settler, repositories ...live.BackgroundJobRepository) *BackgroundSettler {
	if len(repositories) > 0 && repositories[0] != nil {
		settler.loader = &backgroundTaskLoader{BalanceLoader: settler.loader, repository: repositories[0]}
	}
	return &BackgroundSettler{workflow: &WorkflowSettler{settler: settler, kind: "background"}}
}

var _ live.BackgroundBilling = (*BackgroundSettler)(nil)

func (b *BackgroundSettler) Reserve(ctx context.Context, req *gateway.Request) (json.RawMessage, error) {
	if req == nil || len(req.Targets) == 0 {
		return nil, errors.New("billing: background task has no target")
	}
	copyReq := *req
	copyReq.Targets = req.Targets[:1] // the live job durably stores its first selected route
	reservation, err := b.workflow.Reserve(ctx, &copyReq)
	if err != nil {
		return nil, err
	}
	req.Reserve = copyReq.Reserve
	return json.Marshal(reservation)
}

func backgroundHold(req *gateway.Request, data json.RawMessage) (*gateway.Request, workflow.Reservation, *hold, error) {
	var reservation workflow.Reservation
	if err := json.Unmarshal(data, &reservation); err != nil {
		return nil, reservation, nil, err
	}
	if req == nil {
		return nil, reservation, nil, errors.New("billing: missing background request")
	}
	copyReq := *req
	if len(req.Targets) == 0 {
		var snapshot workflowSnapshot
		if err := json.Unmarshal(reservation.Data, &snapshot); err != nil {
			return nil, reservation, nil, err
		}
		copyReq.Targets = []gateway.Target{{ChannelID: snapshot.ChannelID, CredentialID: snapshot.CredentialID}}
	} else {
		copyReq.Targets = req.Targets[:1]
	}
	h, err := restoreWorkflowHold(&copyReq, reservation)
	copyReq.Reserve = h
	return &copyReq, reservation, h, err
}

func (b *BackgroundSettler) Refresh(ctx context.Context, req *gateway.Request, data json.RawMessage) error {
	copyReq, _, h, err := backgroundHold(req, data)
	if err != nil {
		return err
	}
	if _, found, err := b.workflow.committedActual(ctx, h, "completed"); found || err != nil {
		return err
	}
	if err := b.workflow.persistHold(ctx, copyReq, h); err != nil {
		return err
	}
	req.Reserve = h
	return nil
}

func (b *BackgroundSettler) Finalize(ctx context.Context, req *gateway.Request, data json.RawMessage, out gateway.Outcome) error {
	copyReq, _, h, err := backgroundHold(req, data)
	if err != nil {
		return err
	}
	status := "completed"
	if !out.Charge {
		status = "failed"
	}
	if _, found, err := b.workflow.committedActual(ctx, h, status); found {
		return err
	}
	if out.Target == nil {
		out.Target = &copyReq.Targets[0]
	}
	if out.Target.ChannelID != copyReq.Targets[0].ChannelID || out.Target.CredentialID != copyReq.Targets[0].CredentialID {
		return errors.New("billing: background settlement target mismatch")
	}
	target, err := frozenWorkflowTarget(h, *out.Target)
	if err != nil {
		return err
	}
	out.Target = &target
	if err := b.workflow.taskSettler(false).Finalize(ctx, copyReq, out); err != nil {
		return err
	}
	if _, found, err := b.workflow.committedActual(ctx, h, status); found || err != nil {
		return err
	}
	return fmt.Errorf("%w: background settlement pending WAL replay", gateway.ErrBillingUnavailable)
}
