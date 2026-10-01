package anthropic

import "testing"

func TestServerToolUsageSnapshotsDoNotAccumulateTwice(t *testing.T) {
	input, output, search, fetch := int64(12), int64(4), int64(2), int64(1)
	w := &wireUsage{Input: &input, Output: &output}
	w.ServerTools.WebSearch, w.ServerTools.WebFetch = &search, &fetch
	u := &usageState{}
	for range 2 {
		usage, err := u.merge(w)
		if err != nil || usage.ToolCalls["web_search"] != 2 || usage.ToolCalls["web_fetch"] != 1 {
			t.Fatalf("usage=%+v err=%v", usage, err)
		}
	}
}
