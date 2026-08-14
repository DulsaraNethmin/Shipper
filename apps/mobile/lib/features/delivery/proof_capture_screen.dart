import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/auth/provider_only.dart';
import 'package:shipper/core/permissions/permission_copy.dart';
import 'package:shipper/features/delivery/capture_proof_controller.dart';
import 'package:shipper/features/delivery/proof_camera.dart';

/// Photographing the delivery (SHIP-130).
///
/// The application's own camera, not the handset's: one preview and one shutter, for the reasons
/// `proof_camera.dart` sets out — the first of which is that it is the only arrangement in which
/// "never written to the photo library" is a property of this build rather than of whichever camera
/// application the manufacturer shipped.
///
/// ## What it shows when the camera will not open
///
/// `PermissionCopy.cameraDeclined` (SHIP-179), which already says the honest thing: the camera
/// cannot be opened, a reason can be recorded instead, and settings is the way back. **The button
/// that records the reason is SHIP-131** and is deliberately not here — `Docs/01` §4.4's exception
/// path needs `POST /v1/jobs/{id}/milestones` with `proof.exception_reason` (SHIP-116), and offering
/// a button that queued nothing would be a worse dead end than naming the gap.
///
/// ## The screen holds the camera, and gives it back
///
/// A camera is a device resource. It is opened in `initState`, released in `dispose`, and released
/// again as soon as the photograph is taken — a preview left running behind a confirmation panel is
/// a hot phone and a flat battery on the device a driver needs for the rest of the day.
class ProofCaptureScreen extends ConsumerStatefulWidget {
  const ProofCaptureScreen({required this.jobId, super.key});

  /// The job being delivered. From the path, which is what makes this deep-linkable in the same way
  /// the delivery screen is.
  final String jobId;

  @override
  ConsumerState<ProofCaptureScreen> createState() => _ProofCaptureScreenState();
}

class _ProofCaptureScreenState extends ConsumerState<ProofCaptureScreen> {
  ProofCamera? _camera;
  ProofCameraProblem? _problem;
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
    final camera = ref.read(proofCameraProvider)();

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
    } on ProofCameraUnavailable catch (e) {
      if (!mounted) return;
      setState(() {
        _problem = e.problem;
        _opening = false;
      });
    } catch (_) {
      // The screen must reach a settled state whatever the device did. A spinner that never stops
      // is the worst answer available here — worse than "the camera cannot be opened", which at
      // least tells the driver where they stand and, from SHIP-131, offers them a way on.
      if (!mounted) return;
      setState(() {
        _problem = ProofCameraProblem.unavailable;
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
    } on ProofCameraUnavailable catch (e) {
      if (!mounted) return;
      setState(() => _problem = e.problem);
      return;
    }

    // Released before the compression rather than after it. The photograph is taken, so the preview
    // is finished with, and holding a camera open through a second of image processing is the
    // difference a driver feels in the back of the handset.
    await camera.stop();
    if (!mounted) return;
    setState(() => _camera = null);

    final queued = await ref.read(captureProofProvider(widget.jobId).notifier).queueFrom(bytes);

    // A capture that failed has to leave a shutter behind it, or the driver is looking at the
    // permission explanation for a camera that was working a second ago — which reads as the app
    // having lost the photograph *and* the camera. Reopening is what makes "take it again", which is
    // what every one of those failures tells them to do, something they can actually do.
    if (!queued && mounted) await _open();
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(captureProofProvider(widget.jobId));

    return Scaffold(
      appBar: AppBar(title: const Text('Photograph the delivery')),
      body: ProviderOnly(
        child: switch (state.stage) {
          ProofCaptureStage.queued => _Queued(onDone: () => Navigator.of(context).pop()),
          ProofCaptureStage.compressing => const _Working(),
          _ => _CameraOrReason(
              opening: _opening,
              problem: _problem,
              camera: _camera,
              failure: state.message,
              onShutter: _shutter,
            ),
        },
      ),
    );
  }
}

/// The preview and the shutter, or the sentence explaining why there is neither.
class _CameraOrReason extends StatelessWidget {
  const _CameraOrReason({
    required this.opening,
    required this.problem,
    required this.camera,
    required this.failure,
    required this.onShutter,
  });

  final bool opening;
  final ProofCameraProblem? problem;
  final ProofCamera? camera;
  final String? failure;
  final Future<void> Function() onShutter;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    if (opening) {
      return const Center(
        key: Key('proof-capture-opening'),
        child: CircularProgressIndicator(),
      );
    }

    final blocked = problem;
    if (blocked != null || camera == null) {
      return Padding(
        key: const Key('proof-capture-blocked'),
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Icon(Icons.no_photography_outlined, color: theme.colorScheme.error),
            const SizedBox(height: 12),
            // SHIP-179's own words. Australian English, says what happens next, and — importantly —
            // says the delivery can still be finished, which is what stops this being the dead end
            // `Docs/07` §7 calls a defect. SHIP-131 is what makes the second half of the sentence
            // true from this screen.
            Text(PermissionCopy.cameraDeclined, style: theme.textTheme.bodyLarge),
          ],
        ),
      );
    }

    return Column(
      key: const Key('proof-capture-screen'),
      children: <Widget>[
        Expanded(child: camera!.preview()),
        if (failure != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
            child: Text(
              failure!,
              key: const Key('proof-capture-failed'),
              style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.error),
            ),
          ),
        Padding(
          padding: const EdgeInsets.all(20),
          child: SizedBox(
            width: double.infinity,
            child: FilledButton(
              key: const Key('proof-capture-shutter'),
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

/// The one step with a delay a person notices. See [ProofCaptureStage.compressing].
class _Working extends StatelessWidget {
  const _Working();

  @override
  Widget build(BuildContext context) {
    return const Center(
      key: Key('proof-capture-working'),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: <Widget>[
          CircularProgressIndicator(),
          SizedBox(height: 16),
          Text('Making the photograph small enough to send…'),
        ],
      ),
    );
  }
}

/// On the device, in the queue, and not yet anywhere else.
class _Queued extends StatelessWidget {
  const _Queued({required this.onDone});

  final VoidCallback onDone;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      key: const Key('proof-capture-queued'),
      padding: const EdgeInsets.all(24),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Icon(Icons.check_circle_outline, color: theme.colorScheme.primary),
          const SizedBox(height: 12),
          Text('Photograph saved', style: theme.textTheme.titleMedium),
          const SizedBox(height: 8),
          Text(
            // The promise `Docs/07` §4 makes, said to the person who has to trust it — and said
            // *precisely*, because "sent" would be a lie on a phone with no signal and the pending
            // indicator would then contradict this screen.
            'It is on this phone and Shipper will send it when there is a connection. You do not '
            'have to wait here.',
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 24),
          FilledButton(
            key: const Key('proof-capture-done'),
            onPressed: onDone,
            child: const Text('Back to the delivery'),
          ),
        ],
      ),
    );
  }
}
