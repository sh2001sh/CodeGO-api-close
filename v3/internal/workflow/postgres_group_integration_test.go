//go:build pgintegration

package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/suno"
	"github.com/sh2001sh/new-api/v3/migrations"
)

type taskGroupAuth string

func (g taskGroupAuth) Authorize(ctx context.Context, key string) (gateway.Principal, error) {
	principal, err := (apiAuth{}).Authorize(ctx, key)
	principal.Group = string(g)
	return principal, err
}

func taskGroupRepository(t *testing.T) *workflow.PostgresRepository {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error(err)
		}
	})
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT to_regclass('v3_workflow.tasks') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		sql, err := migrations.Read("20260930193000_workflow.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	return &workflow.PostgresRepository{Pool: tx}
}

func TestPostgresSelectedGroupSurvivesReopenPollFailureAndContent(t *testing.T) {
	for _, principalGroup := range []string{"auto", "monthly-pass", "zero-hour"} {
		t.Run(principalGroup, func(t *testing.T) {
			repo := taskGroupRepository(t)
			var upstream *httptest.Server
			polls, downloads, selections := 0, 0, 0
			upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && r.Header.Get("X-Selected") != "routing-group" {
					t.Error("using_group condition lost during native submit/poll")
				}
				switch r.URL.Path {
				case "/suno/submit/MUSIC":
					_, _ = io.WriteString(w, `{"code":"success","data":"native-group-task"}`)
				case "/suno/fetch":
					polls++
					if polls == 1 {
						w.WriteHeader(503)
						_, _ = io.WriteString(w, `{"error":"retry later"}`)
						return
					}
					_, _ = io.WriteString(w, `{"code":"success","data":[{"task_id":"native-group-task","status":"SUCCESS","data":[{"audio_url":"`+upstream.URL+`/content","metadata":{"duration":4}}]}]}`)
				case "/content":
					downloads++
					_, _ = io.WriteString(w, "group-content")
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer upstream.Close()
			target := gateway.Target{ChannelID: 4, CredentialID: 9, Provider: "suno", BaseURL: upstream.URL, Secret: "fixture", UpstreamModel: "suno_music", Group: "routing-group"}
			if err := json.Unmarshal([]byte(`{"operations":[{"mode":"set_header","path":"X-Selected","value":"routing-group","conditions":{"using_group":"routing-group"}}]}`), &target.ParamOverride); err != nil {
				t.Fatal(err)
			}
			var billing *channelPolicySettler
			f := newAPIFixture(t, func(c *workflow.Config) {
				c.Authorizer, c.Planner, c.Repository = taskGroupAuth(principalGroup), apiPlanner{target}, repo
				c.Providers = map[string]native.Adapter{"suno": suno.New(upstream.Client())}
				c.ResolveTarget = func(context.Context, int64, int64) (gateway.Target, error) {
					restored := target
					restored.Group = "changed-channel-group"
					return restored, nil
				}
				c.Clients = func(_ context.Context, selected gateway.Target) (*http.Client, error) {
					selections++
					if selected.Group != "routing-group" {
						t.Error("selected group replaced by principal/current catalog group")
					}
					return upstream.Client(), nil
				}
				billing = &channelPolicySettler{inner: c.Settler, t: t}
				c.Settler = billing
			})
			w := f.request("POST", "/suno/submit/music", "owner", `{"prompt":"original"}`, "")
			if w.Code != 200 {
				t.Fatalf("submit %d %s", w.Code, w.Body.String())
			}
			id := w.Header().Get("X-Task-Id")
			reopened := &workflow.PostgresRepository{Pool: repo.Pool}
			stored, err := reopened.GetOwned(context.Background(), id, 11)
			if err != nil || stored.TargetGroup != "routing-group" || stored.Group != principalGroup {
				t.Fatalf("selected group persistence %+v %v", stored, err)
			}
			if n, err := f.handler.Reconcile(context.Background(), 10); n != 0 || err == nil {
				t.Fatalf("failed poll was not reported %d %v", n, err)
			}
			stored, err = reopened.GetOwned(context.Background(), id, 11)
			if err != nil || stored.TargetGroup != "routing-group" || stored.CostState != "reserved" || stored.LeaseID != "" {
				t.Fatalf("poll failure lost selected group/hold %+v %v", stored, err)
			}
			if n, err := f.handler.Reconcile(context.Background(), 10); n != 1 || err != nil {
				t.Fatalf("reopened poll %d %v", n, err)
			}
			w = f.request("GET", "/v1/videos/"+id+"/content", "owner", "", "")
			if w.Code != 200 || w.Body.String() != "group-content" || polls != 2 || downloads != 1 || selections != 4 || billing.finalizations != 1 {
				t.Fatalf("content %d %s polls=%d downloads=%d clients=%d billing=%d", w.Code, w.Body.String(), polls, downloads, selections, billing.finalizations)
			}
		})
	}
}
