package bedrock

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type credential struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
	Region          string `json:"region"`
	Bearer          string `json:"api_key"`
}

// Existing pipe-delimited secrets remain valid after migration; JSON permits
// short-lived STS credentials and a region inferred from a runtime endpoint.
func credentials(secret, base string) (credential, error) {
	var out credential
	if strings.HasPrefix(strings.TrimSpace(secret), "{") {
		if err := json.Unmarshal([]byte(secret), &out); err != nil {
			return out, fmt.Errorf("bedrock: invalid credential JSON")
		}
	} else {
		parts := strings.Split(secret, "|")
		switch len(parts) {
		case 1:
			out.Bearer = parts[0]
		case 2:
			out.Bearer, out.Region = parts[0], parts[1]
		case 3, 4:
			out.AccessKeyID, out.SecretAccessKey, out.Region = parts[0], parts[1], parts[2]
			if len(parts) == 4 {
				out.SessionToken = parts[3]
			}
		default:
			return out, fmt.Errorf("bedrock: invalid credential format")
		}
		for _, part := range parts {
			if strings.TrimSpace(part) == "" {
				return out, fmt.Errorf("bedrock: empty credential component")
			}
		}
	}
	if out.Region == "" {
		u, err := url.Parse(base)
		if err == nil {
			host := strings.TrimSuffix(u.Hostname(), ".amazonaws.com.cn")
			host = strings.TrimSuffix(host, ".amazonaws.com")
			if strings.HasPrefix(host, "bedrock-runtime.") {
				out.Region = strings.TrimPrefix(host, "bedrock-runtime.")
			}
		}
	}
	if out.Region == "" {
		return out, fmt.Errorf("bedrock: AWS region is required")
	}
	for _, c := range out.Region {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return out, fmt.Errorf("bedrock: invalid AWS region")
		}
	}
	for _, value := range []string{out.AccessKeyID, out.SecretAccessKey, out.SessionToken, out.Bearer} {
		if strings.ContainsAny(value, "\r\n") {
			return out, fmt.Errorf("bedrock: invalid credential component")
		}
	}
	if out.Bearer != "" {
		if out.AccessKeyID != "" || out.SecretAccessKey != "" || out.SessionToken != "" {
			return out, fmt.Errorf("bedrock: cannot mix bearer and AWS credentials")
		}
	} else if strings.TrimSpace(out.AccessKeyID) == "" || strings.TrimSpace(out.SecretAccessKey) == "" {
		return out, fmt.Errorf("bedrock: access key and secret key are required")
	}
	return out, nil
}
