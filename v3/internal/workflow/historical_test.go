package workflow_test

import (
	"strings"
	"testing"
)

func TestHistoricalWorkflowQueryIsOwnedAndDoesNotDispatch(t *testing.T) {
	f := newAPIFixture(t)
	task := ownedTask("historic-video", "openai_video")
	task.Historical, task.Status, task.CostState = true, "completed", "settled"
	task.URL = "https://example.invalid/historic.mp4"
	f.repo.seed(task)
	w := f.request("GET", "/v1/videos/historic-video", "owner", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), task.URL) || strings.Contains(w.Body.String(), "/content") {
		t.Fatalf("historical result=%d %s", w.Code, w.Body.String())
	}
	w = f.request("GET", "/v1/videos/historic-video/content", "owner", "", "")
	if w.Code != 410 || !strings.Contains(w.Body.String(), "historical_content_use_result_url") {
		t.Fatalf("historical content=%d %s", w.Code, w.Body.String())
	}
	w = f.request("GET", "/v1/videos/historic-video", "other", "", "")
	if w.Code != 404 {
		t.Fatalf("historical owner bypass=%d %s", w.Code, w.Body.String())
	}
	if f.provider.submits.Load()+f.provider.polls.Load()+f.provider.contents.Load() != 0 || len(f.settler.reserved) != 0 || len(f.settler.calls) != 0 {
		t.Fatal("historical query dispatched or touched funds")
	}
}
