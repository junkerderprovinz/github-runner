#!/usr/bin/env bash
# Starts one GitHub Actions runner for every repository in RUNNER_REPOS.
#
# A personal GitHub account can only register a self-hosted runner to a single
# repository. Sharing one across repositories needs an organization. This
# script keeps a separate copy of the runner for each repository, all in one
# container, and removes their registrations again when the container stops.
set -uo pipefail

RUNNER_HOME=/actions-runner
RUNNERS_DIR=/runners
STATE_DIR=/run/github-runner
API=https://api.github.com
export RUNNER_ALLOW_RUNASROOT=1

log() { printf '[github-runner] %s\n' "$*"; }
die() { log "ERROR: $*"; exit 1; }

/usr/local/bin/print-banner.sh "GitHub-Runner" "one runner per repository"

[[ -n "${ACCESS_TOKEN:-}" ]] || die "ACCESS_TOKEN is empty. Set a personal access token with administration rights on the listed repositories."
[[ -n "${RUNNER_REPOS:-}" ]] || die "RUNNER_REPOS is empty. List the repositories, for example: owner/repo-one owner/repo-two"

TOKEN=$ACCESS_TOKEN
unset ACCESS_TOKEN

NAME=${RUNNER_NAME:-GitHub-Runner}
LABELS=${LABELS:-self-hosted}
RUN_AS_ROOT=${RUN_AS_ROOT:-true}
DEREGISTER=${DEREGISTER_ON_STOP:-true}
RETRY=${RETRY_SECONDS:-300}
extra_args=()
[[ ${NO_DEFAULT_LABELS:-false} == true ]] && extra_args+=(--no-default-labels)

repos=()
for entry in ${RUNNER_REPOS//[,;]/ }; do
  entry=${entry#https://github.com/}
  entry=${entry%/}
  if [[ $entry != */* ]]; then
    [[ -n "${REPO_OWNER:-}" ]] || die "\"$entry\" has no owner. Write it as owner/$entry or set REPO_OWNER."
    entry="$REPO_OWNER/$entry"
  fi
  repos+=("$entry")
done
((${#repos[@]})) || die "RUNNER_REPOS lists no repository."

mkdir -p "$RUNNERS_DIR" "$STATE_DIR"
rm -f "$STATE_DIR/stopping"
echo "${#repos[@]}" > "$STATE_DIR/expected"

if [[ -e /var/run/docker.sock ]]; then
  groupmod -g "$(stat -c %g /var/run/docker.sock)" docker 2>/dev/null || true
fi

api_token() {
  # Prints a short-lived registration or removal token for a repository.
  local repo=$1 kind=$2 body code
  body=$(curl -sS -w '\n%{http_code}' -X POST \
    -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.github+json" \
    "$API/repos/$repo/actions/runners/$kind-token") || return 1
  code=${body##*$'\n'}
  body=${body%$'\n'*}
  case $code in
    201) jq -r .token <<< "$body" ;;
    401) log "$repo: the access token was rejected (401)." ; return 1 ;;
    403|404) log "$repo: the token cannot manage runners here ($code). It needs administration rights on this repository." ; return 1 ;;
    *) log "$repo: GitHub answered $code." ; return 1 ;;
  esac
}

dir_of() { printf '%s/%s' "$RUNNERS_DIR" "${1//\//__}"; }

as_runner() {
  if [[ $RUN_AS_ROOT == true ]]; then
    "$@"
  else
    gosu runner "$@"
  fi
}

register() {
  local repo=$1 dir reg
  dir=$(dir_of "$repo")
  [[ -f $dir/.runner ]] && return 0
  [[ -d $dir ]] || cp -a "$RUNNER_HOME" "$dir"
  mkdir -p "/tmp/runner-work/${repo//\//__}"
  [[ $RUN_AS_ROOT == true ]] || chown -R runner "$dir" "/tmp/runner-work/${repo//\//__}"
  reg=$(api_token "$repo" registration) || return 1
  (cd "$dir" && as_runner ./config.sh --unattended --replace \
    --url "https://github.com/$repo" --token "$reg" \
    --name "$NAME" --labels "$LABELS" "${extra_args[@]}" \
    --work "/tmp/runner-work/${repo//\//__}" >/dev/null) || return 1
  log "$repo: registered as $NAME."
}

deregister() {
  local repo=$1 dir rem
  dir=$(dir_of "$repo")
  [[ -f $dir/.runner ]] || return 0
  if rem=$(api_token "$repo" remove); then
    (cd "$dir" && as_runner ./config.sh remove --token "$rem" >/dev/null 2>&1) \
      && log "$repo: registration removed."
  fi
  rm -f "$dir/.runner" "$dir/.credentials" "$dir/.credentials_rsaparams"
}

serve() {
  local repo=$1 dir
  dir=$(dir_of "$repo")
  while [[ ! -e $STATE_DIR/stopping ]]; do
    if register "$repo"; then
      (cd "$dir" && as_runner ./run.sh)
      [[ -e $STATE_DIR/stopping ]] && break
      log "$repo: runner stopped, starting it again in 10 seconds."
      sleep 10
    else
      log "$repo: trying again in $RETRY seconds."
      sleep "$RETRY"
    fi
  done
}

stop() {
  log "Stopping."
  trap - TERM INT
  touch "$STATE_DIR/stopping"
  pkill -TERM -f Runner.Listener 2>/dev/null
  for _ in $(seq 1 20); do
    pgrep -f Runner.Listener >/dev/null || break
    sleep 1
  done
  if [[ $DEREGISTER == true ]]; then
    for repo in "${repos[@]}"; do deregister "$repo"; done
  fi
  kill 0 2>/dev/null
  exit 0
}
trap stop TERM INT

log "Starting ${#repos[@]} runner(s): ${repos[*]}"
for repo in "${repos[@]}"; do
  serve "$repo" 2>&1 | sed -u -e '/^[[:space:]]*$/d' -e "/^\[github-runner\]/!s|^|[$repo] |" &
done

wait
