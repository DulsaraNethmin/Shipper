/// The launch-time version gate (`Docs/07` §6, SHIP-168).
///
/// `Docs/07` §6 states the problem this folder exists for: **old builds persist on devices
/// indefinitely and cannot be forced forward the way a web deployment can.** Dart code has no
/// over-the-air path, so a build that does not ask, at launch, whether it is still supported can
/// never be told to stop — and by the time anyone wants to tell it, the answer has to already be
/// in the build that is on the phone. That is why SHIP-167 was pulled forward out of M7 and why
/// this is written before anything is installed on a device.
///
/// ## What is here
///
/// - `running_build.dart` — the build this process is, and where that number comes from. The
///   argument for reading the native build number rather than duplicating it into a define.
/// - `minimum_version.dart` — `GET /v1/app/minimum-version`, and why it travels on the client
///   that carries no credential.
/// - `version_gate.dart` — the comparison as a pure function, the providers, and the widget that
///   replaces the application. The fail-open decision is argued here.
/// - `update_required_screen.dart` — the prompt, in both of its shapes.
///
/// ## The three things most likely to be got wrong later
///
/// **The comparison is `<`.** A build equal to the floor is the oldest *supported* build.
///
/// **An unreachable API does not block.** The gate is a courtesy, not a control — what actually
/// retires a build is `/v1` refusing it. `version_gate.dart` argues this at length; it is the
/// decision to read before changing anything here.
///
/// **No link is an ordinary outcome.** The pilot ships with `IOS_STORE_URL` and
/// `ANDROID_STORE_URL` empty on purpose, so the shape with no button is the shape that runs
/// today. It must never read as a screen that failed to load.
library;
