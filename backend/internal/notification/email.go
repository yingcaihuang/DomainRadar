package notification

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// EmailChannel implements NotificationChannel for SMTP email delivery.
type EmailChannel struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	UseTLS   bool
}

// NewEmailChannel creates an EmailChannel from a ChannelConfig.
func NewEmailChannel(config *ChannelConfig) *EmailChannel {
	port, _ := strconv.Atoi(config.GetSetting("port"))
	if port == 0 {
		port = 587
	}
	useTLS, _ := strconv.ParseBool(config.GetSetting("use_tls"))

	return &EmailChannel{
		Host:     config.GetSetting("host"),
		Port:     port,
		Username: config.GetSetting("username"),
		Password: config.GetSetting("password"),
		From:     config.GetSetting("from"),
		UseTLS:   useTLS,
	}
}

// ChannelType returns "email".
func (c *EmailChannel) ChannelType() string {
	return "email"
}

// Send delivers a notification email.
func (c *EmailChannel) Send(ctx context.Context, notification *Notification) error {
	subject := fmt.Sprintf("[%s] %s - %s",
		strings.ToUpper(notification.Severity),
		notification.AlertType,
		notification.DomainName,
	)

	body := fmt.Sprintf(
		"Alert: %s\nDomain: %s\nSeverity: %s\nType: %s\nTime: %s\nURL: %s\n\n%s",
		notification.Message,
		notification.DomainName,
		notification.Severity,
		notification.AlertType,
		notification.TriggeredAt.Format(time.RFC3339),
		notification.DomainURL,
		notification.Message,
	)

	// Build the email message
	to := c.Username // For now, sends to the configured user; production would use recipients list
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=\"UTF-8\"\r\n\r\n%s",
		c.From, to, subject, body,
	)

	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))

	var auth smtp.Auth
	if c.Username != "" && c.Password != "" {
		auth = smtp.PlainAuth("", c.Username, c.Password, c.Host)
	}

	if c.UseTLS {
		return c.sendWithTLS(addr, auth, c.From, to, []byte(msg))
	}

	return smtp.SendMail(addr, auth, c.From, []string{to}, []byte(msg))
}

// sendWithTLS sends email using an explicit TLS connection (port 465 style).
func (c *EmailChannel) sendWithTLS(addr string, auth smtp.Auth, from, to string, msg []byte) error {
	tlsConfig := &tls.Config{
		ServerName: c.Host,
		MinVersion: tls.VersionTLS12,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("tls dial failed: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return fmt.Errorf("smtp client creation failed: %w", err)
	}
	defer client.Close()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth failed: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp MAIL FROM failed: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp RCPT TO failed: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA failed: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp write failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data failed: %w", err)
	}

	return client.Quit()
}

// TestConnection verifies SMTP connectivity and authentication.
func (c *EmailChannel) TestConnection(ctx context.Context, config *ChannelConfig) error {
	ch := NewEmailChannel(config)
	addr := net.JoinHostPort(ch.Host, strconv.Itoa(ch.Port))

	var conn net.Conn
	var err error

	if ch.UseTLS {
		tlsConfig := &tls.Config{
			ServerName: ch.Host,
			MinVersion: tls.VersionTLS12,
		}
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, tlsConfig)
	} else {
		conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server %s: %w", addr, err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, ch.Host)
	if err != nil {
		return fmt.Errorf("SMTP client error: %w", err)
	}
	defer client.Close()

	// Try STARTTLS if not already using TLS
	if !ch.UseTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsConfig := &tls.Config{
				ServerName: ch.Host,
				MinVersion: tls.VersionTLS12,
			}
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("STARTTLS failed: %w", err)
			}
		}
	}

	// Test authentication if credentials are provided
	if ch.Username != "" && ch.Password != "" {
		auth := smtp.PlainAuth("", ch.Username, ch.Password, ch.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	return client.Quit()
}
