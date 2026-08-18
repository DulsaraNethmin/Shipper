import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/provider_only.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/profile/verification_document.dart';
import 'package:shipper/features/profile/verification_documents_controller.dart';

/// The four documents `Docs/04` §3 collects, and which of them this provider has sent (SHIP-81c).
///
/// ## Why this is `features/profile/` and not an eighth feature
///
/// `Docs/07` §2 draws a closed list of seven and assigns this one in as many words: *"Verification
/// evidence belongs to `profile/`, capture as well as display… the alternative — a `verification/`
/// feature — reads as the tidier answer right up until it duplicates what `profile/` is already
/// for."* `architecture_test.dart` holds the list, so the decision is enforced rather than
/// remembered.
///
/// ## Four rows and no progress bar
///
/// The list is fixed at four and drawn in the platform's own order, so a provider can see at a
/// glance what is outstanding. What it deliberately does **not** show is anything resembling
/// progress towards being verified: `Docs/04` §4 makes that an administrator's decision on the
/// whole record, and four ticks are evidence submitted rather than a state changed. A screen that
/// said "3 of 4" would be read as "one more and I can bid", which is not what any of this means —
/// `Verified` is necessary to bid and is not sufficient, and this application must never imply that
/// a document arriving is a decision being taken.
///
/// ## The list is the platform's answer and never this device's
///
/// A submitted document appears here because `GET /v1/provider/verification/documents` says so,
/// after the capture screen invalidated the read. Appending a row locally would be the client
/// asserting something the platform has not confirmed — and unlike a queued delivery milestone,
/// which `Docs/07` §4 marks as pending precisely because it is unconfirmed, there is no queue here
/// and so no pending state to be honest with.
class VerificationDocumentsScreen extends ConsumerWidget {
  const VerificationDocumentsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Scaffold(
      appBar: AppBar(title: const Text('Verification documents')),
      body: ProviderOnly(
        child: RefreshIndicator(
          onRefresh: () async => ref.invalidate(verificationDocumentsProvider),
          child: ref.watch(verificationDocumentsProvider).when(
                loading: () => const Center(
                  key: Key('verification-documents-loading'),
                  child: CircularProgressIndicator(),
                ),
                error: (error, stack) => _Failed(
                  onRetry: () => ref.invalidate(verificationDocumentsProvider),
                ),
                data: (documents) => _Documents(documents: documents),
              ),
        ),
      ),
    );
  }
}

/// The four rows.
class _Documents extends StatelessWidget {
  const _Documents({required this.documents});

  final List<VerificationDocument> documents;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final submitted = documents.map((document) => document.kind).toSet();

    return ListView(
      key: const Key('verification-documents'),
      padding: const EdgeInsets.all(20),
      children: <Widget>[
        Text(
          // `Docs/04` §3's own words, said to the person they are about. The last sentence is the
          // one that stops this reading as a form: an administrator looks at every one of these by
          // eye, so "sent" is not "approved" and the wait is a real one.
          'Shipper collects four documents from every transport provider. Somebody reviews each of '
          'them, so there is a wait between sending one and being able to bid.',
          key: const Key('verification-documents-preamble'),
          style: theme.textTheme.bodyLarge,
        ),
        const SizedBox(height: 20),
        for (final kind in VerificationDocumentKind.values)
          _DocumentRow(kind: kind, submitted: submitted.contains(kind)),
      ],
    );
  }
}

/// One of the four.
class _DocumentRow extends StatelessWidget {
  const _DocumentRow({required this.kind, required this.submitted});

  final VerificationDocumentKind kind;

  /// Whether the platform holds at least one image of this kind.
  final bool submitted;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Card(
      key: Key('verification-document-${kind.wire}'),
      margin: const EdgeInsets.only(bottom: 12),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Row(
              children: <Widget>[
                Icon(
                  submitted ? Icons.check_circle_outline : Icons.upload_file_outlined,
                  color: submitted ? theme.colorScheme.primary : theme.colorScheme.onSurfaceVariant,
                ),
                const SizedBox(width: 8),
                Expanded(child: Text(kind.label, style: theme.textTheme.titleMedium)),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              submitted
                  // Not "approved", and not "verified". The document is with the platform and a
                  // person has to look at it — see the note on the screen.
                  ? 'Sent to Shipper. Somebody will review it.'
                  : kind.guidance,
              key: Key('verification-document-${kind.wire}-state'),
              style: theme.textTheme.bodyMedium,
            ),
            const SizedBox(height: 12),
            Align(
              alignment: Alignment.centerLeft,
              child: FilledButton.tonal(
                key: Key('verification-document-${kind.wire}-capture'),
                onPressed: () => context.push(Routes.captureDocumentFor(kind.wire)),
                // A retake is a new submission rather than an edit — the platform keeps every one
                // and the newest of a kind is current — so the word changes and the action does not.
                child: Text(submitted ? 'Replace it' : 'Photograph it'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// The list could not be read.
class _Failed extends StatelessWidget {
  const _Failed({required this.onRetry});

  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return ListView(
      key: const Key('verification-documents-failed'),
      padding: const EdgeInsets.all(24),
      children: <Widget>[
        Text(
          'Shipper could not read your documents just now.',
          style: theme.textTheme.titleMedium,
        ),
        const SizedBox(height: 8),
        Text(
          'Nothing has been lost — this is only the list. Try again in a moment.',
          style: theme.textTheme.bodyMedium,
        ),
        const SizedBox(height: 16),
        Align(
          alignment: Alignment.centerLeft,
          child: FilledButton(
            key: const Key('verification-documents-retry'),
            onPressed: onRetry,
            child: const Text('Try again'),
          ),
        ),
      ],
    );
  }
}
