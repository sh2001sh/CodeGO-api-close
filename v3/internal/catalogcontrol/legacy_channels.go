package catalogcontrol

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func legacyProviderIDs() map[int]string {
	return map[int]string{1: "openai", 3: "azure", 4: "ollama", 6: "openaimax", 7: "ohmygpt", 8: "custom", 9: "ails", 10: "aiproxy", 11: "palm", 12: "api2gpt", 13: "aigc2d", 14: "claude", 15: "baidu", 16: "zhipu", 17: "ali", 18: "xunfei", 19: "360", 20: "openrouter", 21: "aiproxy_library", 22: "fastgpt", 23: "tencent", 24: "gemini", 25: "moonshot", 26: "zhipu_v4", 27: "perplexity", 31: "lingyiwanwu", 33: "aws", 34: "cohere", 35: "minimax", 36: "suno", 37: "dify", 38: "jina", 39: "cloudflare", 40: "siliconflow", 41: "vertex", 42: "mistral", 43: "deepseek", 44: "mokaai", 45: "volcengine", 46: "baidu_v2", 47: "xinference", 48: "xai", 49: "coze", 50: "kling", 51: "jimeng", 52: "vidu", 53: "submodel", 54: "doubao_video", 56: "replicate", 57: "codex"}
}

type legacyChannel struct {
	ID                        int64   `json:"id"`
	Name                      string  `json:"name"`
	Type                      int     `json:"type"`
	Key                       string  `json:"key"`
	Status                    int     `json:"status"`
	BaseURL                   string  `json:"base_url"`
	Scope                     string  `json:"channel_scope"`
	OwnerUserID               *int64  `json:"owner_user_id"`
	Models                    string  `json:"models"`
	Group                     string  `json:"group"`
	Priority                  int     `json:"priority"`
	Weight                    int     `json:"weight"`
	MaxConcurrency            int     `json:"marketplace_max_concurrency"`
	MaxUserConcurrency        int     `json:"marketplace_user_max_concurrency"`
	MultiplierCardSupported   bool    `json:"multiplier_card_supported"`
	MultiplierCardUserEnabled *bool   `json:"multiplier_card_user_enabled"`
	AutoBan                   *int    `json:"auto_ban"`
	ModelMapping              string  `json:"model_mapping"`
	ParamOverride             string  `json:"param_override"`
	HeaderOverride            string  `json:"header_override"`
	StatusCodeMapping         string  `json:"status_code_mapping"`
	Settings                  string  `json:"settings"`
	Tag                       *string `json:"tag"`
	Remark                    string  `json:"remark"`
	KeyMode                   string  `json:"key_mode"`
}

func splitList(raw string) []string {
	items := make([]string, 0)
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			items = append(items, value)
		}
	}
	return items
}

func legacyView(c Channel) legacyChannel {
	typeID := 0
	for id, provider := range legacyProviderIDs() {
		if provider == c.Provider {
			typeID = id
			break
		}
	}
	status := 1
	if c.Status == "disabled" {
		status = 2
	}
	if c.Status == "auto_disabled" {
		status = 3
	}
	scope := c.Scope
	if scope == "marketplace" {
		scope = "external"
	}
	modelMapping, _ := json.Marshal(c.ModelMapping)
	headers, _ := json.Marshal(c.HeaderOverride)
	autoBan := 0
	if c.AutoDisable {
		autoBan = 1
	}
	return legacyChannel{ID: c.ID, Name: c.Name, Type: typeID, Status: status, BaseURL: c.BaseURL, Scope: scope, OwnerUserID: c.OwnerUserID, Models: strings.Join(c.Models, ","), Group: strings.Join(c.Groups, ","), Priority: c.Priority, Weight: c.Weight, MaxConcurrency: c.MaxConcurrency, MaxUserConcurrency: c.MaxUserConcurrency, MultiplierCardSupported: c.MultiplierCardSupported, MultiplierCardUserEnabled: c.MultiplierCardUserEnabled, AutoBan: &autoBan, ModelMapping: string(modelMapping), ParamOverride: string(c.ParamOverride), HeaderOverride: string(headers), StatusCodeMapping: string(c.StatusCodeMapping), Settings: string(c.Settings), Tag: c.Tag, Remark: c.Remark}
}

