import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/auth/provider_only.dart';
import 'package:shipper/core/capture/capture_camera.dart';
import 'package:shipper/core/permissions/permission_copy.dart';
import 'package:shipper/features/profile/document_file_source.dart';
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
/// | What a refusal offers | a recorded exception reason (`Docs/01` §4.4) | a file the provider already has (`Docs/04` §3.1, SHIP-81d) |
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

  /// Whether the platform would not open a document picker at all (SHIP-81d).
  ///
  /// Its own flag rather than a stage on the controller, because nothing was submitted: the
  /// controller's state is about an image on its way to the platform, and this is about a screen
  /// that could not obtain one. Rare — unlike a camera there is no permission to refuse — and worth
  /// saying rather than swallowing, because when the camera has also been refused this is a
  /// provider with no route on at all.
  var _pickerFailed = false;

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

  /// The file-upload fallback (SHIP-81d).
  ///
  /// `Docs/04` §3.1 requires it *"so that a refused permission never blocks verification
  /// outright"*, and it is offered **whether or not the camera opened** — for two reasons. A
  /// refused permission is the case the document names, and it is the case where this is the only
  /// route on. But the document a provider needs is very often already on their phone: an insurance
  /// certificate emailed as a scan, an ABN extract downloaded from the tax office. Making them
  /// photograph a screen would be worse evidence for the administrator who has to read it.
  ///
  /// A cancelled picker is not a failure and says nothing: the provider opened it, changed their
  /// mind, and comes back to the screen they left.
  Future<void> _chooseFile() async {
    final Uint8List? bytes;
    try {
      bytes = await ref.read(documentFileSourceProvider).pick();
    } on DocumentFileUnavailable {
      if (!mounted) return;
      setState(() => _pickerFailed = true);
      return;
    }

    if (bytes == null || !mounted) return;

    // The camera is released first if it is open. The same reason the shutter releases it: nothing
    // is going to be photographed now, and a preview running behind a compression is a warm handset.
    await _camera?.stop();
    if (mounted) setState(() => _camera = null);

    final sent = await _submit(bytes);
    if (!sent && mounted && _problem == null) await _open();
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
          _ when blocked => _CameraRefused(
              failure: state.message,
              pickerFailed: _pickerFailed,
              onRetry: _retry,
              onChooseFile: _chooseFile,
            ),
          _ => _CameraOrOpening(
              kind: widget.kind,
              opening: _opening,
              camera: _camera,
              failure: state.message,
              pickerFailed: _pickerFailed,
              onShutter: _shutter,
              onRetry: _retry,
              onChooseFile: _chooseFile,
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
    required this.pickerFailed,
    required this.onShutter,
    required this.onRetry,
    required this.onChooseFile,
  });

  final VerificationDocumentKind kind;
  final bool opening;
  final CaptureCamera? camera;
  final String? failure;
  final bool pickerFailed;
  final Future<void> Function() onShutter;
  final Future<void> Function() onRetry;
  final Future<void> Function() onChooseFile;

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
        // Quiet, and under the shutter rather than beside it: photographing is the ordinary path
        // and stays the obvious one. It is here as well as on the refused panel because the
        // document is often already on the phone — see `_chooseFile`.
        Padding(
          padding: const EdgeInsets.only(bottom: 12),
          child: TextButton(
            key: const Key('capture-document-choose-file'),
            onPressed: () => unawaited(onChooseFile()),
            child: const Text('Attach a file instead'),
          ),
        ),
        if (pickerFailed)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
            child: Text(
              _pickerUnavailable,
              key: const Key('capture-document-picker-failed'),
              style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.error),
            ),
          ),
      ],
    );
  }
}

/// What a provider reads when the platform would not open a picker.
///
/// Written once and shown from both panels. It is **not** in `PermissionCopy`, which is for the
/// words shown around a *permission* — there is none to refuse here, and putting this beside the
/// camera and notification strings would suggest there is.
const _pickerUnavailable =
    'Shipper could not open your files. Try again, or photograph the document instead.';

/// The camera will not open, and the way on (SHIP-81d).
///
/// **Not an error screen, and it must never be drawn as one.** `Docs/04` §3.1 requires a file-upload
/// fallback *"so that a refused permission never blocks verification outright"*, and `Docs/07` §7
/// calls a camera flow that dead-ends on a denied permission a defect. What makes the difference
/// between a defect and a route through is not the copy; it is that the button underneath it
/// submits something.
///
/// The button is a `FilledButton` rather than the quiet `TextButton` the working screen carries,
/// because here it is the *only* thing to do. Settings is the second sentence of
/// `PermissionCopy.verificationCameraDeclined` and has no button at all — it leaves the application,
/// and a provider who has just declined a permission is being offered the way on before the way
/// back.
class _CameraRefused extends StatelessWidget {
  const _CameraRefused({
    required this.failure,
    required this.pickerFailed,
    required this.onRetry,
    required this.onChooseFile,
  });

  final String? failure;
  final bool pickerFailed;
  final Future<void> Function() onRetry;
  final Future<void> Function() onChooseFile;

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
        const SizedBox(height: 20),
        SizedBox(
          width: double.infinity,
          child: FilledButton(
            key: const Key('capture-document-choose-file-blocked'),
            onPressed: () => unawaited(onChooseFile()),
            style: FilledButton.styleFrom(padding: const EdgeInsets.symmetric(vertical: 20)),
            child: const Text('Attach a file instead'),
          ),
        ),
        if (pickerFailed) ...<Widget>[
          const SizedBox(height: 12),
          Text(
            _pickerUnavailable,
            key: const Key('capture-document-picker-failed-blocked'),
            style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.error),
          ),
        ],
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
