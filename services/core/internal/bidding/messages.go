package bidding

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// Job-scoped messaging between a customer and a provider (SHIP-97).
//
// Docs/09's *Done when* is "messages attach to a job and are visible only to its two parties and
// admins", and Docs/11 §6 records why the ticket exists at all: "negotiation happens through
// counter-offers alone. Workable, and it keeps conversations structured, but parties will want to
// ask questions." A counter-offer is a price and two dates; "is there a lift?" is not.
//
// # A conversation is a pair, not a job, and that is the privacy rule rather than a data-model taste
//
// An Open job carries offers from several providers at once. Anything keyed on the job alone would
// put all of them in one room with the customer and with each other, and Docs/01 §4.3's second line
// — "treat provider bid price as private from competing providers" — would then be broken by prose
// rather than by a field. So a conversation is `(job_id, provider_id)`, which is exactly what a
// negotiation is, and migration 000506 stores it that way.
//
// **The read path is what enforces it, and it enforces it by construction.** [postgresStore.messages]
// selects on the pair, and the pair comes from the offer the caller reached the conversation through
// rather than from anything the client sent. There is no parameter that widens it.
//
// # The three readers are Docs/02 §4's, resolved by the code that already resolves them
//
// This file adds no audience rule. [Service.audienceFor] is SHIP-96's, written for the bid history,
// and it answers the identical question — "which of Docs/02 §4's three readers is this caller for
// this negotiation" — because a conversation and a chain are attached to the same pair. So an
// administrator reading a conversation is one `Viewer.Administrator` away, in a package that already
// has the branch.
//
// **What is missing is a credential, not a rule, and that is a fact about the auth class rather than
// a stub.** `authctx.Subject` cannot carry an administrator: Docs/06 §5.2 and SHIP-147 make admin
// sign-in a separate system a user token cannot reach, so `cmd/api` builds every [Viewer] with
// `Administrator` false and there is nothing in this platform that could make it true. SHIP-152 is
// where an administrator's read of a negotiation would be served; what it needs from this package is
// one field on a struct. visibility.go carries the same note for the chain and this is the second
// endpoint behind it.
//
// # Messaging follows the relationship, not the offer, and there is no status gate anywhere here
//
// Nothing below reads a bid's status. **The most useful moment for a customer to ask a question is
// after the award**, when the offer that got them there is `Accepted` and every other one is
// `Rejected` — a rule that closed the conversation when the negotiation closed would shut it exactly
// when the two parties finally have something to arrange. Docs/02 §4 keeps the chain readable after
// a negotiation ends for the same reason, and [Negotiation.CustomerOf] is documented as answering
// "whatever the job's status" precisely so a party keeps their access afterwards.
//
// The offer named in the URL is therefore how a caller *reaches* a conversation and not what the
// conversation is about. 000506 records no `bid_id` and says why: one pair of parties can hold
// several chains on one job, and a conversation that restarted with each of them would lose the
// question the answer was to.
//
// # The budget is not here, and the reason is different from every other file in this package
//
// Everywhere else the invariant is structural: no shape in this package carries anything of the job
// beyond its identifier, so there is no budget field to redact. That is still true of [Message] —
// and it is not the whole answer here, because this is the first response in the domain that carries
// **free text**. Wave 10 and wave 11 both established that a disclosure with no field, no value and
// no digit defeats a closed key set, a word search, a value search and an AST walk alike.
//
// Two things follow, and they are different. **The platform introduces no prose into this response at
// all** — every string in a rendered message is either a JSON key, an identifier, an instant, a party
// name, or the body a party typed — and `TestNothingTheCustomerTypedIsAddedToByThePlatform` holds
// that word by word over rendered output rather than by inspecting the shape. **And a party's own
// words are their own to choose**: a customer who types their budget into a message has disclosed it
// deliberately, which is not the invariant. The invariant is about what the platform exposes, and the
// platform exposes nothing.

// Message is one thing one party said to the other about a job (SHIP-97).
//
// # It carries the negotiation and the author separately, which is [Bid]'s shape
//
// `ProviderID` is *which conversation this is*, and `SentBy` is *who wrote it*. Reading them as one
// field is the mistake 000502 corrected on `bids`, and the correction is copied here rather than
// rediscovered.
//
// There is no sender identifier and no customer identifier. Both callers who can obtain a message
// already know which provider the negotiation is with and which of them they are, which is
// [bidResponse]'s own argument for leaving `ProviderID` off the wire.
type Message struct {
	ID    uuid.UUID
	JobID uuid.UUID

	// ProviderID is the provider the conversation is with, whichever party wrote this message.
	ProviderID uuid.UUID

	// SentBy is the party who wrote it.
	SentBy Party

	// Body is what they wrote, exactly as they wrote it. Trimmed of surrounding whitespace on the
	// way in ([Note.validate]) and never otherwise altered: this is somebody's sentence, and a
	// platform that edited it would be putting words in their mouth.
	Body string

	// Key is the idempotency key the message was sent under.
	//
	// Stored on the row rather than left to the middleware, for the reason [Offer]'s is: Redis makes
	// a retry fast and the column makes it correct. It is never rendered — see [messageResponse].
	Key string

	CreatedAt time.Time
}

