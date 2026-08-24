package securityaudit

import (
	"errors"
	"fmt"
)

func allowDecision(result *GuardResult) Decision {
	return Decision{Kind: DecisionAllow, AllowNextStage: true, Result: result}
}

func guardErrorCode(err error) string {
	var guardErr *GuardError
	if errors.As(err, &guardErr) && guardErr.Code != "" {
		return guardErr.Code
	}
	return ErrorCodeUnavailable
}

func mergeResult(target, source *GuardResult) {
	if target == nil || source == nil {
		return
	}
	target.Safety = source.Safety
	target.Endpoint = source.Endpoint
	target.Categories = uniqueSorted(append(target.Categories, source.Categories...))
	target.MatchedScanners = uniqueSorted(append(target.MatchedScanners, source.MatchedScanners...))
	target.Unknown = uniqueSorted(append(target.Unknown, source.Unknown...))
	if source.Action == "block" || source.Action == "warn" && target.Action == "allow" {
		target.Action = source.Action
	}
}

func splitRunes(value string, limit int) []string {
	runes := []rune(value)
	if len(runes) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = defaultInputLimit
	}
	result := make([]string, 0, (len(runes)+limit-1)/limit)
	for start := 0; start < len(runes); start += limit {
		end := start + limit
		if end > len(runes) {
			end = len(runes)
		}
		result = append(result, string(runes[start:end]))
	}
	return result
}

func formatAuditLog(snapshot Snapshot, decision Decision) string {
	result := decision.Result
	if result == nil {
		return fmt.Sprintf("prompt audit decision=%s request_id=%s hash=%s", decision.Kind, snapshot.RequestID, snapshot.PromptHash)
	}
	return fmt.Sprintf(
		"prompt audit decision=%s request_id=%s group=%s protocol=%s model=%s hash=%s chars=%d categories=%v endpoint=%s latency_ms=%d",
		decision.Kind, snapshot.RequestID, snapshot.Group, snapshot.Protocol, snapshot.Model, snapshot.PromptHash,
		snapshot.PromptLength, result.Categories, result.Endpoint, result.Latency.Milliseconds(),
	)
}
