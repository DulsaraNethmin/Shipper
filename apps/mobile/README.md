# apps/mobile — Flutter client

The single Flutter application for **both** customer and provider roles. The role is chosen
at signup and drives the post-login shell; it is not two apps and not two builds
(`Docs/06` §2, `Docs/07`).

Targets iOS and Android.

## Not yet scaffolded

This directory is a placeholder created by **SHIP-1**. The Flutter project itself arrives in
**SHIP-16** (scaffold), **SHIP-17** (feature-folder structure and state management), and
**SHIP-18** (API client with environment-based base URL).

## Things that are already decided

- Authorisation is **never** decided on the device. The app may hide or disable an action;
  the platform enforces it (`Docs/07` §3).
- The refresh token lives in the iOS Keychain and Android Keystore — never in application
  preferences or clear text on disk (`Docs/06` §5.2, SHIP-48).
- Dart code has **no over-the-air update path**. Anything expected to change under
  operational pressure — category lists, validation limits, policy copy, expiry windows,
  feature switches — belongs server-side in Go (`Docs/06` §5.3).
- The launch-time minimum-version gate (SHIP-168) must ship in the **first** build. It
  cannot be added retroactively to builds already on devices, which is exactly when it is
  needed.
