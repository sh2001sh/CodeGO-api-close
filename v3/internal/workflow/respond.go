package workflow

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, t Task, submit bool) {
	if strings.HasPrefix(r.URL.Path, "/suno/") {
		var data any = taskDTO(t)
		if submit {
			data = t.ID
		}
		writeJSON(w, http.StatusOK, map[string]any{"code": "success", "message": "", "data": data})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/kling/") || strings.HasPrefix(r.URL.Path, "/jimeng/") {
		var data any
		if len(t.Data) > 0 && json.Unmarshal(t.Data, &data) == nil {
			rewriteID(data, t.UpstreamID, t.ID)
			writeJSON(w, http.StatusOK, data)
		} else {
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]string{"task_id": t.ID, "task_status": t.Status}})
		}
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/videos") {
		data := map[string]any{"id": t.ID, "object": "video", "model": t.Model, "status": t.Status,
			"created_at": t.CreatedAt.Unix(), "seconds": t.Units, "error": nil}
		if t.Status == "completed" {
			data["completed_at"] = t.UpdatedAt.Unix()
			data["url"] = "/v1/videos/" + t.ID + "/content"
		}
		if t.Status == "failed" {
			data["error"] = map[string]string{"code": "task_failed", "message": t.Error}
		}
		writeJSON(w, http.StatusOK, data)
		return
	}
	if submit {
		writeJSON(w, http.StatusOK, map[string]string{"task_id": t.ID, "status": t.Status})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": "success", "message": "", "data": taskDTO(t)})
}

func taskDTO(t Task) map[string]any {
	var data any
	if len(t.Data) > 0 && json.Unmarshal(t.Data, &data) == nil {
		rewriteID(data, t.UpstreamID, t.ID)
	}
	return map[string]any{"task_id": t.ID, "platform": t.Provider, "model": t.Model, "action": t.Action,
		"status": t.Status, "fail_reason": t.Error, "data": data, "created_at": t.CreatedAt.Unix(),
		"updated_at": t.UpdatedAt.Unix(), "result_url": "/v1/videos/" + t.ID + "/content"}
}

func rewriteID(v any, upstream, public string) {
	if upstream == "" {
		return
	}
	switch x := v.(type) {
	case map[string]any:
		for key, value := range x {
			if strings.HasPrefix(key, "_v3_") {
				delete(x, key)
				continue
			}
			if s, ok := value.(string); ok && s == upstream && (key == "id" || key == "task_id" || key == "name") {
				x[key] = public
			} else {
				rewriteID(value, upstream, public)
			}
		}
	case []any:
		for _, value := range x {
			rewriteID(value, upstream, public)
		}
	}
}