// Note is a message somebody is trying to send.
//
// A command rather than a [Message], for [Offer]'s reason: the identifier, the conversation and the
// author are the platform's to decide, and a caller who could name them would be naming the party
// they are writing as.
type Note struct {
	Body string
	Key  string
}

// validate is every rule a note has to satisfy before anything is written.
//
// In the domain rather than in the handler (Docs/10 §4.6), so the same rules apply to a caller who
// arrived some other way. The trim is here rather than at the edge for the same reason: a message of
// three spaces is empty, and which layer noticed that must not change the answer.
//
// **[maxMessageLen] is reused rather than copied.** It is 000501's bound on a bid's `message` and
// `ck_job_messages_body` (000506) is the same number deliberately: the same kind of text, written by
// the same two people, in the same negotiation. A second constant would be a second opinion, and the
// day somebody raised one of them the other would stay where it was.
func (n Note) validate() (Note, error) {
	var e validate.Errors

	n.Body = strings.TrimSpace(n.Body)
	switch {
	case n.Body == "":
		e.Add("body", validate.CodeRequired, "Write something to send.")
	case len(n.Body) > maxMessageLen:
		e.Add("body", validate.CodeTooLong, "Keep this to %d characters or fewer.", maxMessageLen)
	}

	if err := e.Err(); err != nil {
		return Note{}, err
	}
	return n, nil
}

// MessageQuery is how a client asks for one page of a conversation.
//
// No filter, unlike [BidQuery]. A conversation is read forward from wherever the reader got to, and
// there is nothing in a message to narrow by that a client could not do itself — no status, no
// author worth filtering on when there are two of them, and a text search over somebody's own
// conversation is a screen nobody has asked for.
type MessageQuery struct {
	// Limit is the page size. Zero means the configured default.
	Limit int

	// After is the position the previous page ended at.
	After MessageCursor
}

// MessageCursor is a position in a conversation: the ordering key of the last row of a page.
//
// Two fields for [BidCursor]'s reason, and the tie is likelier here than anywhere else in this
// domain: two parties answering each other inside one millisecond is the ordinary case rather than
// an edge one, and a cursor that could not break it would repeat or drop a message at exactly the
// page boundary.
type MessageCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

func (c MessageCursor) IsZero() bool { return c.ID == uuid.Nil }

// MessagePage is one page of a conversation, **oldest first**.
//
// The opposite of [BidPage] and the same as [Service.Chain]'s. A work queue is read newest first
// because the newest matters most; a conversation is read forward or it is not a conversation.
type MessagePage struct {
	Messages []Message

	// Next is the cursor for the following page, zero when this is the last one.
	Next MessageCursor

	// HasMore says whether Next names anything.
	HasMore bool
}

// SendMessage records one party's message to the other (SHIP-97).
//
// # Who may write, decided by the same function that decides who may counter
//
// [Service.reachableBid] resolves the offer named in the path, refuses a caller who is neither party
// with the 404 a missing bid gets, and reports which side they are on. That is the whole
// authorisation rule, and reusing it rather than writing a second one is the point: SHIP-87 already
// argued that two authorisation rules for two callers "would have differed the first time one of
// them was corrected".
//
// It is read without a lock, because nothing here reads a status and decides against it. The two
// facts it rests on — `bids.provider_id` and `jobs.customer_id` — are immutable for the life of the
// row, so there is no window for a concurrent write to invalidate the answer. That is why this
// method does not require a transaction, exactly as [Service.PlaceBid] does not and for the same
// reason: correctness here is `ON CONFLICT`'s, which holds statement by statement.
//
// # `created` is false when this key had already sent this message
//
// The same three-step retry [Service.PlaceBid] runs, and it is worth having in full rather than
// trusting the middleware. SHIP-15 replays a stored *response* while its Redis entry lives; the
// column replays the *row* forever. A phone that sent a message, lost its connection and retried an
// hour later outlives any TTL worth setting — and a second copy of a message is worse than a second
// copy of most things, because the other party sees it and cannot tell which sending was the
// mistake.
//
// The read after `ON CONFLICT` declines is the concurrent case rather than a belt-and-braces repeat:
// at READ COMMITTED the second statement takes a fresh snapshot, so the row the winner committed is
// visible to it.
func (s *Service) SendMessage(
	ctx context.Context,
	r db.Runner,
	callerID, jobID, bidID uuid.UUID,
	n Note,
) (Message, bool, error) {
	note, err := n.validate()
	if err != nil {
		return Message{}, false, err
	}
	if note.Key == "" {
		// Unreachable through the served router — SHIP-15's middleware refuses a state-changing
		// request without a key — and checked all the same, for [ErrNoIdempotencyKey]'s reason: the
		// key is a column, and a message stored without one is a message a later retry cannot be
		// matched to.
		return Message{}, false, fmt.Errorf("bidding: messaging on %s: %w", jobID, ErrNoIdempotencyKey)
	}

	reached, err := s.reachableBid(ctx, r, callerID, jobID, bidID, false)
	if err != nil {
		return Message{}, false, err
	}

	// The ordinary retry: this key already wrote in this conversation, from this side.
	if existing, found, err := s.store.messageSentUnder(
		ctx, r, reached.bid.JobID, reached.bid.ProviderID, reached.party, note.Key); err != nil {
		return Message{}, false, err
	} else if found {
		return existing, false, nil
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Message{}, false, fmt.Errorf("bidding: generating a message id: %w", err)
	}

	written, created, err := s.store.insertMessage(ctx, r, Message{
		ID:         id,
		JobID:      reached.bid.JobID,
		ProviderID: reached.bid.ProviderID,
		SentBy:     reached.party,
		Body:       note.Body,
		Key:        note.Key,
	})
	if err != nil {
		return Message{}, false, err
	}
	if created {
		return written, true, nil
	}

	// The concurrent duplicate: another request under this key committed while this one was in the
	// statement. Its row is what answers.
	raced, found, err := s.store.messageSentUnder(
		ctx, r, reached.bid.JobID, reached.bid.ProviderID, reached.party, note.Key)
	if err != nil {
		return Message{}, false, err
	}
	if !found {
		return Message{}, false, fmt.Errorf(
			"bidding: %s wrote nothing on %s and no row carries the key", note.Key, jobID)
	}
	return raced, false, nil
}

