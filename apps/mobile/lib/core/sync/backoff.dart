/// How long a failed operation waits before the sync worker offers it to the platform again
/// (SHIP-125).
///
/// A pure function of one number — how many attempts the operation has already had — so that the
/// policy can be read, argued with and tested without a worker, a queue or a clock. The worker
/// stores the result through `OperationQueue.release(id, nextAttemptAt:)`, which is what makes
/// the wait **durable**: a backoff held in memory is no backoff at all on a handset, which is
/// restarted far more often than a server.
///
/// ## The four numbers, and why each is what it is
///
/// **Base — five seconds.** The first failure is far more often a blip than an outage: a lift, a
/// tunnel mouth, a tower handover. Five seconds is short enough that a driver who taps and pockets
/// the phone never learns there was a problem, and long enough that a genuinely dead link is not
/// probed sixty times a minute.
///
/// **Ceiling — five minutes, and it is the most consequential of the four.** `Docs/07` §4 has the
/// driver record and move on, so the number that matters is *how long after signal returns the
/// work is still sitting on the phone* — and with nothing able to shorten a stored wait (see
/// below), that number **is** the ceiling. Five minutes is the worst case a driver can be asked to
/// accept while watching an indicator that says their work is not recorded yet. It also keeps
/// `Docs/02` §3.1's ladder honest: an operation still unsynced at the four-hour nudge has been
/// tried at least forty-eight times in the preceding four hours, so the nudge means "there is
/// genuinely no signal here" rather than "the worker was asleep". An hour would have been cheaper
/// and would have meant a driver standing under a clear sky for fifty-five minutes with a full
/// bar of signal and an indicator that would not clear.
///
/// **Jitter — equal jitter, half fixed and half spread.** Without it every queued operation on
/// every handset in a region retries in the same instant after a mast comes back, which is the
/// thundering herd a ceiling does not save you from — the ceiling decides *how often*, the jitter
/// decides *whether they arrive together*. Equal jitter rather than full jitter (`0` to the whole
/// delay) because on a handset the floor is worth keeping: full jitter's short draws retry
/// pointlessly early against a link that has not changed, and it buys a spread this application
/// does not need. Half the nominal delay is never skipped, and the other half is spread evenly.
///
/// **What resets it — nothing does, and that is deliberate.** SHIP-124's seam can *set* a wait
/// and cannot clear one: `release` writes `next_attempt_at` and no method takes it back. That is
/// the right shape, because a worker able to pull an operation forward is a worker able to reorder
/// the sequence its ordering key exists to preserve. So the only reset is the operation leaving
/// the queue — accepted and completed, or refused and quarantined — and a new operation starts at
/// zero. Which is exactly why the ceiling is small: **the ceiling is the reset.**
final class Backoff {
  /// [base] must be above zero — a zero base is a retry loop — and [ceiling] at or above it.
  ///
  /// Neither is asserted, because `Duration`'s comparison operators are not constant expressions
  /// and this constructor has to be `const` to be a parameter default. `after` holds to both
  /// anyway: a ceiling below the base clamps the very first wait, and a zero base produces zero
  /// waits, so both are visible in the first test that looks rather than silent.
  const Backoff({
    this.base = const Duration(seconds: 5),
    this.ceiling = const Duration(minutes: 5),
  });

  /// The nominal wait after the first failed attempt.
  final Duration base;

  /// The longest nominal wait, and so the longest an operation can sit on the handset after
  /// signal returns.
  ///
  /// **A backoff with no ceiling is an operation that has stopped retrying** — doubling reaches a
  /// day within sixteen failures, which for a delivery update is indistinguishable from having
  /// been dropped.
  final Duration ceiling;

  /// The wait after [attempts] attempts have been made and have failed.
  ///
  /// [attempts] is `QueuedOperation.attempts`, which `claim()` has already incremented — so the
  /// first failure arrives here as `1` and waits [base]. Values below one are treated as one
  /// rather than rejected: a row written by another build is not worth throwing over.
  ///
  /// [roll] is a value in `[0, 1]`, supplied by the caller so that a test can name the delay it is
  /// asserting on instead of matching a range. It is ordinary randomness, not
  /// `Random.secure` — nothing is being kept unguessable here, unlike an idempotency key.
  Duration after(int attempts, {required double roll}) {
    final doublings = (attempts < 1 ? 1 : attempts) - 1;

    // Doubled in a loop that stops at the ceiling rather than computing `base * 2^n` and clamping
    // afterwards: an operation left offline overnight reaches attempt counts where the shift
    // overflows, and an overflowed nominal delay clamps to whatever sign it landed on.
    var nominal = base;
    for (var i = 0; i < doublings && nominal < ceiling; i++) {
      nominal *= 2;
    }
    if (nominal > ceiling) nominal = ceiling;

    final fixed = nominal ~/ 2;
    return fixed + (nominal - fixed) * roll.clamp(0.0, 1.0);
  }
}
