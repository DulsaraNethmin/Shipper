package notifications

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/google/uuid"
)

// The message copy (SHIP-138): what a notification actually says, per channel.
//
// # The design is what a template may be handed, not what it may say
//
// SHIP-137 rendered a body by concatenating [Rule.Headline] with the job identifier, and its
// comment set the constraint this file inherits: "the day a renderer takes the job as a parameter
// is the day the rule becomes something a reviewer has to check by eye". Docs/01 §4.4 and SHIP-141
// forbid an address, a goods description or a full customer name in a notification, and 000700
// records that the rule holds because the renderer never reads the job.
//
// So these are real templates — text/template, parsed once, with paragraphs and a closing — and
// their input is [content], which has exactly two fields and neither of them comes from the event.
// A template referencing anything else fails to execute, and two tests turn that into a failing
// build rather than a message nobody sends until the day it matters.
//
// That is a stronger guarantee than searching rendered output for forbidden words, which is what
// this could have been: a search knows the words somebody told it about, and a closed input knows
// the fields that exist.
//
// # Why the copy is here and not in the routing table
//
// [Rule.Headline] stays, and it is what a push notification says. What this file adds is everything
// around it — a subject line, an opening, the job reference, the closing every transactional
// message needs — and those differ by channel while the headline does not.
//
// # Why not in the database
//
// CLAUDE.md puts "anything expected to change under operational pressure" server-side, and this is
// server-side already: the Go service is deployed, unlike the Flutter client. Copy in a table would
// be editable without a deploy and also without a review, which for the one text this platform
// sends to people who did not ask for it is the wrong trade. Docs/06 §5.3's list is category lists,
// validation limits, policy copy and feature switches; message templates are not on it.

// content is everything a template may be given.
//
// Two fields, and the point of the type is that there is no third.
//
// **Neither field comes from the event payload**, which is what makes SHIP-141 structural. JobID is
// an identifier — meaningless to anybody who cannot already open the job, which is exactly the
// property a locked screen needs. Headline is a literal typed into [Rules]: it is the sentence a
// product person approved, reproduced, never assembled.
//
// **Adding a field here is a decision about privacy, not about copy.** Whoever wants one should
// read SHIP-141 and 000700 first, and should expect to say what stops it being a street address.
type content struct {
	// JobID is the job this notification is about, rendered as its identifier.
	JobID string

	// Headline is [Rule.Headline] — a fixed literal from the routing table.
	Headline string
}

// The copy, per channel.
//
// Deliberately austere, and deliberately the same shape every time: what happened, which job, what
// to do about it. Docs/01 §4.5 keeps email "for records and for anything the user may need to
// retrieve later", so it says a little more than a push does — and still says nothing a push could
// not have said, because both are rendered from the same two fields.
const (
	// emailSubject carries the headline and the job, because a mailbox holding four of these
	// is a mailbox where they have to be distinguishable at a glance.
	emailSubject = `{{.Headline}} (job {{.JobID}})`

	emailBody = `{{.Headline}}

This message is about job {{.JobID}}.

Open the Shipper app to see the details and to act on it. This mailbox is not monitored, so
please do not reply.

— Shipper`

	// pushBody is what appears on a locked screen. One line, and not because a longer one
	// would not fit: a push renders in front of whoever is holding the phone, which is a
	// display nobody consented to (000700), and every extra sentence is another sentence
	// somebody has to check for an address.
	pushBody = `Job {{.JobID}}`

	// smsBody carries the headline itself, because a text message has no subject line to put
	// it in. No rule routes here today ([ChannelSMS]); the template exists so that a ticket
	// which decides otherwise adds a line to [Rules] and nothing else, which is the promise
	// that channel is held to.
	smsBody = `{{.Headline}} Job {{.JobID}}. Open the Shipper app for the details.`

	// headlineOnly is the subject on the two channels whose subject is the headline and
	// nothing else.
	headlineOnly = `{{.Headline}}`
)

