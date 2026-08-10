/// Notifications — push registration, inbox, deep-link routing (`Docs/07` §2).
///
/// Permission is requested at a moment its value is obvious — after a first bid arrives, not
/// on first launch (`Docs/07` §5). The app stays fully usable when it is declined and offers a
/// route back.
///
/// Push is a prompt, never a channel of record (`Docs/07` §5): anything that matters is also
/// retrievable in-app. Notification content excludes addresses, goods descriptions and full
/// customer names, because it renders on a lock screen (`Docs/05` §4).
///
/// Empty until M5.
library;
