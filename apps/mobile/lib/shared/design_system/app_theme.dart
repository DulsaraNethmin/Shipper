import 'package:flutter/material.dart';

/// The application theme.
///
/// `Docs/07` §2 puts the design system in `shared/`, which is the half of the split that
/// matters: a feature may read the theme, and no feature defines one. Two features that each
/// declare their own accent are how a marketplace ends up looking like two products.
abstract final class AppTheme {
  /// Seed for the Material 3 scheme. One value, so a rebrand is one edit.
  static const _seed = Color(0xFF1B5E5A);

  static final ThemeData light = _build(Brightness.light);
  static final ThemeData dark = _build(Brightness.dark);

  static ThemeData _build(Brightness brightness) {
    return ThemeData(
      useMaterial3: true,
      colorScheme: ColorScheme.fromSeed(
        seedColor: _seed,
        brightness: brightness,
      ),
    );
  }
}
