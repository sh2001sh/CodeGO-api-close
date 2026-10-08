package boot

import (
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
)

func smtpScalar(raw json.RawMessage) (string, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil && string(raw) != "null" {
		return text, nil
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil && number.String() != "" {
		return number.String(), nil
	}
	if string(raw) == "true" || string(raw) == "false" {
		return string(raw), nil
	}
	return "", errors.New("mail: invalid scalar setting")
}

func smtpPort(raw json.RawMessage) (int, error) {
	if len(raw) == 0 {
		return 587, nil
	}
	text, err := smtpScalar(raw)
	if err != nil {
		return 0, errors.New("mail: invalid SMTPPort setting")
	}
	port, err := strconv.Atoi(text)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("mail: invalid SMTPPort setting")
	}
	return port, nil
}

func smtpBoolean(raw json.RawMessage) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	text, err := smtpScalar(raw)
	if err != nil {
		return false, err
	}
	flag, err := strconv.ParseBool(text)
	if err != nil {
		return false, errors.New("mail: invalid boolean setting")
	}
	return flag, nil
}

func smtpLoginServers(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return []string{"smtp.sendcloud.net", "smtp.azurecomm.net"}, nil
	}
	var servers []string
	if json.Unmarshal(raw, &servers) != nil || string(raw) == "null" {
		var text string
		if json.Unmarshal(raw, &text) != nil || text == "" {
			return nil, errors.New("mail: invalid EmailLoginAuthServerList setting")
		}
		servers = strings.Split(text, ",")
	}
	for i, host := range servers {
		host = strings.TrimSpace(host)
		if !validSMTPHost(host) {
			return nil, errors.New("mail: invalid EmailLoginAuthServerList setting")
		}
		servers[i] = host
	}
	return servers, nil
}

func validSMTPHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range strings.ToLower(label) {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}
