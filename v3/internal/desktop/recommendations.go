package desktop

import (
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/identity"
)

func (s *Service) keyModels(r *http.Request, uid int64, k identity.KeyRecord) ([]string, error) {
	rows, err := s.pool.Query(r.Context(), availableModels+`SELECT DISTINCT model FROM available
	 WHERE (group_name=coalesce($2::text,(SELECT group_name FROM v3_identity.users WHERE id=$1))
	 OR $2='auto' AND group_name IN(SELECT v3_identity.auto_groups($1)))
	 AND (cardinality($3::text[]) IS NULL OR cardinality($3::text[])=0 OR model=ANY($3)) ORDER BY model`, uid, k.Group, k.AllowedModels)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func recommendedInput(tool string, models []string) ImportInput {
	pick := func(prefixes ...string) string {
		for _, prefix := range prefixes {
			for _, name := range models {
				if strings.HasPrefix(name, prefix) {
					return name
				}
			}
		}
		return ""
	}
	in := ImportInput{Tool: tool}
	switch tool {
	case "claude":
		in.Model = pick("claude-sonnet-4-5", "claude-sonnet", "claude-3-7-sonnet")
		in.HaikuModel = pick("claude-3-5-haiku", "claude-haiku")
		in.SonnetModel = pick("claude-sonnet-4-5", "claude-sonnet")
		in.OpusModel = pick("claude-opus-4", "claude-opus")
	case "gemini":
		in.Model = pick("gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.0")
	default:
		in.Model = pick("gpt-5.5", "gpt-5", "o3", "o4")
	}
	return in
}
