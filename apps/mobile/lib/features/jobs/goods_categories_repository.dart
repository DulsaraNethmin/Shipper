import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/features/jobs/goods_category.dart';

/// `GET /v1/goods-categories` — the catalogue a job's `goods_category` is chosen from (SHIP-58).
///
/// Its own repository rather than a sixth method on [JobsRepository], and the split is the
/// endpoint's rather than this client's invention: **it describes no job at all.** It is public
/// and unauthenticated, it takes no idempotency key, it is identical for every caller, and it is
/// the one operation in `contracts/paths/jobs.yaml` that is none of those things about a job. A
/// method on the jobs repository would put a call that needs no session behind an interface every
/// other member of which is owner-scoped.
///
/// An interface with one real implementation, for the reason [JobsRepository] is one: a widget
/// test has to be able to hand a screen something that answers, and a stub transport under a
/// concrete class makes every screen test a test of `dio`'s wiring as well.
abstract interface class GoodsCategoriesRepository {
  /// The catalogue currently in force, in the order it is configured.
  ///
  /// **Every entry is returned, including the ones Shipper will not carry.** A refused category
  /// arrives with `carried: false` rather than being left out, which is what lets a form show a
  /// customer what is not taken and what lets publication refuse by name (SHIP-59).
  Future<GoodsCatalogue> catalogue();
}

/// The real one, over [ApiClient].
final class ApiGoodsCategoriesRepository implements GoodsCategoriesRepository {
  const ApiGoodsCategoriesRepository(this._client);

  final ApiClient _client;

  /// Product endpoints live under `/v1` (SHIP-13), and this is a product endpoint despite being
  /// public — `Docs/06` §2.1 puts the operational surface outside the version prefix, and a list
  /// of what the marketplace carries is not operational.
  static const _path = '/v1/goods-categories';

  @override
  Future<GoodsCatalogue> catalogue() async {
    return GoodsCatalogue.fromJson(await _client.getJson(_path));
  }
}

/// The application's goods-categories repository.
final goodsCategoriesRepositoryProvider = Provider<GoodsCategoriesRepository>(
  (ref) => ApiGoodsCategoriesRepository(ref.watch(apiClientProvider)),
);

/// The catalogue, fetched once and shared by every screen that needs it.
///
/// ## Why it is not `autoDispose`, and not cached to disk either
///
/// Two screens read it — the goods step chooses from it, and the review step renders the chosen
/// code back as a label — and a customer moves between them repeatedly while correcting a job.
/// An auto-disposing provider would re-fetch on every one of those moves.
///
/// It is deliberately **not** written to disk the way `AppPolicy` is, and the difference is what
/// the two answers are for. A policy exists to be applied on the launch that has no signal, so a
/// stale copy is better than none. A catalogue exists so that a category withdrawn this morning
/// stops being offered — a stale copy is the exact failure it is meant to prevent, and the job it
/// would let a customer build is one the platform refuses at publication anyway. Fetching per
/// launch is what the endpoint's own contract asks for.
///
/// A failure is Riverpod's to retry, and the screen renders `AsyncError` as a banner with a
/// retry rather than as an empty list: a form drawn with no categories at all reads as "Shipper
/// carries nothing", which is a lie the customer cannot act on.
final goodsCatalogueProvider = FutureProvider<GoodsCatalogue>((ref) {
  return ref.watch(goodsCategoriesRepositoryProvider).catalogue();
});
