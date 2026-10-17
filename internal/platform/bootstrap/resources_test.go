package bootstrap

import "testing"

func TestCommerceBootstrapStartupPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		master  bool
		skip    string
		wantRun bool
		wantErr bool
	}{
		{name: "master retains existing default", master: true, wantRun: true},
		{name: "slave retains existing default"},
		{name: "master skips startup writes", master: true, skip: "true"},
		{name: "slave skips startup writes", skip: "true"},
		{name: "explicit false retains master writes", master: true, skip: "false", wantRun: true},
		{name: "explicit false does not elevate slave", skip: "false"},
		{name: "boolean flag accepts surrounding whitespace", master: true, skip: " TRUE "},
		{name: "invalid master flag fails closed", master: true, skip: "tru", wantErr: true},
		{name: "invalid slave flag also rejects rollout", skip: "tru", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, err := shouldRunCommerceBootstrap(tc.master, tc.skip)
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected error state: %v", err)
			}
			if run != tc.wantRun {
				t.Fatalf("startup writers enabled=%v, want %v", run, tc.wantRun)
			}
		})
	}
}

func TestAuditSchemaStartupHonorsMigrationOptOut(t *testing.T) {
	for _, tc := range []struct {
		name      string
		master    bool
		migration string
		wantRun   bool
	}{
		{name: "master retains unset default", master: true, wantRun: true},
		{name: "slave retains unset default"},
		{name: "master skips DDL with explicit false", master: true, migration: "false"},
		{name: "slave skips DDL with explicit false", migration: "false"},
		{name: "explicit true permits master DDL", master: true, migration: "true", wantRun: true},
		{name: "explicit true does not elevate slave", migration: "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AUTO_DB_MIGRATION", tc.migration)
			if run := shouldRunAuditSchemaBootstrap(tc.master); run != tc.wantRun {
				t.Fatalf("startup audit DDL enabled=%v, want %v", run, tc.wantRun)
			}
		})
	}
}
