import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/app.dart';

/// Entry point.
///
/// Everything above the app widget lives here and nowhere else: one [ProviderScope] at the
/// root, and the app. Riverpod resolves providers through this scope, so a second one — in a
/// test helper, or wrapped around a screen "just for this feature" — silently gives that
/// subtree its own copy of every provider, including the session. Tests override providers on
/// this scope rather than creating another.
void main() {
  runApp(const ProviderScope(child: ShipperApp()));
}
