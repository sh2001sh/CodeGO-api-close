package commerce

import (
	"encoding/base64"
	"errors"
	"net/smtp"
	"strings"
)

type smtpLoginAuth struct {
	host, username, password string
	step                     int
}

func (s *SMTPWalletSender) smtpAuth() smtp.Auth {
	if s.cfg.LoginAuth {
		return &smtpLoginAuth{host: s.host, username: s.cfg.Username, password: s.cfg.Password}
	}
	return smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.host)
}

func (a *smtpLoginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS || server.Name != a.host {
		return "", nil, errors.New("SMTP LOGIN requires verified TLS to the configured server")
	}
	allowed := false
	for _, method := range server.Auth {
		if strings.EqualFold(method, "LOGIN") {
			allowed = true
		}
	}
	if !allowed {
		return "", nil, errors.New("SMTP server does not support AUTH LOGIN")
	}
	a.step = 0
	return "LOGIN", nil, nil
}

func (a *smtpLoginAuth) Next(prompt []byte, more bool) ([]byte, error) {
	if !more {
		if a.step != 2 {
			return nil, errors.New("SMTP LOGIN completed before both authentication steps")
		}
		return nil, nil
	}
	text := strings.ToLower(strings.TrimSpace(string(prompt)))
	switch {
	case a.step == 0 && (text == "username:" || text == "username" || text == "user name:"):
		a.step++
		return []byte(a.username), nil
	case a.step == 1 && (text == "password:" || text == "password"):
		a.step++
		return []byte(a.password), nil
	default:
		return nil, errors.New("SMTP LOGIN returned an unexpected authentication challenge")
	}
}

type smtpRedactedError struct {
	cause error
	text  string
}

func (e smtpRedactedError) Error() string { return e.text }
func (e smtpRedactedError) Unwrap() error { return e.cause }

func (s *SMTPWalletSender) safeSMTPError(err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	values := []string{s.cfg.Username, s.cfg.Password}
	if s.cfg.Username != "" {
		values = append(values, base64.StdEncoding.EncodeToString([]byte(s.cfg.Username)))
	}
	if s.cfg.Password != "" {
		values = append(values, base64.StdEncoding.EncodeToString([]byte(s.cfg.Password)), base64.StdEncoding.EncodeToString([]byte("\x00"+s.cfg.Username+"\x00"+s.cfg.Password)))
	}
	for _, value := range values {
		if value != "" {
			text = strings.ReplaceAll(text, value, "[redacted]")
		}
	}
	if text == err.Error() {
		return err
	}
	return smtpRedactedError{cause: err, text: text}
}
