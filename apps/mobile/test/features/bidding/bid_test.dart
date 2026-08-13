// SHIP-84, SHIP-100 — the shape a provider's own offer is decoded in, and the shape one is sent in.
//
// Held against contracts/paths/bidding.yaml's `Bid` and `BidPlacement`, because the key names are
// the contract: a client reading `amount` where the platform sends `amount_cents` shows a price a
// hundred times too small with no error anywhere, on a build already on somebody's phone.
//
// # The budget half of this file is the point of it
//
// Docs/01 §4.3 and CLAUDE.md keep the customer's maximum away from a provider in every form. The
// platform proves the field never leaves the service — TestTheBidResponseCarriesNothingOfTheCustomers
// holds the serialised response to a closed set of keys at every depth, so a field arriving as
// `max_price` fails as surely as one arriving as `budget_cents`. This proves the other half: that a
// budget arriving anyway — renamed, added by a future version, or injected by anything between the
// two — has nowhere in this type to land.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';

/// An offer exactly as `POST /v1/jobs/{id}/bids` answers with one.
const _wire = <String, dynamic>{
  'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  'job_id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1',
  'status': 'submitted',
  'offered_by': 'provider',
  'amount_cents': 45000,
  'pickup_at': '2026-08-19T23:00:00.000Z',
  'deliver_by': '2026-08-20T07:00:00.000Z',
  'message': 'Can collect from the loading dock any time after eight.',
  'created_at': '2026-08-13T04:15:30.000Z',
  'updated_at': '2026-08-13T04:15:30.000Z',
};

