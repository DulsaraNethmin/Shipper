import 'package:shipper/features/jobs/job_status.dart';

/// What the customer can do to their own job right now (SHIP-77).
///
/// ## The app hides; the platform decides
///
/// `CLAUDE.md` and `Docs/07` §3 are the same sentence twice: **no authorisation decision is made
/// on the device.** So nothing in this file is a permission check. It is a list of what the
/// platform would accept, used to decide what is worth putting in front of somebody — and every
/// action it offers still goes to the platform, which refuses it if it disagrees.
///
/// That is not a technicality. `POST /v1/jobs/{id}/cancel` answers `409 jobs_not_cancellable`
/// when `Docs/02` §2 has no route from the job's status to `cancelled`, and the screen renders
/// that refusal and reloads the job. A customer whose app is showing a stale status — the job was
/// awarded on another device a minute ago — taps Cancel and is told what actually happened,
/// rather than being quietly right or quietly wrong.
///
/// ## Why the list is short, and what is missing from it
///
/// One action exists because one endpoint exists. `routes_golden.txt` serves exactly three
/// customer job routes today: read, edit-a-draft, and cancel. Editing a draft is the job wizard's
/// (SHIP-72…SHIP-75), and resuming an existing draft is SHIP-75 specifically — this screen must
/// **not** offer it, because the only path into the wizard today creates a *new* draft, and a
/// customer who tapped it from an existing one would end up with two.
///
/// Publishing a draft, extending an expiry, comparing bids and awarding one are all endpoints
/// that do not exist yet on this branch. They are named where they appear on screen rather than
/// silently omitted, because "there is nothing you can do here" and "this version cannot do it
/// yet" are different things to be told.
enum JobAction {
  /// End the job. `POST /v1/jobs/{id}/cancel` (SHIP-64).
  cancel;

  /// The button's label.
  ///
  /// "delivery" rather than "job", which is the word the rest of the customer's surface uses —
  /// the publish button says "Publish a delivery" and the list is headed "Your deliveries".
  String get label => switch (this) {
        JobAction.cancel => 'Cancel this delivery',
      };

  /// The key a test names this action by.
  ///
  /// Keyed by the enum's own name, so an action added here cannot be added without one.
  String get widgetKey => 'job-action-$name';
}

/// The actions worth offering for a job in [status].
///
/// `Docs/02` §2 permits `draft → cancelled` and `open`/`negotiating → cancelled`, and permits
/// nothing else towards `cancelled` from a customer. Once a provider has committed, ending the
/// job is a support matter and, after pickup, a dispute (`Docs/02` §6.2).
///
/// **The three statuses are read off that table rather than judged**, and the platform holds the
/// same table on its own side — `Permitted` in `internal/jobs/model.go`, which is what actually
/// decides. This list going stale therefore costs an offered button that gets refused, or a
/// missing button somebody expected. Neither is a security consequence, which is precisely the
/// property that makes it safe for the client to hold an opinion at all.
///
/// A pure function of the status, so "what can be done to a job in this state" is testable
/// without a screen — the same shape as `groupJobsByStatus` for the same reason.
List<JobAction> actionsFor(JobStatus status) {
  return switch (status) {
    JobStatus.draft ||
    JobStatus.open ||
    JobStatus.negotiating =>
      const <JobAction>[JobAction.cancel],
    _ => const <JobAction>[],
  };
}

/// Why there is nothing to offer for a job in [status], in words a customer can act on.
///
/// Returns `null` when [actionsFor] offers something.
///
/// **Not "no actions available"**, which reads as a fault in the app. A job that has been awarded
/// genuinely has no customer action in this release, and an awarded job that needs to end is a
/// support conversation rather than a button — saying so is the difference between somebody
/// contacting support and somebody tapping a greyed control.
String? noActionsBecause(JobStatus status) {
  if (actionsFor(status).isNotEmpty) return null;

  return switch (status) {
    JobStatus.cancelled => 'This delivery has been cancelled. Nothing further can be done to it.',
    JobStatus.completed => 'This delivery is complete.',
    JobStatus.disputed =>
      'This delivery is under review. An administrator will resolve it, and support can tell '
          'you where it stands.',
    JobStatus.delivered =>
      'The provider has recorded this as delivered. Confirming delivery and raising an issue '
          'arrive with the tracking screens.',
    JobStatus.unknown =>
      'This version of Shipper does not recognise the status of this delivery. Update the app '
          'to see it.',

    // Awarded through In transit: a provider has committed and the goods are moving.
    _ => 'A provider has been awarded this delivery. Ending it now is a support matter rather '
        'than something you can do here, and tracking arrives with the delivery screens.',
  };
}
