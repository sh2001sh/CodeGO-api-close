package main

import (
	"context"
	"testing"

	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
)

func TestBlindBoxPromotionIsNotScheduledAndPersistedJobIssuesNothing(t *testing.T) {
	actions, periods := maintenance(&boot.Deps{}, &services{}, workerConfig{})
	if _, scheduled := periods["blind_box_contributions"]; scheduled {
		t.Fatal("retired promotion still scheduled")
	}
	run, exists := actions["blind_box_contributions"]
	if !exists || run(context.Background()) != nil {
		t.Fatal("old persisted job must drain without issuing inventory")
	}
}
