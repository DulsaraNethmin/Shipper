import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/auth/provider_only.dart';
import 'package:shipper/core/permissions/permission_copy.dart';
import 'package:shipper/features/delivery/capture_proof_controller.dart';
import 'package:shipper/features/delivery/milestone.dart';
import 'package:shipper/core/capture/capture_camera.dart';
import 'package:shipper/features/delivery/proof_exception_reason.dart';

/// Photographing the delivery (SHIP-130).
///
/// The application's own camera, not the handset's: one preview and one shutter, for the reasons
/// `proof_camera.dart` sets out — the first of which is that it is the only arrangement in which
/// "never written to the photo library" is a property of this build rather than of whichever camera
/// application the manufacturer shipped.
///
/// ## What it shows when the camera will not open (SHIP-131)
///
/// `PermissionCopy.cameraDeclined` (SHIP-179) has said the honest thing since it was written — the
/// camera cannot be opened, a reason can be recorded instead, and settings is the way back — and the
/// last of those three was a sentence this screen could not act on. **SHIP-131 makes the second half
/// true**: the three reasons `Docs/01` §4.4 names are offered, one is chosen, and it is queued as
/// `POST /v1/jobs/{id}/milestones` with `proof.exception_reason` (SHIP-116).
///
/// `Docs/07` §7 calls a camera flow that dead-ends on a denied permission a defect. What makes the
/// difference between a defect and a route through the job is not the copy; it is that the button
/// underneath it queues something.
///
/// ## The reason is reachable when the camera works, and that is not scope creep
///
/// Only one of `Docs/01` §4.4's three reasons is about the camera. `recipient_objected` and
/// `location_unsafe` are conditions of the **delivery**, and a driver who meets either while holding
/// a perfectly good camera has the same problem SHIP-131 exists to solve — they are standing at a
/// delivery point unable to finish the job. So the working screen carries a quiet way to the same
/// panel, under the shutter rather than beside it: photographing is the ordinary path and stays the
/// obvious one.
///
/// ## Choosing and recording are two taps, deliberately
///
/// A reason cannot be taken back from this screen — `Docs/01` §4.3 requires every one to be recorded
/// and there is no endpoint that removes one — and one-tap-per-reason is three targets a gloved
/// thumb can hit by accident in the rain. The driver selects, reads what they selected, and records.
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
  CaptureCamera? _camera;
  CameraProblem? _problem;
  var _opening = true;

  /// Whether the driver has asked to record a reason instead of photographing (SHIP-131).
  ///
  /// Always true once the camera has refused — there is nothing else this screen can offer — and
  /// settable from the working screen by the driver who meets one of the two reasons that are not
  /// about the camera at all.
  var _reasoning = false;

  /// The reason selected and not yet recorded. See the note on the class about the two taps.
  ProofExceptionReason? _chosen;

  /// The driver's own words, beside the reason rather than instead of it (SHIP-131a).
  ///
  /// `Docs/01` §4.4's list is closed and stays closed — `Docs/04` §5 wants a reason it can group —
  /// and this is the sentence that says which of the three it actually was. "The recipient asked me
  /// not to photograph their door" is what turns a delivery-exception queue entry into a decision,
  /// and until this field there was nowhere to type it.
  final _note = TextEditingController();

  @override
  void initState() {
    super.initState();
    unawaited(_open());
  }

  @override
  void dispose() {
    _note.dispose();
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
      // least tells the driver where they stand and, from SHIP-131, offers them a way on.
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

  /// Records the selected reason and gives the camera back (SHIP-131).
  ///
  /// The camera is released before the queue write rather than after it, for the reason [_shutter]
  /// releases it before compressing: the driver has finished with the preview, and holding a camera
  /// open through a database write is a warm handset for no purpose.
  Future<void> _record() async {
    final reason = _chosen;
    if (reason == null) return;

    await _camera?.stop();
    if (mounted) setState(() => _camera = null);

    await ref
        .read(captureProofProvider(widget.jobId).notifier)
        .queueException(reason, note: _note.text);
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(captureProofProvider(widget.jobId));

    // Once the camera has refused there is nothing else to offer, so the reason panel is not
    // something the driver has to ask for. `Docs/07` §7's dead end is precisely the screen that
    // makes them.
    final blocked = _problem != null || (!_opening && _camera == null);

    return Scaffold(
      appBar: AppBar(title: const Text('Photograph the delivery')),
      body: ProviderOnly(
        child: switch (state.stage) {
          ProofCaptureStage.queued => _Queued(onDone: () => Navigator.of(context).pop()),
          ProofCaptureStage.reasonRecorded =>
            _ReasonRecorded(onDone: () => Navigator.of(context).pop()),
          ProofCaptureStage.compressing => const _Working(),
          _ when blocked || _reasoning => _Reason(
              // The words are `PermissionCopy.cameraDeclined` only when the camera is why. A driver
              // whose recipient objected is not being told their camera will not open.
              blocked: blocked,
              chosen: _chosen,
              note: _note,
              failure: state.message,
              onChoose: (reason) => setState(() => _chosen = reason),
              onRecord: _record,
              onBack: blocked ? null : () => setState(() => _reasoning = false),
            ),
          _ => _CameraOrOpening(
              opening: _opening,
              camera: _camera,
              failure: state.message,
              onShutter: _shutter,
              onCannotPhotograph: () => setState(() => _reasoning = true),
            ),
        },
      ),
    );
  }
}

