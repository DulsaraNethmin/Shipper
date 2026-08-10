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
  static const cameraPurpose =
      'Shipper uses the camera to photograph goods at pickup and delivery, as proof the job '
      'was completed. If you cannot take a photo, you can record the reason instead.';

  /// Shown when the camera has been declined.
  ///
  /// `Docs/01` §4.4 allows a recorded exception reason in place of a photo, so a refusal is a
  /// route through the job rather than the end of it — `Docs/07` §7 calls a camera flow that
  /// dead-ends on a denied permission a defect.
  static const cameraDeclined =
      'Shipper cannot open the camera. You can record a reason for the missing photo and '
      'finish the delivery, or turn the camera on in your device settings.';

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
