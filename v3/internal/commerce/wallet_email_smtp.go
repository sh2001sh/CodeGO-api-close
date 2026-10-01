package commerce

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// SMTPWalletConfig must come from deployment secrets/environment at the
// composition root. Both implicit TLS and mandatory STARTTLS verify certificates.
type SMTPWalletConfig struct {
	Address     string
	Username    string
	Password    string
	From        string
	ImplicitTLS bool
	Timeout     time.Duration
}

type SMTPWalletSender struct {
	cfg  SMTPWalletConfig
	host string
	from *mail.Address
}

func NewSMTPWalletSender(cfg SMTPWalletConfig) (*SMTPWalletSender, error) {
	host, port, err := net.SplitHostPort(cfg.Address)
	if err != nil || host == "" || port == "" || strings.ContainsAny(cfg.Address+cfg.Username+cfg.From, "\r\n") {
		return nil, ErrInvalid
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, ErrInvalid
	}
	if (cfg.Username == "") != (cfg.Password == "") {
		return nil, ErrInvalid
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.Timeout <= 0 || cfg.Timeout > time.Minute {
		return nil, ErrInvalid
	}
	return &SMTPWalletSender{cfg: cfg, host: host, from: from}, nil
}

func (s *SMTPWalletSender) SendWalletRecovery(ctx context.Context, email, code string) (result error) {
	to, err := mail.ParseAddress(email)
	if err != nil || strings.ContainsAny(email, "\r\n") || len(code) != 6 {
		return ErrInvalid
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return ErrInvalid
		}
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	client, closeClient, err := s.dialAuthenticatedClient(ctx)
	if err != nil {
		return err
	}
	defer closeClient()
	if err = client.Mail(s.from.Address); err != nil {
		return err
	}
	if err = client.Rcpt(to.Address); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\nCodeGo payment password recovery code: %s\r\nThis code expires shortly. Do not share it with anyone.\r\n", s.from.String(), to.String(), mime.QEncoding.Encode("UTF-8", "CodeGo 支付密码恢复验证码"), code)
	_, err = writer.Write([]byte(body))
	closeErr := writer.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return client.Quit()
}

// dialAuthenticatedClient connects (implicit TLS or STARTTLS, per config),
// authenticates if credentials were configured, and returns a ready-to-use
// SMTP client along with a cleanup func that closes both client and
// connection. The caller must defer the returned cleanup regardless of error.
func (s *SMTPWalletSender) dialAuthenticatedClient(ctx context.Context) (*smtp.Client, func(), error) {
	dialer := net.Dialer{Timeout: s.cfg.Timeout}
	tlsConfig := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	var err error
	if s.cfg.ImplicitTLS {
		tlsDialer := tls.Dialer{NetDialer: &dialer, Config: tlsConfig}
		conn, err = tlsDialer.DialContext(ctx, "tcp", s.cfg.Address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", s.cfg.Address)
	}
	if err != nil {
		return nil, func() {}, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	closeConn := func() { stop(); _ = conn.Close() }
	if deadline, ok := ctx.Deadline(); ok {
		if err = conn.SetDeadline(deadline); err != nil {
			return nil, closeConn, err
		}
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return nil, closeConn, err
	}
	cleanup := func() { _ = client.Close(); closeConn() }
	if !s.cfg.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return client, cleanup, errors.New("wallet email SMTP server requires TLS support")
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return client, cleanup, err
		}
	}
	if s.cfg.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.host)); err != nil {
			return client, cleanup, err
		}
	}
	return client, cleanup, nil
}
