# shellcheck shell=bash
#
# SHIP-171a — the platform can delete an object it stored.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# # Why this is a file of its own rather than three more lines in 00-stack.sh
#
# The harness's own header states the mechanism: "a track adds a file here and edits none". The
# object-store round trip in 00-stack.sh belongs to SHIP-15p and demonstrates the *stack*; this
# demonstrates a capability the Go adapter acquired, which is a different claim about a different
# thing, and putting it in a file nobody else this wave opens is what keeps a one-ticket change out
# of a shared merge.
#
# 00–09, so this runs before the service is built. Nothing here needs a listening port: the delete
# does not pass through the API and could not — files do not pass through this service, which is
# the rule internal/platform/storage/doc.go is written around and which SHIP-171a did not change.
#
# # This is the first section in this repository that removes anything
#
# **$STORAGE_BUCKET is one bucket per worktree** and the primary tree falls back to `shipper-dev`,
# so a deleting check is safe here for the same reason a `CREATE DATABASE … TEMPLATE` is: the
# namespace is per tree. Every object below is created by this section, under a key carrying $$,
# and every assertion names that key. Nothing that belonged to anybody is touched — which is
# SHIP-171a's own scope line and not merely this section's caution.

ticket "SHIP-171a  the platform can delete an object it stored"

# The Go integration tests, run against the store that is actually running.
#
# The row says "demonstrated against the real object store rather than a fake", so the harness runs
# the tests that do it rather than restating them in curl: a fake would answer whatever it was
# written to answer, and the claim is about what the store holds afterwards. `-count=1` because a
# cached result is not a demonstration.
#
# The pattern names all four, and it is written out rather than given as `TestDelete` — Go's -run
# is an unanchored regular expression, and `TestDelete` does not match `TestDeletingAKey…`, which
# is exactly the way a check silently stops exercising the case it was added for.
pushd "$ROOT/services/core" >/dev/null
if ! delete_log="$(go test ./internal/platform/storage/ -count=1 -v \
  -run 'TestDeleteRemovesAnObjectThePlatformStored|TestDeletingAKeyThatHoldsNothingIsASuccess|TestDeleteRefusesAWrongCredentialRatherThanReportingSuccess|TestDeleteRefusesAKeyItCannotSign' 2>&1)"; then
  echo "$delete_log"
  popd >/dev/null
  fail "the object-deletion tests do not pass"
fi
popd >/dev/null

# A test that did not run is not a test that passed, and a `-run` pattern matching nothing exits 0.
for name in TestDeleteRemovesAnObjectThePlatformStored \
            TestDeletingAKeyThatHoldsNothingIsASuccess \
            TestDeleteRefusesAWrongCredentialRatherThanReportingSuccess \
            TestDeleteRefusesAKeyItCannotSign; do
  grep -q -- "--- PASS: $name" <<<"$delete_log" || { echo "$delete_log"; fail "$name did not run"; }
done
ok "internal/platform/storage deletes an object, twice over, and refuses a wrong credential"

# The same claim end to end at the protocol, so that the section holds it independently of the Go
# tests it just ran. Signed here with the presigner 00-stack.sh defined — sections are sourced into
# one shell, so it is still in scope, and a second copy of a SigV4 signer would be a second answer
# to what a valid signature looks like.
storage_key="verify/$$/deletable.txt"
storage_body="verify-deletable-payload-$$"

put_status="$(curl -s -o /dev/null -w '%{http_code}' -X PUT --data-binary "$storage_body" \
  "$(presign PUT "$storage_key")")"
[[ "$put_status" == "200" ]] \
  || fail "creating the object to delete answered $put_status — run 'make up', which creates $STORAGE_BUCKET"

fetched="$(curl -s "$(presign GET "$storage_key")")"
[[ "$fetched" == "$storage_body" ]] \
  || fail "the object to be deleted reads back as '$fetched', expected '$storage_body'"
ok "an object exists under $storage_key, created by this run for the purpose"

# `-X DELETE` against the pre-signed URL: the same request internal/platform/storage builds, spent
# from the harness so that this section's claim does not depend on the Go test's.
delete_status="$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$(presign DELETE "$storage_key")")"
[[ "$delete_status" == "204" ]] \
  || fail "a pre-signed DELETE answered $delete_status, expected 204"
ok "a pre-signed DELETE removed it"

# **The read is the check.** A status code says what the store answered; only a read says what it
# holds, and a delete that answered 204 while removing nothing fails here rather than passing.
gone_status="$(curl -s -o /dev/null -w '%{http_code}' -I "$(presign HEAD "$storage_key")")"
[[ "$gone_status" == "404" ]] \
  || fail "after the delete, a signed HEAD of $storage_key answered $gone_status, expected 404"
ok "a signed read of the same key now answers 404: the object is gone, not merely reported gone"

# The decision S3.Delete's doc comment argues, at the store rather than in Go: **a DELETE of a key
# that holds nothing answers 204.** This package translates no status, so the idempotence is the
# store's behaviour rather than something the adapter manufactures — and this is the check that
# says so, on the tree, rather than a specification quoted in a comment.
repeat_status="$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$(presign DELETE "$storage_key")")"
[[ "$repeat_status" == "204" ]] \
  || fail "deleting the same key again answered $repeat_status, expected 204"
never_status="$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$(presign DELETE "verify/$$/never-existed.txt")")"
[[ "$never_status" == "204" ]] \
  || fail "deleting a key nothing was ever stored under answered $never_status, expected 204"
ok "the store answers 204 to both, so 'nothing there' is its idempotence rather than our translation"

# And the pairing that makes that safe: the only thing which answers 404 to a DELETE here is a
# missing *bucket*, which is a configuration fault. Folding 404 into success would make a service
# pointed at the wrong bucket report every deletion as done while nothing was deleted — the same
# shape as reading a 403 from S3.Stored as "the driver never uploaded", in the direction that
# destroys evidence rather than the one that blames somebody for losing it.
#
# The override is set inside the command substitution rather than as a `VAR=x presign …` prefix:
# a prefix on a *function* call persists after it returns in POSIX mode, and a section that
# silently left $STORAGE_BUCKET pointing at a bucket that does not exist would break every later
# section rather than this one.
missing_bucket_status="$(
  STORAGE_BUCKET="$STORAGE_BUCKET-no-such-bucket"
  export STORAGE_BUCKET
  curl -s -o /dev/null -w '%{http_code}' -X DELETE "$(presign DELETE "$storage_key")"
)"
[[ "$missing_bucket_status" == "404" ]] \
  || fail "a DELETE against a bucket that does not exist answered $missing_bucket_status, expected 404"
ok "404 is reserved for a missing bucket, which is why S3.Delete treats it as a fault and not as 'gone'"
