#!/usr/bin/env bash
#
# Australian English, checked.
#
# CLAUDE.md and Docs/10 §9.3 both require it, and the existing documents are consistent — but
# almost every tool, template and generated string defaults to American spelling, so the drift
# is continuous rather than occasional. Left unchecked the codebase ends up with `authorize`
# in the code and `authorisation` in the documents describing it, which makes both harder to
# search.
#
# Only unambiguous cases are listed. Anything where the American form is also a legitimate
# identifier — a third-party symbol, a JSON field somebody else defined — has to be excluded
# rather than flagged, or the check becomes noise people learn to ignore.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

# American form on the left, the spelling we use on the right.
declare -a PAIRS=(
    "authorize:authorise"
    "authorized:authorised"
    "authorization:authorisation"
    "unauthorized:unauthorised"
    "minimize:minimise"
    "minimized:minimised"
    "maximize:maximise"
    "organization:organisation"
    "organize:organise"
    "recognize:recognise"
    "serialize:serialise"
    "serialization:serialisation"
    "deserialize:deserialise"
    "normalize:normalise"
    "normalization:normalisation"
    "initialize:initialise"
    "cancelation:cancellation"
    "canceled:cancelled"
    "fulfill:fulfil"
    "traveled:travelled"
    "labeled:labelled"
    "catalog:catalogue"
    "license:licence"     # noun; the verb is "license" in both, but we only use the noun
    "behavior:behaviour"
    "favor:favour"
    "color:colour"
    "center:centre"
    "meter:metre"
    "liter:litre"
    "defense:defence"
    "offense:offence"
    "practise:practice"   # noun; Australian usage is "practice" for the noun
)

# Paths that are not ours to spell.
EXCLUDES=(
    ":(exclude)go.sum"
    ":(exclude)go.mod"
    ":(exclude)**/go.sum"
    ":(exclude)**/go.mod"
    ":(exclude)scripts/check-spelling.sh"
    ":(exclude)**/node_modules/**"
    ":(exclude)**/*.lock"
    ":(exclude)**/pubspec.lock"
    ":(exclude)Docs/09-delivery-backlog-jira.csv"
)

status=0

for pair in "${PAIRS[@]}"; do
    american="${pair%%:*}"
    australian="${pair##*:}"

    # -w so "color" does not match "colorimetry", and -I so binaries are skipped. Searching
    # tracked files only, via git, keeps build output and vendored code out of it.
    if matches=$(git grep -I -n -w -i -- "$american" -- . "${EXCLUDES[@]}" 2>/dev/null); then
        echo "Use '$australian', not '$american':"
        echo "$matches" | sed 's/^/  /'
        echo
        status=1
    fi
done

if [ "$status" -ne 0 ]; then
    echo "Australian English, please — see CLAUDE.md. If a match is a third-party identifier"
    echo "that cannot be renamed, add its path to EXCLUDES in scripts/check-spelling.sh."
    exit 1
fi

echo "spelling: Australian English throughout"
