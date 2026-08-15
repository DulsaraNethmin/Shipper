-- SHIP-140: the device tokens a push notification is addressed to, bound to the device session
-- that registered them.
--
-- 000700 said this table would exist and why it did not: "It carries no device token. SHIP-140
-- owns those, behind SHIP-139's Firebase adapter, and neither exists". Both exist now, so
-- `push` stops being a value in a vocabulary with no address behind it.
--
-- # The grain is the device session, and that is what "clears on sign-out" means here
--
-- The *Done when* is "tokens bind to a device session and clear on sign-out", and the binding is
-- the whole mechanism rather than a label. `device_sessions` (000100) is one row per signed-in
-- device; a registration token from FCM addresses one installation of the app on one handset.
-- Those are the same thing, so the token hangs off the session rather than off the account.
--
-- **Sign-out clears it without anything writing to this table**, which is the part worth reading
-- carefully. 000104 makes revocation a *mark* rather than a delete — `revoked_at` is set and the
-- row stays, so that a device list can say "signed out three weeks ago" — so a cascade here would
-- never fire and would be a guarantee in name only. What clears the token instead is that nothing
-- resolves a push address without the session being live: the consumer asks whether these session
-- identifiers are still usable and addresses only the devices whose are. Revoking a session ends
-- its push delivery in the same transaction that revokes it, with no cross-domain write, no
-- second place for the two facts to disagree, and no change to `internal/identity` — which is a
-- domain this one may not import in either direction.
--
-- The client also deregisters explicitly (SHIP-143, and the endpoint below). That is a courtesy
-- that makes the row legible rather than the control: a handset that is switched off between the
-- sign-out and the next notification never makes the call, and the platform's guarantee cannot
-- depend on it.
--
-- # Why the token is not treated as a credential
--
-- It is not one. An FCM registration token addresses a handset; it authenticates nobody and
-- grants nothing. So it is stored in the clear, unlike device_sessions.refresh_token_hash — the
-- platform has to be able to *send* to it, which a hash makes impossible. What it is, is
-- identifying: it names one person's phone, so it is fingerprinted rather than logged whole
-- wherever it appears in a log line.

CREATE TABLE device_tokens (
    id uuid NOT NULL PRIMARY KEY,

    -- The account the device belongs to. Denormalised from device_sessions.user_id on purpose:
    -- the consumer resolves recipients as accounts and needs "the devices of these people" in
    -- one query over this table, and reaching through device_sessions for it would be a
    -- notifications query joining an identity table — which is the boundary ports.go exists to
    -- keep. It cannot drift, because the session a token binds to is the caller's own and the
    -- caller's account is what the token was minted for.
    user_id uuid NOT NULL,

    -- The signed-in device this token belongs to. See the header: this is the binding the
    -- *Done when* asks for, and the reason sign-out ends push delivery.
    device_session_id uuid NOT NULL,

    -- Which store the app came from. Docs/01 §4.5's push channel is iOS and Android, and FCM
    -- fronts APNs for the first — so this is not how a message is routed, it is what lets
    -- support answer "which handsets is this person signed in on".
    platform text NOT NULL,

    -- The FCM registration token. Stored in the clear because the platform has to send to it;
    -- see the header on why that is not the same decision as a refresh token's.
    token text NOT NULL,

    -- When the client last presented this token for this session. Re-registration is ordinary
    -- traffic — FCM rotates a token on reinstall, on restore to a new device, and periodically
    -- of its own accord — so the app registers at every launch and this moves.
    registered_at timestamptz NOT NULL,

    -- When the platform stopped addressing this device, and why. Marked rather than deleted,
    -- for 000104's reason: a row that vanishes leaves nobody able to tell a device that was
    -- deregistered from one that was never there, and SHIP-176 needs to be able to count
    -- rejections without them being invisible.
    revoked_at timestamptz,
    revoked_reason text,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- Docs/10 §3.4: text with a CHECK, paired with the Go constants in both directions by
    -- TestDeviceTokenPlatformConstraintMatchesTheGoConstants.
    CONSTRAINT ck_device_tokens_platform CHECK (platform IN ('ios', 'android')),

    -- Both columns move together or not at all — the pairing 000104 makes on device_sessions,
    -- and for the same reason: a row that says it was revoked for no reason, or carries a reason
    -- while still being live, is a state no code writes and every report has to handle.
    CONSTRAINT ck_device_tokens_revoked
        CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL)),

    -- Three ways a token stops being addressed, and each has a writer rather than being a value
    -- somebody might want later.
    CONSTRAINT ck_device_tokens_revoked_reason
        CHECK (revoked_reason IS NULL OR revoked_reason IN (
            'deregistered',   -- SHIP-140: the client asked, at sign-out
            'replaced',       -- SHIP-140: the same device registered a different token
            'rejected'        -- SHIP-139: the push provider said this token is dead
        )),

    -- A token that is empty addresses nothing, and a row holding one would be claimed and fail
    -- on every dispatch pass. The upper bound is a sanity limit rather than a specification:
    -- FCM's tokens are around 160 characters today and the format has changed twice.
    CONSTRAINT ck_device_tokens_token CHECK (char_length(token) BETWEEN 1 AND 4096),

    -- ON DELETE RESTRICT throughout, per Docs/10 §3.3. SHIP-171 pseudonymises rather than
    -- deletes, so a cascade here would be a path by which a device disappears without anybody
    -- having signed out — the same argument 000100 makes about sessions.
    CONSTRAINT fk_device_tokens_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT fk_device_tokens_session
        FOREIGN KEY (device_session_id) REFERENCES device_sessions (id) ON DELETE RESTRICT
);

