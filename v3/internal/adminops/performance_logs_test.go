package adminops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeSharedLogCleanupProtectsAllActiveMarkersWithoutCallback(t *testing.T) {
	for _, mode := range []string{"by_days", "by_count"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
			active := []string{"codego-control-11.active.log", "codego-gateway-22.active.log", "codego-worker-33.active.log"}
			names := append(append([]string{}, active...), "codego-worker-00.log", "oneapi-unknown-active.log", "unrelated.txt")
			for i, name := range names {
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, []byte(name), 0600); err != nil {
					t.Fatal(err)
				}
				stamp := now.Add(-time.Duration(3+i) * 24 * time.Hour)
				if err := os.Chtimes(path, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			s := New(nil, nil, Config{LogDir: dir, Now: func() time.Time { return now }}, nil)
			m := adminMux(s, "root")
			w := testHTTP(t, m, "DELETE", "/api/performance/logs?mode="+mode+"&value=1", "", 200)
			var result struct {
				Data LogCleanupResult `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Data.DeletedCount != 1 {
				t.Fatalf("cleanup result = %+v", result.Data)
			}
			if _, err := os.Stat(filepath.Join(dir, "codego-worker-00.log")); !os.IsNotExist(err) {
				t.Fatalf("finalized log not removed: %v", err)
			}
			for _, name := range append(active, "oneapi-unknown-active.log", "unrelated.txt") {
				if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
					t.Fatalf("protected file %s removed: %v", name, err)
				}
			}
		})
	}
}

func TestNativeActiveMarkerProtectionAppliesToEveryFilename(t *testing.T) {
	dir := t.TempDir()
	name := "arbitrary-service.active.log"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("active"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := removeFiles(dir, []LogFileInfo{{Name: name}}, "")
	if err != nil || result.DeletedCount != 0 {
		t.Fatalf("active marker cleanup: %+v %v", result, err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestPerformanceReportsConfiguredLoggingDestination(t *testing.T) {
	for _, configured := range []bool{false, true} {
		dir := ""
		want := "stderr"
		if configured {
			dir = t.TempDir()
			want = "stderr,file"
		}
		m := adminMux(New(nil, nil, Config{LogDir: dir}, nil), "root")
		var stats struct {
			Data struct {
				Output string `json:"log_output"`
			} `json:"data"`
		}
		w := testHTTP(t, m, "GET", "/api/performance/stats", "", 200)
		if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
			t.Fatal(err)
		}
		if stats.Data.Output != want {
			t.Fatalf("stats log_output=%q want %q", stats.Data.Output, want)
		}
		var logs struct {
			Data struct {
				Output  string `json:"output"`
				Enabled bool   `json:"enabled"`
			} `json:"data"`
		}
		w = testHTTP(t, m, "GET", "/api/performance/logs", "", 200)
		if err := json.Unmarshal(w.Body.Bytes(), &logs); err != nil {
			t.Fatal(err)
		}
		if logs.Data.Output != want || logs.Data.Enabled != configured {
			t.Fatalf("logs = %+v", logs.Data)
		}
		if !configured {
			testHTTP(t, m, "DELETE", "/api/performance/logs?mode=by_days&value=1", "", 409)
		}
	}
}
