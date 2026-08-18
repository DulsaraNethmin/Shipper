import 'package:shipper/core/capture/captured_image.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/profile/verification_document.dart';
import 'package:shipper/features/profile/verification_repository.dart';

/// A verification repository that records what it was asked and answers what a test set.
///
/// **It records the compressed image rather than only the fact of a call**, which is what lets a
/// test assert on `Docs/04` §3.1's "compressed on the device" clause from the screen's side: what
/// reaches the platform is the output of the real compressor, so its size and its dimensions are
/// measurable and are not something a mock agreed to.
class FakeVerificationRepository implements VerificationRepository {
  /// What [documents] answers. Empty is the honest default: it is every provider on the day they
  /// register.
  List<VerificationDocument> stored = <VerificationDocument>[];

  /// Thrown by [documents] when set, which is how the list screen's failure state is reached.
  Object? readFailure;

  /// Thrown by [submit] when set. Cleared after one throw when [failOnce] is true, so a test can
  /// drive a failure and then a successful retry.
  Object? submitFailure;
  bool failOnce = false;

  /// Every submission, in order.
  final submissions = <({VerificationDocumentKind kind, CapturedImage image, String key})>[];

  int reads = 0;

  @override
  Future<List<VerificationDocument>> documents() async {
    reads++;
    final failure = readFailure;
    if (failure != null) throw failure;
    return stored;
  }

  @override
  Future<VerificationDocument> submit({
    required VerificationDocumentKind kind,
    required CapturedImage image,
    required String idempotencyKey,
  }) async {
    submissions.add((kind: kind, image: image, key: idempotencyKey));

    final failure = submitFailure;
    if (failure != null) {
      if (failOnce) submitFailure = null;
      throw failure;
    }

    final document = VerificationDocument(
      id: 'doc-${submissions.length}',
      kind: kind,
      submittedAt: DateTime.utc(2026, 8, 18, 4, 15),
    );
    stored = <VerificationDocument>[document, ...stored];
    return document;
  }
}

/// The refusal the platform gives when the PUT has not finished.
///
/// A real `ApiErrorResponse` with the contract's own code rather than a bare exception, because the
/// screen branches on `code` and nothing else — `Docs/10` §4.6 and `CLAUDE.md` both say a client
/// branches on the code and never on the message.
ApiErrorResponse notUploaded() => const ApiErrorResponse(
      statusCode: 409,
      code: 'profiles_document_not_uploaded',
      message: 'the store holds nothing under that key',
    );
