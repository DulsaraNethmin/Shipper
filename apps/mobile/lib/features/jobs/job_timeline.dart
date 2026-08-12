import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';

/// Where a job has reached in its lifecycle, and what is still ahead of it (SHIP-77).
///
/// ## What the platform actually serves, and what it does not
///
/// `job_status_history` exists (SHIP-57a) and records every transition with its actor, its reason
/// and both clocks — but **no endpoint serves it**. `GET /v1/jobs/{id}` answers with the `Job`
/// schema, which is `additionalProperties: false` and carries a single `status` plus `created_at`,
/// `updated_at` and `expires_at`. There is no `history` array and no per-transition timestamp
/// anywhere on the wire today.
///
/// So this is a timeline derived from the current status rather than read from a record of
/// transitions, and the distinction is the whole design of this file:
///
/// - **It says where the job is**, which the response does establish.
/// - **It never says when an earlier step happened**, which the response does not. A step behind
///   the current one carries no timestamp, because inventing one — from `updated_at`, or by
///   spacing them evenly — would put a date in front of a customer that nothing produced.
///
/// When an endpoint serves the history, this function takes it and the steps behind the current
/// one gain their real times. Nothing else about the screen changes, which is the reason the
/// derivation lives in a pure function rather than inside a widget.
///
/// ## "Behind" is a claim about position, not about what happened
///
/// `Docs/02` §2 permits skips: `Awarded → En route to pickup` is a permitted move, so a job may
/// pass `Driver assigned` without a driver ever being nominated, and `Open → Awarded` skips
/// `Negotiating` whenever the first bid is the accepted one. A timeline that ticked every earlier
/// step as *done* would therefore assert things that did not happen.
///
/// [JobTimelinePosition.behind] means only that the job is past this point, which is true of a
/// skipped step as much as of a completed one. The screen renders it accordingly — no tick, no
/// date, no claim.

/// Where a step sits relative to where the job is now.
enum JobTimelinePosition {
  /// The job is past this point. **Not** a claim that this step happened — see the note above.
  behind,

  /// Where the job is now.
  current,

  /// Still to come, on the ordinary path.
  ahead,
}

/// One row of the timeline.
typedef JobTimelineStep = ({
  JobStatus status,
  JobTimelinePosition position,

  /// A timestamp the response genuinely carried for this step, or `null`.
  ///
  /// Raw, as the platform sent it. Formatting is the screen's, so this stays testable against the
  /// contract's own examples rather than against a rendered date.
  String? at,

  /// What [at] is — `Created`, `Last updated` — or `null` when there is no timestamp.
  ///
  /// Carried beside the value because the two are not interchangeable: `updated_at` is the last
  /// time the job changed at all, which is the current step's best available date and is not the
  /// instant the job entered that status.
  String? atLabel,
});

/// The ordinary path a job takes, in `Docs/02` §1's order.
///
/// **`Cancelled` and `Disputed` are deliberately absent.** They are exceptions rather than steps:
/// drawing them as upcoming would tell every customer their delivery is heading for a dispute.
/// A job that reaches one is handled by [jobTimeline]'s other branch.
///
/// Built from `JobStatus.values` rather than typed out, so the order is the enum's — which is the
/// document's — and there is no second list to disagree with the first.
final List<JobStatus> jobLifecycle = List<JobStatus>.unmodifiable(
  JobStatus.values.where(
    (status) =>
        status != JobStatus.cancelled &&
        status != JobStatus.disputed &&
        status != JobStatus.unknown,
  ),
);

/// The timeline for [job].
///
/// Two shapes, because a job has two kinds of status.
///
/// **On the ordinary path** — every step of [jobLifecycle], positioned against the current one.
/// The first step carries `created_at`, which is honest without qualification: the contract states
/// that a job is created as `draft`, so that timestamp *is* when the job entered the first step.
/// The current step carries `updated_at`, labelled as what it is.
///
/// **Off it** — `Cancelled`, `Disputed`, or a status this build has never heard of. Here the
/// client knows two things and no more: every job began as a draft, and this is where it is now.
/// What happened in between is exactly the history the platform does not serve, so the timeline
/// says the two things it knows and stops. A cancelled job showing eight greyed steps it never
/// reached would be a screen filling silence with shape.
List<JobTimelineStep> jobTimeline(Job job) {
  final index = jobLifecycle.indexOf(job.status);

  if (index == -1) {
    return <JobTimelineStep>[
      (
        status: JobStatus.draft,
        position: JobTimelinePosition.behind,
        at: job.createdAt,
        atLabel: 'Created',
      ),
      (
        status: job.status,
        position: JobTimelinePosition.current,
        at: job.updatedAt,
        atLabel: 'Last updated',
      ),
    ];
  }

  return <JobTimelineStep>[
    for (var i = 0; i < jobLifecycle.length; i++)
      (
        status: jobLifecycle[i],
        position: switch (i.compareTo(index)) {
          < 0 => JobTimelinePosition.behind,
          0 => JobTimelinePosition.current,
          _ => JobTimelinePosition.ahead,
        },
        // The first step's date is the job's creation, which is when it entered `draft`. The
        // current step's is the last time anything about the job changed. Every other step has
        // none, and none is what it gets.
        at: i == 0 ? job.createdAt : (i == index ? job.updatedAt : null),
        atLabel: i == 0 ? 'Created' : (i == index ? 'Last updated' : null),
      ),
  ];
}
