/// What the app tells people before it asks them for something (SHIP-179).
///
/// In `core/` rather than in a feature because two features need it and they may not import
/// one another (`Docs/07` §2): `delivery` asks for the camera, `notifications` asks for push.
///
/// `Docs/07` §7 treats store compliance as build work and notes that each item on its list has
/// blocked a real submission. Purpose strings are on that list. Apple rejects a missing one
/// outright and rejects a vague one — "Shipper needs access to your camera" says nothing a
/// reviewer, or a user, can weigh.
///
/// Three rules the copy below follows:
///
/// - **Say what is captured and why**, in the words a driver would use.
/// - **Say what happens if they decline**, because `Docs/01` §4.4 and `Docs/07` §5 both
///   guarantee the app still works. A permission prompt that reads as an ultimatum is refused
///   more often, and then the refusal has to be handled anyway.
/// - **Australian English**, as `CLAUDE.md` requires of user-facing copy.
///
/// [cameraPurpose] is duplicated into `ios/Runner/Info.plist` as `NSCameraUsageDescription`,
/// which is the string iOS itself renders. `permission_copy_test.dart` asserts the two are
/// identical, because there is no other way to notice that one of them was reworded.
abstract final class PermissionCopy {
  /// iOS renders this verbatim in the system camera prompt, and it is what the app shows as
  /// its own rationale beforehand on both platforms.
  ///
  /// ## SHIP-81c widened it, and that is a shipped string changing
  ///
  /// SHIP-179 wrote this when proof of delivery was the only thing this application photographed,
  /// and it said so: *"…to photograph goods at pickup and delivery, as proof the job was
  /// completed."* SHIP-81c gave the camera a second use — a provider photographing the four
  /// documents `Docs/04` §3 collects — and **a purpose string that does not cover a use is a
  /// submission risk rather than a wording preference.** Apple requires the string to describe
  /// every reason the application opens the camera, and a reviewer reading "as proof the job was
  /// completed" while the onboarding flow photographs a driver licence is a reviewer who has found
  /// a discrepancy.
  ///
  /// **Verification is named first, deliberately.** It is the first time most providers meet this
  /// prompt: `Docs/04` §3 collects the documents before anybody can bid, so the camera opens during
  /// onboarding long before it opens at a delivery point.
  ///
  /// The two things `Docs/07` §7 requires of it are unchanged, because they are what Apple rejects
  /// a build for missing: it says what is captured, and it says what happens if the request is
  /// declined. The second is true of both uses — a delivery takes a recorded exception reason
  /// (`Docs/01` §4.4, SHIP-131) and verification takes a file the provider already has
  /// (`Docs/04` §3.1, SHIP-81d).
  static const cameraPurpose =
      'Shipper uses the camera for two things: transport providers photograph the licence, '
      'vehicle registration and insurance documents they are verified against, and drivers '
      'photograph goods at pickup and delivery as proof the job was completed. If you cannot '
      'take a photo, you can record the reason instead.';

  /// Shown when the camera has been declined **at a delivery**.
  ///
  /// `Docs/01` §4.4 allows a recorded exception reason in place of a photo, so a refusal is a
  /// route through the job rather than the end of it — `Docs/07` §7 calls a camera flow that
  /// dead-ends on a denied permission a defect.
  ///
  /// [verificationCameraDeclined] is the same sentence for the other camera surface, and the two
  /// are separate because the routes on are different: a delivery records a reason and finishes,
  /// and a verification document is attached from a file. One string covering both would have to
  /// offer each user the other's way out.
  static const cameraDeclined =
      'Shipper cannot open the camera. You can record a reason for the missing photo and '
      'finish the delivery, or turn the camera on in your device settings.';

  /// Shown when the camera has been declined **while photographing a verification document**.
  ///
  /// `Docs/04` §3.1 puts a hard requirement under this sentence: *"the camera permission may be
  /// declined. A file-upload fallback must exist so that a refused permission never blocks
  /// verification outright."* A provider who cannot photograph their licence cannot be verified,
  /// and a provider who cannot be verified cannot bid — so a dead end here is not an inconvenience,
  /// it is somebody locked out of the marketplace by a permission prompt.
  ///
  /// **SHIP-81c left this honestly short of that requirement and SHIP-81d fills it.** Until the
  /// fallback existed the sentence offered settings alone, because copy promising a button that was
  /// not there is worse than copy that admits where the provider stands. It now offers the file
  /// first and settings second — the order matters, because a provider standing in front of a
  /// refused permission wants the way *on* rather than the way *back*, and the document they need
  /// is very often already a photo or a scan on the phone in their hand.
  static const verificationCameraDeclined =
      'Shipper cannot open the camera. You can attach a photo or a scan of the document from '
      'this device instead, or turn the camera on in your device settings and try again.';

  /// The heading above [notificationsPurpose].
  static const notificationsTitle = 'Know as soon as something happens';

  /// The rationale shown **before** the system prompt.
  ///
  /// `Docs/07` §5 requires the ask at a moment its value is obvious — after a first bid
  /// arrives, not on first launch — so this is written for somebody who has just seen one, not
  /// for somebody who has just installed the app.
  ///
  /// The last sentence is not reassurance for its own sake. `Docs/05` §4 keeps addresses,
  /// goods descriptions and full names out of notification content because it renders on a
  /// locked screen, and saying so is what makes the permission worth granting.
  static const notificationsPurpose =
      'Turn on notifications and Shipper will tell you when a bid arrives, when a job is '
      'awarded, and when a delivery reaches its next milestone. Notifications never include '
      'addresses, goods details or full names, because they appear on a locked screen.';

  /// Shown when notifications have been declined.
  ///
  /// `Docs/07` §5: the app stays fully usable without them, and offers a route back. Push is a
  /// prompt and never a channel of record, so nothing is lost — only noticed later.
  static const notificationsDeclined =
      'Notifications are off. Shipper still shows every bid and delivery update in the app, '
      'and you can turn notifications on in your device settings whenever you like.';
}
