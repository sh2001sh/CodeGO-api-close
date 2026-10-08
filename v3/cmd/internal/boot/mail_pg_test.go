//go:build pgintegration

package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestStoredSMTPEncryptedSettingsAndDynamicEdits(t *testing.T) {
	pool := marketBatchPG(t)
	ctx := context.Background()
	crypto, err := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sender := NewStoredEmailSender(pool, crypto)
	if _, err = sender.load(ctx); !errors.Is(err, ErrStoredMailUnavailable) {
		t.Fatalf("missing settings: %v", err)
	}
	if available, err := sender.Available(ctx); err != nil || available {
		t.Fatalf("missing SMTP advertised as available: %v", err)
	}
	token, _ := json.Marshal("private-mail-password")
	sealed, err := crypto.Encrypt(token)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO v3_platform.settings(key,value) VALUES
	 ('SMTPServer','"smtp.example.test"'),('SMTPPort','"465"'),('SMTPAccount','"operator@example.test"');`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO v3_platform.settings(key,ciphertext,sensitive) VALUES('SMTPToken',$1,true)`, sealed)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := sender.load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "private-mail-password" || cfg.Address != "smtp.example.test:465" || !cfg.ImplicitTLS || cfg.From != cfg.Username {
		t.Fatal("retained encrypted SMTP settings not applied")
	}
	_, err = pool.Exec(ctx, `UPDATE v3_platform.settings SET value='"587"' WHERE key='SMTPPort';
	 INSERT INTO v3_platform.settings(key,value) VALUES('SMTPSSLEnabled','false'),('SMTPForceAuthLogin','true'),('SMTPFrom','"new-sender@example.test"');`)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = sender.load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != "smtp.example.test:587" || cfg.ImplicitTLS || !cfg.LoginAuth || cfg.From != "new-sender@example.test" {
		t.Fatal("mail sender cached old settings instead of dynamic edit")
	}
	if available, err := sender.Available(ctx); err != nil || !available {
		t.Fatalf("configured SMTP not available: %v", err)
	}
	wrong, err := catalog.NewAESGCM(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewStoredEmailSender(pool, wrong).load(ctx); err == nil || strings.Contains(err.Error(), "private-mail-password") {
		t.Fatal("wrong decryption key was accepted or disclosed a secret")
	}
	_, err = pool.Exec(ctx, `UPDATE v3_platform.settings SET ciphertext=NULL,sensitive=false,value='"private-mail-password"' WHERE key='SMTPToken'`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sender.load(ctx); err == nil || strings.Contains(err.Error(), "private-mail-password") {
		t.Fatal("plaintext mail secret was accepted or disclosed")
	}
	_, err = pool.Exec(ctx, `UPDATE v3_platform.settings SET value='"broken-private-mail-password"' WHERE key='SMTPPort'`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sender.load(ctx); err == nil || strings.Contains(err.Error(), "private-mail-password") {
		t.Fatal("malformed settings were accepted or echoed secret input")
	}
}
