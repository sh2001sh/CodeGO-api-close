package legacy

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
	"github.com/tidwall/gjson"
)

func projectBackground(source sourceBackgroundJob, events []sourceBackgroundEvent, secret string) (backgroundAsset, error) {
	job := live.BackgroundJob{ID: source.ID, UserID: source.UserID, KeyID: source.TokenID, Model: source.Model, ChannelID: source.ChannelID,
		Status: source.Status, Stream: source.Stream, Native: source.Native, CancelRequested: source.CancelRequested, Billed: true,
		UpstreamID: source.UpstreamID, LastUpstreamSequence: source.UpstreamSequence, CreatedAt: source.CreatedAt.UTC(), UpdatedAt: source.UpdatedAt.UTC()}
	// Terminal GET/replay/cancel never resolves an upstream credential. Old
	// positional key indexes must not be guessed into native credential IDs.
	if source.RoutingCiphertext != "" {
		plain, err := cmSecret(source.RoutingCiphertext, secret)
		if err != nil {
			return backgroundAsset{}, errors.New("legacy: background routing decryption failed")
		}
		var route struct {
			UsingGroup string `json:"using_group"`
		}
		if !gjson.Valid(plain) || !gjson.Parse(plain).IsObject() || json.Unmarshal([]byte(plain), &route) != nil {
			return backgroundAsset{}, errors.New("legacy: invalid background routing context")
		}
		job.Group = route.UsingGroup
	}
	final, err := cmSecret(source.FinalResponseCiphertext, secret)
	if err != nil {
		return backgroundAsset{}, errors.New("legacy: background final response decryption failed")
	}
	errorJSON, err := cmSecret(source.ErrorCiphertext, secret)
	if err != nil {
		return backgroundAsset{}, errors.New("legacy: background error decryption failed")
	}
	if errorJSON != "" && !json.Valid([]byte(errorJSON)) {
		return backgroundAsset{}, errors.New("legacy: invalid background error JSON")
	}
	if final == "" {
		fallback := map[string]any{"id": job.ID, "object": "response", "model": job.Model, "status": job.Status,
			"created_at": job.CreatedAt.Unix(), "background": true, "output": []any{}, "error": nil}
		if errorJSON != "" {
			fallback["error"] = json.RawMessage(errorJSON)
		}
		job.Snapshot, err = json.Marshal(fallback)
		if err != nil {
			return backgroundAsset{}, err
		}
	} else {
		snapshot := gjson.Parse(final)
		if !gjson.Valid(final) || !snapshot.IsObject() || snapshot.Get("id").Str != job.ID || snapshot.Get("status").Str != job.Status {
			return backgroundAsset{}, errors.New("legacy: background result identity or terminal status differs")
		}
		job.Snapshot = json.RawMessage(final)
		if errorJSON != "" && snapshot.Get("error").Exists() && snapshot.Get("error").Type != gjson.Null && !sameBackgroundError(snapshot.Get("error").Raw, errorJSON) {
			return backgroundAsset{}, errors.New("legacy: background final and standalone error facts disagree")
		}
		if errorJSON != "" && (!snapshot.Get("error").Exists() || snapshot.Get("error").Type == gjson.Null) {
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(job.Snapshot, &fields); err != nil {
				return backgroundAsset{}, errors.New("legacy: invalid background response JSON")
			}
			fields["error"] = json.RawMessage(errorJSON)
			job.Snapshot, err = json.Marshal(fields)
			if err != nil {
				return backgroundAsset{}, err
			}
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	asset := backgroundAsset{job: job, events: make([]live.BackgroundEvent, 0, len(events))}
	if source.LastSequence != int64(len(events))-1 {
		return backgroundAsset{}, errors.New("legacy: background event count differs from saved cursor")
	}
	for i, event := range events {
		payload, err := cmSecret(event.PayloadCiphertext, secret)
		if err != nil {
			return backgroundAsset{}, errors.New("legacy: background event payload decryption failed")
		}
		parsed := gjson.Parse(payload)
		if event.Sequence != int64(i) || !gjson.Valid(payload) || !parsed.IsObject() || parsed.Get("type").Str != event.Type ||
			parsed.Get("sequence_number").Raw != strconv.FormatInt(event.Sequence, 10) {
			return backgroundAsset{}, errors.New("legacy: background events have gaps or invalid payload metadata")
		}
		if id := parsed.Get("response.id"); id.Exists() && id.Str != job.ID {
			return backgroundAsset{}, errors.New("legacy: background event references another response")
		}
		asset.events = append(asset.events, live.BackgroundEvent{Sequence: event.Sequence, Type: event.Type, Payload: []byte(payload)})
	}
	return asset, nil
}

func sameBackgroundError(left, right string) bool {
	decode := func(raw string) (any, error) {
		var value any
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		err := decoder.Decode(&value)
		return value, err
	}
	l, leftErr := decode(left)
	r, rightErr := decode(right)
	return leftErr == nil && rightErr == nil && reflect.DeepEqual(l, r)
}
