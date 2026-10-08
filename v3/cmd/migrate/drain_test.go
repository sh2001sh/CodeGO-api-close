package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/legacy"
)

func TestDrainCLIIsSourceOnlyAndErrorsStillHaveBindingReport(t *testing.T) {
	t.Setenv("V3_PG_DSN", "invalid-target-must-never-be-used")
	t.Setenv("V3_SECRET_KEY", "invalid-crypto-must-never-be-used")
	t.Setenv("V3_SOURCE_PG_DSN", "")
	t.Setenv("V3_DRAIN_SOURCE_SNAPSHOT", "")
	t.Setenv("V3_DRAIN_FROZEN_MANIFEST_SHA256", "")
	var output bytes.Buffer
	err := run(context.Background(), []string{"drain"}, &output)
	var report legacy.SourceDrainReport
	if err == nil || json.Unmarshal(output.Bytes(), &report) != nil || report.Protocol != legacy.SourceDrainProtocol || report.Completed || report.ErrorCode != "source_database_required" || len(report.BinarySHA256) != 64 {
		t.Fatalf("source-only CLI missing complete failure envelope: %s %v", output.String(), err)
	}
	for _, args := range [][]string{{"drain", "-apply"}, {"drain", "-apply", "-offline"}, {"drain", "-offline"}, {"drain", "-skip"}} {
		if _, _, err := parseMigrateArgs(args); err == nil {
			t.Fatalf("mutable/skip drain option accepted: %v", args)
		}
		output.Reset()
		if err := run(context.Background(), args, &output); err == nil || json.Unmarshal(output.Bytes(), &report) != nil || report.Completed || report.ErrorCode != "invalid_drain_arguments" {
			t.Fatalf("invalid arguments lack incomplete report: %v %s %v", args, output.String(), err)
		}
	}
	t.Setenv("V3_DRAIN_SOURCE_SNAPSHOT", "valid'; UPDATE users SET role=100;--")
	output.Reset()
	err = run(context.Background(), []string{"drain"}, &output)
	if err == nil || json.Unmarshal(output.Bytes(), &report) != nil || report.Completed || report.ErrorCode != "invalid_exported_snapshot" {
		t.Fatalf("snapshot SQL injection accepted or envelope missing: %s %v", output.String(), err)
	}
	t.Setenv("V3_DRAIN_SOURCE_SNAPSHOT", "")
	t.Setenv("V3_DRAIN_FROZEN_MANIFEST_SHA256", strings.Repeat("x", 64))
	output.Reset()
	err = run(context.Background(), []string{"drain"}, &output)
	if err == nil || json.Unmarshal(output.Bytes(), &report) != nil || report.Completed || report.ErrorCode != "invalid_frozen_manifest_sha256" {
		t.Fatalf("invalid manifest digest accepted: %s %v", output.String(), err)
	}
}
