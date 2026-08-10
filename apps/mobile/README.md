# apps/mobile — Flutter client

The single Flutter application for **both** customer and provider roles. The role is chosen
at signup and drives the post-login shell; it is not two apps and not two builds
(`Docs/06` §2, `Docs/07`).

Targets iOS and Android.

## Running it

```
flutter pub get
flutter run                          # local, whichever device is attached
```

`make flutter-run` from the repository root does the same and is documented in `mk/flutter.mk`.
There are no web, macOS, Linux or Windows targets, deliberately: they are not in scope, and
each one enlarges both the tree and the CI surface for nothing.

## Deployment floors

**iOS 14.0 and Android API 24**, decided in `Docs/07` §9. They are deployment targets, not
test targets — development runs against a current simulator and emulator.

| Floor | Set in |
|---|---|
| Android API 24 | `minSdk` in `android/app/build.gradle.kts` |
| iOS 14.0 | `IPHONEOS_DEPLOYMENT_TARGET` in `ios/Runner.xcodeproj/project.pbxproj` |

Two places, not the three this used to need, and the difference is worth recording because
both of the missing ones look like they should be edited.

- **There is no `Podfile`.** Flutter 3.44 integrates plugins as Swift packages and
  `flutter create` no longer writes one; adding one by hand re-introduces CocoaPods and makes
  `flutter build ios` print a warning asking for it to be removed again. If a
  CocoaPods-only plugin ever arrives, Flutter generates the `Podfile` itself and
  `flutter_ios_podfile_setup` reads the deployment target from the Xcode project.
- **`ios/Flutter/AppFrameworkInfo.plist` is not where the floor goes.** Flutter rewrites
  `MinimumOSVersion` in the built `App.framework` at build time, to its *own* minimum, which
  is 13.0. Setting 14.0 in the source file changes nothing — verified by reading
  `MinimumOSVersion` out of `build/ios/iphonesimulator/Runner.app/Frameworks/App.framework/Info.plist`
  after a build. The app's own `Info.plist` correctly reports 14.0, which is the value the
  App Store reads.

The application id and bundle identifier are both `au.com.shipper`. **Provisional** until
X-2 and X-3 register it: neither store lets it change once anything has been published.

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
