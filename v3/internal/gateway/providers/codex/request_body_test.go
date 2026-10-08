package codex

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestCodexBodyPreservesRawValuesAndLastDuplicate(t *testing.T) {
	input := `[ { "role": "user", "content": [{"type":"input_text","text":"<hello>\\n"},{"type":"input_image","image_url":"data:image/png;base64,AQ=="}]}, {"type":"function_call_output","call_id":"x","output":"ok"} ]`
	body := []byte(`{"model":"first","model":"last","input":` + input + `,"store":true,"\u0073tore":true,"instructions":"old","instructions":null,"max_tokens":3,"max_tokens":4,"custom":{"id":9007199254740993,"amount":1.2300e+5}}`)
	original := bytes.Clone(body)
	got, err := codexRequestBody(body, "first", "mapped")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, original) || !gjson.ValidBytes(got) || gjson.GetBytes(got, "input").Raw != input {
		t.Fatal("modified original or re-encoded nested input")
	}
	if gjson.GetBytes(got, "model").Str != "mapped" || gjson.GetBytes(got, "store").Bool() || gjson.GetBytes(got, "instructions").Str != "" || gjson.GetBytes(got, "max_tokens").Exists() {
		t.Fatalf("Codex policy mismatch: %s", got)
	}
	if gjson.GetBytes(got, "custom").Raw != `{"id":9007199254740993,"amount":1.2300e+5}` || strings.Count(string(got), `"store":`) != 1 {
		t.Fatal("lost precision or retained duplicate policy field")
	}
}

func TestCodexBodyRejectsMalformedAndPreservesStringInput(t *testing.T) {
	for _, body := range []string{`[]`, `null`, `{}`, `{"input":"hi"} {}`, `{"input":`, `{"input":[1,]}`} {
		_, err := codexRequestBody([]byte(body), "", "")
		if body == `{}` {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil {
			t.Fatalf("invalid object accepted: %s", body)
		}
	}
	got, err := codexRequestBody([]byte(` { "input": "<hi>\u0061", "instructions": "Keep this", "store":false } `), "", "")
	if err != nil || gjson.GetBytes(got, "input").Raw != `"<hi>\u0061"` || gjson.GetBytes(got, "instructions").Str != "Keep this" {
		t.Fatalf("string input/instructions changed: %s %v", got, err)
	}
}
