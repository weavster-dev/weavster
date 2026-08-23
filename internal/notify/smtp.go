package notify

import (
	"bytes"
	"context"
	"fmt"
	"net/smtp"
)

// SMTPNotifier delivers notifications via email (arch §3.1).
type SMTPNotifier struct {
	host string
	from string
	send func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// NewSMTPNotifier returns an SMTP notifier; send defaults to smtp.SendMail.
func NewSMTPNotifier(host, from string) *SMTPNotifier {
	return &SMTPNotifier{host: host, from: from, send: smtp.SendMail}
}

func (s *SMTPNotifier) Notify(_ context.Context, n Notification) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", s.from)
	for i, r := range n.Recipients {
		if i == 0 {
			fmt.Fprintf(&b, "To: %s", r)
		} else {
			fmt.Fprintf(&b, ", %s", r)
		}
	}
	fmt.Fprintf(&b, "\r\nSubject: %s\r\n\r\n%s", n.Subject, n.Body)
	return s.send(s.host, nil, s.from, n.Recipients, b.Bytes())
}

var _ Notifier = (*SMTPNotifier)(nil)
