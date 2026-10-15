#!/usr/bin/env bash
# The one place that knows which files carry the release version, so
# release.sh, the release workflow and the tests agree on the list.
#   show               every carrier and the version it holds, one a line
#   check [X.Y.Z]      fails unless they all agree (and equal X.Y.Z if given)
#   set X.Y.Z          writes X.Y.Z into every carrier
#   files              the carriers' paths, one a line
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
CHART="deploy/charts/stator/Chart.yaml"
# A package.json carries a version only if somebody gave it one; once it
# does, it has to move with the others.
PACKAGES=(web/package.json)
SEMVER_RE='^[0-9]+\.[0-9]+\.[0-9]+$'

chart_field() { sed -n "s/^$1: *\"\{0,1\}\([^\"]*\)\"\{0,1\} *\$/\1/p" "$root/$CHART"; }
package_version() { sed -n 's/^  "version": *"\([^"]*\)",\{0,1\}$/\1/p' "$root/$1"; }

show() {
    printf 'VERSION\t%s\n' "$(tr -d '[:space:]' <"$root/VERSION")"
    printf '%s version\t%s\n' "$CHART" "$(chart_field version)"
    printf '%s appVersion\t%s\n' "$CHART" "$(chart_field appVersion)"
    local p
    for p in "${PACKAGES[@]}"; do
        [[ -f "$root/$p" ]] && grep -q '^  "version":' "$root/$p" && printf '%s\t%s\n' "$p" "$(package_version "$p")"
    done
    return 0
}

case "${1:-}" in
show)
    show
    ;;
check)
    want="${2:-}"
    [[ -n "$want" ]] || want=$(tr -d '[:space:]' <"$root/VERSION")
    bad=$(show | awk -F'\t' -v want="$want" '$2 != want { printf "   %s says %s\n", $1, ($2 == "" ? "nothing" : $2) }')
    if [[ -n "$bad" ]]; then
        echo "The release version files disagree with $want:" >&2
        echo "$bad" >&2
        echo "Run scripts/version-files.sh set $want, or release.sh, which writes them all." >&2
        exit 1
    fi
    ;;
set)
    want="${2:-}"
    [[ "$want" =~ $SEMVER_RE ]] || { echo "Give the version as X.Y.Z, digits only, not '$want'." >&2; exit 2; }
    printf '%s\n' "$want" >"$root/VERSION"
    sed -i -E "s/^version: .*/version: $want/; s/^appVersion: .*/appVersion: \"$want\"/" "$root/$CHART"
    for p in "${PACKAGES[@]}"; do
        [[ -f "$root/$p" ]] && sed -i -E "s/^(  \"version\": *)\"[^\"]*\"/\1\"$want\"/" "$root/$p"
    done
    "$0" check "$want"
    ;;
files)
    printf '%s\n' VERSION "$CHART"
    for p in "${PACKAGES[@]}"; do
        [[ -f "$root/$p" ]] && grep -q '^  "version":' "$root/$p" && printf '%s\n' "$p"
    done
    true
    ;;
*)
    echo "Say what to do: $0 show, $0 check [X.Y.Z], $0 set X.Y.Z or $0 files." >&2
    exit 2
    ;;
esac