void main() {
  group('decoding an offer', () {
    test('reads every field the contract sends', () {
      final bid = Bid.fromJson(_wire);

      expect(bid.id, '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
      expect(bid.jobId, '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1');
      expect(bid.status, BidStatus.submitted);
      expect(bid.offeredBy, BidParty.provider);
      expect(bid.amountCents, 45000);
      expect(bid.pickupAt, '2026-08-19T23:00:00.000Z');
      expect(bid.deliverBy, '2026-08-20T07:00:00.000Z');
      expect(bid.message, 'Can collect from the loading dock any time after eight.');
      expect(bid.supersededBy, isNull);
    });

    test('an offer with no message and no counter against it decodes', () {
      // The ordinary case: `message` and `superseded_by` are both `omitempty` on the wire, so the
      // usual response for a freshly placed offer carries neither.
      final bid = Bid.fromJson(<String, dynamic>{
        'id': 'a',
        'job_id': 'b',
        'status': 'submitted',
        'amount_cents': 39900,
      });

      expect(bid.message, isNull);
      expect(bid.supersededBy, isNull);
      expect(bid.isLive, isTrue);
    });

    test('a status this build has never heard of decodes rather than throwing', () {
      // Docs/07 §6 is built on old builds living on devices indefinitely. A ninth status must not
      // crash a client that cannot name it — there is no over-the-air fix.
      final bid = Bid.fromJson(<String, dynamic>{'id': 'a', 'job_id': 'b', 'status': 'lapsed'});

      expect(bid.status, BidStatus.unknown);
      expect(bid.status.label, 'Not shown by this version');
    });

    test('a party this build has never heard of decodes rather than throwing', () {
      final bid = Bid.fromJson(<String, dynamic>{
        'id': 'a',
        'job_id': 'b',
        'status': 'submitted',
        'offered_by': 'broker',
      });

      expect(bid.offeredBy, BidParty.unknown);
    });

    test('a field this build has never heard of is ignored', () {
      // Docs/07 §6: server changes stay backward compatible by adding fields, and the client
      // tolerates them so an additive change needs no release.
      final bid = Bid.fromJson(<String, dynamic>{..._wire, 'accepted_at': '2026-08-14T00:00:00Z'});

      expect(bid.id, '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
    });

    test('the eight statuses the contract enumerates all decode', () {
      // Docs/10 §3.4 pairs the Go list with `ck_bids_status` in both directions, and this is the
      // third copy. A value missing here decodes as `unknown` and renders as "not shown by this
      // version" — which is the right behaviour for a *ninth* status and the wrong one for one the
      // platform already writes.
      const wire = <String>[
        'draft',
        'submitted',
        'countered',
        'accepted',
        'rejected',
        'withdrawn',
        'expired',
        'superseded',
      ];

      for (final name in wire) {
        final bid = Bid.fromJson(<String, dynamic>{'id': 'a', 'job_id': 'b', 'status': name});
        expect(bid.status, isNot(BidStatus.unknown), reason: '$name decoded as unknown');
        expect(bid.status.wireName, name);
      }
    });
  });

  group('whether an offer is still standing', () {
    test('a submitted offer with nothing against it is live', () {
      expect(Bid.fromJson(_wire).isLive, isTrue);
    });

    test('a submitted offer that has been countered is not', () {
      // SHIP-88 puts the chain link on the displaced row, so an offer can be `submitted` and
      // already superseded. Reading the status alone would show a provider an offer that can be
      // neither changed nor answered as though it were live.
      final bid = Bid.fromJson(<String, dynamic>{..._wire, 'superseded_by': 'a-later-offer'});

      expect(bid.status, BidStatus.submitted);
      expect(bid.isLive, isFalse);
    });

    test('every other status is not live', () {
      for (final status in BidStatus.values.where((s) => s != BidStatus.submitted)) {
        expect(status.isLive, isFalse, reason: '${status.wireName} should not read as live');
      }
    });
  });

  group('the budget has nowhere to land', () {
    test('a response carrying one decodes without it, under any name', () {
      // Not a search for the word. SHIP-83 found that a search catches `budget_cents` and misses
      // `max_price`, so the platform holds its responses to a closed set of keys; this holds the
      // client's type to the same standard from the other direction — whatever the key is called,
      // there is no field for it and `toJson` cannot produce one.
      final bid = Bid.fromJson(<String, dynamic>{
        ..._wire,
        'budget_cents': 150000,
        'max_price': 150000,
        'budget': 1500,
        'customer_maximum_cents': 150000,
      });

      final round = bid.toJson();

      for (final key in round.keys) {
        expect(
          key,
          isNot(anyOf(
            contains('budget'),
            contains('price'),
            contains('maximum'),
          )),
          reason: 'Bid must carry no field a customer’s maximum could be read out of '
              '(Docs/01 §4.3). `$key` is one.',
        );
      }

      expect(round.values, isNot(contains(150000)));
      expect(round.values, isNot(contains(1500)));
    });

    test('every key the type does produce is one the contract names', () {
      // The closed-key-set assertion, on the client's side, and the mirror of `providerBidKeys` in
      // internal/bidding/http_test.go. A field added here later shows up as a failing test naming
      // it, which is the only mechanism that catches "somebody added a budget and called it
      // something else".
      //
      // `amount_cents` is on this list and is **the provider's own price**, sent by this device.
      // The contract says so in as many words: it has nothing to do with anything the customer set.
      expect(
        Bid.fromJson(_wire).toJson().keys.toSet(),
        <String>{
          'id',
          'job_id',
          'status',
          'offered_by',
          'amount_cents',
          'pickup_at',
          'deliver_by',
          'message',
          'superseded_by',
          'created_at',
          'updated_at',
        },
      );
    });
  });

  group('an offer on its way to the platform', () {
    test('sends the three required fields under the contract’s own names', () {
      const placement = BidPlacement(
        amountCents: 45000,
        pickupAt: '2026-08-20T09:00:00+10:00',
        deliverBy: '2026-08-20T17:00:00+10:00',
      );

      expect(placement.toJson(), <String, dynamic>{
        'amount_cents': 45000,
        'pickup_at': '2026-08-20T09:00:00+10:00',
        'deliver_by': '2026-08-20T17:00:00+10:00',
      });
    });

    test('an empty message is omitted rather than sent blank', () {
      // Storing `""` would make "no conditions" and "an empty note" the same thing on a screen that
      // renders the message.
      const placement = BidPlacement(
        amountCents: 1,
        pickupAt: 'a',
        deliverBy: 'b',
        message: '   ',
      );

      expect(placement.toJson().containsKey('message'), isFalse);
    });

    test('a message is trimmed and sent', () {
      const placement = BidPlacement(
        amountCents: 1,
        pickupAt: 'a',
        deliverBy: 'b',
        message: '  Two people and a tail lift.  ',
      );

      expect(placement.toJson()['message'], 'Two people and a tail lift.');
    });

    test('there is no field for whose offer this is and none for its status', () {
      // `BidPlacement` is `additionalProperties: false`, so an unknown field is refused rather than
      // ignored. The bidder is whoever the token says is calling, and a bid's status is the
      // platform's — a provider id in a body would be an authorisation decision made from client
      // input, which Docs/07 §3 puts on the platform.
      const placement = BidPlacement(amountCents: 1, pickupAt: 'a', deliverBy: 'b');

      expect(placement.toJson().keys, isNot(contains('provider_id')));
      expect(placement.toJson().keys, isNot(contains('status')));
      expect(placement.toJson().keys, isNot(contains('job_id')));
    });
  });
}
