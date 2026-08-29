import 'dart:async';

import 'package:shipper/features/jobs/goods_categories_repository.dart';
import 'package:shipper/features/jobs/goods_category.dart';

/// One entry the platform could have served.
///
/// Built from the `GoodsCategory` example in `contracts/paths/jobs.yaml`, so a field renamed in
/// the contract shows up here rather than only on a device.
GoodsCategory aCategory({
  String code = 'general_freight',
  String label = 'General freight',
  String? description = 'Palletised or boxed goods needing no special handling.',
  bool carried = true,
  bool provisional = true,
}) {
  return GoodsCategory(
    code: code,
    label: label,
    description: description,
    carried: carried,
    provisional: provisional,
  );
}

/// A catalogue shaped like the one X-9 approved: some carried, some not.
///
/// Two of each is the smallest list that can tell the two apart, and one refused entry alone would
/// let a screen that rendered *every* entry as refused pass.
GoodsCatalogue aCatalogue() {
  return GoodsCatalogue(
    categories: <GoodsCategory>[
      aCategory(),
      aCategory(
        code: 'furniture',
        label: 'Furniture and white goods',
        description: 'Household items, assembled or flat-packed.',
      ),
      aCategory(
        code: 'dangerous_goods',
        label: 'Dangerous goods',
        description: 'Explosives, compressed gases, corrosives.',
        carried: false,
      ),
      aCategory(
        code: 'live_animals',
        label: 'Live animals',
        description: null,
        carried: false,
      ),
    ],
  );
}

/// A [GoodsCategoriesRepository] that answers from a script and records that it was asked.
///
/// The catalogue is reference data rather than the caller's own, so there is nothing to key by and
/// nothing to record beyond the number of reads — which is the one thing worth asserting, because
/// `goodsCatalogueProvider` exists to make several screens share one fetch.
class FakeGoodsCategoriesRepository implements GoodsCategoriesRepository {
  var reads = 0;

  /// What the fetch answers with.
  GoodsCatalogue Function() answer = aCatalogue;

  /// Set to make the fetch throw instead of answering.
  ///
  /// **It persists until a test clears it**, rather than applying to one call. Riverpod retries a
  /// failed provider on its own — ten times, with backoff — so a failure that cleared itself would
  /// make what the screen shows depend on how many retries had fired by the time the test looked.
  Object? failure;

  @override
  Future<GoodsCatalogue> catalogue() async {
    reads++;

    final thrown = failure;
    if (thrown != null) throw thrown;

    return answer();
  }
}
