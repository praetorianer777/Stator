#!/usr/bin/env bash
# PreToolUse on Edit|Write|NotebookEdit|Bash: all work happens on an issue
# branch named <type>/<issue>-<slug>, never on main, and nothing is pushed
# unless ./run-tests.sh passes.
set -uo pipefail

BRANCH_RE='^(feat|fix|chore|docs|refactor|test|perf|ci|build|revert)/[0-9]+-[a-z0-9][a-z0-9._-]*$'
HINT="Work only on issue branches named <type>/<issue>-<slug> (e.g. fix/42-audio-regression). Find or create the GitHub issue first (see the gh skill), then: gh issue develop <N> --name <type>/<N>-<slug> --base main --checkout"

input="$(cat)"
tool="$(jq -r '.tool_name // empty' <<< "$input")"
project="$(realpath -m "${CLAUDE_PROJECT_DIR:-$(jq -r '.cwd // empty' <<< "$input")}")"
cwd="$(jq -r '.cwd // empty' <<< "$input")"
cwd="${cwd:-$project}"

# Worktrees are separate checkouts of this repo with their own branch and their
# own copy of run-tests.sh, so the checkout is resolved from what is being acted
# on, not from the project directory. They share a common git dir, which is what
# tells a worktree of this repo apart from an unrelated repo on disk.
# rev-parse prints the common dir relative to its own working directory, so it
# is resolved there rather than wherever this hook happens to run.
git_common_dir() { (cd "$1" 2>/dev/null && realpath -m "$(git rev-parse --git-common-dir 2>/dev/null)"); }
project_git_dir="$(git_common_dir "$project")"

checkout_for() {
  local dir="$1" top common
  top="$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null)" || return 1
  common="$(git_common_dir "$dir")"
  [[ "$common" == "$project_git_dir" ]] || return 1
  printf '%s' "$top"
}

decide() {
  jq -n --arg d "$1" --arg r "$2" '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: $d, permissionDecisionReason: $r}}'
  exit 0
}
deny() { decide deny "$1"; }
ask() { decide ask "$1"; }

valid() { [[ "$1" =~ $BRANCH_RE ]]; }

# A rebase that stops on a conflict detaches HEAD, so symbolic-ref yields
# nothing and every git command would be refused — including the `git add` and
# `git rebase --continue` that are the only way out. Both rebase backends record
# the branch being rebased in head-name, which rev-parse locates for a worktree
# too, where it does not sit under .git/ (#97).
current_branch() {
  local repo="$1" branch head_name d
  branch="$(git -C "$repo" symbolic-ref --quiet --short HEAD 2>/dev/null)" && { printf %s "$branch"; return; }
  for d in rebase-merge rebase-apply; do
    head_name="$(cd "$repo" 2>/dev/null && realpath -m "$(git rev-parse --git-path "$d/head-name" 2>/dev/null)")"
    [[ -f "$head_name" ]] || continue
    branch="$(<"$head_name")"
    printf %s "${branch#refs/heads/}"
    return
  done
  printf %s "(detached HEAD)"
}

# True for `git merge --ff-only <upstream>` / `git reset --hard <upstream>` where
# <upstream> is exactly the tracking branch of the checked-out branch.
syncs_with_upstream() {
  local repo="$1" branch="$2"; shift 2
  local upstream target="" mode=0
  upstream="$(git -C "$repo" rev-parse --abbrev-ref --symbolic-full-name "$branch@{upstream}" 2>/dev/null)" || return 1
  [[ -n "$upstream" ]] || return 1
  for a in "$@"; do
    case "$a" in
      --ff-only|--hard) mode=1 ;;
      -*) return 1 ;;
      *) [[ -n "$target" ]] && return 1; target="$a" ;;
    esac
  done
  (( mode )) && [[ "$target" == "$upstream" ]]
}
is_branch() { git -C "$repo" show-ref --verify --quiet "refs/heads/$1"; }

# Stands in for quoted text whose value only the shell knows.
QUOTED=$'\x1f'
UNRESOLVABLE_RE='[$`*?[{]'

