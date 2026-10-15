package http

import (
	"strings"
	"testing"
)

func TestMergeIndexShellRefreshesSEOFields(t *testing.T) {
	built := []byte(`<!doctype html><html lang="en"><head><title>Code Go</title><meta name="title" content="Code Go" /><meta name="description" content="old desc" /></head><body><div id="root"></div></body></html>`)
	source := []byte(`<!doctype html><html lang="zh-CN"><head><title>Code Go | Codex API、Claude Code API、Codex 中转、Claude 中转</title><meta name="title" content="Code Go | Codex API、Claude Code API、Codex 中转、Claude 中转" /><meta name="description" content="new desc" /><meta name="keywords" content="Codex API, Claude Code API" /></head><body><h1>Code Go：Codex API、Claude Code API、Codex 中转与 Claude 中转统一入口</h1><div id="root"></div></body></html>`)

	merged := string(MergeIndexShell(built, source))

	checks := []string{
		`<html lang="zh-CN">`,
		`<title>Code Go | Codex API、Claude Code API、Codex 中转、Claude 中转</title>`,
		`meta name="description" content="new desc"`,
		`meta name="keywords" content="Codex API, Claude Code API"`,
		`<h1>Code Go：Codex API、Claude Code API、Codex 中转与 Claude 中转统一入口</h1>`,
	}

	for _, want := range checks {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged shell missing %q: %s", want, merged)
		}
	}
}
