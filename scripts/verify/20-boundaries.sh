# shellcheck shell=bash
#
# SHIP-10, SHIP-11 — the eight domains, the adapter tree, and the import lint that holds them
# apart.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# Nothing here touches the running service. It stays in the 20s rather than the 00s because the
# throwaway fixture it builds wants $WORKDIR, and because a reader looking for "what checks the
# boundaries" should find one file rather than a range.

ticket "SHIP-10  package skeleton for the eight domains and the adapter tree"

for d in identity profiles fleet jobs bidding delivery notifications admin; do
  [[ -d "services/core/internal/$d" ]] || fail "domain package internal/$d is missing"
done
ok "all eight domains from Docs/06 §3 have a package"

for a in email sms push storage geocoding; do
  [[ -d "services/core/internal/platform/$a" ]] || fail "adapter internal/platform/$a is missing"
done
ok "the platform/ tree holds the five adapters from Docs/06 §4.1"

# ---------------------------------------------------------------------------------------
ticket "SHIP-11  the import lint fails on a crossed boundary"

pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/lintboundaries" ./cmd/lintboundaries
popd >/dev/null

pushd "$ROOT/services/core" >/dev/null
"$WORKDIR/lintboundaries" >/dev/null || fail "the lint reports a violation in this repository"
popd >/dev/null
ok "this repository crosses no boundary"

# A throwaway module that breaks each rule, so the check is shown to fail and not merely
# to pass. A lint nobody has watched fail is a lint nobody knows works.
fixture="$WORKDIR/fixture"
mkdir -p "$fixture/internal/jobs" "$fixture/internal/bidding" \
         "$fixture/internal/platform/email" "$fixture/internal/identity"
printf 'module github.com/DulsaraNethmin/Shipper/services/core\n\ngo 1.25\n' >"$fixture/go.mod"
printf 'package bidding\n'  >"$fixture/internal/bidding/pkg.go"
printf 'package identity\n' >"$fixture/internal/identity/pkg.go"
printf 'package jobs\n\nimport _ "github.com/DulsaraNethmin/Shipper/services/core/internal/bidding"\n' \
  >"$fixture/internal/jobs/pkg.go"
printf 'package email\n\nimport _ "github.com/DulsaraNethmin/Shipper/services/core/internal/identity"\n' \
  >"$fixture/internal/platform/email/pkg.go"

pushd "$fixture" >/dev/null
if "$WORKDIR/lintboundaries" >"$WORKDIR/lint.log" 2>&1; then
  popd >/dev/null
  cat "$WORKDIR/lint.log"
  fail "the lint passed a module that crosses two boundaries"
fi
popd >/dev/null

grep -q "domain imports domain" "$WORKDIR/lint.log" \
  || { cat "$WORKDIR/lint.log"; fail "a domain importing another domain was not reported"; }
grep -q "adapter imports domain" "$WORKDIR/lint.log" \
  || { cat "$WORKDIR/lint.log"; fail "an adapter importing a domain was not reported"; }
ok "a domain importing a domain, and an adapter importing a domain, both fail the build"