func (s *Server) legacyGetChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	c, err := scanChannel(s.pool.QueryRow(r.Context(), channelSelect+` WHERE c.id=$1`, id))
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, legacyView(c))
}

func (s *Server) legacyListChannels(w http.ResponseWriter, r *http.Request) {
	p, n := page(r)
	if r.URL.Query().Get("page") == "" {
		if old, _ := strconv.Atoi(r.URL.Query().Get("p")); old > 0 && old <= 1000000 {
			p = old
		}
	}
	keyword := r.URL.Query().Get("keyword")
	group := r.URL.Query().Get("group")
	model := r.URL.Query().Get("model")
	where := ` WHERE ($1='' OR c.name ILIKE '%'||$1||'%') AND ($2='' OR EXISTS(SELECT 1 FROM v3_catalog.channel_groups cg WHERE cg.channel_id=c.id AND cg.group_name=$2)) AND ($3='' OR EXISTS(SELECT 1 FROM v3_catalog.channel_models cm WHERE cm.channel_id=c.id AND cm.model=$3))`
	var total int64
	if err := s.pool.QueryRow(r.Context(), `SELECT count(*) FROM v3_catalog.channels c`+where, keyword, group, model).Scan(&total); err != nil {
		s.dbError(w, err)
		return
	}
	rows, err := s.pool.Query(r.Context(), channelSelect+where+` ORDER BY c.id DESC LIMIT $4 OFFSET $5`, keyword, group, model, n, (p-1)*n)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer rows.Close()
	items := make([]legacyChannel, 0, n)
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			s.dbError(w, err)
			return
		}
		items = append(items, legacyView(c))
	}
	if err = rows.Err(); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items, "total": total, "page": p, "page_size": n})
}

// legacySaveChannelWrapper unwraps the optional POST envelope ({mode, channel,
// batch_add_set_key_prefix_2_name}) around a legacy channel payload. For
// non-POST requests the raw payload is returned unchanged.
func legacySaveChannelWrapper(method string, raw json.RawMessage) (out json.RawMessage, mode string, batchNames bool, errCode, errMsg string) {
	if method != http.MethodPost {
		return raw, "", false, "", ""
	}
	var wrapper struct {
		Mode       string          `json:"mode"`
		Channel    json.RawMessage `json:"channel"`
		BatchNames bool            `json:"batch_add_set_key_prefix_2_name"`
	}
	if json.Unmarshal(raw, &wrapper) != nil {
		return raw, "", false, "invalid_channel", "Invalid channel payload"
	}
	if wrapper.Mode != "" && wrapper.Mode != "single" && wrapper.Mode != "multi_to_single" && wrapper.Mode != "batch" {
		return raw, "", false, "invalid_mode", "Unknown channel creation mode"
	}
	mode, batchNames = wrapper.Mode, wrapper.BatchNames
	if len(wrapper.Channel) != 0 {
		raw = wrapper.Channel
	}
	return raw, mode, batchNames, "", ""
}

