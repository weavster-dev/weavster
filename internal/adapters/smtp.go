package adapters

import (
	"bytes"
	"context"
	"fmt"
	"net/smtp"
)

// SMTPSink delivers messages via SMTP (spec §8).
type SMTPSink struct {
	host string
	from string
	to   []string
	send func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// NewSMTPSink returns an SMTP sink; send defaults to smtp.SendMail.
func NewSMTPSink(host, from string, to []string) *SMTPSink {
	return &SMTPSink{
		host: host,
		from: from,
		to:   to,
		send: smtp.SendMail,
	}
}

func (s *SMTPSink) Name() string { return "smtp" }

func (s *SMTPSink) Write(_ context.Context, m Message) error {
	msg := buildMail(s.from, s.to, m.Body)
	if err := s.send(s.host, nil, s.from, s.to, msg); err != nil {
		return err
	}
	return nil
}

func (s *SMTPSink) Close() error { return nil }

// buildMail renders a minimal RFC 5322 message.
func buildMail(from string, to []string, body []byte) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from)
	for i, t := range to {
		if i == 0 {
			fmt.Fprintf(&b, "To: %s", t)
		} else {
			fmt.Fprintf(&b, ", %s", t)
		}
	}
	b.WriteString("\r\n")
	b.WriteString("Subject: Weavster message\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.Write(body)
	return b.Bytes()
}
