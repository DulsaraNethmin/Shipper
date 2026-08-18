import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/auth/provider_only.dart';
import 'package:shipper/core/capture/capture_camera.dart';
import 'package:shipper/core/permissions/permission_copy.dart';
import 'package:shipper/features/profile/verification_document.dart';
import 'package:shipper/features/profile/verification_documents_controller.dart';

/// Photographing one verification document (SHIP-81c).
///
/// The application's own camera, not the handset's: one preview and one shutter, for the reasons
/// `core/capture/capture_camera.dart` sets out — the first of which is that it is the only
/// arrangement in which *"verification images must not be written to the device photo library"*
/// (`Docs/04` §3.1) is a property of this build rather than of whichever camera application the
/// manufacturer shipped.
///
/// ## It is a near-twin of `proof_capture_screen.dart` and is deliberately not the same screen
///
/// The two share their camera, their compressor and their store, which is what SHIP-81c's move to
/// `core/capture/` was for. What they do not share is what happens next, and it is not a detail:
///
/// | | Proof of delivery | A verification document |
/// |---|---|---|
/// | Where it goes | the offline queue, sent later | three requests, now |
/// | What a refusal offers | a recorded exception reason (`Docs/01` §4.4) | a file the provider already has (`Docs/04` §3.1) |
/// | What "done" says | "on this phone, Shipper will send it" | "Shipper has it, somebody will review it" |
///
/// A single screen parameterised over those three would be a screen whose every branch is one
/// caller's, which is the shape that quietly acquires a fourth difference nobody notices.
///
/// ## The screen holds the camera, and gives it back
///
/// A camera is a device resource. It is opened in `initState`, released in `dispose`, and released
/// again as soon as the photograph is taken — a preview left running behind a confirmation panel is
/// a hot phone and a flat battery.
class CaptureDocumentScreen extends ConsumerStatefulWidget {
  const CaptureDocumentScreen({required this.kind, super.key});

  /// Which of the four is being photographed. From the path, which is what makes this
  /// deep-linkable in the same way the delivery capture screen is.
  final VerificationDocumentKind kind;

  @override
  ConsumerState<CaptureDocumentScreen> createState() => _CaptureDocumentScreenState();
}

class _CaptureDocumentScreenState extends ConsumerState<CaptureDocumentScreen> {
  CaptureCamera? _camera;
  CameraProblem? _problem;
  var _opening = true;

  @override
  void initState() {
    super.initState();
    unawaited(_open());
  }

  @override
  void dispose() {
    unawaited(_camera?.stop());
    super.dispose();
  }

  Future<void> _open() async {
    final camera = ref.read(captureCameraProvider)();

    try {
      await camera.start();
      if (!mounted) {
        await camera.stop();
        return;
      }
      setState(() {
        _camera = camera;
        _opening = false;
      });
    } on CameraUnavailable catch (e) {
      if (!mounted) return;
      setState(() {
        _problem = e.problem;
        _opening = false;
      });
    } catch (_) {
      // The screen must reach a settled state whatever the device did. A spinner that never stops
      // is the worst answer available here — worse than "the camera cannot be opened", which at
      // least says where the provider stands.
      if (!mounted) return;
      setState(() {
        _problem = CameraProblem.unavailable;
        _opening = false;
      });
    }
  }

  Future<void> _shutter() async {
    final camera = _camera;
    if (camera == null) return;

    final Uint8List bytes;
    try {
      bytes = await camera.capture();
    } on CameraUnavailable catch (e) {
      if (!mounted) return;
      setState(() => _problem = e.problem);
      return;
    }

    // Released before the compression rather than after it. The photograph is taken, so the preview
    // is finished with, and holding a camera open through a second of image processing is a warm
    // handset for no purpose.
    await camera.stop();
    if (!mounted) return;
    setState(() => _camera = null);

    final sent = await _submit(bytes);

    // A submission that failed has to leave a shutter behind it, or the provider is looking at the
    // permission explanation for a camera that was working a second ago — which reads as the app
    // having lost the photograph *and* the camera. Reopening is what makes "take it again" something
    // they can actually do, beside the "send it again" the failure itself offers.
    if (!sent && mounted) await _open();
  }

  /// Compresses, sends, and re-reads the list when the platform has it.
  ///
  /// The invalidation is here rather than in the controller because it is a fact about what the
  /// *other* screen should now show, and a controller that reached into a sibling provider's cache
  /// would be a controller with an opinion about navigation.
  Future<bool> _submit(Uint8List bytes) async {
    final sent = await ref.read(captureDocumentProvider(widget.kind).notifier).submit(bytes);
    if (sent) ref.invalidate(verificationDocumentsProvider);
    return sent;
  }

