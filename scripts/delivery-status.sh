#!/usr/bin/env bash
#
# What the repository itself says has been done.
#
# Docs/11 is the tracker a person maintains. This is the half a machine can check: every
# ticket claimed by a commit subject, counted against the backlog in Docs/09. A tracker
# nobody verifies drifts, and this one has form — CLAUDE.md described the project as being
# at SHIP-9 for the whole time SHIP-10 to SHIP-15 was written.
#
# It reads commit subjects, so it sees what a commit *claimed*. A commit that finishes two
# tickets while naming one under-reports, which is why Docs/11 §3 and §4 carry the nuance
# and this only ever reports a floor.
#
#   make status

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

BACKLOG="Docs/09-delivery-backlog.md"
TRACKER="Docs/11-delivery-status.md"
REF="${1:-HEAD}"

[[ -f "$BACKLOG" ]] || { echo "cannot find $BACKLOG" >&2; exit 1; }

bold=$'\033[1m'; green=$'\033[32m'; yellow=$'\033[33m'; dim=$'\033[2m'; off=$'\033[0m'

# --- the backlog: every ticket, its points, its milestone ------------------------------
#
# Rows look like:  | SHIP-15a | Engineering conventions … | 5 | Docs 10 exists … | SHIP-15 |
# Milestones are  ## M0 — Foundation  /  ## Track X — External dependencies

backlog="$(awk -F'|' '
    /^## Track X/            { milestone = "X";  next }
    /^## M[0-9]/             { split($0, m, " "); milestone = substr(m[2], 1, 2); next }
    /^\| *(SHIP|X)-[0-9]+/ {
        id = $2; pts = $4
        gsub(/^[ \t]+|[ \t]+$/, "", id)
        gsub(/^[ \t]+|[ \t]+$/, "", pts)
        if (id ~ /^(SHIP|X)-[0-9]+[a-z]?$/ && pts ~ /^[0-9]+$/)
            print id "\t" pts "\t" milestone
    }
' "$BACKLOG")"

total_tickets=$(wc -l <<<"$backlog" | tr -d ' ')
total_points=$(awk -F'\t' '{s += $2} END {print s+0}' <<<"$backlog")

# --- git: every ticket a commit subject claims -----------------------------------------
#
# The convention from CLAUDE.md is `<TICKET-ID>: <imperative summary>`, so the claim is
# whatever sits before the first colon.

# Two early commits used a range — `SHIP-1-9:` and `SHIP-10-15:` — before the one-ticket-
# per-commit convention settled. Expanding them keeps the git side honest about work that
# genuinely landed; without it this reports 4 of 200 and reads like nothing has happened.
# New commits should name a single ticket, so nothing else will need this.
claimed="$(git log --format='%s' "$REF" 2>/dev/null \
    | sed -n -E 's/^((SHIP|X)-[0-9]+(-[0-9]+)?[a-z]?):.*/\1/p' \
    | awk -F- '
        # SHIP-10-15  ->  SHIP-10 … SHIP-15
        NF == 3 { for (i = $2; i <= $3; i++) print $1 "-" i; next }
        { print }
      ' \
    | sort -u)"

# --- report ----------------------------------------------------------------------------

printf '\n%sDelivery status%s  %s(%s)%s\n\n' "$bold" "$off" "$dim" "$(git rev-parse --abbrev-ref "$REF" 2>/dev/null || echo "$REF")" "$off"

# --- the tracker's own list, which is authoritative -------------------------------------
#
# Docs/11 §10 holds a ```done fenced block. It is what a person asserts is finished; git is
# the check on it, not the source, because a commit subject names one ticket even when the
# change closed two.

declared=""
if [[ -f "$TRACKER" ]]; then
    declared="$(awk '/^```done/ {inside = 1; next} /^```/ {inside = 0} inside' "$TRACKER" \
        | tr ' ' '\n' | sed '/^$/d' | sort -u)"
fi

if [[ -z "$declared" ]]; then
    echo "  no \`\`\`done block found in $TRACKER — falling back to commit subjects" >&2
    declared="$claimed"
