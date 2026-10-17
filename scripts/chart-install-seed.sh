#!/usr/bin/env bash
# Fills an instance through its API with what a team using it would have made,
# and checks after an upgrade that all of it is still there:
#   chart-install-seed.sh seed  <base url> <admin email> <password> <state file>
#   chart-install-seed.sh check <base url> <admin email> <password> <state file>
# seed writes what it made to the state file, which check reads. Used by
# scripts/chart-install-cnpg-test.sh; needs only curl and jq.
set -euo pipefail

MODE="$1" BASE="$2" EMAIL="$3" PASSWORD="$4" STATE="$5"
API="$BASE/api/v1"
JAR="$(mktemp)"
SPACE=SEEDED
PAGES=2
# A page's first version comes with its creation; each edit adds one.
EDITS=2
# The worker makes the example space, a few dozen pages, in the background.
EXAMPLE_WAIT_SECONDS=300
trap 'rm -f "$JAR" "$JAR".*' EXIT

fail() { printf '   ❌ %s\n' "$*" >&2; exit 1; }
ok() { printf '   ✅ %s\n' "$*"; }

# call <method> <path> [json body]: the answer's body on 2xx, else a failure
# naming the call and what came back.
call() {
  local method="$1" path="$2" body="${3:-}" code
  local args=(-s -o "$JAR.out" -w '%{http_code}' -X "$method" -b "$JAR" -c "$JAR")
  [[ -n "$body" ]] && args+=(-H 'Content-Type: application/json' -d "$body")
  code="$(curl "${args[@]}" "$API$path")"
  [[ "$code" == 2* ]] || fail "$method $path answered $code: $(head -c 400 "$JAR.out")"
  cat "$JAR.out"
}

sign_in() {
  call POST /auth/login "$(jq -nc --arg e "$EMAIL" --arg p "$PASSWORD" '{email: $e, password: $p}')" >/dev/null
}

doc() {
  jq -nc --arg t "$1" '{type: "doc", content: [{type: "paragraph", content: [{type: "text", text: $t}]}]}'
}

seed() {
  local home page version attachment theme spec token example i n
  sign_in
  home="$(call POST /spaces "$(jq -nc --arg k "$SPACE" '{key: $k, name: "Seeded before the upgrade"}')" | jq -r .space.homePageId)"
  local pages=()
  for i in $(seq 1 "$PAGES"); do
    page="$(call POST /pages "$(jq -nc --arg p "$home" --arg t "Page $i" --argjson b "$(doc "Page $i, first words")" \
      '{parentId: $p, title: $t, body: $b, publish: true}')")"
    version="$(jq -r .page.version <<<"$page")"
    page="$(jq -r .page.id <<<"$page")"
    for n in $(seq 1 "$EDITS"); do
      version="$(call PATCH "/pages/$page" "$(jq -nc --arg t "Page $i, edit $n" --argjson b "$(doc "Page $i after edit $n")" --argjson v "$version" \
        '{title: $t, body: $b, version: $v}')" | jq -r .page.version)"
    done
    pages+=("$(jq -nc --arg id "$page" --arg t "Page $i, edit $EDITS" --argjson v "$version" '{id: $id, title: $t, versions: $v}')")
  done
  ok "space $SPACE with $PAGES pages of $((EDITS + 1)) versions each"

  head -c 50000 /dev/urandom >"$JAR.file"
  attachment="$(curl -fsS -b "$JAR" -F "file=@$JAR.file;filename=seeded.bin;type=application/octet-stream" \
    "$API/pages/$(jq -r .id <<<"${pages[0]}")/attachments" | jq -r .attachment.id)" || fail "the attachment was not taken"
  ok "an attachment on the first page"

  spec="$(call GET /themes/examples | jq -c '.examples[0].spec')"
  theme="$(call POST /themes "$(jq -nc --argjson s "$spec" '{name: "Seeded theme", shared: true, spec: $s}')" | jq -r .theme.id)"
  call PUT /themes/active "$(jq -nc --arg id "$theme" '{themeId: $id}')" >/dev/null
  ok "a theme, chosen"

  token="$(call POST /tokens '{"name": "seeded", "scopes": ["read"]}' | jq -r .token.secret)"
  ok "a personal API token"

  call POST /example-space '{"language": "en"}' >/dev/null
  for _ in $(seq 1 "$EXAMPLE_WAIT_SECONDS"); do
    example="$(call GET /example-space)"
    [[ "$(jq -r '.job.state // empty' <<<"$example")" == failed ]] && fail "the example space was not made: $example"
    [[ "$(jq -r '.space.key // empty' <<<"$example")" != "" && "$(jq -r '.job.state // "done"' <<<"$example")" == "done" ]] && break
    sleep 1
  done
  example="$(jq -r '.space.key // empty' <<<"$example")"
  [[ -n "$example" ]] || fail "the example space was not made in ${EXAMPLE_WAIT_SECONDS}s"
  ok "the example space $example"

  jq -n --arg space "$SPACE" --argjson pages "[$(IFS=,; echo "${pages[*]}")]" --arg attachment "$attachment" \
    --arg sha "$(sha256sum "$JAR.file" | cut -d' ' -f1)" --arg theme "$theme" --arg token "$token" --arg example "$example" \
    '{space: $space, pages: $pages, attachment: $attachment, sha: $sha, theme: $theme, token: $token, example: $example}' >"$STATE"
}

check() {
  local s page got
  s="$(cat "$STATE")"
  sign_in
  call GET "/spaces/$(jq -r .space <<<"$s")" >/dev/null
  while read -r page; do
    got="$(call GET "/pages/$(jq -r .id <<<"$page")")"
    [[ "$(jq -r .page.title <<<"$got")" == "$(jq -r .title <<<"$page")" ]] || fail "page $(jq -r .id <<<"$page") is titled $(jq -r .page.title <<<"$got"), want $(jq -r .title <<<"$page")"
    got="$(call GET "/pages/$(jq -r .id <<<"$page")/versions" | jq -r .total)"
    [[ "$got" == "$(jq -r .versions <<<"$page")" ]] || fail "page $(jq -r .id <<<"$page") has $got versions, want $(jq -r .versions <<<"$page")"
  done < <(jq -c '.pages[]' <<<"$s")
  ok "the space and its pages, with every version"

  got="$(curl -fsS -b "$JAR" "$API/attachments/$(jq -r .attachment <<<"$s")" | sha256sum | cut -d' ' -f1)" || fail "the attachment does not download"
  [[ "$got" == "$(jq -r .sha <<<"$s")" ]] || fail "the attachment's bytes changed"
  ok "the attachment, byte for byte"

  got="$(call GET /themes/active | jq -r '.theme.id // empty')"
  [[ "$got" == "$(jq -r .theme <<<"$s")" ]] || fail "the active theme is [$got], want $(jq -r .theme <<<"$s")"
  ok "the theme, still chosen"

  got="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $(jq -r .token <<<"$s")" "$API/spaces/$(jq -r .space <<<"$s")")"
  [[ "$got" == 200 ]] || fail "the token reads the space with $got, want 200"
  ok "the token reads the space"

  got="$(call GET /example-space | jq -r '.space.key // empty')"
  [[ "$got" == "$(jq -r .example <<<"$s")" ]] || fail "the example space is [$got], want $(jq -r .example <<<"$s")"
  ok "the example space"
}

case "$MODE" in
seed) seed ;;
check) check ;;
*) echo "Say what to do: $0 seed or $0 check, then the base URL, email, password and state file." >&2; exit 2 ;;
esac
