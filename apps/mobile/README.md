# apps/mobile — Flutter client

The single Flutter application for **both** customer and provider roles. The role is chosen
at signup and drives the post-login shell; it is not two apps and not two builds
(`Docs/06` §2, `Docs/07`).

Targets iOS and Android.

## Running it

```
make up && make run                             # the API, from the repository root
make flutter-run d=<device>                     # local, against this worktree's HTTP_PORT
make flutter-run d=<device> flavour=staging
```

`make flutter-run` reads `HTTP_PORT` out of `deploy/.env`, which is set per git worktree, so
the client points at *this* checkout's API without anybody editing Dart. `flutter run` on its
own works too and defaults to port 8080.

There are no web, macOS, Linux or Windows targets, deliberately: they are not in scope, and
each one enlarges both the tree and the CI surface for nothing.

## Environments

Three, selected at build time by `--dart-define` (`lib/core/api/api_environment.dart`):

| Define | Values | Effect |
|---|---|---|
| `SHIPPER_ENV` | `local` (default), `staging`, `production` | Which deployment the build talks to |
| `SHIPPER_API_PORT` | default `8080` | Local only. The port `make run` is serving on |
| `SHIPPER_API_BASE_URL` | unset | Overrides everything. For a physical device on the LAN |

`--dart-define` rather than Xcode and Gradle flavours because it is the mechanism with the
fewest moving parts that still decides at **build** time. A run-time switch is one a support
call can talk somebody into flipping. The half this does not give us is `Docs/07` §8's
requirement that a tester hold a staging build and a production build on **one device** — that
needs a distinct application id per environment, which is genuinely Xcode and Gradle flavours,
and it belongs with the signing work at SHIP-24…SHIP-27.

**The local base URL differs by platform and this is not cosmetic.** An iOS simulator shares
the host's network stack and reaches it on `localhost`; an Android emulator's `localhost` is
the emulated device itself and the host is `10.0.2.2`. A client that uses `localhost` for both
works perfectly on iOS and reaches nothing on Android — a defect that survives review because
whoever wrote it tested on the platform where it works. `api_environment_test.dart` asserts
both.

The staging and production hostnames are **provisional**: neither is registered, neither is
deployed, and no ticket owns the DNS. They are written out so the three environments are
genuinely three, and `SHIPPER_API_BASE_URL` overrides them until they exist.

`make flutter-test-defines` runs the environment test once per flavour, because a single test
run compiles with one set of defines and therefore cannot prove that any other flavour works.

## Structure and state

`lib/` is organised by **feature**, mirroring the platform domains, exactly as `Docs/07` §2
draws it:

```
lib/
  main.dart        one ProviderScope, one app, nothing else
  core/            api, auth, storage, errors, queue, routing, the app widget
  features/        identity, profile, fleet, jobs, bidding, delivery, notifications
  shared/          design_system, formatting, validation
```

**Features do not import one another.** Shared behaviour moves to `core/` or `shared/`.
`test/architecture_test.dart` enforces it — the Dart counterpart of the Go boundary lint
(SHIP-11), and for the same reason: a boundary that only a document asserts is a boundary that
is already being crossed. It catches both the `package:shipper/features/…` form and the
relative `../other_feature/…` form, and it also asserts that `lib/features/` holds exactly the
seven folders `Docs/07` §2 names, so an eighth feature is a decision somebody recorded rather
than a folder that appeared.

The stack is closed, in `Docs/10` §8.3 and `Docs/07` §9. **Riverpod** for state, **go_router**
for routing, **`freezed` + `json_serializable`** for models, **`dio`** for transport, and
**Drift over SQLite** for the offline queue. A second state library or a second router is a
defect, not a preference.

How Riverpod is used here:

- **One `ProviderScope`, at the root of `main.dart`.** A second one anywhere gives its subtree
  a private copy of every provider, including the session. Tests override providers on the root
  scope rather than creating another.
- **The router is a provider**, not a constant. `Docs/07` §1 requires customer and provider
  surfaces to stay genuinely separate inside one app, and what a user sees follows session
  state, so the navigation graph has to be able to change when the session does.
- **A redirect is never an authorisation decision** (`Docs/07` §3). It may keep a signed-out
  user off a screen that would be empty; anything the account is not entitled to do fails
  server-side whether or not the route was reachable.

Drift is decided but **not yet a dependency**. SHIP-17 is structure; the queue is SHIP-124.
`lib/core/queue/` holds the reasoning and nothing else, because an unused native dependency in
both platform builds buys nothing that a folder and a paragraph do not.

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
