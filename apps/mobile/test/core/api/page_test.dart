// The list envelope every collection endpoint answers with (Docs/10 §4.5).
//
// Small, and worth having anyway: the two cases below are ones that never occur in development —
// a customer with no jobs, and a page that is the last one — and both break a client that assumed
// otherwise on the first real account rather than in testing.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';

/// The identity of the decode, so a malformed row is visibly the test's doing rather than the
/// envelope's.
Map<String, dynamic> _itself(Map<String, dynamic> row) => row;

void main() {
  test('an empty page is an empty list, not a null one', () {
    final page = ApiPage.fromJson(
      <String, dynamic>{'data': <Object?>[], 'has_more': false},
      _itself,
    );

    expect(page.data, isEmpty);
    expect(page.isEmpty, isTrue);
    expect(page.nextCursor, isNull);
    expect(page.hasMore, isFalse);
  });

  test('a body that is not the envelope decodes to an empty page rather than throwing', () {
    // A proxy's error page, or a 502 with no body. A cast failure here would surface to the
    // customer as a crash rather than as "something went wrong, try again".
    for (final body in <Map<String, dynamic>>[
      <String, dynamic>{},
      <String, dynamic>{'data': null},
      <String, dynamic>{'data': 'nope'},
    ]) {
      expect(ApiPage.fromJson(body, _itself).data, isEmpty, reason: '$body');
    }
  });

  test('the cursor is carried through untouched, and an empty one is no cursor', () {
    const token = 'MR8yMDI2LTA4LTExVDAzOjMwOjAwWh8wMTk4ZjJjMS02YjQwLTdhMTE';

    final page = ApiPage.fromJson(
      <String, dynamic>{
        'data': <Object?>[
          <String, dynamic>{'id': 'a'},
        ],
        'next_cursor': token,
        'has_more': true,
      },
      _itself,
    );

    expect(page.nextCursor, token);
    expect(page.hasMore, isTrue);
    expect(page.data.single['id'], 'a');

    // The empty string is what "no cursor" looks like on the way in as well, so the two have to
    // arrive as the same thing — otherwise a client asks for the page after nowhere.
    final last = ApiPage.fromJson(
      <String, dynamic>{'data': <Object?>[], 'next_cursor': '', 'has_more': false},
      _itself,
    );
    expect(last.nextCursor, isNull);
  });

  test('has_more is read rather than inferred from the cursor', () {
    // Absent `has_more` is false, and that is deliberately the safe direction: a client that
    // guessed "more" from a missing cursor would ask for the page after the end forever.
    final page = ApiPage.fromJson(
      <String, dynamic>{
        'data': <Object?>[
          <String, dynamic>{'id': 'a'},
        ],
      },
      _itself,
    );

    expect(page.hasMore, isFalse);
  });
}
