// SHIP-77 — the two pure halves of "status timeline and available actions".
//
// Both are functions of the job's status and are tested as functions, for the reason SHIP-76 gave
// for `groupJobsByStatus`: where a job has reached, and what may be done to it, are facts about
// the data rather than about a layout. The widget tests then check that what these produce is
// what is drawn.
//
// The timeline's hardest property is the one it does *not* claim. Docs/02 §2 permits skips —
// `Awarded → En route to pickup` is a permitted move — so a step behind the current one may never
// have happened, and a client that ticked it with a date would be asserting something no response
// said. Several tests below exist only to hold that line.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/features/jobs/job_actions.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/job_timeline.dart';

import 'fake_jobs_repository.dart';

/// The statuses in a timeline, in the order it produced them.
List<JobStatus> _statuses(List<JobTimelineStep> steps) =>
    steps.map((step) => step.status).toList();

/// The one step positioned as current.
JobTimelineStep _current(List<JobTimelineStep> steps) =>
    steps.singleWhere((step) => step.position == JobTimelinePosition.current);

void main() {
  group('the lifecycle the timeline walks', () {
    test('is Docs/02 §1 in its own order, without the two exceptions', () {
      // Cancelled and Disputed are exceptions rather than steps: drawing them as upcoming would
      // tell every customer their delivery is heading for a dispute. `unknown` is not a platform
      // status at all.
      expect(jobLifecycle, <JobStatus>[
        JobStatus.draft,
        JobStatus.open,
        JobStatus.negotiating,
        JobStatus.awarded,
        JobStatus.driverAssigned,
        JobStatus.enRouteToPickup,
        JobStatus.pickedUp,
        JobStatus.inTransit,
        JobStatus.delivered,
        JobStatus.completed,
      ]);
    });

    test('is derived from the enum, so there is no second order to disagree with the first', () {
      // The order comes from JobStatus.values, which is declared in Docs/02 §1's order. A status
      // inserted in the right place there is walked in the right place here with nothing to edit.
      expect(
        jobLifecycle,
        JobStatus.values
            .where((s) => s != JobStatus.cancelled)
            .where((s) => s != JobStatus.disputed)
            .where((s) => s != JobStatus.unknown)
            .toList(),
      );
    });
  });

  group('a job on the ordinary path', () {
    test('shows every step, with the current one marked', () {
      final steps = jobTimeline(aJob(status: JobStatus.open));

      expect(_statuses(steps), jobLifecycle);
      expect(_current(steps).status, JobStatus.open);
    });

    test('everything before the current step is behind it and everything after is ahead', () {
      final steps = jobTimeline(aJob(status: JobStatus.pickedUp));
      final behind = steps.where((s) => s.position == JobTimelinePosition.behind);
      final ahead = steps.where((s) => s.position == JobTimelinePosition.ahead);

      expect(_statuses(behind.toList()), <JobStatus>[
        JobStatus.draft,
        JobStatus.open,
        JobStatus.negotiating,
        JobStatus.awarded,
        JobStatus.driverAssigned,
        JobStatus.enRouteToPickup,
      ]);
      expect(_statuses(ahead.toList()), <JobStatus>[
        JobStatus.inTransit,
        JobStatus.delivered,
        JobStatus.completed,
      ]);
    });

    test('a draft has nothing behind it', () {
      final steps = jobTimeline(aJob());

      expect(steps.where((s) => s.position == JobTimelinePosition.behind), isEmpty);
      expect(_current(steps).status, JobStatus.draft);
    });

    test('a completed job has nothing ahead of it', () {
      final steps = jobTimeline(aJob(status: JobStatus.completed));

      expect(steps.where((s) => s.position == JobTimelinePosition.ahead), isEmpty);
      expect(_current(steps).status, JobStatus.completed);
    });
  });

  group('the timestamps it will and will not claim', () {
    test('the first step carries the creation time, which is when the job entered it', () {
      // The contract states that a job is created as `draft`, so this one is honest without
      // qualification — unlike every other step's.
      final steps = jobTimeline(aJob(status: JobStatus.open, createdAt: '2026-08-11T03:30:00Z'));

      expect(steps.first.status, JobStatus.draft);
      expect(steps.first.at, '2026-08-11T03:30:00Z');
      expect(steps.first.atLabel, 'Created');
    });

    test('the current step carries updated_at, labelled as what it is', () {
      // `updated_at` is the last time the job changed at all. It is the best date the response
      // carries for where the job is now, and it is not the instant it entered that status —
      // which is why it is labelled rather than presented as a transition time.
      final steps = jobTimeline(
        aJob(status: JobStatus.awarded, createdAt: '2026-08-11T03:30:00Z')
            .copyWith(updatedAt: '2026-08-12T09:00:00Z'),
      );

      expect(_current(steps).at, '2026-08-12T09:00:00Z');
      expect(_current(steps).atLabel, 'Last updated');
    });

    test('a step that may have been skipped carries no time at all', () {
      // Docs/02 §2 permits `Awarded → En route to pickup`, so a job here may never have had a
      // driver nominated. "Past this point" is true; "this happened, on this date" is not, and
      // the second is what a timestamp would assert.
      final steps = jobTimeline(aJob(status: JobStatus.enRouteToPickup));
      final skippable = steps.singleWhere((s) => s.status == JobStatus.driverAssigned);

      expect(skippable.position, JobTimelinePosition.behind);
      expect(skippable.at, isNull);
      expect(skippable.atLabel, isNull);
    });

    test('no step between the first and the current one carries a time', () {
      final steps = jobTimeline(aJob(status: JobStatus.delivered));
      final middle = steps.skip(1).where((s) => s.position == JobTimelinePosition.behind);

      expect(middle, isNotEmpty);
      expect(middle.every((s) => s.at == null), isTrue);
    });

    test('a missing timestamp is an ordinary outcome, not a crash', () {
      // Every timestamp on the Job schema but `created_at` and `updated_at` is optional, and
      // Docs/07 §6 makes a required field a decode that throws — so the model treats all of them
      // as nullable and this has to as well.
      final steps = jobTimeline(
        aJob(status: JobStatus.open, createdAt: '').copyWith(updatedAt: null),
      );

      expect(steps.first.at, isEmpty);
      expect(_current(steps).at, isNull);
    });
  });

  group('a job off the ordinary path', () {
    test('a cancelled job shows where it began and where it ended, and nothing invented', () {
      // Two things the client knows: every job is created as a draft, and this is where it is
      // now. What happened in between is exactly the per-transition history no endpoint serves,
      // and eight greyed steps it may never have reached would be a screen filling silence.
      final steps = jobTimeline(
        aJob(status: JobStatus.cancelled, createdAt: '2026-08-11T03:30:00Z')
            .copyWith(updatedAt: '2026-08-13T22:00:00Z'),
      );

      expect(_statuses(steps), <JobStatus>[JobStatus.draft, JobStatus.cancelled]);
      expect(steps.first.position, JobTimelinePosition.behind);
      expect(steps.first.at, '2026-08-11T03:30:00Z');
      expect(_current(steps).status, JobStatus.cancelled);
      expect(_current(steps).at, '2026-08-13T22:00:00Z');
    });

    test('a disputed job takes the same shape', () {
      final steps = jobTimeline(aJob(status: JobStatus.disputed));

      expect(_statuses(steps), <JobStatus>[JobStatus.draft, JobStatus.disputed]);
    });

    test('a status this build has never heard of is shown, not dropped', () {
      // Docs/07 §6. A thirteenth status must not make a job's timeline vanish — the customer
      // would have no way of knowing anything was missing, and Dart has no over-the-air fix.
      final steps = jobTimeline(aJob(status: JobStatus.unknown));

      expect(_statuses(steps), <JobStatus>[JobStatus.draft, JobStatus.unknown]);
      expect(_current(steps).status, JobStatus.unknown);
    });
  });

  group('what the platform would permit', () {
    test('a job Docs/02 §2 has a customer route out of can be cancelled', () {
      for (final status in <JobStatus>[JobStatus.draft, JobStatus.open, JobStatus.negotiating]) {
        expect(actionsFor(status), <JobAction>[JobAction.cancel], reason: '$status');
      }
    });

    test('a job a provider has committed to offers nothing', () {
      // Docs/02 §6.2: once a bid is awarded, ending the job is a support matter, and after
      // pickup it is a dispute. The platform answers `jobs_not_cancellable` and a 409, and the
      // app not offering the button is a convenience on top of that rather than a substitute.
      const committed = <JobStatus>[
        JobStatus.awarded,
        JobStatus.driverAssigned,
        JobStatus.enRouteToPickup,
        JobStatus.pickedUp,
        JobStatus.inTransit,
        JobStatus.delivered,
        JobStatus.completed,
        JobStatus.cancelled,
        JobStatus.disputed,
        JobStatus.unknown,
      ];

      for (final status in committed) {
        expect(actionsFor(status), isEmpty, reason: '$status');
      }
    });

    test('every status either offers an action or says why it does not', () {
      // The case that would otherwise be silent: a status added to the enum and to no branch
      // here draws an empty section with no heading and no explanation, which reads as a screen
      // that failed to load rather than as a job nothing can be done to.
      for (final status in JobStatus.values) {
        final actions = actionsFor(status);
        final because = noActionsBecause(status);

        if (actions.isEmpty) {
          expect(because, isNotNull, reason: '$status offers nothing and explains nothing');
          expect(because, isNotEmpty, reason: '$status');
        } else {
          expect(because, isNull, reason: '$status offers something and explains it away');
        }
      }
    });

    test('every action has a key naming itself', () {
      for (final action in JobAction.values) {
        expect(action.widgetKey, 'job-action-${action.name}');
        expect(action.label, isNotEmpty);
      }
    });
  });
}
