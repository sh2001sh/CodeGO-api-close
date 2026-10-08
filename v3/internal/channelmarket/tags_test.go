package channelmarket

import (
	"encoding/json"
	"testing"
)

func TestTagsCuratedDedupedAndClearable(t *testing.T) {
	tags, err := normalizeGroupTags([]string{"openai", "openai", "anthropic", "google", "deepseek", "qwen", "openai"})
	if err != nil || len(tags) != 5 {
		t.Fatalf("distinct tags %v %v", tags, err)
	}
	for _, values := range [][]string{{"openai", "anthropic", "google", "deepseek", "qwen", "xai"}, {"coding"}, {"wechat-contact"}, {"OpenAI"}, {""}} {
		if _, err := normalizeGroupTags(values); err == nil {
			t.Fatalf("accepted prohibited tags %v", values)
		}
	}
	data, err := safeSettings(CreateRequest{Tags: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]json.RawMessage
	if err = json.Unmarshal(data, &settings); err != nil || string(settings["tags"]) != "[]" {
		t.Fatalf("clear tags lost from merge payload: %s %v", data, err)
	}
	if updateChangesConfig(map[string]json.RawMessage{"tags": json.RawMessage(`["openai"]`)}, false) {
		t.Fatal("tags unnecessarily require re-verification")
	}
	visible := visibleProviderTags([]string{"coding", "translation", "openai", "openai", "google", "advertisement"})
	if len(visible) != 2 || visible[0] != "openai" || visible[1] != "google" {
		t.Fatalf("obsolete tags mapped or leaked: %v", visible)
	}
}

func TestPendingNameFieldsAreOwnerOnly(t *testing.T) {
	for _, actor := range []Actor{{}, {UserID: 2}} {
		c := ChannelView{OwnerUserID: 1, ChannelPolicy: ChannelPolicy{SubmittedName: "Candidate", NameStatus: "pending", NameReviewReason: "private review"}}
		hideNameReview(&c, actor)
		if c.SubmittedName != "" || c.NameStatus != "" || c.NameReviewReason != "" {
			t.Fatalf("pending name leaked to %+v", actor)
		}
	}
	for _, actor := range []Actor{{UserID: 1}, {UserID: 3, Admin: true}} {
		c := ChannelView{OwnerUserID: 1, ChannelPolicy: ChannelPolicy{SubmittedName: "Candidate", NameStatus: "pending"}}
		hideNameReview(&c, actor)
		if c.SubmittedName == "" {
			t.Fatalf("owner review missing for %+v", actor)
		}
	}
}