// channelTemplate is one channel's pair, parsed and in source.
//
// Subject and body are separate templates because they are separate columns, and because a subject
// line with a newline in it is a header injection in most mail transports —
// TestNoSubjectCanCarryANewline holds that.
//
// **The sources are kept beside the parsed forms so a test can substitute into them by hand and
// demand the result byte-for-byte.** That is what turns "a template says nothing but its fixed text
// and these two values" from a claim into an assertion: every rule, on every channel, must render
// exactly the source with `{{.Headline}}` and `{{.JobID}}` replaced and nothing else changed. A
// template gaining a conditional, a function call or a third field fails it.
type channelTemplate struct {
	subject *template.Template
	body    *template.Template

	subjectSource string
	bodySource    string
}

// templates is the closed set, keyed by channel. Parsed once at init, and a parse failure panics.
//
// A template that does not parse is a message that can never be sent, inside a process whose whole
// purpose is sending messages. Discovering it at start-up is a failed deploy; discovering it at the
// first award is a customer who was never told their job had been taken.
//
// Complete over [Channels], which TestEveryChannelHasATemplate holds: a channel added to the
// vocabulary with no copy behind it is a row the dispatcher claims and can never complete.
var templates = map[Channel]channelTemplate{
	ChannelEmail: {
		subject:       template.Must(template.New("email.subject").Parse(emailSubject)),
		body:          template.Must(template.New("email.body").Parse(emailBody)),
		subjectSource: emailSubject,
		bodySource:    emailBody,
	},
	ChannelPush: {
		// The subject *is* the headline on a push: it is the bold line the handset shows,
		// and passing it through a template would be a slot around a literal.
		subject:       template.Must(template.New("push.subject").Parse(headlineOnly)),
		body:          template.Must(template.New("push.body").Parse(pushBody)),
		subjectSource: headlineOnly,
		bodySource:    pushBody,
	},
	ChannelSMS: {
		subject:       template.Must(template.New("sms.subject").Parse(headlineOnly)),
		body:          template.Must(template.New("sms.body").Parse(smsBody)),
		subjectSource: headlineOnly,
		bodySource:    smsBody,
	},
}

// Render is what a notification says on one channel.
//
// It returns an error rather than panicking, although every template here is executed against the
// same closed input by a test: [Consume] runs inside a transaction holding a Kafka partition's
// progress, and a panic there stops a consumer where an error stops one message.
//
// # It refuses to render copy that breaks SHIP-141's rules, and this is the last writer
//
// [Redactions] runs over the rendered subject and the rendered body, and a problem in either is
// [ErrRedacted] rather than a string somebody has to notice. **This is the only place a
// notification's text is written**: [Consume] renders and inserts in the same loop, so nothing
// reaches the `notifications` table that has not been through here.
//
// Both halves are checked, not only the body. On a push the *subject* is the bold line the handset
// shows above everything else, so it is the more visible of the two on the surface this ticket is
// about; a rule that guarded the body alone would guard the quieter half.
//
// Failing closed is the right trade even though it parks a Kafka partition. Every input here is a
// compile-time literal — the templates in this file and [Rule.Headline] in rules.go — so a redaction
// failure is a build defect that `TestNoRuleCanRenderCopyThatBreaksTheRedactionRules` catches before
// it is deployed. Reaching this branch at runtime means something is wrong that nobody predicted,
// and a notification nobody receives is recoverable where a street address on a stranger's lock
// screen is not.
func Render(channel Channel, rule Rule, jobID uuid.UUID) (subject, body string, err error) {
	tmpl, known := templates[channel]
	if !known {
		return "", "", fmt.Errorf("notifications: %q has no template", channel)
	}

	data := content{JobID: jobID.String(), Headline: rule.Headline}

	subject, err = execute(tmpl.subject, data)
	if err != nil {
		return "", "", err
	}
	body, err = execute(tmpl.body, data)
	if err != nil {
		return "", "", err
	}

	for _, part := range []struct {
		what string
		text string
	}{{"subject", subject}, {"body", body}} {
		if problems := Redactions(part.text); len(problems) > 0 {
			return "", "", fmt.Errorf("notifications: the %s %s on %s: %w",
				channel, part.what, strings.Join(problems, "; "), ErrRedacted)
		}
	}
	return subject, body, nil
}

func execute(t *template.Template, data content) (string, error) {
	var out strings.Builder
	if err := t.Execute(&out, data); err != nil {
		return "", fmt.Errorf("notifications: rendering %s: %w", t.Name(), err)
	}
	return out.String(), nil
}
