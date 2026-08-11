// SHIP-51's inline validation, on the device half of the split.
//
// Every case below is one this device can answer without a round trip. What it deliberately does
// *not* test is whether the platform agrees — that is the platform's decision (Docs/07 §2), and
// registration_screen_test.dart covers the form rendering its answer.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/shared/validation/validators.dart';

void main() {
  group('email', () {
    test('accepts what a mail system would', () {
      for (final address in [
        'alice@example.com',
        'alice.smith+shipper@example.com.au',
        'a@b.co',
        '  alice@example.com  ',
      ]) {
        expect(Validators.email(address), isNull, reason: address);
      }
    });

    test('refuses what cannot be an address', () {
      for (final address in ['', '   ', 'alice', 'alice@', '@example.com', 'alice@example',
        'alice@example.', 'alice@@example.com', 'alice smith@example.com']) {
        expect(Validators.email(address), isNotNull, reason: '"$address"');
      }
    });

    test('says something different about a blank field than about a malformed one', () {
      // A person who has not typed anything has made a different mistake from one who typed an
      // address wrongly, and telling them both "enter a valid email address" is worse for the
      // first of them.
      expect(Validators.email(''), isNot(Validators.email('alice')));
    });
  });

  group('australian mobile', () {
    test('accepts the forms a person actually types', () {
      // The platform normalises to E.164 (SHIP-30), so all of these are one number to it and
      // must be one number here.
      for (final number in [
        '0412345678',
        '0412 345 678',
        '0412-345-678',
        '(04) 1234 5678',
        '+61412345678',
        '+61 412 345 678',
      ]) {
        expect(Validators.australianMobile(number), isNull, reason: number);
      }
    });

    test('refuses letters, which is the case that matters', () {
      // The platform's normaliser deliberately keeps anything that is not punctuation, so
      // `0412 34a 678` is refused rather than silently repaired into somebody else's number
      // (Docs/11 §3). Letting it leave the device would only move the refusal.
      expect(Validators.australianMobile('0412 34a 678'), isNotNull);
      expect(Validators.australianMobile('not a number'), isNotNull);
    });

    test('refuses a number that is too short or absurdly long', () {
      expect(Validators.australianMobile('0412'), isNotNull);
      expect(Validators.australianMobile('0412345678901234567'), isNotNull);
      expect(Validators.australianMobile(''), isNotNull);
    });

    test('names the shape a person types, not the standard', () {
      // Somebody told their number "is not E.164" has learnt nothing. The platform's own message
      // says the same thing, deliberately.
      expect(Validators.australianMobile('nope'), contains('0412 345 678'));
    });
  });

  group('password', () {
    test('accepts the contract minimum and above', () {
      expect(Validators.password('correct-horse-battery-staple'), isNull);
      expect(Validators.password('a' * Validators.passwordMinimumLength), isNull);
    });

    test('refuses below the contract minimum', () {
      expect(Validators.password('a' * (Validators.passwordMinimumLength - 1)), isNotNull);
      expect(Validators.password(''), isNotNull);
    });

    test('has no composition rule, because composition rules produce Password1!', () {
      // contracts/paths/identity.yaml cites NIST SP 800-63B for this. A rule here that the
      // platform does not have would refuse passwords the platform accepts, with no way to
      // override it from a build already on a phone.
      expect(Validators.password('aaaaaaaaaaaa'), isNull);
      expect(Validators.password('           '), isNull);
    });
  });

  group('verification code', () {
    test('accepts exactly six digits', () {
      expect(Validators.verificationCode('408213'), isNull);
      expect(Validators.verificationCode(' 408213 '), isNull);
    });

    test('refuses anything that is not the contract shape', () {
      for (final code in ['', '40821', '4082134', '40821a', 'abcdef']) {
        expect(Validators.verificationCode(code), isNotNull, reason: '"$code"');
      }
    });
  });

  group('verification token', () {
    test('checks presence and nothing else', () {
      // Checking the 43-character base64url shape would be the client deciding which of the
      // platform's own tokens it is prepared to present — and a token format that changed
      // server-side would then be refused by every installed build before it reached the
      // endpoint that could accept it.
      expect(Validators.verificationToken('9qE2vT7bYw1sJk4pNc0aRlX8oZgHdM3uQiV6yB5tCfE'), isNull);
      expect(Validators.verificationToken('short'), isNull);
      expect(Validators.verificationToken(''), isNotNull);
      expect(Validators.verificationToken(null), isNotNull);
    });
  });
}