/// The preview and the shutter, while there is a camera to draw one with.
class _CameraOrOpening extends StatelessWidget {
  const _CameraOrOpening({
    required this.opening,
    required this.camera,
    required this.failure,
    required this.onShutter,
    required this.onCannotPhotograph,
  });

  final bool opening;
  final CaptureCamera? camera;
  final String? failure;
  final Future<void> Function() onShutter;
  final VoidCallback onCannotPhotograph;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    if (opening || camera == null) {
      return const Center(
        key: Key('proof-capture-opening'),
        child: CircularProgressIndicator(),
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
          padding: const EdgeInsets.fromLTRB(20, 20, 20, 8),
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
        Padding(
          padding: const EdgeInsets.only(bottom: 12),
          child: TextButton(
            // Quiet, and under the shutter rather than beside it: photographing is the ordinary path
            // and stays the obvious one. It is here because only one of `Docs/01` §4.4's three
            // reasons is about the camera — see the note on the screen.
            key: const Key('proof-cannot-photograph'),
            onPressed: onCannotPhotograph,
            child: const Text('I cannot photograph this delivery'),
          ),
        ),
      ],
    );
  }
}

/// `Docs/01` §4.4's exception path: the three reasons, and the one that was chosen (SHIP-131).
///
/// **Not an error screen, and it must never be drawn as one.** An exception is evidence rather than
/// the absence of it — a reason from a closed list, recorded in the same transaction and the same
/// table as the photographs — and a driver who reads this as a failure will keep trying the camera
/// at a door somebody is waiting behind.
///
/// There is deliberately **no free-text option in place of the list**. `Docs/01` §4.4 wrote the list
/// closed and `Docs/04` §5 gives the reason: a reason nobody can group is a moderation queue nobody
/// can triage. **The driver's own words go beside the selection and never instead of it**
/// (SHIP-131a) — the milestone's `reason` field, optional and 500 characters, on the same request as
/// the chosen `exception_reason`. The record button is still disabled until one of the three is
/// selected, and a note alone records nothing.
///
/// That is what makes the pair useful rather than redundant. The selection is what a queue can group
/// and a `CHECK` constraint can hold; the sentence is which of the three it actually was. "The
/// recipient asked me not to photograph their door" is a decision a moderator can make, and
/// `recipient_objected` on its own is a row they have to ring somebody about.
class _Reason extends StatelessWidget {
  const _Reason({
    required this.blocked,
    required this.chosen,
    required this.note,
    required this.failure,
    required this.onChoose,
    required this.onRecord,
    required this.onBack,
  });

  /// Whether the camera is why this panel is showing, which decides the sentence at the top.
  final bool blocked;

  final ProofExceptionReason? chosen;

  /// The driver's own words. Held by the screen so it survives the panel being rebuilt on every
  /// selection — a note typed and then lost by choosing a second reason is a note typed once.
  final TextEditingController note;

  final String? failure;
  final void Function(ProofExceptionReason) onChoose;
  final Future<void> Function() onRecord;

