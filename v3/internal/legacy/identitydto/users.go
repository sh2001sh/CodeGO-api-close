package identitydto

import (
	"encoding/json"
	"errors"
	"strings"
)

func userInput(fields map[string]json.RawMessage, path string) error {
	for field, names := range map[string]map[int]string{
		"role": {1: "user", 10: "admin", 100: "root"}, "status": {1: "active", 2: "disabled"},
	} {
		if raw := fields[field]; len(raw) != 0 && raw[0] != '"' {
			var code int
			if json.Unmarshal(raw, &code) != nil || names[code] == "" {
				return errors.New("unknown role or status")
			}
			fields[field], _ = json.Marshal(names[code])
		}
	}
	if strings.HasPrefix(path, "/api/user/") {
		// Legacy full-object PUTs include readonly identity fields.
		for _, field := range []string{"username", "created_at", "updated_at", "last_login_at", "deleted_at", "aff_code", "inviter_id", "remark", "access_token", "aff_quota", "affiliate_micro_credits"} {
			if path != "/api/user/" || field != "username" {
				delete(fields, field)
			}
		}
		if path == "/api/user/self" {
			for _, field := range []string{"id", "role", "status", "group"} {
				delete(fields, field)
			}
		}
	}
	return nil
}

func userOutput(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for field, codes := range map[string]map[string]int{"role": {"user": 1, "admin": 10, "root": 100}, "status": {"active": 1, "disabled": 2}} {
		var name string
		if json.Unmarshal(fields[field], &name) == nil {
			fields[field], _ = json.Marshal(codes[name])
		}
	}
	if raw := fields["affiliate_micro_credits"]; raw != nil {
		var amount int64
		if err := json.Unmarshal(raw, &amount); err != nil || amount < 0 {
			return nil, errors.New("invalid affiliate amount")
		}
		fields["aff_quota"], _ = json.Marshal(amount / 2)
	}
	return json.Marshal(fields)
}

func adaptOutput(path string, raw json.RawMessage) (json.RawMessage, error) {
	if strings.HasPrefix(path, "/api/token/") {
		if strings.HasSuffix(path, "/key") {
			return raw, nil
		}
		return collection(raw, keyOutput)
	}
	if path == "/api/user/self" || path == "/api/user/" || strings.HasPrefix(path, "/api/user/") && !strings.Contains(strings.TrimPrefix(path, "/api/user/"), "/") {
		var session map[string]json.RawMessage
		if json.Unmarshal(raw, &session) == nil && session["user"] != nil {
			flattened, err := userOutput(session["user"])
			if err != nil {
				return nil, err
			}
			var user map[string]json.RawMessage
			if err = json.Unmarshal(flattened, &user); err != nil {
				return nil, err
			}
			for key, value := range user {
				session[key] = value
			}
			return json.Marshal(session)
		}
		if path == "/api/user/groups" {
			return raw, nil
		}
		return collection(raw, userOutput)
	}
	return raw, nil
}

func collection(raw json.RawMessage, convert func(json.RawMessage) (json.RawMessage, error)) (json.RawMessage, error) {
	if string(raw) == "null" {
		return raw, nil
	}
	if len(raw) > 0 && raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		for i, item := range items {
			var err error
			items[i], err = convert(item)
			if err != nil {
				return nil, err
			}
		}
		return json.Marshal(items)
	}
	var page map[string]json.RawMessage
	if json.Unmarshal(raw, &page) == nil && page["items"] != nil {
		items, err := collection(page["items"], convert)
		if err != nil {
			return nil, err
		}
		page["items"] = items
		return json.Marshal(page)
	}
	return convert(raw)
}
