package email

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTP is a single-session SMTP server, enough of RFC 5321 to accept one message.
//
// A real listener speaking the real protocol rather than a mock of the client, because what
// is worth testing here is the bytes that reach a server — the header block, the CRLF line
// endings and the terminating dot are exactly what a mock would assert into existence
// rather than verify.
type fakeSMTP struct {
	t        *testing.T
	listener net.Listener
	mu       sync.Mutex
	received string
	from     string
	to       string
	done     chan struct{}
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	s := &fakeSMTP{t: t, listener: listener, done: make(chan struct{})}
	go s.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return s
}

func (s *fakeSMTP) addr() (host string, port int) {
	a := s.listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port
}

func (s *fakeSMTP) serve() {
	defer close(s.done)
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	reader := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }

	write("220 fake ESMTP")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"):
			// No STARTTLS and no AUTH advertised: this server is the unencrypted local
			// catcher the "none" mode exists for.
			write("250-fake greets you")
			write("250 8BITMIME")
		case strings.HasPrefix(command, "HELO"):
			write("250 fake greets you")
		case strings.HasPrefix(command, "MAIL FROM"):
			s.mu.Lock()
			s.from = strings.TrimSpace(line)
			s.mu.Unlock()
			write("250 OK")
		case strings.HasPrefix(command, "RCPT TO"):
			s.mu.Lock()
			s.to = strings.TrimSpace(line)
			s.mu.Unlock()
			write("250 OK")
		case strings.HasPrefix(command, "DATA"):
			write("354 send it")
			var body strings.Builder
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if dataLine == ".\r\n" {
					break
				}
				body.WriteString(dataLine)
			}
			s.mu.Lock()
			s.received = body.String()
			s.mu.Unlock()
			write("250 queued")
		case strings.HasPrefix(command, "QUIT"):
			write("221 bye")
			return
		default:
			write("250 OK")
		}
	}
}

func (s *fakeSMTP) message() string {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.received
}

func (s *fakeSMTP) envelope() (from, to string) {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.from, s.to
}

// newTestSMTP builds a sender pointed at the fake, with no credential and no encryption.
func newTestSMTP(t *testing.T, server *fakeSMTP) *SMTP {
	t.Helper()
	host, port := server.addr()
	sender, err := NewSMTP(SMTPOptions{
		Host:       host,
		Port:       port,
		Sender:     "no-reply@shipper.com.au",
		Encryption: EncryptionNone,
	})
	if err != nil {
		t.Fatalf("NewSMTP: %v", err)
	}
	return sender
}

// SHIP-187b: the SMTP transport delivers a message a mail server accepts.
func TestSMTPSendsAWellFormedMessage(t *testing.T) {
	server := newFakeSMTP(t)
	sender := newTestSMTP(t, server)

	if err := sender.Send(context.Background(),
		"driver@example.com", "Verify your email", "Your code is 123456."); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := server.message()
	for _, want := range []string{
		"From: no-reply@shipper.com.au",
		"To: driver@example.com",
		"Subject: Verify your email",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Your code is 123456.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("message does not contain %q\n--- message ---\n%s", want, got)
		}
	}

	// The header block ends at the first blank line, and the body must be after it. A
	// message whose body lands among the headers is delivered and displayed empty.
	headers, body, found := strings.Cut(got, "\r\n\r\n")
	if !found {
		t.Fatalf("no blank line separating headers from body:\n%s", got)
	}
	if strings.Contains(headers, "Your code is") {
		t.Errorf("the body is inside the header block:\n%s", headers)
	}
	if !strings.Contains(body, "Your code is 123456.") {
		t.Errorf("body = %q", body)
	}

	from, to := server.envelope()
	if !strings.Contains(from, "no-reply@shipper.com.au") {
		t.Errorf("MAIL FROM = %q", from)
	}
	if !strings.Contains(to, "driver@example.com") {
		t.Errorf("RCPT TO = %q", to)
	}
}

// A recipient carrying a line break would write its own headers into the message. The
// address is the one field here that arrives verbatim from a registration form.
func TestSMTPRefusesAHeaderInjection(t *testing.T) {
	server := newFakeSMTP(t)
	sender := newTestSMTP(t, server)

	cases := map[string]struct{ to, subject string }{
		"recipient smuggling a Bcc": {
			to:      "driver@example.com\r\nBcc: attacker@example.com",
			subject: "Verify your email",
		},
		"subject smuggling a header": {
			to:      "driver@example.com",
			subject: "Verify\nX-Priority: 1",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := sender.Send(context.Background(), c.to, c.subject, "body")
			if err == nil {
				t.Fatal("Send accepted a value containing a line break")
			}
			if !strings.Contains(err.Error(), "line break") {
				t.Errorf("error = %v, want it to name the line break", err)
			}
		})
	}
}

// A password on a clear connection is disclosed to every host on the path, and everything
// still works — which is why it has to be refused rather than warned about.
func TestNewSMTPRefusesACredentialWithoutEncryption(t *testing.T) {
	_, err := NewSMTP(SMTPOptions{
		Host:       "mail.example.com",
		Username:   "shipper",
		Password:   "hunter2",
		Sender:     "no-reply@shipper.com.au",
		Encryption: EncryptionNone,
	})
	if err == nil {
		t.Fatal("NewSMTP accepted a credential over an unencrypted connection")
	}
	if !strings.Contains(err.Error(), "unencrypted") {
		t.Errorf("error = %v, want it to explain the refusal", err)
	}
}

// Half a credential authenticates as nobody, and the symptom is the first send rather than
// the start-up — the same argument config.credentialBearingDefaults makes for the storage
// key and its identifier.
func TestNewSMTPRefusesHalfACredential(t *testing.T) {
	for name, opts := range map[string]SMTPOptions{
		"username alone": {Host: "mail.example.com", Sender: "a@b.com", Username: "shipper"},
		"password alone": {Host: "mail.example.com", Sender: "a@b.com", Password: "hunter2"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewSMTP(opts); err == nil {
				t.Fatal("NewSMTP accepted half a credential")
			}
		})
	}
}

func TestNewSMTPRefusesIncompleteOptions(t *testing.T) {
	for name, opts := range map[string]SMTPOptions{
		"no host":   {Sender: "a@b.com"},
		"no sender": {Host: "mail.example.com"},
		"bad mode":  {Host: "mail.example.com", Sender: "a@b.com", Encryption: "tls-ish"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewSMTP(opts); err == nil {
				t.Fatalf("NewSMTP accepted %s", name)
			}
		})
	}
}

// A bare UTF-8 subject header is not legal and is mangled by enough clients to matter.
func TestSMTPEncodesANonASCIISubject(t *testing.T) {
	server := newFakeSMTP(t)
	sender := newTestSMTP(t, server)

	if err := sender.Send(context.Background(),
		"driver@example.com", "Trip to Kalgoorlie – confirmed", "body"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := server.message()
	if strings.Contains(got, "Subject: Trip to Kalgoorlie – confirmed") {
		t.Error("the en dash was written into the header unencoded")
	}
	if !strings.Contains(got, "=?utf-8?q?") {
		t.Errorf("subject is not RFC 2047 encoded:\n%s", got)
	}
}
