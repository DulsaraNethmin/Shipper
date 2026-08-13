import 'dart:async';

import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/open_job.dart';
import 'package:shipper/features/jobs/open_jobs_repository.dart';

/// A job the platform could have offered this provider.
///
/// Built from the `OpenJob` example in `contracts/paths/fleet.yaml` so that a field renamed in the
/// contract shows up here rather than only on a device.
///
/// **There is no budget parameter and there must never be one.** `Docs/01` §4.3 keeps the
/// customer's maximum away from a provider in every form, and a fixture that could carry one would
/// be the first place a screen could be written against a field the platform does not send.
OpenJob anOpenJob({
  String id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  JobStatus status = JobStatus.open,
  JobRegion? pickup = const JobRegion(suburb: 'Newtown', state: 'NSW', postcode: '2042'),
  JobRegion? dropoff = const JobRegion(suburb: 'Geelong', state: 'VIC', postcode: '3220'),
  String? goodsDescription = 'Three-seat sofa, wrapped',
  int? lengthCm = 190,
  int? widthCm = 90,
  int? heightCm = 85,
  double? weightKg = 65,
  String? vehicleRequirement = 'Van or larger, two people to lift',
  String? handlingNotes = 'Second floor, no lift',
  JobTimeWindow? pickupWindow = const JobTimeWindow(start: '2026-08-20T23:00:00.000Z'),
  JobTimeWindow? dropoffWindow,
  String? expiresAt = '2026-08-25T03:30:00.000Z',
  String? createdAt = '2026-08-11T03:30:00.000Z',
}) {
  return OpenJob(
    id: id,
    status: status,
    pickup: pickup,
    dropoff: dropoff,
    goodsDescription: goodsDescription,
    lengthCm: lengthCm,
    widthCm: widthCm,
    heightCm: heightCm,
    weightKg: weightKg,
    vehicleRequirement: vehicleRequirement,
    handlingNotes: handlingNotes,
    pickupWindow: pickupWindow,
    dropoffWindow: dropoffWindow,
    expiresAt: expiresAt,
    createdAt: createdAt,
  );
}

/// A job published with two regions and nothing else yet.
///
/// **The ordinary case rather than a degenerate one.** The wizard collects the addresses first, so
/// a job can be open to bids before its owner has described the goods — the platform omits every
/// field they have not filled in.
OpenJob aBareOpenJob({
  String id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1',
  JobStatus status = JobStatus.open,
  JobRegion? pickup = const JobRegion(suburb: 'Darwin', state: 'NT', postcode: '0800'),
}) {
  return anOpenJob(
    id: id,
    status: status,
    pickup: pickup,
    dropoff: null,
    goodsDescription: null,
    lengthCm: null,
    widthCm: null,
    heightCm: null,
    weightKg: null,
    vehicleRequirement: null,
    handlingNotes: null,
    pickupWindow: null,
    expiresAt: null,
  );
}

/// One recorded call.
typedef OpenJobsCall = ({String? cursor});

/// An [OpenJobsRepository] that answers from a script and records what it was asked.
///
/// The screen is tested against this rather than against a stub transport, because a widget test
/// that also exercises `dio`'s wiring fails for two reasons and reads as one. What actually
/// reaches the wire is `open_jobs_repository_test.dart`'s subject, against the contract.
///
/// It records the **cursor of every read**, which is what makes the paging assertable: the second
/// page is a separate code path a provider only reaches by asking for it, and a cursor passed back
/// as anything other than exactly what arrived is refused by the platform rather than misread.
class FakeOpenJobsRepository implements OpenJobsRepository {
  final calls = <OpenJobsCall>[];

  /// What the feed answers with, page by page.
  ///
  /// A list rather than a single value, because the interesting behaviour of this screen is what
  /// the *second* answer does to the first. The last entry is reused once the list runs out, so a
  /// test that does not care about paging can leave it at one.
  var pages = <ApiPage<OpenJob>>[const ApiPage<OpenJob>(data: <OpenJob>[])];

  /// Thrown instead of answering, on every call until it is cleared. `null` never fails.
  ///
  /// Settable mid-test on purpose: "a refresh that fails leaves the work on screen" needs a read
  /// that succeeds and then one that does not.
  Object? failure;

  /// Held open until completed, so a test can assert what the screen shows while a read is
  /// genuinely in flight rather than already finished.
  Completer<void>? gate;

  int get reads => calls.length;

  /// Every cursor presented, in order. The first is `null` — the first page names no position.
  List<String?> get cursors => calls.map((c) => c.cursor).toList(growable: false);

  @override
  Future<ApiPage<OpenJob>> openJobs({String? cursor}) async {
    final attempt = calls.length;
    calls.add((cursor: cursor));

    final held = gate;
    if (held != null) await held.future;

    final thrown = failure;
    if (thrown != null) throw thrown;

    return pages[attempt < pages.length ? attempt : pages.length - 1];
  }
}
