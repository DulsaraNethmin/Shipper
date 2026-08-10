/// Jobs — creation, publication, discovery, detail (`Docs/07` §2).
///
/// Status names come from `Docs/02` §1 and are generated rather than typed: `Docs/10` §8.2
/// makes `contracts/statuses.yaml` the source for all three languages. Status is never a
/// field this app sets — it reads one (`CLAUDE.md`).
///
/// **The customer's budget never reaches a provider's device** (`Docs/01` §4.3). Not as an
/// amount, a band, or a "budget supplied" flag. That is enforced server-side, and this
/// package must not reintroduce it by inferring one.
///
/// Empty until M2.
library;