  /// Back to the preview, or `null` when there is no preview to go back to.
  final VoidCallback? onBack;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return ListView(
      key: const Key('proof-capture-blocked'),
      padding: const EdgeInsets.all(24),
      children: <Widget>[
        Icon(
          Icons.no_photography_outlined,
          // The camera's colour scheme, not the error one. `Docs/01` §4.4 makes this a route
          // through the job rather than a fault, and a red icon says the opposite of the copy.
          color: theme.colorScheme.onSurfaceVariant,
        ),
        const SizedBox(height: 12),
        Text(
          // SHIP-179's own words when the camera is why: Australian English, says what happens next,
          // and says the delivery can still be finished. The other sentence is for a driver whose
          // camera is working perfectly and whose problem is the delivery point.
          blocked
              ? PermissionCopy.cameraDeclined
              : 'You can record why there is no photograph and finish the delivery. Shipper keeps '
                  'the reason with the delivery, and the customer is shown it.',
          key: const Key('proof-exception-preamble'),
          style: theme.textTheme.bodyLarge,
        ),
        const SizedBox(height: 24),
        Text(
          'Why is there no photograph?',
          style: theme.textTheme.titleSmall?.copyWith(color: theme.colorScheme.primary),
        ),
        const SizedBox(height: 8),
        // `RadioGroup` rather than a `groupValue` on each tile: the per-tile form is deprecated
        // after Flutter 3.32 and `make flutter-analyze` treats an `info` as a failure, so the
        // deprecated shape would not have got past the gate.
        RadioGroup<ProofExceptionReason>(
          groupValue: chosen,
          onChanged: (picked) {
            if (picked != null) onChoose(picked);
          },
          child: Column(
            children: <Widget>[
              for (final reason in ProofExceptionReasonCopy.offered)
                RadioListTile<ProofExceptionReason>(
                  // Keyed by the wire form rather than the label: a test naming
                  // `proof-exception-camera_unavailable` is naming the contract, and the label is
                  // copy that may be reworded.
                  key: Key('proof-exception-${reason.wireName}'),
                  value: reason,
                  contentPadding: EdgeInsets.zero,
                  title: Text(reason.driverPrompt),
                ),
            ],
          ),
        ),
        const SizedBox(height: 16),
        // Under the three and above the button that commits them: the driver chooses, then says
        // what the choice does not cover, then records. A field above the list would be answering a
        // question nobody had asked yet.
        TextField(
          key: const Key('proof-exception-note'),
          controller: note,
          maxLength: milestoneNoteMaxLength,
          maxLines: 3,
          minLines: 1,
          textCapitalization: TextCapitalization.sentences,
          keyboardType: TextInputType.multiline,
          decoration: const InputDecoration(
            labelText: 'Anything to add? (optional)',
            helperText: 'Kept with the delivery. The customer can see it.',
            counterText: '',
            border: OutlineInputBorder(),
          ),
        ),

        if (failure != null) ...[
          const SizedBox(height: 8),
          Text(
            failure!,
            key: const Key('proof-exception-failed'),
            style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.error),
          ),
        ],
        const SizedBox(height: 16),
        SizedBox(
          width: double.infinity,
          child: FilledButton(
            key: const Key('proof-exception-record'),
            // Disabled until something is selected. Two taps, and the second is the commitment —
            // see the note on the screen about why one is not enough.
            onPressed: chosen == null ? null : () => unawaited(onRecord()),
            style: FilledButton.styleFrom(padding: const EdgeInsets.symmetric(vertical: 20)),
            child: const Text('Record this and finish the delivery'),
          ),
        ),
        if (onBack != null)
          TextButton(
            key: const Key('proof-exception-back'),
            onPressed: onBack,
            child: const Text('Take the photograph instead'),
          ),
      ],
    );
  }
}

/// A reason is on the device, in the queue, and there will be no photograph.
///
/// **Different words from [_Queued], and that is the whole reason the stage exists.** "Photograph
/// saved" said about a recorded exception is a driver who believes they photographed a delivery they
/// did not, and finds out weeks later in a dispute.
class _ReasonRecorded extends StatelessWidget {
  const _ReasonRecorded({required this.onDone});

  final VoidCallback onDone;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      key: const Key('proof-exception-recorded'),
      padding: const EdgeInsets.all(24),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Icon(Icons.check_circle_outline, color: theme.colorScheme.primary),
          const SizedBox(height: 12),
          Text('Reason recorded', style: theme.textTheme.titleMedium),
          const SizedBox(height: 8),
          Text(
            // The promise `Docs/07` §4 makes, said precisely: "sent" would be a lie on a phone with
            // no signal, and the pending indicator would then contradict this screen. It also says
            // the delivery is done, because that is the question the driver is actually asking.
            'The delivery is recorded as complete, with the reason there is no photograph. It is on '
            'this phone and Shipper will send it when there is a connection.',
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 24),
          FilledButton(
            key: const Key('proof-exception-done'),
            onPressed: onDone,
            child: const Text('Back to the delivery'),
          ),
        ],
      ),
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