fi

done_points=0
done_count=0
unknown=""

while read -r id; do
    [[ -n "$id" ]] || continue
    row="$(awk -F'\t' -v want="$id" '$1 == want {print; exit}' <<<"$backlog")"
    if [[ -z "$row" ]]; then
        unknown+="  $id"
        continue
    fi
    pts=$(cut -f2 <<<"$row")
    done_points=$((done_points + pts))
    done_count=$((done_count + 1))
done <<<"$declared"

printf '  %sdone%s      %s%d%s of %d tickets, %s%d%s of %d points\n' \
    "$bold" "$off" "$green" "$done_count" "$off" "$total_tickets" \
    "$green" "$done_points" "$off" "$total_points"
printf '  remaining %d tickets, %d points\n\n' \
    $((total_tickets - done_count)) $((total_points - done_points))

# Per milestone, so it is obvious which one is actually in progress.
printf '  %sby milestone%s\n' "$bold" "$off"
for m in X M0 M1 M2 M3 M4 M5 M6 M7; do
    m_total=$(awk -F'\t' -v m="$m" '$3 == m {n++} END {print n+0}' <<<"$backlog")
    [[ "$m_total" -gt 0 ]] || continue
    m_done=0
    while read -r id; do
        [[ -n "$id" ]] || continue
        awk -F'\t' -v want="$id" -v m="$m" '$1 == want && $3 == m {found = 1} END {exit !found}' <<<"$backlog" \
            && m_done=$((m_done + 1))
    done <<<"$declared"
    bar=""
    [[ "$m_done" -eq "$m_total" ]] && bar="$green" || { [[ "$m_done" -gt 0 ]] && bar="$yellow"; }
    printf '    %-3s %s%2d%s / %-3d\n' "$m" "$bar" "$m_done" "$off" "$m_total"
done

if [[ -n "$unknown" ]]; then
    printf '\n  %sclaimed by a commit but not in the backlog%s\n' "$yellow" "$off"
    printf '    %s\n' "$unknown"
    printf '    %sadd them to %s, or correct the commit subject%s\n' "$dim" "$BACKLOG" "$off"
fi

# --- does the tracker agree? ------------------------------------------------------------
#
# Not a diff of the whole file — only whether every ticket git thinks is finished is
# mentioned somewhere in Docs/11. That is enough to catch the failure that matters: work
# landing without the tracker being updated in the same change.

if [[ ! -f "$TRACKER" ]]; then
    printf '\n  %s%s is missing%s\n\n' "$yellow" "$TRACKER" "$off"
    exit 1
fi

# Landed but undeclared is the drift that matters: work merged and the tracker not updated
# in the same change. That is what leaves a fresh session with a wrong picture, so it fails.
undeclared="$(comm -13 <(printf '%s\n' "$declared") <(printf '%s\n' "$claimed") || true)"

# Declared but unclaimed is usually legitimate — SHIP-149 landed inside a commit whose
# subject names SHIP-28, because one change closed two tickets. Worth showing, not failing.
unclaimed="$(comm -23 <(printf '%s\n' "$declared") <(printf '%s\n' "$claimed") || true)"

if [[ -n "$unclaimed" ]]; then
    printf '\n  %sdeclared done, but no commit subject names them%s\n' "$dim" "$off"
    printf '    %s\n' "$(tr '\n' ' ' <<<"$unclaimed")"
    printf '    %sfine when one commit closed two tickets; check it is not wishful%s\n' "$dim" "$off"
fi

if [[ -n "$undeclared" ]]; then
    printf '\n  %slanded in git but not in %s §10%s\n' "$yellow" "$TRACKER" "$off"
    printf '    %s\n' "$(tr '\n' ' ' <<<"$undeclared")"
    printf '    %sadd them — a tracker updated later is a tracker that is wrong%s\n\n' "$dim" "$off"
    exit 1
fi

printf '\n  %s✓%s %s agrees with the commit history\n\n' "$green" "$off" "$TRACKER"