-- One live token per signed-in device.
--
-- This is what makes re-registration idempotent without the handler remembering anything: an app
-- that registers a new token at launch revokes the row it had and inserts one, and a second
-- process racing it is refused by the index rather than leaving the device addressed twice.
-- Partial on revoked_at, so the history of a device's tokens stays readable.
CREATE UNIQUE INDEX uq_device_tokens_live_session
    ON device_tokens (device_session_id) WHERE revoked_at IS NULL;

-- One live row per token, across every account.
--
-- FCM reissues a token to a handset that has been restored from another device's backup, so the
-- same string can legitimately arrive from a second session — and the platform must then address
-- one device, not two. Without this, a push meant for the previous owner reaches the new one.
CREATE UNIQUE INDEX uq_device_tokens_live_token
    ON device_tokens (token) WHERE revoked_at IS NULL;

-- The consumer's read: the live devices of the people a rule resolved to. Partial on exactly its
-- predicate, for the reason idx_notifications_undelivered is (000700).
CREATE INDEX idx_device_tokens_user_live
    ON device_tokens (user_id) WHERE revoked_at IS NULL;

-- Docs/10 §3.3: every foreign key is indexed. The partial index above does not serve this one,
-- and this is also the read that answers "what is registered against this session".
CREATE INDEX idx_device_tokens_session
    ON device_tokens (device_session_id);

CREATE TRIGGER device_tokens_set_updated_at
    BEFORE UPDATE ON device_tokens
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE device_tokens IS
    'One live FCM registration token per signed-in device (SHIP-140). Bound to device_sessions, so revoking a session ends push delivery to it with no write here.';
COMMENT ON COLUMN device_tokens.token IS
    'An FCM registration token. It addresses a handset and authenticates nobody, which is why it is stored in the clear and a refresh token is not.';
COMMENT ON COLUMN device_tokens.revoked_reason IS
    'Why the platform stopped addressing this device — the client asked, the device registered a different token, or the push provider said it was dead.';

-- ---------------------------------------------------------------------------------------
-- The deduplication key, now that a recipient can be reachable at more than one address.
--
-- 000700 wrote `uq_notifications_event_recipient_channel` — one notification per person per
-- channel per event — and that was exactly right while every channel had one address per person.
-- Push does not: a customer signed in on a phone and a tablet has two live device tokens, and
-- both have to be told. Under the old index the second row is refused by ON CONFLICT DO NOTHING
-- and the tablet is never notified, silently and for every event.
--
-- **The fix is deliberately not "add address to the existing index".** That would also change the
-- rule for email, where the address is resolved from `users` at write time — so a person who
-- changed their email address between two deliveries of one event would receive it twice, which
-- is precisely the duplicate uq_notifications_event_recipient_channel exists to prevent. Two
-- partial indexes state the two rules exactly:
--
--   email and sms   one notification per person per channel per event, whatever the address
--   push            one notification per device per event
--
-- The idempotence SHIP-137 rests on is unchanged in both cases: a redelivered event resolves the
-- same people and the same device tokens, so every insert is refused and the consumer writes
-- nothing. It is still the database that knows, not the consumer.

DROP INDEX uq_notifications_event_recipient_channel;

CREATE UNIQUE INDEX uq_notifications_event_recipient_channel
    ON notifications (event_id, recipient_id, channel) WHERE channel <> 'push';

CREATE UNIQUE INDEX uq_notifications_event_recipient_device
    ON notifications (event_id, recipient_id, address) WHERE channel = 'push';

COMMENT ON INDEX uq_notifications_event_recipient_channel IS
    'One notification per person per channel per event, for the channels with one address per person (SHIP-137).';
COMMENT ON INDEX uq_notifications_event_recipient_device IS
    'One push per device per event (SHIP-140). A person signed in on two handsets is two rows, and both are the same notification.';
