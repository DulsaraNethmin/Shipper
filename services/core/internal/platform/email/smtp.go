package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SMTP delivers a message over SMTP (SHIP-187b).
//
// # Why this exists beside a perfectly good HTTP adapter
//
// Because SMTP is the one transport every buyer already has. A deployment is sold to an
// organisation that has a mail provider, an address its customers recognise and, almost
// always, a host and a credential it can hand over in a minute — where reaching that same
// provider over HTTP means finding its API, learning its body shape and filling in four
// more variables. The HTTP adapter is better when a vendor's API is what somebody wants;
// this is better when nobody wants to think about it.
//
// It is also what makes the demonstration environment free. Mailpit takes SMTP on 1025
// with no account, no credential and no cost, and shows every message it receives in a
// browser — so a walkthrough can open the verification email in front of a buyer rather
// than describing it. Nothing else on offer does that without somebody signing up first.
//
// # What it deliberately does not do
//
// No retry, for the reason provider.go gives: a retry policy needs to know which failures
// are worth repeating, and a blind loop against a server rejecting the credential is how a
// sender gets itself blocked. No connection pooling either — a verification email is sent
// once per registration, and a pool that holds an idle connection open is a thing to
// diagnose when the far end drops it silently.
type SMTP struct {
	addr       string
	auth       smtp.Auth
	sender     string
	encryption Encryption
	serverName string
	timeout    time.Duration
}

// Encryption is how the connection to the server is protected.
type Encryption string

const (
	// EncryptionStartTLS connects in clear and upgrades with STARTTLS. What almost every
	// provider expects on 587, and the default.
	EncryptionStartTLS Encryption = "starttls"

	// EncryptionImplicit negotiates TLS before the greeting, on 465.
	EncryptionImplicit Encryption = "implicit"

	// EncryptionNone is plain SMTP with no TLS at all.
	//
	// **Only ever right for a server on the same host that never leaves it** — Mailpit in
	// the demonstration stack is the case it exists for. A credential may not be sent over
	// one: NewSMTP refuses the combination outright rather than leaving it to be noticed,
	// because a password on a clear connection is a password anybody on the path has.
	EncryptionNone Encryption = "none"
)

// defaultSMTPTimeout bounds the whole exchange — dial, greeting, handshake and data.
//
// net/smtp offers no context, so the bound is a deadline on the connection. Twenty seconds
// is generous for a healthy server and far below anything a caller would tolerate; the
// send is off a user-facing path either way, exactly as the HTTP adapter's is.
const defaultSMTPTimeout = 20 * time.Second

// SMTPOptions is everything needed to reach a mail server.
type SMTPOptions struct {
	// Host and Port of the server. Port defaults per Encryption: 587 for STARTTLS, 465 for
	// implicit TLS, 25 for none.
	Host string
	Port int

	// Username and Password authenticate the session. Both empty means no authentication
	// is attempted, which is what a local catcher wants and what a real provider refuses.
	Username string
	Password string

	// Sender is the From address, as in Options.Sender.
	Sender string

	// Encryption defaults to EncryptionStartTLS.
	Encryption Encryption

	// Timeout bounds the exchange. Defaults to defaultSMTPTimeout.
	Timeout time.Duration
}

// NewSMTP validates opts and returns the SMTP implementation.
//
// It fails at construction rather than at first send, for the reason NewProvider does: a
// missing credential found when the first customer registers is an outage, and found at
// startup it is a failed deploy.
func NewSMTP(opts SMTPOptions) (*SMTP, error) {
	if opts.Host == "" {
		return nil, fmt.Errorf("email: smtp needs a host")
	}
	if opts.Sender == "" {
		return nil, fmt.Errorf("email: smtp needs a sender address")
	}

	encryption := opts.Encryption
	if encryption == "" {
		encryption = EncryptionStartTLS
	}

	port := opts.Port
	if port == 0 {
		switch encryption {
		case EncryptionImplicit:
			port = 465
		case EncryptionNone:
			port = 25
		default:
			port = 587
		}
	}

	var auth smtp.Auth
	switch {
	case opts.Username == "" && opts.Password == "":
		// No authentication. Legitimate for a local catcher and nothing else.
	case opts.Username == "" || opts.Password == "":
		return nil, fmt.Errorf("email: smtp needs both a username and a password, or neither")
	case encryption == EncryptionNone:
		// Refused rather than permitted-with-a-warning. net/smtp's own PlainAuth makes the
		// same call and it is the right one: a credential on a clear connection is
		// disclosed to every host on the path, and the failure is silent by nature —
		// everything works, which is why nobody looks.
		return nil, fmt.Errorf("email: smtp will not send a credential over an unencrypted " +
			"connection; use starttls or implicit encryption, or configure no credential")
	default:
		auth = smtp.PlainAuth("", opts.Username, opts.Password, opts.Host)
	}

	switch encryption {
	case EncryptionStartTLS, EncryptionImplicit, EncryptionNone:
	default:
		return nil, fmt.Errorf("email: %q is not an encryption mode (want starttls, implicit, or none)",
			opts.Encryption)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultSMTPTimeout
	}

	return &SMTP{
		addr:       net.JoinHostPort(opts.Host, strconv.Itoa(port)),
		auth:       auth,
		sender:     opts.Sender,
		encryption: encryption,
		serverName: opts.Host,
		timeout:    timeout,
	}, nil
}

