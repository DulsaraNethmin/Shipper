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
#
# # Two scopes, because a framework name is not a spelling
#
# CLAUDE.md scopes the rule to "documents and user-facing copy". Under apps/** the American
# form of a dozen of these words *is* the API: Flutter has `Center`, `color:` and `behavior:`,
# Tailwind has `items-center`, Firebase has `initializeApp`, and json_serializable generates
# `serialize`. Renaming them is not an option and waiving them one by one would be hundreds of
# lines of noise, which is the failure this file's own header warns about.
#
# So IDENTIFIER_PAIRS below are checked everywhere *except* apps/**, and PAIRS — the words that
# cannot be a framework symbol — are checked everywhere including apps/**. The copy a customer
# actually reads stays covered, which is the half that matters most.
#
# # One-off waivers
#
# A line containing `spelling:ok` is skipped, for the single unrenameable identifier rather than
# the whole file. The case this exists for is the HTTP header `Authorization`, which is spelled
# by RFC 9110 and not by us, and which the Go service will meet at SHIP-44 exactly as the
# clients meet it now:
#
#     const authHeader = "Authorization" // spelling:ok — HTTP header name, RFC 9110
#
# Prefer a waiver to an exclude: it names the reason on the line that needs it, and it does not
# quietly stop checking everything else in the file.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

# American form on the left, the spelling we use on the right.
#
# Checked everywhere, apps/** included: none of these can be a framework symbol, and several are
# load-bearing for this product — `cancelled` is a job status (Docs/02 §1), `licence` is what a
# provider is verified against (Docs/04), and `authorisation` is the word half the architecture
# documents turn on.
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
    "cancelation:cancellation"
    "canceled:cancelled"
    "fulfill:fulfil"
    "traveled:travelled"
    "labeled:labelled"
    "license:licence"     # noun; the verb is "license" in both, but we only use the noun
    "favor:favour"
    "meter:metre"
    "liter:litre"
    "defense:defence"
    "offense:offence"
    "practise:practice"   # noun; Australian usage is "practice" for the noun
)

# Checked everywhere except apps/**, because under apps/** each of these is a framework API name
# rather than a word anybody chose. The right-hand spelling is still required of us in prose, in
# Go, and in the documents.
declare -a IDENTIFIER_PAIRS=(
    "serialize:serialise"           # json_serializable, and TS serialize()
    "serialization:serialisation"
    "deserialize:deserialise"
    "normalize:normalise"           # CSS resets
    "normalization:normalisation"
    "initialize:initialise"         # Firebase initializeApp, Gradle, Xcode templates
    "catalog:catalogue"             # Gradle version catalogs
    "behavior:behaviour"            # GestureDetector behavior:, ScrollBehavior, CSS
    "color:colour"                  # every Flutter widget, and Tailwind
    "center:centre"                 # Center(), MainAxisAlignment.center, items-center
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
    # The pnpm lockfile is machine-written and every word in it is a package name somebody
    # else chose — supports-color, color-convert, normalize-path. It does not match *.lock,
    # and it did not exist to exclude until SHIP-22 added the workspace.
    ":(exclude)pnpm-lock.yaml"
    ":(exclude)Docs/09-delivery-backlog-jira.csv"
    # Manifests whose field names are defined by the packaging tool, not by us — `license` is
    # theirs, and the description beside it is not user-facing copy.
    ":(exclude)**/package.json"
    ":(exclude)**/pubspec.yaml"
)

status=0

# check_pair <american> <australian> <pathspec>...
#
# -w so "color" does not match "colorimetry", and -I so binaries are skipped. Searching tracked
# files only, via git, keeps build output and vendored code out of it. The grep -v drops lines
# carrying an explicit waiver; under pipefail an all-filtered result reports no match, which is
# what we want.
check_pair() {
    local american="$1"
    local australian="$2"
    shift 2

    local matches
    if matches=$(git grep -I -n -w -i -- "$american" -- "$@" 2>/dev/null | grep -v 'spelling:ok'); then
        echo "Use '$australian', not '$american':"
        echo "$matches" | sed 's/^/  /'
        echo
        status=1
    fi
}

# Pass one: every pair, over everything that is not an application.
for pair in "${PAIRS[@]}" "${IDENTIFIER_PAIRS[@]}"; do
    check_pair "${pair%%:*}" "${pair##*:}" . ":(exclude)apps/**" "${EXCLUDES[@]}"
done

# Pass two: the pairs that cannot be a framework symbol, over the applications.
for pair in "${PAIRS[@]}"; do
    check_pair "${pair%%:*}" "${pair##*:}" "apps/" "${EXCLUDES[@]}"
done

if [ "$status" -ne 0 ]; then
    echo "Australian English, please — see CLAUDE.md and Docs/10 §9.3."
    echo
    echo "If the match is an identifier that cannot be renamed — an HTTP header, a third-party"
    echo "symbol — append 'spelling:ok' to the line in a comment, with the reason. Add a path to"
    echo "EXCLUDES only when the whole file is somebody else's to spell."
    exit 1
fi

echo "spelling: Australian English throughout"