// legacyBuildChannelFromOld translates a decoded legacy channel payload into
// the internal Channel representation, including provider/status/scope
// mapping, group/model defaults and JSON configuration fields.
func legacyBuildChannelFromOld(old legacyChannel) (c Channel, provider string, errCode string, errMsg string) {
	provider = legacyProviderIDs()[old.Type]
	if provider == "" {
		return Channel{}, "", "invalid_provider", "Unknown provider type"
	}
	if old.Status < 0 || old.Status > 3 {
		return Channel{}, "", "invalid_status", "Invalid channel status"
	}
	status := "enabled"
	if old.Status == 2 {
		status = "disabled"
	}
	if old.Status == 3 {
		status = "auto_disabled"
	}
	scope := old.Scope
	if scope == "external" {
		scope = "marketplace"
	}
	c = Channel{ID: old.ID, Name: old.Name, Provider: provider, Status: status, BaseURL: old.BaseURL, Scope: scope, OwnerUserID: old.OwnerUserID, Priority: old.Priority, Weight: old.Weight, MaxConcurrency: old.MaxConcurrency, MaxUserConcurrency: old.MaxUserConcurrency, MultiplierCardSupported: old.MultiplierCardSupported, MultiplierCardUserEnabled: old.MultiplierCardUserEnabled, AutoDisable: old.AutoBan == nil || *old.AutoBan != 0, Groups: splitList(old.Group), Models: splitList(old.Models), Tag: old.Tag, Remark: old.Remark, AppendCredentials: old.KeyMode == "append"}
	if len(c.Groups) == 0 {
		c.Groups = []string{"default"}
	}
	for _, item := range []struct {
		raw string
		dst any
	}{{old.ModelMapping, &c.ModelMapping}, {old.HeaderOverride, &c.HeaderOverride}} {
		if item.raw != "" && json.Unmarshal([]byte(item.raw), item.dst) != nil {
			return Channel{}, "", "invalid_configuration", "Configuration must contain JSON objects"
		}
	}
	c.ParamOverride = json.RawMessage(old.ParamOverride)
	c.StatusCodeMapping = json.RawMessage(old.StatusCodeMapping)
	c.Settings = json.RawMessage(old.Settings)
	return c, provider, "", ""
}

// legacyAttachCredentials parses the legacy key field into credential inputs
// on c, enforcing that creation requests supply at least one credential.
func legacyAttachCredentials(c *Channel, old legacyChannel, provider string, method string) (errCode, errMsg string) {
	if old.Key == "" {
		if method == http.MethodPost {
			return "missing_secret", "New channels require a credential"
		}
		return "", ""
	}
	secrets, err := legacySecrets(old.Key)
	if err != nil {
		return "invalid_credentials", err.Error()
	}
	for _, secret := range secrets {
		if secret = strings.TrimSpace(secret); secret != "" {
			kind := "api_key"
			if provider == "codex" {
				kind = "oauth"
			}
			c.Credentials = append(c.Credentials, CredentialInput{Secret: secret, Kind: kind})
		}
	}
	if method == http.MethodPost && len(c.Credentials) == 0 {
		return "missing_secret", "New channels require a credential"
	}
	return "", ""
}

func (s *Server) legacySaveChannel(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if !decode(w, r, &raw) {
		return
	}
	raw, mode, batchNames, errCode, errMsg := legacySaveChannelWrapper(r.Method, raw)
	if errCode != "" {
		fail(w, 400, errCode, errMsg)
		return
	}
	var old legacyChannel
	if json.Unmarshal(raw, &old) != nil {
		fail(w, 400, "invalid_channel", "Invalid channel payload")
		return
	}
	c, provider, errCode, errMsg := legacyBuildChannelFromOld(old)
	if errCode != "" {
		fail(w, 400, errCode, errMsg)
		return
	}
	if errCode, errMsg = legacyAttachCredentials(&c, old, provider, r.Method); errCode != "" {
		fail(w, 400, errCode, errMsg)
		return
	}
	if r.Method == http.MethodPost {
		c.ID = 0
	}
	if r.Method == http.MethodPut {
		if old.ID <= 0 {
			fail(w, 400, "invalid_id", "Expected a positive channel identifier")
			return
		}
		r.SetPathValue("id", strconv.FormatInt(old.ID, 10))
	}
	if err := c.validate(); err != nil {
		fail(w, 400, "invalid_channel", err.Error())
		return
	}
	if mode == "batch" {
		s.legacyCreateChannelBatch(w, r, c, batchNames)
		return
	}
	body, err := json.Marshal(c)
	if err != nil {
		fail(w, 400, "invalid_channel", "Invalid channel configuration")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	s.saveChannel(w, r)
}

func (s *Server) legacyGroups(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT name FROM v3_catalog.groups ORDER BY name`)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer rows.Close()
	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			s.dbError(w, err)
			return
		}
		names = append(names, name)
	}
	if err = rows.Err(); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, names)
}
