/// One page of a collection, in the envelope every list endpoint answers with (`Docs/10` §4.5).
///
/// ```json
/// { "data": [ … ], "next_cursor": "…", "has_more": true }
/// ```
///
/// It lives in `core/api` rather than in the first feature to need one because the envelope is a
/// property of the API and not of jobs — bids, notifications and the provider feed all return it.
/// The Go service made the same call at SHIP-66 and put it in `internal/pagination`.
///
/// ## The cursor is opaque and this class is what keeps it that way
///
/// [nextCursor] is a token the endpoint issued. Nothing in the client may read it, compose one,
/// or infer a position from it: the contract says its encoding may change and that a cursor from
/// an encoding no longer served is refused rather than misread. So it is a `String?` that is
/// passed back exactly as it arrived, and there is deliberately no constructor that builds one.
///
/// ## Paging is keyset, and [hasMore] is carried rather than inferred
///
/// `has_more` is a field of its own even though "there is a next cursor" looks like the same
/// fact. It is not: no cursor is also what the *first* request looks like, so a client that
/// inferred the end of the list from an absent cursor would decide it had reached the end before
/// it started.
class Page<T> {
  const Page({required this.data, this.nextCursor, this.hasMore = false});

  /// The items, in the order the platform returned them.
  ///
  /// **Never null.** The platform sends `[]` for a customer with nothing, and that is worth
  /// stating because it is the case a client iterating a nullable list breaks on — the first
  /// time a new account opens the app, and never in testing.
  final List<T> data;

  /// Pass back as `?cursor=` to fetch the next page. `null` on the last one.
  final String? nextCursor;

  /// Whether asking again with [nextCursor] would return anything.
  final bool hasMore;

  bool get isEmpty => data.isEmpty;

  /// Reads the envelope, tolerating everything about it that might change.
  ///
  /// `Docs/07` §6 requires unknown fields to be ignored so an additive server change needs no
  /// app release; this reads the three keys it knows and steps over the rest. It also copes with
  /// the envelope arriving wrong — a proxy's error page, a `data` that is not an array — because
  /// the alternative is a cast failure with no message a screen can show.
  ///
  /// [item] is allowed to throw. A row missing its `id` is the contract being broken rather than
  /// a field being added, and a caller that quietly dropped it would show a customer a list with
  /// a job silently missing from it.
  static Page<T> fromJson<T>(
    Map<String, dynamic> json,
    T Function(Map<String, dynamic>) item,
  ) {
    final Object? rows = json['data'];
    final Object? cursor = json['next_cursor'];
    final Object? more = json['has_more'];

    return Page<T>(
      data: rows is List
          ? rows.whereType<Map<String, dynamic>>().map(item).toList(growable: false)
          : const [],
      nextCursor: cursor is String && cursor.isNotEmpty ? cursor : null,
      hasMore: more is bool && more,
    );
  }
}
