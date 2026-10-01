package legacy

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// v2 kept gateway behavior, provider metadata and network settings in three
// fields. The native catalog exposes one typed settings object and proxy URL.
func channelConfiguration(c *sourceChannel) error {
	settings := map[string]any{}
	for _, value := range []string{c.Setting, c.Settings} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		var decoded map[string]any
		if json.Unmarshal([]byte(value), &decoded) != nil || decoded == nil {
			return errors.New("channel settings must be JSON objects")
		}
		for key, value := range decoded {
			settings[key] = value
		}
	}
	if c.Other != "" {
		settings["api_version"] = c.Other
	}
	cardEnabled := c.CardSupported
	if c.CardUserEnabled != nil {
		cardEnabled = *c.CardUserEnabled
	}
	settings["multiplier_card_user_enabled"] = cardEnabled
	if proxy, exists := settings["proxy"]; exists {
		value, ok := proxy.(string)
		if !ok {
			return errors.New("channel proxy must be text")
		}
		if value != "" {
			u, err := url.Parse(value)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") {
				return errors.New("channel proxy URL is invalid")
			}
			c.ProxyURL = value
		}
		delete(settings, "proxy")
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	c.Settings = string(encoded)
	if c.Organization != nil && *c.Organization != "" {
		headers := map[string]any{}
		if json.Unmarshal([]byte(jsonObject(c.HeaderOverride)), &headers) != nil || headers == nil {
			return errors.New("channel header override must be a JSON object")
		}
		if _, explicit := headers["OpenAI-Organization"]; !explicit {
			headers["OpenAI-Organization"] = *c.Organization
		}
		encoded, err = json.Marshal(headers)
		if err != nil {
			return err
		}
		c.HeaderOverride = string(encoded)
	}
	// Cloudflare and Coze native adapters consume a compound secret. Preserve
	// the old token together with its account/bot ID held in Channel.Other.
	if (c.Type == 39 || c.Type == 49) && c.Other != "" {
		secrets := splitSecrets(c.Key)
		for index, secret := range secrets {
			if json.Valid([]byte(secret)) || strings.Contains(secret, "|") {
				continue
			}
			field := "account_id"
			if c.Type == 49 {
				field = "bot_id"
			}
			encoded, err = json.Marshal(map[string]string{field: c.Other, "token": secret})
			if err != nil {
				return err
			}
			secrets[index] = string(encoded)
		}
		c.Key = strings.Join(secrets, "\n")
	}
	return nil
}
