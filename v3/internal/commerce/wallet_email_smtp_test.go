package commerce

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestWalletSMTPRequiresTLSBeforeAuthentication(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	commands := make(chan string, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = conn.Write([]byte("220 test SMTP\r\n"))
		reader := bufio.NewReader(conn)
		line, _ := reader.ReadString('\n')
		commands <- line
		_, _ = conn.Write([]byte("250-test\r\n250 AUTH PLAIN\r\n"))
		line, _ = reader.ReadString('\n')
		commands <- line
	}()
	sender, err := NewSMTPWalletSender(SMTPWalletConfig{Address: listener.Addr().String(), From: "sender@example.test", Username: "smtp-user", Password: "smtp-password", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err = sender.SendWalletRecovery(context.Background(), "receiver@example.test", "123456"); err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("unencrypted server accepted %v", err)
	}
	<-done
	close(commands)
	for command := range commands {
		if strings.Contains(command, "AUTH") || strings.Contains(command, "MAIL") {
			t.Fatal("plaintext SMTP transmitted credentials or message")
		}
	}
}

func TestWalletSMTPConfigAndCancellation(t *testing.T) {
	for _, cfg := range []SMTPWalletConfig{{Address: "smtp.test", From: "sender@example.test"}, {Address: "smtp.test:587", From: "bad\r\nmail"}, {Address: "smtp.test:587", From: "sender@example.test", Username: "user"}} {
		if _, err := NewSMTPWalletSender(cfg); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid SMTP config accepted %v", err)
		}
	}
	sender, err := NewSMTPWalletSender(SMTPWalletConfig{Address: "127.0.0.1:1", From: "sender@example.test", ImplicitTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = sender.SendWalletRecovery(ctx, "receiver@example.test", "123456"); !errors.Is(err, context.Canceled) {
		t.Fatalf("SMTP ignored cancellation: %v", err)
	}
	if err = sender.SendWalletRecovery(ctx, "receiver@example.test\r\nBcc: attacker@test", "123456"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("header injection accepted %v", err)
	}
}
