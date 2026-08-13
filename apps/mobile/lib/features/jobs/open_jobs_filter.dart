import 'package:flutter/foundation.dart';

import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/open_job.dart';

/// How a provider has narrowed their own feed (SHIP-99).
///
/// ## What this is, and — more importantly — what it is not
///
/// `GET /v1/jobs/open` accepts `limit` and `cursor` and **no filter of any kind**. The contract
/// takes that position deliberately: eligibility is the platform's decision, and a parameter that
/// widened or narrowed the feed would be a second place for that answer to be argued with
/// (`Docs/07` §3). The same contract then names where narrowing does belong — "a provider
/// narrowing their own feed further is the client's business" — which is this file.
///
/// So this is **subtractive and can only ever be subtractive**. Every job it is shown has already
/// been through the platform's four eligibility checks; all this can do is hide some of them from
/// the person who asked to hide them. There is no arrangement of these fields that shows a
/// provider a job the platform did not offer them, which is the property that keeps a client-side
/// filter a convenience rather than a control that disagrees with the server.
///
/// ## The options are derived from the jobs, never compiled in
///
/// `CLAUDE.md` and `Docs/07` §1 keep anything that changes under operational pressure server-side,
/// because Dart has no over-the-air update path. A list of Australian states written here would be
/// a vocabulary needing a store release; [pickupStatesIn] reads the states the platform actually
/// sent instead, so the chips a provider sees are a fact about their feed rather than a guess this
/// build made about the country.
///
/// [JobStatus] is the one exception and it is not one: it is a **contract enum** already in the
/// client for the customer's own screens, and [statusesIn] still derives which of its values to
/// offer from what was read rather than assuming both can appear.
///
/// ## An empty set means "everything", not "nothing"
///
/// Both fields start empty and an empty field narrows nothing. That is what makes the default a
/// feed rather than a blank screen, and it is why [matches] tests emptiness before membership.
@immutable
class OpenJobsFilter {
  const OpenJobsFilter({
    this.pickupStates = const <String>{},
    this.statuses = const <JobStatus>{},
  });

  /// The pickup states a provider has chosen to see. Empty means every state.
  final Set<String> pickupStates;

  /// The statuses a provider has chosen to see. Empty means both.
  final Set<JobStatus> statuses;

  /// Whether the provider has narrowed anything at all.
  bool get isEmpty => pickupStates.isEmpty && statuses.isEmpty;

  bool get isNotEmpty => !isEmpty;

  /// Whether [job] survives the narrowing.
  bool matches(OpenJob job) {
    if (pickupStates.isNotEmpty && !pickupStates.contains(job.pickupState)) return false;
    if (statuses.isNotEmpty && !statuses.contains(job.status)) return false;
    return true;
  }

  /// The filter with [state] added if it was absent, removed if it was present.
  ///
  /// A toggle rather than a setter, because the control is a row of chips and a provider carrying
  /// two states' worth of work should be able to watch both.
  OpenJobsFilter togglePickupState(String state) {
    return OpenJobsFilter(
      pickupStates: _toggled(pickupStates, state),
      statuses: statuses,
    );
  }

  /// The filter with [status] added if it was absent, removed if it was present.
  OpenJobsFilter toggleStatus(JobStatus status) {
    return OpenJobsFilter(
      pickupStates: pickupStates,
      statuses: _toggled(statuses, status),
    );
  }

  /// Every narrowing removed.
  OpenJobsFilter cleared() => const OpenJobsFilter();

  static Set<T> _toggled<T>(Set<T> set, T value) {
    final next = Set<T>.of(set);
    if (!next.remove(value)) next.add(value);
    return next;
  }

  @override
  bool operator ==(Object other) {
    return other is OpenJobsFilter &&
        setEquals(other.pickupStates, pickupStates) &&
        setEquals(other.statuses, statuses);
  }

  @override
  int get hashCode => Object.hash(
        Object.hashAllUnordered(pickupStates),
        Object.hashAllUnordered(statuses),
      );

  @override
  String toString() => 'OpenJobsFilter(pickupStates: $pickupStates, statuses: $statuses)';
}

/// The jobs in [jobs] that survive [filter], in the platform's own order.
///
/// A pure function so the narrowing can be read and tested without a screen, exactly as
/// `groupJobsByStatus` is for the customer's list. It is the whole of what "with filters" means on
/// this screen, and a widget test asserting on cards would be testing the layout as well.
List<OpenJob> narrowOpenJobs(List<OpenJob> jobs, OpenJobsFilter filter) {
  if (filter.isEmpty) return jobs;
  return jobs.where(filter.matches).toList(growable: false);
}

/// The pickup states present in [jobs], in alphabetical order.
///
/// **Read from the jobs rather than from a list of the eight states**, which is what stops a
/// vocabulary being compiled into a client that cannot be updated over the air. It also keeps the
/// control honest: a chip is offered only when there is work behind it.
///
/// Jobs with no pickup state contribute nothing. Every job in this feed has a pickup — the
/// eligibility filter matches a declared region against it — so the case is the platform's future
/// rather than today's data, and dropping it is better than offering a chip labelled with nothing.
List<String> pickupStatesIn(List<OpenJob> jobs) {
  final states = <String>{
    for (final job in jobs)
      if (job.pickupState.isNotEmpty) job.pickupState,
  };

  return states.toList(growable: false)..sort();
}

/// The statuses present in [jobs], in `Docs/02` §1's own order.
///
/// The order comes from `JobStatus.values` rather than from a list written here — the same
/// mechanism the customer's grouping uses — so a status added in the right place is offered in the
/// right place with nothing else to edit. Only `open` and `negotiating` can reach this feed today.
List<JobStatus> statusesIn(List<OpenJob> jobs) {
  final present = <JobStatus>{for (final job in jobs) job.status};

  return <JobStatus>[
    for (final status in JobStatus.values)
      if (present.contains(status)) status,
  ];
}

/// Whether a facet with these options is worth drawing.
///
/// **One option filters nothing.** A single chip that hides the entire feed when it is tapped off
/// is a control that can only make the screen worse, and a provider whose work is all in one state
/// should be shown work rather than a row of buttons. Two or more is when the question "which of
/// these?" starts having an answer.
bool facetIsUseful(List<Object?> options) => options.length > 1;
