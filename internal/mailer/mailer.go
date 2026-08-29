package mailer

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"newsanalyzer/internal/models"
)

type Mailer struct{ S models.Settings }

func New(s models.Settings) *Mailer { return &Mailer{S: s} }

const (
	dialTimeout = 15 * time.Second
	sendTimeout = 60 * time.Second
)

func (m *Mailer) Send(to []string, subject, html string) error {
	if m.S.SMTPHost == "" {
		return fmt.Errorf("smtp not configured")
	}
	addr := fmt.Sprintf("%s:%d", m.S.SMTPHost, m.S.SMTPPort)
	from := m.S.SMTPFrom
	if from == "" {
		from = m.S.SMTPUser
	}
	msg := buildMessage(from, to, subject, html)
	var auth smtp.Auth
	if m.S.SMTPUser != "" {
		auth = smtp.PlainAuth("", m.S.SMTPUser, m.S.SMTPPassword, m.S.SMTPHost)
	}
	var conn net.Conn
	var err error
	if m.S.SMTPPort == 465 {
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: dialTimeout}, "tcp", addr, &tls.Config{ServerName: m.S.SMTPHost})
	} else {
		conn, err = net.DialTimeout("tcp", addr, dialTimeout)
	}
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(sendTimeout))
	return send(conn, m.S.SMTPHost, m.S.SMTPPort, auth, from, to, msg)
}

// send drives the SMTP conversation over an established connection that
// already carries a deadline. For non-465 ports it upgrades via STARTTLS
// when the server offers it, matching net/smtp.SendMail semantics.
func send(conn net.Conn, host string, port int, auth smtp.Auth, from string, to []string, msg []byte) error {
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Quit()
	if port != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
				return err
			}
		}
	}
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}

func buildMessage(from string, to []string, subject, html string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", mimeEncode(subject))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(html)
	return []byte(b.String())
}

func mimeEncode(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?UTF-8?B?" + base64(s) + "?="
		}
	}
	return s
}

func base64(s string) string {
	const tab = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b := []byte(s)
	var out strings.Builder
	for i := 0; i < len(b); i += 3 {
		n := 0
		k := 0
		for j := 0; j < 3; j++ {
			n <<= 8
			if i+j < len(b) {
				n |= int(b[i+j])
				k++
			}
		}
		for j := 0; j < 4; j++ {
			if j <= k {
				out.WriteByte(tab[(n>>uint(6*(3-j)))&0x3F])
			} else {
				out.WriteByte('=')
			}
		}
	}
	return out.String()
}