// Send dispatches one message, satisfying identity.EmailSender.
func (s *SMTP) Send(ctx context.Context, to, subject, body string) error {
	if to == "" {
		return ErrNoRecipient
	}

	// Checked before a connection is opened. A recipient carrying a newline is not a
	// malformed address, it is an attempt to write extra headers into the message — a Bcc
	// to somewhere else is the classic one — and the address is the one field here that
	// comes straight from whatever somebody typed into a registration form.
	if err := noHeaderInjection("recipient", to); err != nil {
		return err
	}
	if err := noHeaderInjection("subject", subject); err != nil {
		return err
	}

	message := s.compose(to, subject, body)

	client, err := s.dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if s.encryption == EncryptionStartTLS {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return fmt.Errorf("email: smtp: %s does not offer STARTTLS", s.addr)
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.serverName, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("email: smtp: starttls: %w", err)
		}
	}

	if s.auth != nil {
		if err := client.Auth(s.auth); err != nil {
			// net/smtp puts the server's response in the error and never the credential.
			return fmt.Errorf("email: smtp: authenticating: %w", err)
		}
	}

	if err := client.Mail(s.sender); err != nil {
		return fmt.Errorf("email: smtp: sender rejected: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("email: smtp: recipient rejected: %w", err)
	}

	// DotWriter converts a lone newline to CRLF and escapes a line that begins with a full
	// stop, which is why the message is composed with plain newlines above.
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: smtp: opening the message body: %w", err)
	}
	if _, err := w.Write([]byte(message)); err != nil {
		return fmt.Errorf("email: smtp: writing the message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: smtp: server rejected the message: %w", err)
	}

	// Quit rather than Close alone, so the server is told the session ended properly and
	// does not log a dropped connection for every message this service sends.
	if err := client.Quit(); err != nil {
		return fmt.Errorf("email: smtp: closing the session: %w", err)
	}
	return nil
}

// dial opens the connection and returns a client speaking on it.
func (s *SMTP) dial(ctx context.Context) (*smtp.Client, error) {
	dialer := &net.Dialer{Timeout: s.timeout}

	var (
		conn net.Conn
		err  error
	)
	if s.encryption == EncryptionImplicit {
		conn, err = tls.DialWithDialer(dialer, "tcp", s.addr,
			&tls.Config{ServerName: s.serverName, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", s.addr)
	}
	if err != nil {
		return nil, fmt.Errorf("email: smtp: dialling %s: %w", s.addr, err)
	}

	// The deadline is how the caller's cancellation reaches net/smtp, which takes no
	// context of its own. Whichever of the two is sooner wins.
	deadline := time.Now().Add(s.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, s.serverName)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("email: smtp: greeting from %s: %w", s.addr, err)
	}
	return client, nil
}

// compose builds the RFC 5322 message.
func (s *SMTP) compose(to, subject, body string) string {
	var b strings.Builder
	b.WriteString("From: " + s.sender + "\n")
	b.WriteString("To: " + to + "\n")
	// RFC 2047 encoded-word, so a subject containing anything outside ASCII survives. A
	// bare UTF-8 subject header is not legal and is mangled by enough clients to matter.
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\n")
	b.WriteString("MIME-Version: 1.0\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\n")
	b.WriteString("\n")
	b.WriteString(body)
	return b.String()
}

// noHeaderInjection refuses a value that would break out of its header.
func noHeaderInjection(field, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("email: %s contains a line break, which would inject a header", field)
	}
	return nil
}
