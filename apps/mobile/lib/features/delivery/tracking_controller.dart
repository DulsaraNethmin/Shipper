import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/delivery/delivery_repository.dart';
import 'package:shipper/features/delivery/delivery_tracking.dart';

part 'tracking_controller.freezed.dart';

/// How one delivery is going, to the customer who owns it (SHIP-133).
@freezed
abstract class TrackingState with _$TrackingState {
  const factory TrackingState({
    /// The **first** read is in flight, or a retry after a failure is.
    @Default(true) bool loading,

    /// A further page of milestones is being read.
    @Default(false) bool loadingMore,

    /// Whether a complete read has arrived at all.
    ///
    /// What separates "nothing has been recorded yet" from "not yet asked", and the separation is
    /// the whole of the empty state: a job that was published this morning has an empty milestone
    /// list, and a client that treated that as "still loading" would leave the customer watching a
    /// spinner until somebody collected their goods.
    @Default(false) bool loaded,

    /// Who is carrying it, or that nobody is yet.
    DeliveryDriver? driver,

    /// Every milestone read so far, newest first by the actor's clock.
    @Default(<RecordedMilestone>[]) List<RecordedMilestone> milestones,

    /// Every piece of evidence on this delivery, newest acted first.
    ///
    /// **Never persisted.** Each carries a URL signed for this caller and good until it expires; a
    /// stored copy is a stored credential. See [DeliveryProof].
    @Default(<DeliveryProof>[]) List<DeliveryProof> proof,

    /// The position to ask from next, for the milestone list. **Opaque.**
    String? nextCursor,

    /// Whether asking again would return more milestones.
    @Default(false) bool hasMore,

    /// What the last read failed with, or `null`.
    ApiFailure? failure,
  }) = _TrackingState;

  const TrackingState._();

  /// The most recent milestone the **platform** holds, or `null` when it holds none.
  ///
  /// This is SHIP-133's *Done when* in one getter, and the word doing the work is *confirmed*.
  /// Everything on [milestones] is confirmed by construction: it is the platform's own record, and
  /// `accepted_at` — when the platform received it — is required on every row. There is nothing to
  /// filter, and a client that invented a "confirmed" predicate would be describing a distinction
  /// this endpoint does not have.
  ///
  /// **What is genuinely unconfirmed lives somewhere else entirely**: SHIP-129's queue, on the
  /// provider's handset, which no customer can see and none should. A milestone recorded in a valley
  /// an hour ago is not a fact about the delivery until the platform has it.
  ///
  /// `first` and not a search, because the endpoint sorts by the actor's clock — newest first — and
  /// re-sorting on the device would be a second opinion about an order the platform already holds.
  RecordedMilestone? get latest => milestones.isEmpty ? null : milestones.first;

  /// The evidence for the delivery itself, or `null` when nothing has been recorded for it.
  ///
  /// The one photograph — or the one reasoned exception — that `Docs/01` §4.4 requires before a job
  /// can be `Delivered`, and the thing a customer opens this screen for. Evidence recorded for
  /// *other* milestones is real and is on [proof]; it is drawn under this rather than instead of it.
  DeliveryProof? get deliveryProof {
    for (final record in proof) {
      if (record.milestone == 'delivered') return record;
    }
    return null;
  }

  /// Evidence recorded against milestones other than the delivery.
  ///
  /// A provider may photograph a collection as readily as a delivery — `POST /v1/jobs/{id}/milestones`
  /// accepts `proof` on any of the five — so this is an ordinary case rather than a leftover.
  List<DeliveryProof> get otherProof =>
      proof.where((record) => record.milestone != 'delivered').toList(growable: false);

  /// The platform has been asked and has nothing recorded on this delivery.
  ///
  /// **Not a fault, and the ordinary state of every job before a provider does anything.** It is
  /// also what a customer sees on a draft: the read shelf makes the job's owner a party whatever the
  /// status, so a job that has not been published answers with an empty everything.
  bool get nothingRecorded => loaded && milestones.isEmpty && proof.isEmpty;

  /// Nothing has arrived and nothing has failed — the only state a spinner belongs in.
  bool get isFirstLoad => loading && !loaded && failure == null;

  /// Nothing arrived and the reason is a failure worth offering a retry for.
  bool get failedOutright => !loaded && failure != null;
}

