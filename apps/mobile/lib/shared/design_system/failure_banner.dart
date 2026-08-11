import 'package:flutter/material.dart';

import 'package:shipper/core/errors/api_failure.dart';

/// A failure the user has to see, in the one shape every screen shows it in.
///
/// In `shared/design_system` because `Docs/07` §2 puts presentation two features would otherwise
/// each invent there, and "how a refused request looks" is exactly that — four signup screens
/// need it before any other feature exists, and identity may not be the package the other seven
/// import it from.
///
/// Two rules it keeps:
///
/// - **It shows [ApiFailure.userMessage] and never the exception.** `Docs/07` §7 treats
///   user-facing copy as build work, and a stack trace or a `DioException` in front of somebody
///   is a defect rather than a diagnostic.
/// - **It shows the request ID when there is one.** SHIP-12 puts it in the body as well as the
///   header for this reason: somebody reporting a problem sends a screenshot, and a header does
///   not survive one.
class FailureBanner extends StatelessWidget {
  const FailureBanner(this.failure, {super.key});

  final ApiFailure failure;

  /// The request ID, when the platform's own error contract carried one.
  String? get _reference => switch (failure) {
        ApiErrorResponse(:final requestId) => requestId,
        _ => null,
      };

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final reference = _reference;

    return Container(
      key: const Key('failure-banner'),
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: scheme.errorContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            failure.userMessage,
            style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onErrorContainer),
          ),
          if (reference != null) ...[
            const SizedBox(height: 4),
            Text(
              'Reference $reference',
              style: theme.textTheme.bodySmall?.copyWith(color: scheme.onErrorContainer),
            ),
          ],
        ],
      ),
    );
  }
}
