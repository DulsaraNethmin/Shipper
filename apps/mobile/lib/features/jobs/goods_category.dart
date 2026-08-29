import 'package:freezed_annotation/freezed_annotation.dart';

part 'goods_category.freezed.dart';
part 'goods_category.g.dart';

/// One entry in the goods catalogue — the `GoodsCategory` schema in `contracts/paths/jobs.yaml`
/// (SHIP-58).
///
/// ## This is reference data, and it is not compiled into the app
///
/// `CLAUDE.md` names category lists first among the things that live server-side because they move
/// under operational pressure, and Flutter has no over-the-air path for Dart code. A category
/// withdrawn on legal advice has to stop being offered without a store release, so the list is
/// fetched rather than written here. There is deliberately no compiled fallback: an app that
/// guessed the catalogue would offer a category somebody had just withdrawn, which is the single
/// outcome the endpoint exists to prevent.
///
/// ## Every field the contract marks required is required here, and `Job` does the opposite
///
/// `Job` makes almost everything nullable so that a field the platform stops sending cannot crash
/// an installed build. **This type takes the other decision**, and the difference is what happens
/// when a field goes missing.
///
/// A `Job` is decoded on every customer screen in the app; a catalogue is decoded in one place,
/// for one form, and its failure is contained to that form — [GoodsCategoriesRepository]'s caller
/// shows a banner and a retry. So the choice is between a visible, recoverable refusal and a
/// silent wrong answer, and [carried] is the field that makes it stark: defaulted to `true` it
/// offers a customer dangerous goods, and defaulted to `false` it tells them Shipper carries
/// nothing, with nothing on screen to say why. The contract says `carried` is never omitted; this
/// takes it at its word and fails loudly if that stops being true.
@freezed
abstract class GoodsCategory with _$GoodsCategory {
  const factory GoodsCategory({
    /// The stored form, and the value sent as a job's `goods_category` — lower snake case.
    ///
    /// Stable across a change of wording, which is why it and not [label] is what a job holds.
    required String code,

    /// The short human name, in Australian English. Shown, never sent.
    required String label,

    /// A one-line explanation beneath the label, or `null` when the catalogue gives none.
    String? description,

    /// Whether a job in this category may be **published**.
    ///
    /// A refused category is served in the list rather than left out, so the form can show a
    /// customer what Shipper does not take instead of leaving them to guess from an absence.
    ///
    /// **A draft may name a refused category and publishing one is refused** — `Docs/07` §3 lets
    /// the app hide or disable and leaves the decision to the platform, and SHIP-59 makes that
    /// refusal by name, quoting the catalogue's own wording.
    required bool carried,

    /// Whether the entry still awaits the legal review X-4 owns.
    ///
    /// True for every entry in the list shipping today: `Docs/11` §5 records that the owner
    /// approved a provisional list in reduced form so the product could be walked end to end, and
    /// the reference data says so rather than the app pretending otherwise. Per entry rather than
    /// per catalogue, because a reviewer is far likelier to confirm most of a list and query two
    /// than to bless or reject all of it at once.
    required bool provisional,
  }) = _GoodsCategory;

  const GoodsCategory._();

  factory GoodsCategory.fromJson(Map<String, dynamic> json) => _$GoodsCategoryFromJson(json);
}

/// The catalogue as one response — `{ "categories": [...] }`.
///
/// An object with one key rather than a bare array, which is the contract's own shape and its
/// reasoning: `Docs/07` §6 keeps responses additive, and a top-level array is the one shape that
/// cannot gain a field.
@freezed
abstract class GoodsCatalogue with _$GoodsCatalogue {
  const factory GoodsCatalogue({
    @Default(<GoodsCategory>[]) List<GoodsCategory> categories,
  }) = _GoodsCatalogue;

  const GoodsCatalogue._();

  factory GoodsCatalogue.fromJson(Map<String, dynamic> json) => _$GoodsCatalogueFromJson(json);

  /// The entry for [code], or `null` when the catalogue no longer serves it.
  ///
  /// `null` is an ordinary answer rather than an error: a draft saved last week can name a
  /// category withdrawn since, and the customer has to be able to see what their own job says.
  GoodsCategory? byCode(String? code) {
    if (code == null || code.isEmpty) return null;
    for (final category in categories) {
      if (category.code == code) return category;
    }
    return null;
  }

  /// Whether [code] names something Shipper will not carry.
  ///
  /// **A code the catalogue does not serve is not refused here**, and that is the interesting
  /// case: this cannot tell "withdrawn from the list" from "never existed", and guessing either
  /// way would be an authorisation decision on the device (`Docs/07` §3). The app shows what it
  /// knows and lets `POST /v1/jobs/{id}/publish` decide.
  bool refuses(String? code) => byCode(code)?.carried == false;
}