/// Reads how one delivery is going, for the customer who owns it (SHIP-133).
///
/// ## Three reads, and they are made together because they answer one question
///
/// `/delivery/detail`, `/delivery/milestones` and `/delivery/proof`. A screen that read them one at a
/// time would draw three spinners resolving separately, which for a person refreshing a delivery
/// reads as a screen that cannot make up its mind. They are awaited together and applied in one
/// assignment, so the screen moves from "loading" to "loaded" once.
///
/// **A further page of milestones is the exception**, and it is appended alone: the driver and the
/// evidence have not changed and re-reading them would mint a fresh set of signed URLs for
/// photographs already on screen.
///
/// ## Nothing is cached, and that is a decision about a credential
///
/// Every `download_url` in a proof response is signed for this caller and expires. **A cached
/// response is a cache of expiring links**, so this holds one in memory for the life of the screen
/// and nothing writes one down. Leaving the screen and coming back re-reads and gets fresh URLs,
/// which is the arrangement the endpoint documents rather than a limitation of it.
///
/// ## It decides nothing about the delivery
///
/// `Docs/02` §3.1 puts every transition on the platform, and this holds no copy of `Docs/02` §2's
/// table, does not derive a status from a milestone, and never concludes that one milestone implies
/// another. The contract is explicit that a milestone may move nothing: reading a status out of this
/// list would be reading something it does not say.
class TrackingController extends Notifier<TrackingState> {
  TrackingController(this.jobId);

  /// The delivery being tracked. From the route, which is the only place it comes from.
  final String jobId;

  @override
  TrackingState build() {
    // Reading the repository is synchronous and the requests are not: `_load` suspends at its first
    // await, so this returns before anything assigns to `state`. Nothing here reads `state`, which
    // on this path does not exist yet.
    unawaited(_load());
    return const TrackingState();
  }

  /// Reads everything again, keeping what is on screen while it is in flight.
  ///
  /// Returned rather than awaited internally so `RefreshIndicator` can hold its spinner until the
  /// read finishes. It is also **how a customer gets a working photograph after a link has
  /// expired**, which is why the screen says so rather than leaving them looking at a broken image.
  Future<void> refresh() => _load();

  /// Asks again after a failure, with the spinner back.
  Future<void> retry() {
    state = state.copyWith(loading: true, failure: null);
    return _load();
  }

  /// Reads the next page of milestones and appends it.
  Future<void> loadMore() async {
    final cursor = state.nextCursor;
    if (cursor == null || state.loadingMore) return;

    state = state.copyWith(loadingMore: true, failure: null);

    try {
      final page = await ref
          .read(deliveryRepositoryProvider)
          .milestones(jobId: jobId, cursor: cursor);
      if (!ref.mounted) return;

      state = state.copyWith(
        loadingMore: false,
        milestones: <RecordedMilestone>[...state.milestones, ...page.data],
        nextCursor: page.nextCursor,
        hasMore: page.hasMore,
        failure: null,
      );
    } on ApiFailure catch (failure) {
      _failed(failure);
    } catch (error) {
      _failed(const ApiMalformedResponse(statusCode: 0));
    }
  }

  Future<void> _load() async {
    final delivery = ref.read(deliveryRepositoryProvider);

    try {
      // Together rather than in sequence. Three round trips one after another is three times the
      // latency on a handset, for three answers that are drawn in one frame.
      final read = await Future.wait<Object>(<Future<Object>>[
        delivery.driver(jobId: jobId),
        delivery.milestones(jobId: jobId),
        delivery.proof(jobId: jobId),
      ]);
      if (!ref.mounted) return;

      final driver = read[0] as DeliveryDriver;
      final milestones = read[1] as ApiPage<RecordedMilestone>;
      final proof = read[2] as List<DeliveryProof>;

      state = state.copyWith(
        loading: false,
        loadingMore: false,
        loaded: true,
        driver: driver,
        milestones: milestones.data,
        proof: proof,
        nextCursor: milestones.nextCursor,
        hasMore: milestones.hasMore,
        failure: null,
      );
    } on ApiFailure catch (failure) {
      _failed(failure);
    } catch (error) {
      // Past `ApiClient`'s mapping: a response whose rows are not milestones. Nothing a customer can
      // act on, and the same thing to them as any other failure.
      _failed(const ApiMalformedResponse(statusCode: 0));
    }
  }

  /// Records a failure **without discarding what is already on screen**.
  ///
  /// A refresh that fails should leave the customer looking at the delivery they had, with a banner
  /// saying the reload did not work — not with an error where their proof of delivery was.
  void _failed(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(loading: false, loadingMore: false, failure: failure);
  }
}

/// How one delivery is going, keyed by the job's id.
///
/// **Auto-disposed**, like every other read in this client (`Docs/07` §3): what a device holds about
/// a job goes with the token at sign-out. Here it does one thing more — it is what stops a set of
/// signed proof URLs outliving the screen that was entitled to them.
final trackingProvider =
    NotifierProvider.autoDispose.family<TrackingController, TrackingState, String>(
  TrackingController.new,
);
