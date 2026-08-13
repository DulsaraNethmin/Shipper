import 'dart:async';

/// One pending wake-up, held by the sync worker (SHIP-125).
///
/// A seam over `Timer` for two reasons, and only the second is about testing.
///
/// The first is that **the worker must never hold more than one**. Every trigger arms a wake-up
/// for the next moment an operation may be tried, so a worker that armed rather than re-armed
/// would accumulate one timer per trigger and drain N times over. [arm] replacing whatever was
/// pending makes that structural instead of remembered.
///
/// The second is that "the worker sleeps until the earliest stored `next_attempt_at`" is the whole
/// of its scheduling policy, and the only way to assert it is to read the delay it asked for. A
/// test with a real `Timer` would have to wait out a backoff to find out whether the backoff was
/// right, which is a slow test that proves less.
abstract interface class SyncAlarm {
  /// Arranges for [fire] after [delay], **replacing** any wake-up already pending.
  void arm(Duration delay, void Function() fire);

  /// Cancels the pending wake-up, if there is one.
  void disarm();
}

/// The real one.
final class TimerAlarm implements SyncAlarm {
  Timer? _timer;

  @override
  void arm(Duration delay, void Function() fire) {
    _timer?.cancel();
    // A delay already in the past is a wake-up that is due now rather than one to skip.
    _timer = Timer(delay.isNegative ? Duration.zero : delay, fire);
  }

  @override
  void disarm() {
    _timer?.cancel();
    _timer = null;
  }
}
