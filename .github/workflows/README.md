# CI workflows

Placeholder created by **SHIP-1**. The workflows themselves arrive in **SHIP-20** (Go build,
vet, test) and **SHIP-21** (Flutter analyze and test), with signing and store upload in
**SHIP-24**…**SHIP-27**.

## Path-filter every workflow, from the first one

`Docs/08` Step 1 makes this a requirement rather than an optimisation: **macOS runners for
iOS builds cost roughly ten times Linux minutes**, and a Go-only change must not trigger one.

Each workflow declares the paths it cares about:

| Workflow | Triggers on |
|---|---|
| Go build, vet, test | `services/core/**`, its own workflow file |
| Flutter analyze, test | `apps/mobile/**`, its own workflow file |
| iOS signed build (macOS runner) | `apps/mobile/**`, its own workflow file |
| Android signed build | `apps/mobile/**`, its own workflow file |
| Admin panel | `apps/admin/**`, its own workflow file |
| Driver portal | `apps/driver-portal/**`, its own workflow file |

A workflow that ignores its own file will not re-run when that file is fixed, which is a
confusing way to debug CI. Include it in the filter.

## Secrets

Signing keys, keystores, provisioning profiles, and service-account JSON live in the CI
secret store (`Docs/06` §5.2) — never in the repository, and never on a developer machine as
the only copy. `.gitignore` covers the known filenames, but check the diff: a leaked signing
key means rotating it everywhere it was trusted.
