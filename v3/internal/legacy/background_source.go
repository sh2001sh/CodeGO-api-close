package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
)

type sourceBackgroundJob struct {
	ID                      string    `json:"id"`
	UserID                  int64     `json:"user_id"`
	TokenID                 int64     `json:"token_id"`
	Model                   string    `json:"model"`
	Status                  string    `json:"status"`
	Stream                  bool      `json:"stream"`
	Native                  bool      `json:"native_background"`
	ChannelID               int64     `json:"channel_id"`
	RoutingCiphertext       string    `json:"routing_context_ciphertext"`
	FinalResponseCiphertext string    `json:"final_response_ciphertext"`
	ErrorCiphertext         string    `json:"error_ciphertext"`
	UpstreamID              string    `json:"upstream_response_id"`
	UpstreamSequence        int64     `json:"upstream_sequence"`
	LastSequence            int64     `json:"last_sequence"`
	CancelRequested         bool      `json:"cancel_requested"`
	Billed                  *bool     `json:"billed"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type sourceBackgroundEvent struct {
	ID                int64     `json:"id"`
	JobID             string    `json:"job_id"`
	Sequence          int64     `json:"sequence"`
	Type              string    `json:"type"`
	PayloadCiphertext string    `json:"payload_ciphertext"`
	CreatedAt         time.Time `json:"created_at"`
}

type backgroundAsset struct {
	job    live.BackgroundJob
	events []live.BackgroundEvent
}

var migratedBackgroundID = regexp.MustCompile(`^resp_bg_[A-Za-z0-9_-]+$`)

func backgroundSourceTable(sources map[string]string, name string) string {
	if table := sources["gateway_"+name]; table != "" {
		return table
	}
	return sources[name]
}

func loadSourceBackground(ctx context.Context, tx pgx.Tx, sources map[string]string, report *Report) ([]backgroundAsset, error) {
	jobTable := backgroundSourceTable(sources, "responses_background_jobs")
	eventTable := backgroundSourceTable(sources, "responses_background_events")
	jobs := map[string]sourceBackgroundJob{}
	if err := walkHistory(ctx, tx, jobTable, func(raw json.RawMessage) error {
		var job sourceBackgroundJob
		if err := json.Unmarshal(raw, &job); err != nil {
			return errors.New("legacy: invalid background job metadata")
		}
		if jobs[job.ID].ID != "" || !migratedBackgroundID.MatchString(job.ID) || len(job.ID) > 64 || job.UserID <= 0 || job.TokenID <= 0 || job.Model == "" ||
			job.CreatedAt.IsZero() || job.UpdatedAt.Before(job.CreatedAt) || job.ChannelID < 0 || job.UpstreamSequence < -1 || job.LastSequence < -1 {
			return errors.New("legacy: invalid background identity, dates or cursor")
		}
		if job.Status != "completed" && job.Status != "failed" && job.Status != "cancelled" {
			return errors.New("legacy: finish or cancel pending background work before migration")
		}
		if job.Billed != nil && !*job.Billed {
			return errors.New("legacy: unsettled background job cannot be imported")
		}
		jobs[job.ID] = job
		return nil
	}); err != nil {
		return nil, err
	}
	grouped := map[string][]sourceBackgroundEvent{}
	eventIDs := map[int64]bool{}
	if err := walkHistory(ctx, tx, eventTable, func(raw json.RawMessage) error {
		var event sourceBackgroundEvent
		if json.Unmarshal(raw, &event) != nil || event.ID <= 0 || eventIDs[event.ID] || jobs[event.JobID].ID == "" || event.Sequence < 0 ||
			event.Type == "" || strings.ContainsAny(event.Type, "\r\n") || event.CreatedAt.IsZero() {
			return errors.New("legacy: invalid background event identity or owner")
		}
		eventIDs[event.ID] = true
		grouped[event.JobID] = append(grouped[event.JobID], event)
		return nil
	}); err != nil {
		return nil, err
	}
	report.Counts["responses_background_jobs"] = int64(len(jobs))
	report.Counts["responses_background_events"] = int64(len(eventIDs))
	if len(jobs) == 0 {
		return nil, nil
	}
	users, keys, channels, err := backgroundSourceOwners(ctx, tx, sources)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(jobs))
	for id := range jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	assets := make([]backgroundAsset, 0, len(jobs))
	for _, id := range ids {
		source := jobs[id]
		if !users[source.UserID] || keys[source.TokenID] != source.UserID || (source.ChannelID > 0 && !channels[source.ChannelID]) {
			return nil, errors.New("legacy: background owner, API key or channel is absent or inconsistent")
		}
		asset, err := projectBackground(source, grouped[id], os.Getenv("V3_MIGRATION_SOURCE_CRYPTO_SECRET"))
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func backgroundSourceOwners(ctx context.Context, tx pgx.Tx, sources map[string]string) (map[int64]bool, map[int64]int64, map[int64]bool, error) {
	users, channels := map[int64]bool{}, map[int64]bool{}
	keys := map[int64]int64{}
	for _, name := range []string{"users", "tokens", "channels"} {
		err := walkHistory(ctx, tx, sources[name], func(raw json.RawMessage) error {
			var row struct {
				ID     int64 `json:"id"`
				UserID int64 `json:"user_id"`
			}
			if json.Unmarshal(raw, &row) != nil || row.ID <= 0 {
				return errors.New("legacy: invalid background authority metadata")
			}
			switch name {
			case "users":
				users[row.ID] = true
			case "tokens":
				keys[row.ID] = row.UserID
			case "channels":
				channels[row.ID] = true
			}
			return nil
		})
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return users, keys, channels, nil
}
