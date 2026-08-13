// SHIP-99 — "with filters", and the shape they had to take.
//
// GET /v1/jobs/open accepts `limit` and `cursor` and nothing else. The contract says so in as many
// words and says why: eligibility is the platform's decision, and a filter parameter would be a
// second place for that answer to be argued with (Docs/07 §3). It then names where the narrowing
// does belong — "a provider narrowing their own feed further is the client's business" — which is
// what this file tests.
//
// The property that keeps that safe is tested first and deliberately: the narrowing is
// **subtractive**. There is no arrangement of a filter that shows a provider a job the platform did
// not offer them, so a client-side filter cannot disagree with server-side eligibility. It can only
// hide.
//
// Tested as pure functions, exactly as `groupJobsByStatus` is for the customer's list: "which jobs
// match" is a fact about the data rather than about a layout, and a widget test asserting on cards
// would be testing both at once.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/open_job.dart';
import 'package:shipper/features/jobs/open_jobs_filter.dart';

import 'fake_open_jobs_repository.dart';

OpenJob _in(String state, {String id = 'j', JobStatus status = JobStatus.open}) {
  return anOpenJob(id: id, status: status, pickup: JobRegion(state: state, suburb: 'Somewhere'));
}

void main() {
  group('narrowing can only remove', () {
    test('an empty filter is every job, in the platform’s own order', () {
      final jobs = <OpenJob>[_in('NSW', id: 'a'), _in('VIC', id: 'b'), _in('QLD', id: 'c')];

      expect(narrowOpenJobs(jobs, const OpenJobsFilter()), same(jobs));
      expect(
        narrowOpenJobs(jobs, const OpenJobsFilter()).map((j) => j.id).toList(),
        <String>['a', 'b', 'c'],
      );
    });

    test('a narrowed feed is always a subset of what the platform offered', () {
      // The whole safety argument in one assertion. Whatever is selected, the result is a subset:
      // the filter has no way to add a job, so it has no way to show one the platform's four
      // eligibility checks refused.
      final jobs = <OpenJob>[
        _in('NSW', id: 'a'),
        _in('VIC', id: 'b', status: JobStatus.negotiating),
        _in('QLD', id: 'c'),
      ];

      final filters = <OpenJobsFilter>[
        const OpenJobsFilter(),
        const OpenJobsFilter(pickupStates: <String>{'NSW'}),
        const OpenJobsFilter(pickupStates: <String>{'NSW', 'VIC'}),
        const OpenJobsFilter(statuses: <JobStatus>{JobStatus.negotiating}),
        const OpenJobsFilter(pickupStates: <String>{'VIC'}, statuses: <JobStatus>{JobStatus.open}),
        // A state no job is in, which is reachable: a provider narrows to VIC on page one and
        // pulls to refresh into a page that has none.
        const OpenJobsFilter(pickupStates: <String>{'TAS'}),
      ];

      for (final filter in filters) {
        expect(
          narrowOpenJobs(jobs, filter).map((j) => j.id).toSet(),
          isA<Set<String>>().having(
            (ids) => ids.difference(jobs.map((j) => j.id).toSet()),
            'jobs the filter invented',
            isEmpty,
          ),
          reason: '$filter produced a job the platform never offered',
        );
      }
    });
  });

  group('the two facets', () {
    test('a pickup state narrows to that state', () {
      final jobs = <OpenJob>[_in('NSW', id: 'a'), _in('VIC', id: 'b'), _in('NSW', id: 'c')];

      expect(
        narrowOpenJobs(jobs, const OpenJobsFilter(pickupStates: <String>{'NSW'}))
            .map((j) => j.id)
            .toList(),
        <String>['a', 'c'],
      );
    });

    test('two states selected shows both, because a provider may run between them', () {
      final jobs = <OpenJob>[_in('NSW', id: 'a'), _in('VIC', id: 'b'), _in('QLD', id: 'c')];

      expect(
        narrowOpenJobs(jobs, const OpenJobsFilter(pickupStates: <String>{'NSW', 'VIC'}))
            .map((j) => j.id)
            .toList(),
        <String>['a', 'b'],
      );
    });

    test('a status narrows to jobs in it', () {
      // Docs/02 §1 keeps a negotiating job open to eligible bids, so this is a provider choosing
      // whether to price against company rather than the platform withholding anything.
      final jobs = <OpenJob>[
        _in('NSW', id: 'a'),
        _in('NSW', id: 'b', status: JobStatus.negotiating),
      ];

      expect(
        narrowOpenJobs(jobs, const OpenJobsFilter(statuses: <JobStatus>{JobStatus.negotiating}))
            .map((j) => j.id)
            .toList(),
        <String>['b'],
      );
    });

    test('the two facets are an intersection, not a union', () {
      final jobs = <OpenJob>[
        _in('NSW', id: 'a'),
        _in('NSW', id: 'b', status: JobStatus.negotiating),
        _in('VIC', id: 'c', status: JobStatus.negotiating),
      ];

      final filter = const OpenJobsFilter(
        pickupStates: <String>{'NSW'},
        statuses: <JobStatus>{JobStatus.negotiating},
      );

      expect(narrowOpenJobs(jobs, filter).map((j) => j.id).toList(), <String>['b']);
    });

    test('a selection nothing matches narrows to nothing rather than to everything', () {
      // The failure mode worth naming: a filter that silently fell back to "everything" when it
      // matched nothing would show a provider work in a state they had just excluded.
      final jobs = <OpenJob>[_in('NSW', id: 'a')];

      expect(narrowOpenJobs(jobs, const OpenJobsFilter(pickupStates: <String>{'TAS'})), isEmpty);
    });
  });

  group('toggling', () {
    test('adds what was absent and removes what was present', () {
      var filter = const OpenJobsFilter();

      filter = filter.togglePickupState('NSW');
      expect(filter.pickupStates, <String>{'NSW'});

      filter = filter.togglePickupState('VIC');
      expect(filter.pickupStates, <String>{'NSW', 'VIC'});

      filter = filter.togglePickupState('NSW');
      expect(filter.pickupStates, <String>{'VIC'});
    });

    test('the two facets do not disturb one another', () {
      final filter = const OpenJobsFilter()
          .togglePickupState('NSW')
          .toggleStatus(JobStatus.negotiating)
          .togglePickupState('VIC');

      expect(filter.pickupStates, <String>{'NSW', 'VIC'});
      expect(filter.statuses, <JobStatus>{JobStatus.negotiating});
    });

    test('clearing removes both', () {
      final filter = const OpenJobsFilter()
          .togglePickupState('NSW')
          .toggleStatus(JobStatus.open)
          .cleared();

      expect(filter.isEmpty, isTrue);
    });

    test('two filters holding the same selection are equal', () {
      // Freezed's copyWith compares by value, so a filter that was not value-equal would rebuild
      // the feed on every toggle that changed nothing.
      expect(
        const OpenJobsFilter(pickupStates: <String>{'NSW', 'VIC'}),
        const OpenJobsFilter(pickupStates: <String>{'VIC', 'NSW'}),
      );
      expect(
        const OpenJobsFilter(pickupStates: <String>{'NSW'}).hashCode,
        const OpenJobsFilter(pickupStates: <String>{'NSW'}).hashCode,
      );
    });
  });

  group('the options offered', () {
    test('the states come from the jobs, not from a list of the eight', () {
      // CLAUDE.md keeps anything that changes under operational pressure server-side, and Dart has
      // no over-the-air update path. A compiled-in list of states would be a vocabulary needing a
      // store release; these chips are a fact about this provider's feed.
      final jobs = <OpenJob>[_in('VIC', id: 'a'), _in('NSW', id: 'b'), _in('VIC', id: 'c')];

      expect(pickupStatesIn(jobs), <String>['NSW', 'VIC']);
    });

    test('a job with no pickup state contributes no chip', () {
      final jobs = <OpenJob>[_in('NSW', id: 'a'), anOpenJob(id: 'b', pickup: null)];

      expect(pickupStatesIn(jobs), <String>['NSW']);
    });

    test('the statuses come out in Docs/02 §1’s order', () {
      // From JobStatus.values rather than from a list written here, so a status added in the right
      // place is offered in the right place with nothing else to edit.
      final jobs = <OpenJob>[
        _in('NSW', id: 'a', status: JobStatus.negotiating),
        _in('NSW', id: 'b', status: JobStatus.open),
      ];

      expect(statusesIn(jobs), <JobStatus>[JobStatus.open, JobStatus.negotiating]);
    });

    test('a facet with one option is not worth drawing', () {
      // One chip can only ever hide the whole feed. A provider whose work is all in one state
      // should be shown work rather than a row of buttons.
      expect(facetIsUseful(<String>['NSW']), isFalse);
      expect(facetIsUseful(<String>[]), isFalse);
      expect(facetIsUseful(<String>['NSW', 'VIC']), isTrue);
    });
  });
}
