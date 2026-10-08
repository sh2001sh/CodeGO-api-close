package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Service) serviceStatus(ctx context.Context) (map[string]any, error) {
	rows, err := s.pool.Query(ctx, `SELECT key,value FROM v3_platform.settings WHERE key IN ('Notice','Maintenance','DesktopMaintenance') AND NOT sensitive`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	notice := ""
	maintenance := false
	for rows.Next() {
		var key string
		var raw json.RawMessage
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var value string
		if key == "Notice" {
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
			notice = value
		} else {
			var enabled bool
			if json.Unmarshal(raw, &enabled) == nil {
				maintenance = maintenance || enabled
			} else if json.Unmarshal(raw, &value) == nil {
				switch strings.ToLower(strings.TrimSpace(value)) {
				case "1", "true", "yes", "on", "enabled":
					maintenance = true
				}
			}
		}
	}
	status := "ok"
	action := ""
	if notice != "" {
		status = "notice"
		action = "Review the latest service notice before retrying."
	}
	if maintenance {
		status = "maintenance"
		action = "Wait for maintenance to finish."
	}
	return map[string]any{"status": status, "notice": notice, "maintenance": maintenance, "recommended_action": action, "affected_scopes": []string{}}, rows.Err()
}
func (s *Service) statusHTTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.device(w, r, "account:read"); !ok {
		return
	}
	out, err := s.serviceStatus(r.Context())
	reply(w, out, err)
}
