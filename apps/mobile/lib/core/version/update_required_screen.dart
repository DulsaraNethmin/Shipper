import 'dart:async';

import 'package:flutter/material.dart';

/// How the app leaves for the store. Declared here, with the screen that needs it, so
/// `version_gate.dart` can supply one without the two files importing each other.
typedef StoreOpener = Future<bool> Function(Uri destination);

/// What a build below the floor shows, instead of the application (SHIP-168).
///
/// ## Two shapes, and the second is not a broken first
///
/// `Docs/09` asks for "an update prompt linking to the store", and the platform is explicitly
/// allowed to send no link: `IOS_STORE_URL` and `ANDROID_STORE_URL` both default to empty,
/// `store_url` carries `omitempty`, and `internal/config/config.go` records that as a decision —
/// "the client shows the prompt without a link when these are blank". During the pilot,
/// distribution is TestFlight and Play internal testing (`Docs/01` §8) and there is no public
/// listing to point at, so **the shape with no link is the shape the pilot actually ships**.
///
/// The failure to avoid is that shape reading as a screen that did not load. So both shapes carry
/// the same icon, the same headline and the same explanation, and the difference is only in the
/// last element: a button that goes there, or a sentence saying where to go. There is no spinner,
/// no empty space where a control should be, and no disabled button — a greyed-out "Update" is
/// precisely the thing that reads as broken.
///
/// ## The copy says what to do, in both shapes
///
/// A person who is blocked out of an app needs one thing from this screen: the next action. With
/// a link that is a button. Without one it is a sentence naming where they installed Shipper
/// from, which is true whether that was TestFlight, Play internal testing, or eventually a public
/// listing — the screen does not have to know which.
///
/// ## And when the link is there but does not open
///
/// A button that appears to do nothing is worse than no button. `launchUrl` returns `false` when
/// nothing on the device handles the destination, so a failed open falls back to the same
/// sentence the no-link shape shows, and prints the destination so it can be reached by hand.
class UpdateRequiredScreen extends StatefulWidget {
  const UpdateRequiredScreen({super.key, required this.storeUrl, required this.openStore});

  /// Where the platform said to send the user, or `null` for the pilot's shape.
  final String? storeUrl;

  final StoreOpener openStore;

  @override
  State<UpdateRequiredScreen> createState() => _UpdateRequiredScreenState();
}

class _UpdateRequiredScreenState extends State<UpdateRequiredScreen> {
  var _openFailed = false;

  /// The destination, or `null` when there is not a usable one.
  ///
  /// A string that will not parse, or one with no scheme, is not a link — `launchUrl` would
  /// refuse it and the user would be left tapping a button. Treating it as absent puts them on
  /// the shape that at least tells them what to do.
  Uri? get _destination {
    final raw = widget.storeUrl;
    if (raw == null || raw.isEmpty) return null;
    final uri = Uri.tryParse(raw);
    return (uri != null && uri.hasScheme) ? uri : null;
  }

  Future<void> _open(Uri destination) async {
    var opened = false;
    try {
      opened = await widget.openStore(destination);
    } catch (_) {
      // Anything the platform channel raises means the same thing here: they are still on this
      // screen. It must not become an unhandled error on top of a screen with no way off it.
      opened = false;
    }
    if (!mounted) return;
    setState(() => _openFailed = !opened);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final destination = _destination;
    final showInstruction = destination == null || _openFailed;

    return Scaffold(
      key: const Key('update-required'),
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.symmetric(horizontal: 32, vertical: 24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                Icon(Icons.system_update, size: 64, color: theme.colorScheme.primary),
                const SizedBox(height: 24),
                Text(
                  'Update Shipper to keep going',
                  key: const Key('update-required-title'),
                  textAlign: TextAlign.center,
                  style: theme.textTheme.headlineSmall,
                ),
                const SizedBox(height: 12),
                Text(
                  'This version of Shipper is no longer supported. Install the latest '
                  'version and everything carries on where it left off.',
                  key: const Key('update-required-body'),
                  textAlign: TextAlign.center,
                  style: theme.textTheme.bodyMedium,
                ),
                if (destination != null) ...<Widget>[
                  const SizedBox(height: 32),
                  FilledButton(
                    key: const Key('update-open-store'),
                    onPressed: () => unawaited(_open(destination)),
                    child: const Text('Update Shipper'),
                  ),
                ],
                if (showInstruction) ...<Widget>[
                  const SizedBox(height: 24),
                  _Instruction(destination: _openFailed ? destination : null),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// What to do when this screen cannot do it for you.
///
/// In a filled container rather than as loose grey text, so it reads as the instruction it is
/// rather than as the caption on something that failed to appear.
class _Instruction extends StatelessWidget {
  const _Instruction({required this.destination});

  /// Printed only when a link was tried and did not open. Nothing to show in the ordinary
  /// no-link case, where there was never a destination.
  final Uri? destination;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Container(
      decoration: BoxDecoration(
        color: theme.colorScheme.secondaryContainer,
        borderRadius: BorderRadius.circular(12),
      ),
      padding: const EdgeInsets.all(16),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Icon(
                Icons.storefront_outlined,
                size: 20,
                color: theme.colorScheme.onSecondaryContainer,
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  'Open the app store you installed Shipper from and install the '
                  'latest version.',
                  key: const Key('update-no-store-link'),
                  style: theme.textTheme.bodyMedium,
                ),
              ),
            ],
          ),
          if (destination != null) ...<Widget>[
            const SizedBox(height: 12),
            SelectableText(
              destination.toString(),
              key: const Key('update-store-url'),
              style: theme.textTheme.bodySmall,
            ),
          ],
        ],
      ),
    );
  }
}
