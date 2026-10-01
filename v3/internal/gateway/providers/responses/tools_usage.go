package responses

import (
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type toolMeter struct {
	seen          map[string]bool
	counts        map[string]int64
	searchPreview bool
}

func newToolMeter(req *gateway.Request) toolMeter {
	m := toolMeter{}
	for _, tool := range gjson.GetBytes(req.Body, "tools").Array() {
		if tool.Get("type").Str == "web_search_preview" {
			m.searchPreview = true
		}
	}
	return m
}

// Added/done events and the final response all describe the same invocation;
// stable item IDs prevent their snapshots from charging the tool repeatedly.
func (m *toolMeter) apply(root gjson.Result, u *gateway.Usage) *gateway.Usage {
	if item := root.Get("item"); item.IsObject() {
		m.observe(item, root.Get("output_index").Int())
	}
	output := root.Get("output")
	if response := root.Get("response"); response.IsObject() {
		output = response.Get("output")
	}
	for index, item := range output.Array() {
		m.observe(item, int64(index))
	}
	if len(m.counts) == 0 {
		return u
	}
	if u == nil {
		u = &gateway.Usage{Estimated: true}
	}
	u.ToolCalls = make(map[string]int64, len(m.counts))
	for name, count := range m.counts {
		u.ToolCalls[name] = count
	}
	return u
}

func (m *toolMeter) observe(item gjson.Result, index int64) {
	typ := item.Get("type").Str
	switch typ {
	case "web_search_call", "file_search_call", "computer_call", "code_interpreter_call", "image_generation_call", "mcp_call":
	default:
		return
	}
	id := item.Get("id").Str
	if id == "" {
		id = "#" + strconv.FormatInt(index, 10)
	}
	key := typ + ":" + id
	if m.seen[key] {
		return
	}
	if m.seen == nil {
		m.seen = make(map[string]bool)
		m.counts = make(map[string]int64)
	}
	m.seen[key] = true
	name := strings.TrimSuffix(typ, "_call")
	if name == "web_search" && m.searchPreview {
		name = "web_search_preview"
	}
	m.counts[name]++
}