  /// A second attempt at the upload, from the image already on this device.
  ///
  /// **Not a second photograph**, which is the whole reason the compressed file outlives a failure:
  /// `Docs/04` §3.1 requires it cleared once uploaded and says nothing about before, and a provider
  /// whose connection dropped should not be asked to find their licence again.
  Future<void> _retry() async {
    final sent = await ref.read(captureDocumentProvider(widget.kind).notifier).retry();
    if (sent) ref.invalidate(verificationDocumentsProvider);
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(captureDocumentProvider(widget.kind));

    // **`_problem` and nothing else.** `_camera == null` is also true for the moment between the
    // shutter and the reopen, and reading that as a refusal would tell a provider whose camera works
    // perfectly that their permission was denied. `proof_capture_screen.dart` can widen the test
    // because a refusal and a released camera lead to the same panel there; here they do not.
    final blocked = _problem != null;

    return Scaffold(
      appBar: AppBar(title: Text(widget.kind.label)),
      body: ProviderOnly(
        child: switch (state.stage) {
          DocumentCaptureStage.submitted =>
            _Submitted(kind: widget.kind, onDone: () => Navigator.of(context).pop()),
          DocumentCaptureStage.compressing => const _Working(
              key: Key('capture-document-compressing'),
              words: 'Making the photograph small enough to send…',
            ),
          DocumentCaptureStage.uploading => const _Working(
              key: Key('capture-document-uploading'),
              words: 'Sending it to Shipper…',
            ),
          _ when blocked => _CameraRefused(failure: state.message, onRetry: _retry),
          _ => _CameraOrOpening(
              kind: widget.kind,
              opening: _opening,
              camera: _camera,
              failure: state.message,
              onShutter: _shutter,
              onRetry: _retry,
            ),
        },
      ),
    );
  }
}

/// The preview and the shutter, while there is a camera to draw one with.
class _CameraOrOpening extends StatelessWidget {
  const _CameraOrOpening({
    required this.kind,
    required this.opening,
    required this.camera,
    required this.failure,
    required this.onShutter,
    required this.onRetry,
  });

  final VerificationDocumentKind kind;
  final bool opening;
  final CaptureCamera? camera;
  final String? failure;
  final Future<void> Function() onShutter;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    if (opening || camera == null) {
      return const Center(
        key: Key('capture-document-opening'),
        child: CircularProgressIndicator(),
      );
    }

    return Column(
      key: const Key('capture-document-screen'),
      children: <Widget>[
        Expanded(child: camera!.preview()),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
          child: Text(
            kind.guidance,
            key: Key('capture-document-guidance-${kind.wire}'),
            style: theme.textTheme.bodyMedium,
          ),
        ),
        if (failure != null) ...<Widget>[
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
            child: Text(
              failure!,
              key: const Key('capture-document-failed'),
              style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.error),
            ),
          ),
          TextButton(
            key: const Key('capture-document-retry'),
            onPressed: () => unawaited(onRetry()),
            child: const Text('Send it again'),
          ),
        ],
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 20),
          child: SizedBox(
            width: double.infinity,
            child: FilledButton(
              key: const Key('capture-document-shutter'),
              onPressed: () => unawaited(onShutter()),
              style: FilledButton.styleFrom(padding: const EdgeInsets.symmetric(vertical: 20)),
              child: const Text('Take the photograph'),
            ),
          ),
        ),
      ],
    );
  }
}

/// The camera will not open.
///
/// **This is a dead end until SHIP-81d, and it is recorded as one rather than dressed up.**
/// `Docs/04` §3.1 requires a file-upload fallback *"so that a refused permission never blocks
/// verification outright"*, and `Docs/07` §7 calls a camera flow that dead-ends on a denied
/// permission a defect. SHIP-81c stops here honestly — settings, or the image already on the phone
/// if one attempt has been made — and SHIP-81d is the ticket that puts a second button under this
/// sentence.
class _CameraRefused extends StatelessWidget {
  const _CameraRefused({required this.failure, required this.onRetry});

  final String? failure;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return ListView(
      key: const Key('capture-document-blocked'),
      padding: const EdgeInsets.all(24),
      children: <Widget>[
        Icon(Icons.no_photography_outlined, color: theme.colorScheme.onSurfaceVariant),
        const SizedBox(height: 12),
        Text(
          PermissionCopy.verificationCameraDeclined,
          key: const Key('capture-document-declined'),
          style: theme.textTheme.bodyLarge,
        ),
        if (failure != null) ...<Widget>[
          const SizedBox(height: 16),
          Text(
            failure!,
            key: const Key('capture-document-blocked-failed'),
            style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.error),
          ),
          const SizedBox(height: 8),
          Align(
            alignment: Alignment.centerLeft,
            child: FilledButton(
              key: const Key('capture-document-blocked-retry'),
              onPressed: () => unawaited(onRetry()),
              child: const Text('Send it again'),
            ),
          ),
        ],
      ],
    );
  }
}

/// The one or two steps with a delay a person notices.
class _Working extends StatelessWidget {
  const _Working({required this.words, super.key});

  final String words;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: <Widget>[
          const CircularProgressIndicator(),
          const SizedBox(height: 16),
          Text(words),
        ],
      ),
    );
  }
}

/// The platform has it.
class _Submitted extends StatelessWidget {
  const _Submitted({required this.kind, required this.onDone});

  final VerificationDocumentKind kind;
  final VoidCallback onDone;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      key: const Key('capture-document-submitted'),
      padding: const EdgeInsets.all(24),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Icon(Icons.check_circle_outline, color: theme.colorScheme.primary),
          const SizedBox(height: 12),
          Text('${kind.label} sent', style: theme.textTheme.titleMedium),
          const SizedBox(height: 8),
          Text(
            // **"Sent", not "verified", and the difference is the whole of `Docs/04` §4.** An
            // administrator reviews every image by eye and records a decision; a document arriving
            // is evidence, not an outcome. A provider told "verified" here would go looking for
            // work they cannot bid on yet.
            'Shipper has it, and the photograph has been removed from this phone. Somebody will '
            'review it along with your other documents.',
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 24),
          FilledButton(
            key: const Key('capture-document-done'),
            onPressed: onDone,
            child: const Text('Back to your documents'),
          ),
        ],
      ),
    );
  }
}
