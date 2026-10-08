package boot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func mailValues() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"SMTPServer": json.RawMessage(`"mail.example.test"`), "SMTPAccount": json.RawMessage(`"sender@example.test"`), "SMTPToken": json.RawMessage(`"fixture-password"`),
	}
}

func TestStoredSMTPDefaultsAndRetainedTLSLOGINSelection(t *testing.T) {
	for _, test := range []struct {
		name, key, value string
		tls, login       bool
		port             string
	}{
		{"defaults", "", "", false, false, "587"},
		{"port465", "SMTPPort", "465", true, false, "465"},
		{"ssl", "SMTPSSLEnabled", "true", true, false, "587"},
		{"force", "SMTPForceAuthLogin", `"true"`, false, true, "587"},
		{"outlookServer", "SMTPServer", `"smtp-mail.outlook.com"`, false, true, "587"},
		{"outlookAccount", "SMTPAccount", `"user@outlook.com"`, false, true, "587"},
		{"officeAccount", "SMTPAccount", `"user@tenant.onmicrosoft.com"`, false, true, "587"},
		{"sendcloud", "SMTPServer", `"smtp.sendcloud.net"`, false, true, "587"},
		{"azurecomm", "SMTPServer", `"smtp.azurecomm.net"`, false, true, "587"},
		{"customArray", "EmailLoginAuthServerList", `["mail.example.test"]`, false, true, "587"},
		{"customString", "EmailLoginAuthServerList", `"other.test,mail.example.test"`, false, true, "587"},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := mailValues()
			if test.key != "" {
				values[test.key] = json.RawMessage(test.value)
			}
			cfg, err := storedSMTPConfig(values)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ImplicitTLS != test.tls || cfg.LoginAuth != test.login || !strings.HasSuffix(cfg.Address, ":"+test.port) || cfg.From != cfg.Username {
				t.Fatal("SMTP defaults, TLS, LOGIN or From fallback changed")
			}
		})
	}
}

func TestStoredSMTPRejectsMalformedAndSecretBearingConfiguration(t *testing.T) {
	if _, err := storedSMTPConfig(nil); !errors.Is(err, ErrStoredMailUnavailable) {
		t.Fatalf("missing SMTP accepted: %v", err)
	}
	for _, test := range []struct{ key, value string }{
		{"SMTPServer", `null`}, {"SMTPServer", `"smtp://bad.example.test"`}, {"SMTPServer", `"bad\r\nhost"`},
		{"SMTPPort", `"0"`}, {"SMTPPort", `65536`}, {"SMTPPort", `"587x"`}, {"SMTPPort", `[]`},
		{"SMTPSSLEnabled", `"yes"`}, {"SMTPForceAuthLogin", `{}`}, {"SMTPFrom", `"bad\r\nBcc: stranger@test"`},
		{"SMTPToken", `123`}, {"SMTPToken", `null`}, {"SMTPToken", `""`}, {"SMTPAccount", `""`},
		{"EmailLoginAuthServerList", `null`}, {"EmailLoginAuthServerList", `[42]`}, {"EmailLoginAuthServerList", `"https://bad.test"`},
	} {
		t.Run(test.key+test.value, func(t *testing.T) {
			values := mailValues()
			values[test.key] = json.RawMessage(test.value)
			if _, err := storedSMTPConfig(values); err == nil {
				t.Fatal("malformed SMTP accepted")
			} else if strings.Contains(err.Error(), "fixture-password") {
				t.Fatal("SMTP error disclosed password")
			}
		})
	}
	sender := NewStoredEmailSender(nil, nil)
	if err := sender.SendAccountEmail(context.Background(), "receiver@example.test", "CodeGo", "body"); !errors.Is(err, ErrStoredMailUnavailable) {
		t.Fatalf("absent settings pretended delivery: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sender.SendWalletRecovery(ctx, "receiver@example.test", "123456"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled send ignored: %v", err)
	}
}
