package commerce

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

func TestSMTPLOGINRequiresTLSCorrectServerAndPromptOrder(t *testing.T) {
	sender, err := NewSMTPWalletSender(SMTPWalletConfig{Address: "smtp.fixture.test:465", From: "operator@fixture.test", Username: "operator", Password: "fixture-token", LoginAuth: true, ImplicitTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range []smtp.ServerInfo{
		{Name: "smtp.fixture.test", TLS: false, Auth: []string{"LOGIN"}},
		{Name: "other.test", TLS: true, Auth: []string{"LOGIN"}},
		{Name: "smtp.fixture.test", TLS: true, Auth: []string{"PLAIN"}},
	} {
		if _, _, err = sender.smtpAuth().Start(&server); err == nil {
			t.Fatal("unsafe/unsupported LOGIN server accepted")
		}
	}
	auth := sender.smtpAuth()
	method, response, err := auth.Start(&smtp.ServerInfo{Name: "smtp.fixture.test", TLS: true, Auth: []string{"LOGIN"}})
	if err != nil || method != "LOGIN" || response != nil {
		t.Fatal("LOGIN selection changed")
	}
	if _, err = auth.Next([]byte("Password:"), true); err == nil {
		t.Fatal("password sent before username challenge")
	}
	if _, err = auth.Next([]byte("echo fixture-token"), true); err == nil || strings.Contains(err.Error(), "fixture-token") {
		t.Fatal("unknown challenge accepted or secret echoed")
	}
	if _, err = auth.Next(nil, false); err == nil {
		t.Fatal("premature auth success accepted")
	}
	if _, err = auth.Next([]byte("Username:"), true); err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Next([]byte("Username:"), true); err == nil {
		t.Fatal("replayed username challenge accepted")
	}
}

func TestSMTPAuthLOGINOverActuallyVerifiedLocalTLS(t *testing.T) {
	certificate, roots := smtpFixtureCertificate(t)
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := tls.NewListener(tcp, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	defer func() { _ = listener.Close() }()
	finished := make(chan error, 1)
	go func() { finished <- serveSMTPLOGINFixture(listener) }()
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", listener.Addr().String(), &tls.Config{RootCAs: roots, ServerName: "smtp.fixture.test", MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if len(conn.ConnectionState().VerifiedChains) == 0 {
		t.Fatal("fixture TLS was not certificate-verified")
	}
	client, err := smtp.NewClient(conn, "smtp.fixture.test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	sender, err := NewSMTPWalletSender(SMTPWalletConfig{Address: "smtp.fixture.test:465", From: "operator@fixture.test", Username: "operator", Password: "fixture-token", LoginAuth: true, ImplicitTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Auth(sender.smtpAuth()); err != nil {
		t.Fatal(err)
	}
	if err = client.Quit(); err != nil {
		t.Fatal(err)
	}
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
}

func smtpFixtureCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "smtp.fixture.test"}, DNSNames: []string{"smtp.fixture.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}

func serveSMTPLOGINFixture(listener net.Listener) error {
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	write := func(text string) error { _, err := conn.Write([]byte(text)); return err }
	read := func() (string, error) { line, err := reader.ReadString('\n'); return strings.TrimSpace(line), err }
	if err = write("220 fixture SMTP\r\n"); err != nil {
		return err
	}
	command, err := read()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(command, "EHLO ") {
		return errors.New("fixture expected EHLO")
	}
	if err = write("250-smtp.fixture.test\r\n250 AUTH LOGIN PLAIN\r\n"); err != nil {
		return err
	}
	command, err = read()
	if err != nil {
		return err
	}
	if command != "AUTH LOGIN" {
		return errors.New("configured LOGIN transport selected another mechanism")
	}
	for _, part := range []struct{ prompt, value string }{{"Username:", "operator"}, {"Password:", "fixture-token"}} {
		if err = write("334 " + base64.StdEncoding.EncodeToString([]byte(part.prompt)) + "\r\n"); err != nil {
			return err
		}
		encoded, err := read()
		if err != nil {
			return err
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || string(decoded) != part.value {
			return errors.New("LOGIN response did not match the requested credential")
		}
	}
	if err = write("235 authenticated\r\n"); err != nil {
		return err
	}
	command, err = read()
	if err != nil {
		return err
	}
	if command != "QUIT" {
		return errors.New("fixture expected QUIT")
	}
	return write("221 goodbye\r\n")
}

func TestSMTPLOGINDoesNotAuthenticateBeforeRequiredSTARTTLS(t *testing.T) {
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
		_, _ = conn.Write([]byte("220 SMTP\r\n"))
		reader := bufio.NewReader(conn)
		line, _ := reader.ReadString('\n')
		commands <- line
		_, _ = conn.Write([]byte("250-test\r\n250 AUTH LOGIN\r\n"))
		line, _ = reader.ReadString('\n')
		commands <- line
	}()
	sender, err := NewSMTPWalletSender(SMTPWalletConfig{Address: listener.Addr().String(), From: "operator@fixture.test", Username: "operator", Password: "fixture-token", LoginAuth: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err = sender.SendAccountEmail(context.Background(), "receiver@fixture.test", "CodeGo", "body"); err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("unencrypted LOGIN server accepted: %v", err)
	}
	<-done
	close(commands)
	for command := range commands {
		if strings.Contains(command, "AUTH") || strings.Contains(command, "MAIL") {
			t.Fatal("LOGIN sent credentials/message before verified TLS")
		}
	}
}

func TestSMTPDeliveryErrorsRedactCredentialsWithoutLosingCancellation(t *testing.T) {
	sender, err := NewSMTPWalletSender(SMTPWalletConfig{Address: "smtp.fixture.test:465", From: "operator@fixture.test", Username: "operator", Password: "fixture-token"})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"operator", "fixture-token", base64.StdEncoding.EncodeToString([]byte("fixture-token")), base64.StdEncoding.EncodeToString([]byte("\x00operator\x00fixture-token"))} {
		cause := errors.New("SMTP refused " + secret)
		safe := sender.safeSMTPError(cause)
		if strings.Contains(safe.Error(), secret) || !errors.Is(safe, cause) {
			t.Fatal("delivery error disclosed credential or lost cause")
		}
	}
	if !errors.Is(sender.safeSMTPError(context.Canceled), context.Canceled) {
		t.Fatal("SMTP redaction lost cancellation")
	}
}

func TestSMTPTransportFailureDoesNotEchoConfiguredCredentials(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = conn.Write([]byte("530 operator fixture-token rejected\r\n"))
	}()
	sender, err := NewSMTPWalletSender(SMTPWalletConfig{Address: listener.Addr().String(), Username: "operator", Password: "fixture-token", From: "sender@fixture.test", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = sender.SendAccountEmail(context.Background(), "receiver@fixture.test", "CodeGo", "body")
	<-done
	if err == nil {
		t.Fatal("failed SMTP greeting pretended delivery")
	}
	if strings.Contains(err.Error(), "operator") || strings.Contains(err.Error(), "fixture-token") {
		t.Fatal("SMTP transport error exposed configured credentials")
	}
}