# Prints where cd $2 from $1 lands, or fails when only the shell could tell.
# A failed cd leaves the shell where it was, so with $3 set $2 must exist.
resolve_dir() {
  local base="$1" arg="$2" must_exist="${3:-}"
  [[ "$arg" == *"$QUOTED"* || "$arg" =~ $UNRESOLVABLE_RE ]] && return 1
  case "$arg" in
    "~"|"~/"*) arg="$HOME${arg:1}" ;;
    "~"*) return 1 ;;
    /*) ;;
    *) [[ -n "$base" ]] || return 1; arg="$base/$arg" ;;
  esac
  arg="$(realpath -m "$arg")"
  [[ -z "$must_exist" || -d "$arg" ]] || return 1
  printf '%s' "$arg"
}

# Moves $dir the way cd, pushd or popd would; an empty $dir means lost track.
follow_cd() {
  local verb="$1" to=""
  shift
  while [[ "${1:-}" =~ ^-[LPe@n]+$ ]]; do shift; done
  [[ "${1:-}" == -- ]] && shift
  case "$verb" in
    popd)
      if (( $# == 0 && ${#stack[@]} )); then to="${stack[-1]}"; unset 'stack[-1]'; fi
      ;;
    *)
      if (( $# == 0 )); then
        [[ "$verb" == cd ]] && to="$HOME"
      elif [[ "$verb" == cd && "$1" == - ]]; then
        to="$prev"
      elif [[ ! "$1" =~ ^[+-][0-9]+$ ]]; then
        to="$(resolve_dir "$dir" "$1" 1)"
      fi
      [[ "$verb" == pushd ]] && stack+=("$dir")
      ;;
  esac
  prev="$dir"
  dir="$to"
}

# Redirections are not arguments of the command: "git merge --ff-only
# origin/main 2>&1" used to look like a merge with two targets, and every
# rule that inspects the argument list was reading them (#76).
strip_redirections() {
  local a skip_operand=0
  args=()
  for a in "$@"; do
    if (( skip_operand )); then skip_operand=0; continue; fi
    case "$a" in
      [0-9]*'>'*|'>'*|'<'*)
        [[ "$a" =~ (\>|\<)$ ]] && skip_operand=1
        continue
        ;;
    esac
    args+=("$a")
  done
}

run_tests() {
  local repo="$1" log
  log="$(mktemp)"
  # run-tests.sh gives each checkout its own stack, so gates running in
  # parallel worktrees do not collide (#95).
  if ! (cd "$repo" && ./run-tests.sh) > "$log" 2>&1; then
    local tail_out
    tail_out="$(tail -n 60 "$log")"
    rm -f "$log"
    deny "Push blocked: ./run-tests.sh failed. Fix the failures, commit, and push again.
$tail_out"
  fi
  rm -f "$log"
}

case "$tool" in
  Edit|Write|NotebookEdit)
    path="$(jq -r '.tool_input.file_path // .tool_input.notebook_path // empty' <<< "$input")"
    [[ -n "$path" ]] || exit 0
    path="$(realpath -m "$path")"
    # Files outside this repo (scratchpad, memory) are not project work.
    repo="$(checkout_for "$(dirname "$path")")" || exit 0
    branch="$(current_branch "$repo")"
    valid "$branch" || deny "Refusing to edit $path on branch '$branch'. $HINT"
    ;;

  Bash)
    cmd="$(jq -r '.tool_input.command // empty' <<< "$input")"
    [[ -n "$cmd" ]] || exit 0
    # A cd earlier in the command decides where later segments run (#207),
    # so a cwd outside this repo no longer settles the matter on its own.
    dir="$cwd" prev="" lost="" stack=() gates=()
    if repo="$(checkout_for "$cwd")"; then branch="$(current_branch "$repo")"; else repo="" branch=""; fi

    # Heredoc bodies and quoted strings are data (commit messages, issue
    # bodies), not commands; blank them before splitting into segments. A
    # quoted plain word such as a path keeps its value so cd can follow it.
    segments="$(perl -0pe '
      s/(<<-?\s*([\x27"]?)(\w+)\2[^\n]*\n).*?^\s*\3[ \t]*$/$1/gms;
      sub plain { my $s = shift; $s =~ m{\A[\w.\/\@%+=:,-]+\z} ? $s : "\x1f" }
      s{"((?:[^"\\]|\\.)*)"}{plain($1)}gse;
      s{\x27([^\x27]*)\x27}{plain($1)}gse;
      s/\s*(?:&&|\|\||;|\||\n)\s*/\n/g;
    ' <<< "$cmd")"

    # Walk the segments in order so "git switch -c fix/1-x && git commit"
    # is judged against the branch the commit will actually land on.
    while IFS= read -r seg; do
      read -ra t <<< "$seg"
      # Strip env assignments and wrappers (time git push, env -i git commit, ...).
      k=0
      while [[ "${t[k]:-}" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]] \
        || [[ "${t[k]:-}" =~ ^(time|command|exec|nohup|nice|stdbuf|env|sudo|doas)$ ]]; do
        [[ "${t[k]:-}" == env ]] && while [[ "${t[k+1]:-}" == -* ]]; do ((k++)); done
        ((k++))
      done
      t=("${t[@]:k}")
      (( ${#t[@]} )) || continue

      case "${t[0]}" in
        cd|pushd|popd)
          strip_redirections "${t[@]:1}"
          follow_cd "${t[0]}" "${args[@]}"
          [[ -n "$dir" ]] || lost="${t[*]}"
          continue
          ;;
        gh)
          # Outside this repo a merge is none of our business, but a cd the
          # guard lost track of may have led back into it.
          if [[ "${t[1]:-} ${t[2]:-}" == "pr merge" ]] && { [[ -z "$dir" ]] || checkout_for "$dir" >/dev/null; }; then
            ask "Merging a PR is the user's decision."
          fi
          continue
          ;;
        git) ;;
        *) continue ;;
      esac

      target="$dir"
      i=1
      while [[ "${t[i]:-}" == -* ]]; do
        if [[ "${t[i]}" == -C ]]; then
          target="$(resolve_dir "$target" "${t[i+1]:-.}")" || lost="git -C ${t[i+1]:-}"
        fi
        [[ "${t[i]}" == -C || "${t[i]}" == -c ]] && ((i++))
        ((i++))
      done
      sub="${t[i]:-}"
      strip_redirections "${t[@]:i+1}"
      if [[ -z "$target" ]]; then
        case "$sub" in
          add|mv|rm|restore|apply|commit|merge|rebase|cherry-pick|revert|reset|am|push)
            deny "Refusing 'git $sub' after '${lost//$QUOTED/\"...\"}': the guard cannot tell which checkout that leads to. Run git from the checkout itself without cd, or name it with git -C <absolute path>."
            ;;
        esac
        continue
      fi
      # -C into another checkout is judged by that checkout's branch. Worktrees
      # live under .claude/worktrees inside the repo directory, so the path
      # alone does not say which checkout it belongs to — ask git.
      target_repo="$(checkout_for "$target")" || continue
      if [[ "$target_repo" != "$repo" ]]; then
        repo="$target_repo"
        branch="$(current_branch "$repo")"
      fi

      case "$sub" in
        checkout|switch)
          created=0
          for ((j = 0; j < ${#args[@]}; j++)); do
            # A short-flag cluster counts too: -qc, -qb, --quiet -c, ...
            if [[ "${args[j]}" =~ ^-[a-zA-Z]*[bBcC]$ ]]; then
              new="${args[j+1]:-}"
              valid "$new" || deny "Branch name '$new' is not allowed. $HINT"
              branch="$new"
              created=1
            fi
          done
          if (( !created )); then
            # The branch is the first argument that names one: with "git checkout
            # -q main" only args[0] was looked at, so the flag hid the branch and
            # everything after it in the same command was judged against the
            # branch we were on before (#76).
            for a in "${args[@]}"; do
              [[ "$a" == -* ]] && continue
              if is_branch "$a"; then branch="$a"; fi
              break
            done
          fi
          ;;
        branch)
          if [[ "${#args[@]}" -ge 1 && "${args[0]}" != -* ]]; then
            valid "${args[0]}" || deny "Branch name '${args[0]}' is not allowed. $HINT"
          fi
          ;;
        add|mv|rm|restore|apply|commit|merge|rebase|cherry-pick|revert|reset|am)
          # Catching up with the remote is not working on main: refusing it is
          # what left main behind after every merged PR, until a release was cut
          # from stale code (#64). Only a move onto the branch's own upstream is
          # allowed, and only as a fast-forward or a reset to exactly that ref.
          if [[ "$sub" == merge || "$sub" == reset ]] && syncs_with_upstream "$repo" "$branch" "${args[@]}"; then
            continue
          fi
          valid "$branch" || deny "Refusing 'git $sub' on branch '$branch'. $HINT"
          ;;
        push)
          for a in "${args[@]}"; do
            [[ "$a" =~ (^|:|/)(main|master)$ || "$a" == --all || "$a" == --mirror ]] \
              && deny "Pushing to main is not allowed; push the issue branch and open a PR instead."
          done
          valid "$branch" || deny "Refusing to push from branch '$branch'. $HINT"
          [[ " ${gates[*]} " == *" $repo "* ]] || gates+=("$repo")
          ;;
      esac
    done <<< "$segments"

    for g in "${gates[@]}"; do run_tests "$g"; done
    ;;
esac
exit 0
