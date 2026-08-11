import 'package:flutter/material.dart';

/// What a cold start shows while the keychain is read (SHIP-49).
///
/// It exists so the first frame commits to nothing. The read takes a few milliseconds on a
/// warm device and noticeably longer on a cold one, and the alternative to a screen like this
/// is guessing — showing the sign-in screen and snatching it away, or showing an empty home
/// shell to somebody who is not signed in.
///
/// Deliberately wordless. Copy here would be read for a few frames by a user who did not ask
/// a question, and would need translating for the sake of it.
class StartingScreen extends StatelessWidget {
  const StartingScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return const Scaffold(
      body: Center(
        child: CircularProgressIndicator(key: Key('session-restoring')),
      ),
    );
  }
}