// Messages reads one conversation, oldest first, for one of Docs/02 §4's three readers (SHIP-97).
//
// # It answers the audience as well as the page, exactly as [Service.Chain] does
//
// The caller is told *which* reader the platform decided they are, not merely that they were let in.
// SHIP-96's argument holds here unchanged: the reason a caller may read matters to more than the
// yes-or-no, and a later ticket asking "which of them is this" wants an answer rather than three
// predicates to re-derive.
//
// **That is also the whole of the administrative half.** An administrator is a [Viewer] with
// `Administrator` set; nothing else in this method changes, because reachability is what differs
// between the three readers and not what a row shows them. `cmd/api` cannot set the flag today — see
// the file header — so the clause is met in the domain and unreachable from the wire, which is
// recorded rather than worked around.
//
// # No transaction and no lock
//
// A read, and a message that arrived under it would produce a shorter page rather than an
// inconsistent one — the reading [Service.Chain] and [Service.Bids] both take.
func (s *Service) Messages(
	ctx context.Context,
	r db.Runner,
	viewer Viewer,
	jobID, bidID uuid.UUID,
	q MessageQuery,
) (MessagePage, Audience, error) {
	if bidID == uuid.Nil || jobID == uuid.Nil || viewer.ID == uuid.Nil {
		return MessagePage{}, AudienceNone,
			fmt.Errorf("bidding: %s on %s: %w", bidID, jobID, ErrBidNotFound)
	}

	bid, err := s.store.readBid(ctx, r, bidID)
	if err != nil {
		return MessagePage{}, AudienceNone, err
	}
	if bid.JobID != jobID {
		// The job in the path is compared for [Service.reachableBid]'s reason: a bid is addressed
		// under its own job, so a client pairing a real bid with the wrong one is naming something
		// that does not exist.
		return MessagePage{}, AudienceNone,
			fmt.Errorf("bidding: %s is not on %s: %w", bidID, jobID, ErrBidNotFound)
	}

	audience, err := s.audienceFor(ctx, r, viewer, bid)
	if err != nil {
		return MessagePage{}, AudienceNone, err
	}
	if !audience.permitted() {
		// The 404 a bid that does not exist gets, byte-identically. A competing provider who could
		// tell "not for you" from "no such thing" would have confirmed that a negotiation exists on
		// a job they are bidding against, which is the disclosure visibility.go's fourth rule is for.
		return MessagePage{}, AudienceNone, fmt.Errorf(
			"bidding: %s is none of Docs/02 §4's readers of %s: %w", viewer.ID, bidID, ErrNotBidOwner)
	}

	limit := q.Limit
	switch {
	case limit <= 0:
		limit = pagination.DefaultLimit
	case limit > pagination.MaxLimit:
		limit = pagination.MaxLimit
	}

	// One more row than was asked for, which answers "is there another page" without a second query.
	// The extra is dropped below and never reaches a caller.
	found, err := s.store.messages(ctx, r, bid.JobID, bid.ProviderID, q.After, limit+1)
	if err != nil {
		return MessagePage{}, AudienceNone, err
	}

	page := MessagePage{Messages: found}
	if len(found) > limit {
		page.Messages = found[:limit]

		last := page.Messages[limit-1]
		page.Next = MessageCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		page.HasMore = true
	}
	return page, audience, nil
}
