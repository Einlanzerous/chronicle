#!/usr/bin/env bash
# CHRN-132 / CHRN-68: the timed restore drill, as one script.
#
# Restores the real `chronicle` database (and `asr`) from a backup into a
# scratch cluster that cannot reach anything real, then proves the APPLICATION
# came back and not only the bytes: the shape, every authored row, the tier
# boundary, the production binary booting disarmed, the text and audio being
# served, and the guard triggers firing on rows. SERV-217 restores the whole
# cluster; this is the part only Chronicle can check. The plan is rev 2 of
# CHRN-68 (`GET /v1/tickets/CHRN-68/plan`); this header says what the script
# does, the plan says why.
#
#   scripts/restore-drill.sh run hub              stage 1: from the local hub
#   scripts/restore-drill.sh run desk             stage 2: from the desktop copy
#                                                 (interactive: asks for the
#                                                 repository password)
#   scripts/restore-drill.sh run dir <dir>        from a directory holding
#                                                 chronicle.dump, asr.dump and
#                                                 globals.sql (what selftest uses)
#   scripts/restore-drill.sh extract|scratch-up|restore|check|boot|serve-checks|teardown
#                                                 the same steps one at a time
#   scripts/restore-drill.sh selftest             throwaway containers only:
#                                                 shows every check red
#
# Runs on `imperial-construct`, where the Docker daemon is; it is not part of
# verify.sh, which should not need Docker.
#
# WHAT IT TOUCHES
#   - The hub repository: read-only bind mount, `--no-lock`, `--no-cache`, in a
#     container with its OWN name (the nightly job force-removes its fixed
#     one). The kernel, not care, is what makes the drill unable to write,
#     lock or prune it.
#   - Production Postgres: two SELECT-only queries (scripts/restore-drill/
#     content.sql and shape.sql, plus audio.sql and migrations.sql) through
#     `docker exec postgres psql`, with default_transaction_read_only=on. The
#     script refuses to send a file that contains a mutating verb. The
#     manifests leave the container as digests, never as text.
#   - Everything else is a container it creates: `chronicle-drill-pg` on a
#     network created --internal, no published port, data on tmpfs. All
#     plaintext (dumps, the audio copy, env files) lives in one mode-0700
#     directory on tmpfs and is removed at teardown.
#
# EXIT: 0 every check passed; 1 a check failed (named on the last lines);
# 2 a refusal or a precondition — nothing was concluded.
set -uo pipefail
umask 077

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SQL="$HERE/restore-drill"
ROOT="$(cd "$HERE/.." && pwd)"

# --- configuration (all overridable, selftest overrides every one) ----------
DRILL_PG="${DRILL_PG:-chronicle-drill-pg}"                  # the scratch server
DRILL_NET="${DRILL_NET:-chronicle-drill-net}"               # the --internal network
DRILL_APP="${DRILL_APP:-chronicle-drill-app}"               # the restored instance
LIVE_PG="${DRILL_LIVE_PG:-postgres}"                        # what manifests are read from
LIVE_APP="${DRILL_LIVE_APP:-chronicle}"                     # whose image digest is booted
FORBIDDEN_NET="${DRILL_FORBIDDEN_NET:-construct_net}"
HUB_REPO="${DRILL_HUB_REPO:-/data/backups/restic-hub}"
DESK_REPO="${DRILL_DESK_REPO:-rest:http://imperial-prime-wsl:8000/construct/}"
STAGE_DIR="/data/backups/staging/shared"
RESTIC_IMAGE="${RESTIC_IMAGE:-restic/restic:0.18.1@sha256:39d9072fb5651c80d75c7a811612eb60b4c06b32ffe87c2e9f3c7222e1797e76}"
REFERENCE_DIR="${DRILL_REFERENCE_DIR:-/data/chronicle/eval/reference}"
LIVE_DB="${DRILL_LIVE_DB:-chronicle}"
LIVE_AUDIO_DIR="${DRILL_AUDIO_SRC:-}"                        # derived from LIVE_APP when empty
WORK="${DRILL_WORK:-${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/chronicle-drill}"

FAILS="$WORK/fails"
RESULTS="$WORK/results"

# --- small helpers -----------------------------------------------------------
log()  { printf '%s\n' "$*" >&2; }
die()  { printf 'restore-drill: REFUSED: %s\n' "$*" >&2; exit 2; }
now()  { awk '{print $1}' /proc/uptime; }   # seconds since boot: monotonic
mark() { printf '%s %s\n' "$1" "$(now)" >>"$WORK/marks"; }
fail() { printf '%s\t%s\n' "$1" "${2:-}" >>"$FAILS"; log "  FAIL  $1${2:+ — $2}"; }
pass() { log "  ok    $1${2:+ — $2}"; printf '%s\t%s\n' "$1" "${2:-}" >>"$RESULTS"; }
# notrun: a check that could not run says so, in the record, by name. It is
# never skipped silently and it is never counted as a pass.
notrun() { log "  NOT RUN  $1 — $2"; printf '%s\t%s\n' "$1" "$2" >>"$WORK/notrun"; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is not installed"; }

init_work() {
  mkdir -p "$WORK" && chmod 700 "$WORK" || die "cannot create $WORK"
  # Plaintext dumps must never land on a disk that outlives the drill.
  [[ "$(stat -f -c %T "$WORK")" == tmpfs ]] \
    || die "$WORK is not on tmpfs (set DRILL_WORK to a directory on tmpfs): the dumps are plaintext"
  : >>"$FAILS"; : >>"$RESULTS"; : >>"$WORK/marks"; : >>"$WORK/notrun"
}

# sp: psql on the SCRATCH server, as its superuser, over the container's local
# socket. Never used against anything else.
sp() { docker exec -i -e PGOPTIONS='-c timezone=UTC -c client_min_messages=warning' "$DRILL_PG" psql -U postgres -X -q -v ON_ERROR_STOP=1 "$@"; }

# assert_select_only refuses to send a query file to production if, with its
# comments removed, it contains a verb that writes or changes anything.
assert_select_only() {
  local f="$1" body
  body="$(sed -E 's/--.*$//' "$f")"
  if grep -qiE '\b(insert|update|delete|drop|alter|create|grant|revoke|truncate|copy|call|do|vacuum|reindex|cluster|lock|set|reset|listen|notify|prepare|execute|comment|refresh)\b' <<<"$body"; then
    die "$f contains a statement that is not a plain SELECT; it is never sent to production"
  fi
  grep -qiE '^\s*(with|select)\b' <<<"$body" || die "$f does not begin with SELECT or WITH"
}

# lq: run one query file against the LIVE server, read-only, in UTC. The only
# door to production this script has.
lq() {
  local file="$1" db="${2:-$LIVE_DB}"
  assert_select_only "$file"
  docker exec -i -e PGOPTIONS='-c default_transaction_read_only=on -c timezone=UTC' \
    "$LIVE_PG" psql -U postgres -d "$db" -X -q -At -F $'\t' -v ON_ERROR_STOP=1 -f - <"$file"
}
# sq: the same query file against the restored copy.
sq() {
  local file="$1" db="${2:-$LIVE_DB}"
  docker exec -i -e PGOPTIONS='-c timezone=UTC' \
    "$DRILL_PG" psql -U postgres -d "$db" -X -q -At -F $'\t' -v ON_ERROR_STOP=1 -f - <"$file"
}

# --- guards: the script cannot be pointed at anything real -------------------
# guard_names: every name this script is ever allowed to REMOVE is a
# chronicle-drill* name and is not the live server or app it reads from. It is
# the first thing main does, before any subcommand and before any trap exists,
# and it is repeated at each destructive call so that none of them relies on
# its callers: an EXIT trap runs cmd_teardown after every refusal, and a
# teardown that trusted its names would remove the thing just refused.
guard_names() {
  local n
  for n in "$DRILL_PG" "$DRILL_APP" "$DRILL_NET"; do
    case "$n" in
      chronicle-drill*) ;;
      *) die "'$n' is not a chronicle-drill* name; this script never removes anything else" ;;
    esac
  done
  [[ "$DRILL_PG" != "$LIVE_PG" && "$DRILL_APP" != "$LIVE_APP" ]] \
    || die "a drill name is the same as the live server or app it reads from"
}

guard_target() {
  local c="$DRILL_PG" nets n ports
  case "$c" in
    chronicle-drill*) ;;
    *) die "'$c' is not a chronicle-drill* container" ;;
  esac
  [[ "$c" != "postgres" && "$c" != "postgres-dev" ]] || die "'$c' is a shared Postgres server"
  docker inspect "$c" >/dev/null 2>&1 || die "scratch server '$c' is not running (scratch-up first)"
  nets="$(docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$c")"
  [[ " $nets " != *" $FORBIDDEN_NET "* ]] || die "'$c' is attached to $FORBIDDEN_NET"
  for n in $nets; do
    [[ "$(docker network inspect -f '{{.Internal}}' "$n")" == "true" ]] \
      || die "'$c' is on network '$n', which is not --internal"
  done
  ports="$(docker inspect -f '{{len .HostConfig.PortBindings}}' "$c")"
  [[ "$ports" == "0" ]] || die "'$c' publishes a port; the scratch server must not answer on the host"
}

# guard_empty runs once, before the restore: a server that already holds a
# database with the real name is not scratch, whatever it is called.
guard_empty() {
  local n
  for n in chronicle chronicle_test; do
    [[ -z "$(sp -d postgres -At -c "SELECT 1 FROM pg_database WHERE datname = '$n'")" ]] \
      || die "'$DRILL_PG' already holds a database named $n"
  done
}

# assert_disarmed refuses to START the restored instance if its docker
# arguments or env file could reach anything real. The pruner it carries
# unlinks files; it must only ever see a copy.
assert_disarmed() {
  local a envfile="" bad
  local -a args=("$@")
  bad='CHRONICLE_(INBOX_DIR|ASR_URL|ASR_TOKEN|SCRIBE_OLLAMA_URL|SWITCHYARD_URL|AMBER_URL|CF_ACCESS_TEAM_DOMAIN)='
  for ((i = 0; i < ${#args[@]}; i++)); do
    a="${args[i]}"
    case "$a" in
      --env-file) envfile="${args[i+1]}" ;;
      -v | --volume | --mount)
        local m="${args[i+1]}"
        [[ "$m" != /data/chronicle* && "$m" != *"source=/data/chronicle"* ]] \
          || die "refusing to start: $m mounts /data/chronicle"
        [[ -z "$LIVE_AUDIO_DIR" || "$m" != "$LIVE_AUDIO_DIR":* ]] \
          || die "refusing to start: $m mounts the real audio directory"
        ;;
      -e | --env)
        grep -qE "^$bad" <<<"${args[i+1]}" && die "refusing to start: ${args[i+1]%%=*} is set in the restored instance"
        ;;
      --network)
        [[ "${args[i+1]}" != "$FORBIDDEN_NET" ]] || die "refusing to start on $FORBIDDEN_NET"
        ;;
    esac
  done
  if [[ -n "$envfile" ]] && grep -qE "^$bad" "$envfile"; then
    die "refusing to start: $(grep -oE "^$bad" "$envfile" | head -1 | tr -d =) is set in the restored instance"
  fi
  return 0
}

# --- extraction --------------------------------------------------------------
# filter_globals keeps only the statements that concern the three roles this
# database restores with, and strips every password verifier. The unfiltered
# file carries a verifier for every role in the estate (the producer does not
# pass --no-role-passwords), so it is streamed through this and never written.
filter_globals() {
  awk '
    /^(CREATE|ALTER) ROLE (chronicle|chronicle_tier1|asr)( |;)/ { print; next }
    /^GRANT .* TO (chronicle|chronicle_tier1|asr)( |;)/        { print; next }
  ' | sed -E "s/ PASSWORD '[^']*'//"
}

restic_hub() {
  need signet
  signet exec -secret construct-backup/RESTIC_PASSWORD_HUB -- \
    docker run --rm --name "chronicle-drill-restic-hub-$$" --network none \
      --user "$(id -u):$(id -g)" -e HOME=/tmp -e RESTIC_PASSWORD_HUB \
      -v "$HUB_REPO:/hub:ro" --entrypoint sh "$RESTIC_IMAGE" \
      -c 'export RESTIC_PASSWORD="$RESTIC_PASSWORD_HUB" RESTIC_REPOSITORY=/hub; exec restic --no-lock --no-cache "$@"' sh "$@"
}

# hub_window_reason prints why the hub must not be read now, or nothing.
hub_window_reason() {
  local hm
  hm="$(TZ=America/Chicago date +%H%M)"
  if ((10#$hm >= 225 && 10#$hm <= 320)); then
    echo "the nightly backup runs 02:25-03:20 CDT; the drill does not read the hub then"
  elif systemctl is-active --quiet construct-backup.service 2>/dev/null; then
    echo "construct-backup.service is active; the drill does not read the hub meanwhile"
  fi
}
guard_hub_window() {
  local why; why="$(hub_window_reason)"
  [[ -z "$why" ]] || die "$why"
}

hub_ids() { restic_hub snapshots --json | jq -r '.[].id' | sort; }

pick_snapshot() { # <runner...>: newest 'shared' snapshot that contains chronicle.dump
  local id ids
  ids="$("$@" snapshots --tag shared --json | jq -r 'sort_by(.time) | reverse | .[].id')" || return 1
  for id in $ids; do
    if "$@" ls "$id" "$STAGE_DIR/chronicle.dump" 2>/dev/null | grep -qx "$STAGE_DIR/chronicle.dump"; then
      echo "$id"; return 0
    fi
  done
  return 1
}

record_sums() { # <stage>: the SHA-256 of the three files as stored, before any filtering
  ( cd "$WORK" && sha256sum chronicle.dump asr.dump >"sums.$1.txt" && cat globals.sha >>"sums.$1.txt" )
}

extract_hub() {
  need jq; guard_hub_window
  local sid f
  sid="$(pick_snapshot restic_hub)" || { log "no hub snapshot holds chronicle.dump"; return 2; }
  echo "$sid" >"$WORK/snapshot.hub"
  hub_ids >"$WORK/hub.ids.before" || { log "cannot list the hub's snapshots"; return 2; }
  mark extract-start
  for f in chronicle.dump asr.dump; do
    restic_hub dump "$sid" "$STAGE_DIR/$f" >"$WORK/$f" || { log "restic dump $f failed"; return 2; }
  done
  restic_hub dump "$sid" "$STAGE_DIR/globals.sql" \
    | tee >(sha256sum | sed 's/-$/globals.sql/' >"$WORK/globals.sha") \
    | filter_globals >"$WORK/globals.filtered.sql" || { log "restic dump globals failed"; return 2; }
  wait
  record_sums hub
  mark files-on-disk
  hub_ids >"$WORK/hub.ids.after"
}

extract_desk() {
  need jq; need signet
  [[ -t 0 ]] || die "stage 2 asks for the desktop repository's password and needs a terminal"
  local pw sid f orig out="$WORK/desk-out"
  printf 'desk repository password (not echoed): ' >&2
  read -rs pw; echo >&2
  [[ -n "$pw" ]] || die "no password given"
  mkdir -p "$out"
  export DRILL_DESK_RESTIC_PASSWORD="$pw"; unset pw
  restic_desk() {
    signet exec -secret construct-backup/REST_USER_DESK -secret construct-backup/REST_PASSWORD_DESK -- \
      docker run --rm --name "chronicle-drill-restic-desk-$$" --network host \
        --user "$(id -u):$(id -g)" -e HOME=/tmp \
        -e REST_USER_DESK -e REST_PASSWORD_DESK -e DRILL_DESK_RESTIC_PASSWORD -e DESK_REPO="$DESK_REPO" \
        -v "$out:/out" --entrypoint sh "$RESTIC_IMAGE" \
        -c 'export RESTIC_PASSWORD="$DRILL_DESK_RESTIC_PASSWORD" RESTIC_REPOSITORY="$DESK_REPO" RESTIC_REST_USERNAME="$REST_USER_DESK" RESTIC_REST_PASSWORD="$REST_PASSWORD_DESK"; exec restic --no-lock --no-cache "$@"' sh "$@"
  }
  sid="$(pick_snapshot restic_desk)" || { log "no desk snapshot holds chronicle.dump"; unset DRILL_DESK_RESTIC_PASSWORD; return 2; }
  echo "$sid" >"$WORK/snapshot.desk"
  # restic copy gives a copied snapshot its own id and keeps the hub's in
  # `original`; that, not the desk id, is what names the same snapshot.
  orig="$(restic_desk snapshots --tag shared --json | jq -r --arg id "$sid" '.[] | select(.id == $id) | .original // ""')"
  mark extract-start
  restic_desk restore "$sid" --target /out \
    --include "$STAGE_DIR/chronicle.dump" --include "$STAGE_DIR/asr.dump" --include "$STAGE_DIR/globals.sql" \
    || { unset DRILL_DESK_RESTIC_PASSWORD; return 2; }
  unset DRILL_DESK_RESTIC_PASSWORD
  mv "$out$STAGE_DIR/chronicle.dump" "$out$STAGE_DIR/asr.dump" "$WORK/" || return 2
  sha256sum <"$out$STAGE_DIR/globals.sql" | sed 's/-$/globals.sql/' >"$WORK/globals.sha"
  filter_globals <"$out$STAGE_DIR/globals.sql" >"$WORK/globals.filtered.sql"
  rm -rf "$out"
  record_sums desk
  mark files-on-disk
  compare_desk_with_hub "$orig"
}

# compare_desk_with_hub <original>: where the hub still holds the snapshot the
# desk copy came from, the three files' SHA-256 must be equal. The hub side is
# a fresh `restic dump | sha256sum`; nothing here relies on a stage 1 that has
# since been torn down. If it cannot run it says why, by name, in the record.
compare_desk_with_hub() {
  local orig="$1" f got why
  if [[ -z "$orig" ]]; then
    notrun desk-matches-hub "the desk snapshot has no 'original': it was not copied from the hub"; return 0
  fi
  why="$(hub_window_reason)"
  if [[ -n "$why" ]]; then notrun desk-matches-hub "$why"; return 0; fi
  if ! hub_ids 2>/dev/null | grep -qx "$orig"; then
    notrun desk-matches-hub "the hub no longer holds ${orig:0:8} (forgotten by its retention)"; return 0
  fi
  : >"$WORK/sums.hubfresh.txt"
  for f in chronicle.dump asr.dump globals.sql; do
    got="$(restic_hub dump "$orig" "$STAGE_DIR/$f" | sha256sum | cut -d' ' -f1)" \
      && [[ -n "$got" ]] || { notrun desk-matches-hub "restic dump of $f from the hub failed"; return 0; }
    printf '%s  %s\n' "$got" "$f" >>"$WORK/sums.hubfresh.txt"
  done
  if cmp -s "$WORK/sums.hubfresh.txt" "$WORK/sums.desk.txt"; then
    pass desk-matches-hub "hub snapshot ${orig:0:8} and its desk copy: three files, identical SHA-256"
  else
    fail desk-matches-hub "hub snapshot ${orig:0:8} and its desk copy differ: $(diff "$WORK/sums.hubfresh.txt" "$WORK/sums.desk.txt" | grep -c '^[<>]') line(s)"
  fi
}

extract_dir() { # <dir>
  local d="$1" f
  [[ -d "$d" ]] || die "$d is not a directory"
  mark extract-start
  for f in chronicle.dump asr.dump globals.sql; do [[ -f "$d/$f" ]] || die "$d/$f is missing"; done
  cp "$d/chronicle.dump" "$d/asr.dump" "$WORK/"
  sha256sum <"$d/globals.sql" | sed 's/-$/globals.sql/' >"$WORK/globals.sha"
  filter_globals <"$d/globals.sql" >"$WORK/globals.filtered.sql"
  record_sums dir
  mark files-on-disk
}

cmd_extract() {
  init_work
  case "${1:-}" in
    hub)  extract_hub  ;;
    desk) extract_desk ;;
    dir)  extract_dir "${2:-}" ;;
    *)    die "extract hub | desk | dir <path>" ;;
  esac || return $?
  local r
  r="$(grep -c '^CREATE ROLE' "$WORK/globals.filtered.sql")"
  [[ "$r" -eq 3 ]] || { log "globals filter kept $r CREATE ROLE statements, expected 3"; return 2; }
  grep -qi 'password' "$WORK/globals.filtered.sql" && { log "a password verifier survived the filter"; return 2; }
  log "extracted: $(cut -d' ' -f1 "$WORK"/sums.*.txt | head -3 | cut -c1-12 | tr '\n' ' ')(sha256 prefixes)"
}

# --- the scratch server ------------------------------------------------------
rand() { openssl rand -hex 16; }

cmd_scratch_up() {
  guard_names
  init_work
  case "$DRILL_PG" in chronicle-drill*) ;; *) die "'$DRILL_PG' is not a chronicle-drill* container" ;; esac
  case "$DRILL_NET" in chronicle-drill*) ;; *) die "'$DRILL_NET' is not a chronicle-drill* network" ;; esac
  docker inspect "$LIVE_PG" >/dev/null 2>&1 || die "no server named '$LIVE_PG' to take the image from"
  local image
  image="$(docker inspect -f '{{.Image}}' "$LIVE_PG")"
  guard_names
  docker rm -fv "$DRILL_PG" >/dev/null 2>&1
  if docker network inspect "$DRILL_NET" >/dev/null 2>&1; then
    [[ "$(docker network inspect -f '{{.Internal}}' "$DRILL_NET")" == "true" ]] \
      || die "network $DRILL_NET exists and is not --internal"
  else
    docker network create --internal "$DRILL_NET" >/dev/null || die "cannot create $DRILL_NET"
  fi
  docker run -d --name "$DRILL_PG" --network "$DRILL_NET" --label chronicle-drill=1 \
    --tmpfs /var/lib/postgresql/data -v "$WORK:/drill:ro" \
    -e POSTGRES_PASSWORD="$(rand)" "$image" >/dev/null || die "cannot start $DRILL_PG"
  local i
  for i in $(seq 1 60); do
    docker exec "$DRILL_PG" pg_isready -q -h 127.0.0.1 -U postgres && break
    sleep 1
  done
  docker exec "$DRILL_PG" pg_isready -q -h 127.0.0.1 -U postgres || die "$DRILL_PG did not become ready"
  mark scratch-ready
  log "scratch server $DRILL_PG up on $DRILL_NET (image $(docker inspect -f '{{.Config.Image}}' "$DRILL_PG" | cut -c1-19))"
}

# --- restore -----------------------------------------------------------------
cmd_restore() {
  init_work
  guard_target
  guard_empty
  [[ -s "$WORK/chronicle.dump" && -s "$WORK/globals.filtered.sql" ]] || die "nothing extracted (extract first)"
  local err="$WORK/restore.err" db rc=0 pw1 pw2 pw3
  : >"$err"
  # 1. the three roles, no passwords yet.
  sp -d postgres -f - <"$WORK/globals.filtered.sql" 2>>"$err" || rc=1
  pw1="$(rand)"; pw2="$(rand)"; pw3="$(rand)"
  sp -d postgres -c "ALTER ROLE chronicle PASSWORD '$pw1'; ALTER ROLE chronicle_tier1 PASSWORD '$pw2'; ALTER ROLE asr PASSWORD '$pw3'" >/dev/null 2>>"$err" || rc=1
  printf 'CHRONICLE_PW=%s\nTIER1_PW=%s\n' "$pw1" "$pw2" >"$WORK/pw.env"
  # 2. whole databases, with --create (it is what carries the database ACL),
  #    never --clean and never a row-level replay (CH092 and CH041 refuse it).
  for db in chronicle asr; do
    if [[ "${DRILL_SELFTEST_NOCREATE:-}" == 1 && "$db" == chronicle ]]; then
      # Test-only: the wrong recipe, so selftest can show the shape check red.
      sp -d postgres -c "CREATE DATABASE chronicle OWNER chronicle" 2>>"$err" || rc=1
      docker exec "$DRILL_PG" pg_restore -U postgres --exit-on-error -d chronicle "/drill/$db.dump" 2>>"$err" || rc=1
    else
      docker exec "$DRILL_PG" pg_restore -U postgres --create --exit-on-error -d postgres "/drill/$db.dump" 2>>"$err" || rc=1
    fi
  done
  # 3. any error or warning at all fails the drill.
  if [[ $rc -ne 0 || -s "$err" ]]; then
    fail restore "pg_restore/psql reported: $(head -c 300 "$err" | tr '\n' ' ')"
    return 1
  fi
  mark restore-complete
  pass restore "chronicle and asr restored whole, with --create, no warnings"
}

# --- the checks --------------------------------------------------------------
check_shape() {
  local db live restored
  for db in chronicle asr; do
    live="$WORK/shape.live.$db"; restored="$WORK/shape.restored.$db"
    lq "$SQL/shape.sql" "$db" >"$live" || { fail shape "could not read the live $db shape"; return 1; }
    sq "$SQL/shape.sql" "$db" >"$restored" || { fail shape "could not read the restored $db shape"; return 1; }
    if [[ "$db" == chronicle ]]; then
      lq "$SQL/migrations.sql" "$db" >>"$live" || return 1
      sq "$SQL/migrations.sql" "$db" >>"$restored" || return 1
    fi
    if ! diff -u "$live" "$restored" >"$WORK/shape.diff.$db"; then
      fail shape "$db differs: $(grep -c '^[-+][^-+]' "$WORK/shape.diff.$db") line(s); first: $(grep -m1 '^[-+][^-+]' "$WORK/shape.diff.$db" | cut -c1-160)"
      return 1
    fi
  done
  pass shape "$(wc -l <"$WORK/shape.live.chronicle") chronicle lines and $(wc -l <"$WORK/shape.live.asr") asr lines identical, live and restored"
}

dump_time() { # the archive's own creation time, not the restic snapshot's
  local hdr
  hdr="$(docker exec "$DRILL_PG" pg_restore -l /drill/chronicle.dump | sed -n 's/^; Archive created at //p' | head -1)"
  [[ -n "$hdr" ]] || return 1
  date -u -d "$hdr" +%Y-%m-%dT%H:%M:%SZ
}

check_content() {
  local dt out
  dt="$(dump_time)" || { fail content "cannot read the archive creation time from pg_restore -l"; return 1; }
  echo "$dt" >"$WORK/dump_time"
  sp -d postgres -c 'DROP DATABASE IF EXISTS drillcmp' -c 'CREATE DATABASE drillcmp' >/dev/null || return 1
  sp -d drillcmp -c 'CREATE TABLE res_raw (t text, pk text, fz text, cls text, md text, upd text, cre text); CREATE TABLE live_raw (LIKE res_raw)' || return 1
  lq "$SQL/content.sql" >"$WORK/content.live" || { fail content "could not read the live manifest"; return 1; }
  sq "$SQL/content.sql" >"$WORK/content.restored" || { fail content "could not read the restored manifest"; return 1; }
  local copy="COPY TBL FROM STDIN (FORMAT csv, DELIMITER E'\\t', NULL E'\\x02', QUOTE E'\\x01')"
  sp -d drillcmp -c "${copy/TBL/res_raw}" <"$WORK/content.restored" || return 1
  sp -d drillcmp -c "${copy/TBL/live_raw}" <"$WORK/content.live" || return 1
  sp -d drillcmp -c "
    CREATE VIEW res AS SELECT t, pk, fz, cls, md, nullif(upd, '')::timestamptz AS upd, cre::timestamptz AS cre FROM res_raw;
    CREATE VIEW live AS SELECT t, pk, fz, cls, md, nullif(upd, '')::timestamptz AS upd, cre::timestamptz AS cre FROM live_raw" || return 1
  out="$(sp -d drillcmp -At -F $'\t' -v "dump=$dt" -f - <"$SQL/compare.sql")" || { fail content "comparison query failed"; return 1; }
  printf '%s\n' "$out" >"$WORK/content.compare"
  local nfail
  nfail="$(grep -c '^FAIL' "$WORK/content.compare")"
  grep '^LOSS' "$WORK/content.compare" >"$WORK/loss.txt" || true
  if [[ "$nfail" -gt 0 ]]; then
    fail content "$nfail row(s) failed; first: $(grep -m1 '^FAIL' "$WORK/content.compare" | tr '\t' ' ' | cut -c1-200)"
    return 1
  fi
  pass content "$(wc -l <"$WORK/content.restored") restored rows, every one matches live or was legitimately changed; dump time $dt; listed for a person: $(grep -c '^LIST' "$WORK/content.compare"); measured loss rows: $(awk -F'\t' '{s+=$4} END{print s+0}' "$WORK/loss.txt")"
}

# sqlstate <role> <db> <sql> [stdin]: the SQLSTATE an error carried, or OK.
sqlstate() {
  local role="$1" db="$2" sql="$3" out
  out="$(docker exec -i "$DRILL_PG" psql -U "$role" -d "$db" -X -q -v VERBOSITY=verbose -c "$sql" 2>&1 >/dev/null)"
  if [[ -z "$out" ]]; then echo OK; else sed -nE 's/^ERROR: +([A-Z0-9]{5}):.*/\1/p' <<<"$out" | head -1; fi
}

write_app_env() {
  local owner
  # shellcheck disable=SC1091
  . "$WORK/pw.env"
  owner="$(sp -d chronicle -At -c "SELECT email FROM tier2.users WHERE is_owner LIMIT 1")"
  [[ -n "$owner" ]] || { log "the restored database has no owner account"; return 1; }
  mkdir -p "$WORK/wiki-empty"
  {
    echo "CHRONICLE_DATABASE_URL=postgres://chronicle:$CHRONICLE_PW@$DRILL_PG:5432/chronicle?sslmode=disable"
    echo "CHRONICLE_TIER1_DATABASE_URL=postgres://chronicle_tier1:$TIER1_PW@$DRILL_PG:5432/chronicle?sslmode=disable"
    echo "CHRONICLE_OWNER_EMAIL=$owner"
    echo "CHRONICLE_AUDIO_DIR=/audio"
    echo "CHRONICLE_TIER1_WIKI_DIR=/wiki-empty"
    echo "CHRONICLE_PORT=4009"
    echo "CHRONICLE_LOG_FORMAT=text"
    echo "CHRONICLE_COOKIE_SECURE=false"
  } >"$WORK/app.env"
}

# app_run <extra docker flags> -- <chronicle args>: the restored instance's
# image (or, for selftest, a tree-built binary), on the drill network only.
resolve_live_audio() {
  [[ -z "$LIVE_AUDIO_DIR" && -n "$LIVE_APP" ]] || return 0
  LIVE_AUDIO_DIR="$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$LIVE_APP" 2>/dev/null | sed -n 's/^CHRONICLE_AUDIO_DIR=//p')"
}

app_run() {
  resolve_live_audio
  local -a flags=() cargs=()
  while [[ $# -gt 0 && "$1" != "--" ]]; do flags+=("$1"); shift; done
  shift
  cargs=("$@")
  local -a pre=() image
  if [[ -n "${DRILL_CHRONICLE_BIN:-}" ]]; then
    image="$(docker inspect -f '{{.Image}}' "$LIVE_PG")"
    pre=(--entrypoint /chronicle -v "$DRILL_CHRONICLE_BIN:/chronicle:ro")
  else
    image="$(docker inspect -f '{{.Image}}' "$LIVE_APP")" || die "no '$LIVE_APP' container to take the image from"
  fi
  local -a base=(docker run --network "$DRILL_NET" --env-file "$WORK/app.env"
    -v "$WORK/audio:/audio" -v "$WORK/wiki-empty:/wiki-empty:ro" "${pre[@]}")
  # The extra hooks exist so selftest can show the guard refusing; they go
  # through the same guard as everything else.
  [[ -n "${DRILL_SELFTEST_EXTRA_ENV:-}" ]] && base+=(-e "$DRILL_SELFTEST_EXTRA_ENV")
  [[ -n "${DRILL_SELFTEST_EXTRA_MOUNT:-}" ]] && base+=(-v "$DRILL_SELFTEST_EXTRA_MOUNT")
  assert_disarmed "${base[@]:2}" "${flags[@]}"
  "${base[@]}" "${flags[@]}" "$image" "${cargs[@]}"
}

check_tier1() {
  local out rc n s
  write_app_env || return 1
  mkdir -p "$WORK/audio"
  out="$(app_run --rm -- tier1-audit 2>&1)"; rc=$?
  if [[ $rc -ne 0 ]]; then
    fail tier1-audit "tier1-audit exited $rc on the restored database: $(tail -n 3 <<<"$out" | tr '\n' ' ' | cut -c1-200)"
    return 1
  fi
  # The check must be able to fail: widen on scratch, expect exactly one finding.
  sp -d chronicle -c 'GRANT SELECT ON tier2.notes TO chronicle_tier1' || return 1
  out="$(app_run --rm -- tier1-audit 2>&1)"; rc=$?
  sp -d chronicle -c 'REVOKE SELECT ON tier2.notes FROM chronicle_tier1' || return 1
  n="$(grep -oE '[0-9]+ finding' <<<"$out" | head -1 | grep -oE '^[0-9]+')"
  if [[ $rc -eq 0 || "$n" != 1 ]]; then
    fail tier1-audit "a deliberate widening was not caught as exactly one finding (exit $rc, findings '${n:-none}')"
    return 1
  fi
  s="$(sqlstate chronicle_tier1 chronicle 'SELECT count(*) FROM tier2.memos')"
  [[ "$s" == OK ]] || { fail tier1-audit "chronicle_tier1 cannot read tier2.memos ($s)"; return 1; }
  s="$(sqlstate chronicle_tier1 chronicle 'SELECT 1 FROM tier2.notes LIMIT 1')"
  [[ "$s" == 42501 ]] || { fail tier1-audit "chronicle_tier1 reading tier2.notes gave '$s', not 42501"; return 1; }
  s="$(sqlstate chronicle_tier1 chronicle "INSERT INTO tier2.memos (author_id, content_hash, byte_size) VALUES (gen_random_uuid(), repeat('a', 64), 1)")"
  [[ "$s" == 42501 ]] || { fail tier1-audit "chronicle_tier1 inserting into tier2.memos gave '$s', not 42501"; return 1; }
  pass tier1-audit "clean; a widening is exactly one finding; reads memos, refused on notes and on insert (42501)"
}

cmd_check() {
  init_work
  guard_target
  local c failed=0
  local -a which=("$@")
  [[ ${#which[@]} -gt 0 ]] || which=(shape content tier1)
  for c in "${which[@]}"; do
    case "$c" in
      shape)   check_shape   || failed=1 ;;
      content) check_content || failed=1 ;;
      tier1)   check_tier1   || failed=1 ;;
      *) die "unknown check '$c'" ;;
    esac
  done
  mark manifests-pass
  return $failed
}

# --- boot, and the checks that need a running instance -----------------------
APP_TOKEN=""

app_exec() { docker exec "$DRILL_APP" "$@"; }

# api <GET|POST> <path> [json]: sets API_STATUS and leaves the body in
# $API_BODY. Never called in a subshell, or the status would be lost.
API_STATUS=""
API_BODY=""
api() {
  local method="$1" path="$2" body="${3:-}" hdr
  local -a w=(wget -S -q -O - --header 'Content-Type: application/json')
  [[ -n "$APP_TOKEN" ]] && w+=(--header "Authorization: Bearer $APP_TOKEN")
  [[ "$method" == POST ]] && w+=(--post-data "$body")
  hdr="$WORK/api.hdr"; API_BODY="$WORK/api.body"
  app_exec "${w[@]}" "http://127.0.0.1:4009$path" >"$API_BODY" 2>"$hdr"
  API_STATUS="$(awk '/^ *HTTP\//{c=$2} END{print c}' "$hdr")"
}

cmd_boot() {
  guard_names
  init_work
  guard_target
  write_app_env || return 1
  rm -rf "$WORK/audio"; mkdir -p "$WORK/audio"
  resolve_live_audio
  local src="$LIVE_AUDIO_DIR"
  [[ -n "$src" && -d "$src" ]] || { fail boot "no live audio directory to copy (set DRILL_AUDIO_SRC)"; return 1; }
  # Order is fixed: inventory, copy the tree, read the live manifest (done by
  # the content check), then boot.
  find "$src" -path "$src/.uploads" -prune -o -type f -printf '%P\t%s\n' | sort >"$WORK/audio.before"
  ( cd "$src" && tar --exclude=./.uploads -cf - . ) | ( cd "$WORK/audio" && tar -xf - )
  guard_names
  docker rm -fv "$DRILL_APP" >/dev/null 2>&1
  app_run -d --name "$DRILL_APP" -- serve >/dev/null || { fail boot "the container did not start"; return 1; }
  local i
  for i in $(seq 1 60); do
    api GET /readyz
    [[ "$API_STATUS" == 200 ]] && break
    sleep 1
  done
  if [[ "$API_STATUS" != 200 ]]; then
    fail boot "/readyz never answered 200: $(docker logs --tail 3 "$DRILL_APP" 2>&1 | tr '\n' ' ' | cut -c1-240)"
    return 1
  fi
  api GET /healthz
  [[ "$API_STATUS" == 200 ]] || { fail boot "/healthz answered '$API_STATUS'"; return 1; }
  mark readyz-200
  local migs_before migs_after
  migs_before="$(<"$WORK/shape.restored.chronicle" grep -c '^mig|' || true)"
  migs_after="$(sp -d chronicle -At -c 'SELECT count(*) FROM public.schema_migrations')"
  [[ "$migs_before" == "$migs_after" ]] || { fail boot "boot applied migrations ($migs_before -> $migs_after): the snapshot predates the binary"; return 1; }
  local ran
  if [[ -n "${DRILL_CHRONICLE_BIN:-}" ]]; then
    ran="a TREE-BUILT binary (not a release image) in $(docker inspect -f '{{.Config.Image}}' "$LIVE_PG" | cut -c1-24)"
  else
    ran="the production image $(docker inspect -f '{{.Image}}' "$DRILL_APP" | cut -c1-19) read from container $LIVE_APP"
  fi
  pass boot "$ran, on the drill network; /healthz and /readyz 200; schema_migrations unchanged"
}

app_session() {
  local out token
  out="$(app_run --rm -- mint-invite 2>&1)" || { fail serve "mint-invite failed"; return 1; }
  token="$(sed -n 's/^ *token: *//p' <<<"$out" | head -1)"
  [[ -n "$token" ]] || { fail serve "mint-invite printed no token"; return 1; }
  APP_TOKEN=""
  api POST /auth/session "{\"token\":\"$token\",\"device_label\":\"restore-drill\"}"
  [[ "$API_STATUS" == 200 ]] || { fail serve "POST /auth/session answered '$API_STATUS'"; return 1; }
  APP_TOKEN="$(jq -r .session_token "$API_BODY")"
  [[ -n "$APP_TOKEN" && "$APP_TOKEN" != null ]]
}

check_serve() {
  local id hash author want got bad=0 n=0
  # Every restored memo that has a complete transcript serves the stored text.
  while IFS=$'\t' read -r id want; do
    n=$((n + 1))
    api GET "/transcripts/$id"
    if [[ "$API_STATUS" != 200 ]]; then bad=$((bad + 1)); log "    transcript $id: HTTP $API_STATUS"; continue; fi
    got="$(jq -j .text "$API_BODY" | md5sum | cut -d' ' -f1)"
    [[ "$got" == "$want" ]] || { bad=$((bad + 1)); log "    transcript $id: text digest differs"; }
  done < <(sp -d chronicle -At -F $'\t' -c "SELECT memo_id, md5(text) FROM tier2.transcripts ORDER BY memo_id")
  [[ $bad -eq 0 ]] || { fail serve-text "$bad of $n transcript(s) did not serve their stored text"; return 1; }
  pass serve-text "$n transcript(s), complete and partial, each served with the text digest of the restored row"
}

check_audio() {
  local id author hash got
  read -r id author hash < <(
    sp -d chronicle -At -F ' ' -c "SELECT id, author_id, content_hash FROM tier2.memos WHERE audio_pruned_at IS NULL ORDER BY captured_at LIMIT 50" \
      | while read -r i a h; do [[ -f "$WORK/audio/$a/${h:0:2}/$h" ]] && { echo "$i $a $h"; break; }; done)
  if [[ -z "${id:-}" ]]; then fail serve-audio "no unpruned restored memo has its recording in the copy"; return 1; fi
  got="$(app_exec wget -q -O - --header "Authorization: Bearer $APP_TOKEN" "http://127.0.0.1:4009/audio/$id" | sha256sum | cut -d' ' -f1)"
  [[ "$got" == "$hash" ]] || { fail serve-audio "GET /audio/$id returned bytes whose SHA-256 is not the memo's content_hash"; return 1; }
  pass serve-audio "one recording served, SHA-256 equal to content_hash"
}

# The endpoint compares a restored database with a tree copied today, so its
# answer is PREDICTED from the endpoint's own definition (internal/audio/
# usage.go) and then every predicted entry is explained from live state.
check_storage() {
  local expect="$WORK/audio.expected" files="$WORK/audio.files" live="$WORK/audio.live"
  local missing orphans r
  sq "$SQL/audio.sql" chronicle >"$WORK/audio.restored" || { fail storage "cannot read the restored memos"; return 1; }
  lq "$SQL/audio.sql" >"$live" || { fail storage "cannot read the live memos"; return 1; }
  awk -F'\t' '$4 == "false" {print $2 "/" substr($3,1,2) "/" $3}' "$WORK/audio.restored" | sort >"$expect"
  ( cd "$WORK/audio" && find . -path ./.uploads -prune -o -type f -print | sed 's|^\./||' | sort ) >"$files"
  missing="$(comm -23 "$expect" "$files")"
  orphans="$(comm -13 "$expect" "$files")"
  api GET /admin/storage
  cp "$API_BODY" "$WORK/storage.json"
  [[ "$API_STATUS" == 200 ]] || { fail storage "GET /admin/storage answered '$API_STATUS'"; return 1; }
  r="$(jq -r '.reconciliation | "\(.missing) \(.orphans) \(.mismatched)"' "$WORK/storage.json")"
  local gm go gx
  read -r gm go gx <<<"$r"
  local pm po
  pm="$(grep -c . <<<"$missing")"; po="$(grep -c . <<<"$orphans")"
  [[ -n "$missing" ]] || pm=0
  [[ -n "$orphans" ]] || po=0
  if [[ "$gm" != "$pm" || "$go" != "$po" || "$gx" != 0 ]]; then
    fail storage "endpoint says missing=$gm orphans=$go mismatched=$gx; predicted missing=$pm orphans=$po mismatched=0"
    return 1
  fi
  # The samples must lie inside the predicted sets.
  # (The endpoint names a recording author/hash; the file is author/xx/hash.)
  local s mref oref
  mref="$(sed -E 's|^([^/]+)/[^/]+/|\1/|' <<<"$missing")"
  oref="$(sed -E 's|^([^/]+)/[^/]+/|\1/|' <<<"$orphans")"
  for s in $(jq -r '.reconciliation.missing_sample[]?' "$WORK/storage.json"); do
    grep -qxF "$s" <<<"$mref" || { fail storage "a missing sample ($s) is outside the predicted set"; return 1; }
  done
  for s in $(jq -r '.reconciliation.orphan_sample[]?' "$WORK/storage.json"); do
    grep -qxF "$s" <<<"$oref" || { fail storage "an orphan sample ($s) is outside the predicted set"; return 1; }
  done
  # Explain every predicted entry from live state; one with no reason fails.
  local line h unexplained=0
  while IFS= read -r line; do
    [[ -n "$line" ]] || continue
    h="${line##*/}"
    awk -F'\t' -v h="$h" '$3 == h && $4 == "true" {f=1} END{exit !f}' "$live" \
      || { unexplained=$((unexplained + 1)); log "    missing but not pruned live: $line"; }
  done <<<"$missing"
  while IFS= read -r line; do
    [[ -n "$line" ]] || continue
    h="${line##*/}"
    awk -F'\t' -v h="$h" '$3 == h {f=1} END{exit !f}' "$live" \
      || { unexplained=$((unexplained + 1)); log "    orphan no live memo claims: $line"; }
  done <<<"$orphans"
  [[ $unexplained -eq 0 ]] || { fail storage "$unexplained predicted entr(y|ies) have no explanation in live state"; return 1; }
  pass storage "endpoint counts equal the prediction (missing=$pm orphans=$po mismatched=0), every entry explained"
}

check_guards() {
  local ref dref memo hash s ok=1 author
  api POST /pages '{"path":"drill"}'
  [[ "$API_STATUS" == 201 || "$API_STATUS" == 409 ]] || { fail guards "creating a page answered '$API_STATUS'"; return 1; }
  api POST /notes '{"page":"drill","title":"restore drill","body":"first"}'
  ref="$(jq -r .ref "$API_BODY")"
  [[ "$API_STATUS" == 201 && -n "$ref" && "$ref" != null ]] || { fail guards "creating a note answered '$API_STATUS'"; return 1; }
  api POST "/notes/$ref/revisions" '{"body":"second"}'
  [[ "$API_STATUS" == 201 ]] || { fail guards "appending a revision answered '$API_STATUS'"; return 1; }
  api POST /discussions '{"title":"restore drill","body":"one"}'
  dref="$(jq -r .discussion.ref "$API_BODY")"
  [[ "$API_STATUS" == 201 && -n "$dref" && "$dref" != null ]] || { fail guards "opening a discussion answered '$API_STATUS'"; return 1; }
  api POST "/discussions/$dref/turns" '{"body":"two"}'
  [[ "$API_STATUS" == 201 ]] || { fail guards "appending a turn answered '$API_STATUS'"; return 1; }

  author="$(sp -d chronicle -At -c "SELECT id FROM tier2.users WHERE is_owner LIMIT 1")"
  expect_state() { # <code> <label> <sql>
    s="$(sqlstate chronicle chronicle "$3")"
    [[ "$s" == "$1" ]] || { log "    $2: expected $1, got $s"; ok=0; }
  }
  expect_state CH002 "moving captured_at" "UPDATE tier2.memos SET captured_at = captured_at + interval '1 day' WHERE id = (SELECT id FROM tier2.memos LIMIT 1)"
  expect_state CH005 "rewriting a complete transcript" "UPDATE tier2.transcripts SET text = text || 'x' WHERE id = (SELECT id FROM tier2.transcripts WHERE NOT partial LIMIT 1)"
  expect_state CH003 "inserting a memo as triaged" "INSERT INTO tier2.memos (author_id, content_hash, byte_size, state) VALUES ('$author', repeat('b', 64), 1, 'triaged')"
  expect_state CH040 "updating a note revision" "UPDATE tier2.note_revisions SET body = 'x' WHERE id = (SELECT id FROM tier2.note_revisions LIMIT 1)"
  expect_state CH090 "updating a turn" "UPDATE tier2.discussion_turns SET body = 'x' WHERE id = (SELECT id FROM tier2.discussion_turns LIMIT 1)"
  s="$(printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$(cat /proc/sys/kernel/random/uuid)" "$(sp -d chronicle -At -c 'SELECT id FROM tier2.discussions LIMIT 1')" 99 "$author" person x \
    | docker exec -i "$DRILL_PG" psql -U chronicle -d chronicle -X -q -v VERBOSITY=verbose \
        -c 'COPY tier2.discussion_turns (id, discussion_id, seq, author_id, author_kind, body) FROM STDIN' 2>&1 >/dev/null \
    | sed -nE 's/^ERROR: +([A-Z0-9]{5}):.*/\1/p' | head -1)"
  [[ "$s" == CH092 ]] || { log "    COPY carrying author_kind: expected CH092, got ${s:-OK}"; ok=0; }
  [[ $ok -eq 1 ]] || { fail guards "a guard did not refuse with its own SQLSTATE (above)"; return 1; }
  pass guards "CH002 CH005 CH003 CH040 CH090 CH092 each refused with its own SQLSTATE, on rows made through the API"
}

check_reference() {
  local f h total=0 bad=0
  if [[ ! -d "$REFERENCE_DIR" ]]; then fail reference "$REFERENCE_DIR does not exist"; return 1; fi
  for f in "$REFERENCE_DIR"/*; do
    [[ -e "$f" ]] || continue
    total=$((total + 1))
    h="$(basename "$f")"; h="${h%%.*}"
    [[ -n "$(sp -d chronicle -At -c "SELECT 1 FROM tier2.memos WHERE content_hash = '$h'")" ]] || bad=$((bad + 1))
  done
  [[ $bad -eq 0 ]] || { fail reference "$bad of $total reference file(s) name a content_hash that is not in the restored memos"; return 1; }
  pass reference "$total reference file(s) each join a restored memo (the files themselves are in no backup)"
}

cmd_serve_checks() {
  init_work
  guard_target
  docker inspect "$DRILL_APP" >/dev/null 2>&1 || die "the restored instance is not running (boot first)"
  local failed=0
  app_session || return 1
  check_serve     || failed=1
  check_audio     || failed=1
  check_storage   || failed=1
  check_guards    || failed=1
  check_reference || failed=1
  mark last-api-check
  return $failed
}

# --- teardown and the record -------------------------------------------------
# Disarming has a second proof besides "/data/chronicle was never mounted":
# every recording that left the live tree during the drill must belong to a
# memo production itself now marks pruned. Production's own pruner keeps
# running throughout and is allowed to do that on purpose.
check_live_tree() {
  [[ -s "$WORK/audio.before" && -n "$LIVE_AUDIO_DIR" && -d "$LIVE_AUDIO_DIR" ]] || return 0
  local p sz h gone=0 bad=0 changed=0
  local -A after=()
  find "$LIVE_AUDIO_DIR" -path "$LIVE_AUDIO_DIR/.uploads" -prune -o -type f -printf '%P\t%s\n' | sort >"$WORK/audio.after"
  while IFS=$'\t' read -r p sz; do after["$p"]="$sz"; done <"$WORK/audio.after"
  while IFS=$'\t' read -r p sz; do
    if [[ -z "${after[$p]+x}" ]]; then
      gone=$((gone + 1))
      h="${p##*/}"
      awk -F'\t' -v h="$h" '$3 == h && $4 == "true" {x=1} END{exit !x}' "$WORK/audio.live" 2>/dev/null || bad=$((bad + 1))
    elif [[ "${after[$p]}" != "$sz" ]]; then
      changed=$((changed + 1))
    fi
  done <"$WORK/audio.before"
  if [[ $bad -gt 0 || $changed -gt 0 ]]; then
    fail live-audio-tree "$bad recording(s) left the live tree and live does not say they were pruned; $changed changed size"
    return 1
  fi
  pass live-audio-tree "$(wc -l <"$WORK/audio.before") files before, $(wc -l <"$WORK/audio.after") after; $gone left, all pruned by production; none changed size"
}

cmd_teardown() {
  guard_names
  docker rm -fv "$DRILL_APP" "$DRILL_PG" >/dev/null 2>&1
  docker network rm "$DRILL_NET" >/dev/null 2>&1
  rm -rf "$WORK"
  log "torn down: containers, network and $WORK removed"
}

phase() { # <from> <to>
  awk -v a="$1" -v b="$2" '$1==a{x=$2} $1==b{y=$2} END{ if (x != "" && y != "") printf "%.1f s", y-x; else printf "n/a" }' "$WORK/marks"
}

record() {
  local n r
  echo
  echo "== restore drill record =="
  echo "source:        ${DRILL_SOURCE:-?}   snapshot: $(cat "$WORK"/snapshot.* 2>/dev/null | head -1)"
  echo "dump time:     $(cat "$WORK/dump_time" 2>/dev/null || echo n/a)"
  echo "corpus:        $(ls -l "$WORK/chronicle.dump" 2>/dev/null | awk '{print $5}') byte chronicle.dump; $(sp -d chronicle -At -c 'SELECT count(*) FROM tier2.memos' 2>/dev/null) memos, $(sp -d chronicle -At -c 'SELECT count(*) FROM tier2.transcripts' 2>/dev/null) transcripts"
  echo "extraction:    $(phase extract-start files-on-disk)"
  echo "scratch ready: $(phase files-on-disk scratch-ready)"
  echo "restore:       $(phase scratch-ready restore-complete)"
  echo "manifests:     $(phase restore-complete manifests-pass)"
  echo "boot to ready: $(phase manifests-pass readyz-200)"
  echo "api checks:    $(phase readyz-200 last-api-check)"
  echo "TOTAL extraction start -> /readyz 200: $(phase extract-start readyz-200)"
  if [[ -s "$WORK/loss.txt" ]]; then
    echo "measured loss (live-only rows after the dump; table, rows, oldest created_at):"
    awk -F'\t' '{printf "  %-24s %-6s %s\n", $3, $4, $5}' "$WORK/loss.txt"
  else
    echo "measured loss: no live-only rows after the dump"
  fi
  if [[ -s "$WORK/content.compare" ]] && grep -q '^LIST' "$WORK/content.compare"; then
    n="$(grep -c '^LIST' "$WORK/content.compare")"
    echo "listed for a person, by key ($n):"
    grep '^LIST' "$WORK/content.compare" | head -100 | awk -F'\t' '{printf "  %-44s %-24s %s\n", $2, $3, $4}'
    [[ "$n" -le 100 ]] || echo "  ... and $((n - 100)) more"
  else
    echo "listed for a person: none"
  fi
  if [[ -s "$WORK/shape.restored.chronicle" ]]; then
    n="$(grep -c '^trg|' "$WORK/shape.restored.chronicle")"
    r="$(grep -c '^trg|.*|enabled=O|' "$WORK/shape.restored.chronicle")"
    echo "triggers:      $n in the restored chronicle database; enabled: $r$([[ "$n" == "$r" ]] && echo ' (all enabled)' || echo ' (NOT ALL ENABLED)')"
  fi
  if [[ -s "$WORK/hub.ids.before" ]]; then
    echo "hub snapshots: $(wc -l <"$WORK/hub.ids.before") before extraction, $(wc -l <"$WORK/hub.ids.after" 2>/dev/null || echo ?) after; $(cmp -s "$WORK/hub.ids.before" "$WORK/hub.ids.after" && echo 'identical lists' || echo 'LISTS DIFFER')"
    echo "               $(cut -c1-8 "$WORK/hub.ids.before" | tr '\n' ' ')"
  fi
  if docker inspect "$DRILL_PG" >/dev/null 2>&1; then
    echo "scratch server:"
    for n in $(docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$DRILL_PG"); do
      echo "  network $n internal=$(docker network inspect -f '{{.Internal}}' "$n")"
    done
    echo "  published ports: $(docker inspect -f '{{len .HostConfig.PortBindings}}' "$DRILL_PG"); data on tmpfs: $(docker inspect -f '{{if .HostConfig.Tmpfs}}yes{{else}}NO{{end}}' "$DRILL_PG")"
  fi
  if docker inspect "$DRILL_APP" >/dev/null 2>&1; then
    echo "restored instance:"
    echo "  image: $(docker inspect -f '{{.Config.Image}}' "$DRILL_APP" | cut -c1-30)$([[ -n "${DRILL_CHRONICLE_BIN:-}" ]] && echo ' (tree-built binary mounted)')"
    echo "  mounts:"
    docker inspect -f '{{range .Mounts}}    {{.Source}} -> {{.Destination}} {{if .RW}}rw{{else}}ro{{end}}{{"\n"}}{{end}}' "$DRILL_APP"
    echo "  CHRONICLE_* variables present (names only): $(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$DRILL_APP" | sed 's/=.*//' | grep -c '^CHRONICLE_') : $(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$DRILL_APP" | sed 's/=.*//' | grep '^CHRONICLE_' | sort | tr '\n' ' ')"
    echo "  outward variables present: $(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$DRILL_APP" | grep -cE '^CHRONICLE_(INBOX_DIR|ASR_URL|ASR_TOKEN|SCRIBE_OLLAMA_URL|SWITCHYARD_URL|AMBER_URL|CF_ACCESS_TEAM_DOMAIN)=') (must be 0)"
  fi
  echo "not in this figure: rebuilding the host, recovering secrets (SERV-211), the other databases (SERV-217), DNS and the edge"
  echo "NOT BACKED UP (the volume gap): nothing under /data/chronicle is in any backup; the eval reference files are not covered by this drill"
  echo "checks:"
  sed 's/^/  ok    /' "$RESULTS"
  [[ ! -s "$WORK/notrun" ]] || sed 's/^/  NOT RUN  /' "$WORK/notrun"
  [[ ! -s "$FAILS" ]] || sed 's/^/  FAIL  /' "$FAILS"
}

cmd_run() {
  local stage="${1:-}"
  init_work
  DRILL_SOURCE="$stage"
  [[ "$stage" == hub || "$stage" == desk || "$stage" == dir ]] || die "run hub | desk | dir <path>"
  trap '[[ "${DRILL_KEEP:-}" == 1 ]] || cmd_teardown' EXIT
  cmd_extract "$@" || { log "extraction failed; nothing was concluded"; return 2; }
  cmd_scratch_up || return 2
  cmd_restore || { record; return 1; }
  cmd_check || true
  cmd_boot || true
  if docker inspect "$DRILL_APP" >/dev/null 2>&1; then cmd_serve_checks || true; fi
  check_live_tree || true
  record
  if [[ -s "$FAILS" ]]; then
    log "restore drill FAILED: $(cut -f1 "$FAILS" | sort -u | tr '\n' ' ')"
    return 1
  fi
  log "restore drill passed"
}

# --- selftest ----------------------------------------------------------------
# Throwaway containers only: its own Postgres, its own network, a binary built
# from this tree. It never connects to chronicle, chronicle_test or
# postgres-dev. Clean run first, then the restore is damaged six ways, one at
# a time, and each damage must turn the check that should catch it red.
ST_PREFIX="chronicle-drill-selftest"
st_cleanup() {
  [[ "${DRILL_SELFTEST_KEEP:-}" == 1 ]] && return 0
  docker rm -fv "${ST_PREFIX}-live" "${ST_PREFIX}-pg" "${ST_PREFIX}-app" "${ST_PREFIX}-forbidden-c" >/dev/null 2>&1
  docker network rm "${ST_PREFIX}-net" "${ST_PREFIX}-forbidden" >/dev/null 2>&1
  [[ -z "${ST_DIR:-}" ]] || rm -rf "$ST_DIR"
  rm -rf "$WORK"
}

st_expect_red() { # <label> <check-name-it-must-fail-on>
  if grep -q "^$2	" "$FAILS"; then
    log "  RED as required: $1 -> $2 failed: $(grep -m1 "^$2	" "$FAILS" | cut -f2 | cut -c1-170)"
    return 0
  fi
  log "  NOT RED: $1 did not fail the $2 check"
  ST_BAD=$((ST_BAD + 1))
}

st_expect_refusal() { # <label> <command...>
  local label="$1"; shift
  local out rc
  out="$("$@" 2>&1)"; rc=$?
  if [[ $rc -eq 2 && "$out" == *REFUSED* ]]; then
    log "  REFUSED as required: $label"
  else
    log "  NOT REFUSED: $label (exit $rc)"
    ST_BAD=$((ST_BAD + 1))
  fi
}

# st_stub_refusal <label> <env assignments...> -- <script args...>: run the
# script as a child with a `docker` that only logs its arguments, so what a
# refusal does on its way out can be read without any risk to a real container.
# The child must exit 2 with REFUSED, and the log must hold no `rm` and no
# `network rm`: a refusal that then tears down what it refused is the failure
# this is here to catch (the EXIT trap of `run`, and teardown/boot called
# directly). Goes through the real main, so the real trap is installed.
ST_STUB_LOG=""
st_stub_refusal() {
  local label="$1"; shift
  local -a envs=()
  while [[ "$1" != "--" ]]; do envs+=("$1"); shift; done
  shift
  local out rc
  : >"$ST_STUB_LOG"
  out="$(env "${envs[@]}" PATH="$ST_DIR/stub:$PATH" STUB_LOG="$ST_STUB_LOG" DRILL_WORK="$ST_DIR/stubwork" \
    bash "${BASH_SOURCE[0]}" "$@" 2>&1)"; rc=$?
  if [[ $rc -eq 2 && "$out" == *REFUSED* ]] && ! grep -qE '(^| )(rm|network rm) ' "$ST_STUB_LOG"; then
    log "  REFUSED, nothing removed: $label ($(wc -l <"$ST_STUB_LOG") docker call(s) logged, none a removal)"
  else
    log "  NOT SAFE: $label (exit $rc; removals logged: $(grep -cE '(^| )(rm|network rm) ' "$ST_STUB_LOG"))"
    ST_BAD=$((ST_BAD + 1))
  fi
}

st_fresh_restore() { # restore into a clean scratch server from the extracted files
  : >"$FAILS"; : >"$RESULTS"
  cmd_scratch_up >/dev/null || return 1
  cmd_restore
}

cmd_selftest() {
  need docker; need go; need jq; need openssl
  ST_BAD=0
  DRILL_PG="${ST_PREFIX}-pg"; DRILL_NET="${ST_PREFIX}-net"; DRILL_APP="${ST_PREFIX}-app"
  LIVE_PG="${ST_PREFIX}-live"; LIVE_APP=""; LIVE_DB=chronicle
  FORBIDDEN_NET="${ST_PREFIX}-forbidden"
  WORK="${DRILL_WORK:-${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/${ST_PREFIX}}"
  FAILS="$WORK/fails"; RESULTS="$WORK/results"
  ST_DIR="$(mktemp -d "${XDG_RUNTIME_DIR:-/tmp}/${ST_PREFIX}-src.XXXXXX")"
  LIVE_AUDIO_DIR="$ST_DIR/audio"
  REFERENCE_DIR="$ST_DIR/reference"
  guard_names
  trap st_cleanup EXIT
  st_cleanup; mkdir -p "$ST_DIR"; init_work

  local image bin="$ST_DIR/chronicle"
  image="$(docker image inspect -f '{{.Id}}' postgres:16.15-alpine 2>/dev/null)" || die "postgres:16.15-alpine is not pulled"
  log "== building the binary from this tree"
  ( cd "$ROOT" && CGO_ENABLED=0 go build -o "$bin" ./cmd/chronicle ) || die "go build failed"
  export DRILL_CHRONICLE_BIN="$bin"

  log "== a throwaway 'live' server, provisioned the way deploy/provision-db.sh does it"
  docker network create --internal "${ST_PREFIX}-net" >/dev/null
  docker run -d --name "$LIVE_PG" --network "${ST_PREFIX}-net" --tmpfs /var/lib/postgresql/data \
    -e POSTGRES_PASSWORD="$(rand)" "$image" >/dev/null
  local i
  for i in $(seq 1 60); do docker exec "$LIVE_PG" pg_isready -q -h 127.0.0.1 -U postgres && break; sleep 1; done
  docker exec "$LIVE_PG" psql -U postgres -X -q -c "CREATE ROLE asr LOGIN PASSWORD 'x'" -c 'CREATE DATABASE asr OWNER asr' || die "live asr"
  local pw1 pw2; pw1="$(rand)"; pw2="$(rand)"
  PG_CONTAINER="$LIVE_PG" CHRONICLE_DB_PASSWORD="$pw1" CHRONICLE_TIER1_DB_PASSWORD="$pw2" \
    "$ROOT/deploy/provision-db.sh" >/dev/null || die "provision-db.sh failed against the throwaway server"
  docker run --rm --network "${ST_PREFIX}-net" -e "CHRONICLE_DATABASE_URL=postgres://chronicle:$pw1@$LIVE_PG:5432/chronicle?sslmode=disable" \
    -v "$bin:/chronicle:ro" --entrypoint /chronicle "$image" migrate >/dev/null || die "migrate failed"
  st_seed

  log "== a backup of it: pg_dump -Fc -Z0 and pg_dumpall --globals-only, as SERV-43 takes them"
  docker exec "$LIVE_PG" pg_dump -U postgres -Fc -Z0 chronicle >"$ST_DIR/chronicle.dump"
  docker exec "$LIVE_PG" pg_dump -U postgres -Fc -Z0 asr >"$ST_DIR/asr.dump"
  docker exec "$LIVE_PG" pg_dumpall -U postgres --globals-only >"$ST_DIR/globals.sql"
  # Rows written AFTER the dump: they are the measured loss, and the clean run
  # must report them without failing.
  st_seed_after_dump
  sleep 2

  log "== the SQL files are SELECT-only"
  local f
  for f in shape migrations content audio; do
    ( assert_select_only "$SQL/$f.sql" ) || { log "  NOT SELECT-ONLY: $f.sql"; ST_BAD=$((ST_BAD + 1)); }
  done
  printf 'DELETE FROM tier2.memos;\n' >"$ST_DIR/bad.sql"
  st_expect_refusal "a query file that deletes is never sent to production" bash -c "$(declare -f die assert_select_only); assert_select_only '$ST_DIR/bad.sql'"

  log "== refusals: the script cannot be pointed at anything real"
  local save_pg="$DRILL_PG"
  DRILL_PG=postgres;     st_expect_refusal "restore target is the 'postgres' container" guard_target
  DRILL_PG=postgres-dev; st_expect_refusal "restore target is the 'postgres-dev' container" guard_target
  DRILL_PG="$save_pg"
  docker network create --internal "${ST_PREFIX}-forbidden" >/dev/null
  docker run -d --name "${ST_PREFIX}-forbidden-c" --network "${ST_PREFIX}-forbidden" --tmpfs /var/lib/postgresql/data \
    -e POSTGRES_PASSWORD="$(rand)" "$image" >/dev/null
  DRILL_PG="${ST_PREFIX}-forbidden-c"
  st_expect_refusal "restore target is attached to the forbidden network" guard_target
  DRILL_PG="$save_pg"
  # A server that already holds a database named chronicle.
  cmd_extract dir "$ST_DIR" >/dev/null || die "extract failed"
  cmd_scratch_up >/dev/null
  sp -d postgres -c 'CREATE DATABASE chronicle' >/dev/null
  st_expect_refusal "restore target already holds a database named chronicle" cmd_restore
  # The restored instance: the real data directory, and each forbidden variable.
  sp -d postgres -c 'DROP DATABASE chronicle' >/dev/null
  printf 'CHRONICLE_OWNER_EMAIL=x@example.invalid\n' >"$WORK/app.env"
  DRILL_SELFTEST_EXTRA_MOUNT="/data/chronicle:/audio2:ro" \
    st_expect_refusal "the restored instance with /data/chronicle mounted" app_run --rm -- version
  local v
  for v in CHRONICLE_INBOX_DIR=/x CHRONICLE_ASR_URL=http://x CHRONICLE_SCRIBE_OLLAMA_URL=http://x CHRONICLE_SWITCHYARD_URL=http://x CHRONICLE_AMBER_URL=http://x; do
    DRILL_SELFTEST_EXTRA_ENV="$v" st_expect_refusal "the restored instance with ${v%%=*} set" app_run --rm -- version
  done

  log "== refusals through the whole script: the EXIT trap and teardown must not remove what was refused"
  mkdir -p "$ST_DIR/stub" "$ST_DIR/stubwork"
  ST_STUB_LOG="$ST_DIR/stub/docker.log"
  printf '#!/usr/bin/env bash\nprintf "%%s\\n" "$*" >>"$STUB_LOG"\nexit 0\n' >"$ST_DIR/stub/docker"
  chmod +x "$ST_DIR/stub/docker"
  # Positive control, so the assertions below are not vacuous: with
  # chronicle-drill* names a teardown DOES remove, and the stub sees it.
  : >"$ST_STUB_LOG"
  env PATH="$ST_DIR/stub:$PATH" STUB_LOG="$ST_STUB_LOG" DRILL_WORK="$ST_DIR/stubwork" \
    DRILL_PG=chronicle-drill-ctl-pg DRILL_APP=chronicle-drill-ctl-app DRILL_NET=chronicle-drill-ctl-net \
    bash "${BASH_SOURCE[0]}" teardown >/dev/null 2>&1
  if grep -q 'rm -fv chronicle-drill-ctl-app chronicle-drill-ctl-pg' "$ST_STUB_LOG" && grep -q 'network rm chronicle-drill-ctl-net' "$ST_STUB_LOG"; then
    log "  control: a teardown of chronicle-drill* names does remove them, and the stub sees it"
  else
    log "  CONTROL FAILED: the stub did not see a legitimate teardown"; ST_BAD=$((ST_BAD + 1))
  fi
  st_stub_refusal "run with DRILL_PG=postgres"        DRILL_PG=postgres      -- run dir "$ST_DIR"
  st_stub_refusal "run with DRILL_PG=postgres-dev"    DRILL_PG=postgres-dev  -- run dir "$ST_DIR"
  st_stub_refusal "run with DRILL_APP=chronicle"      DRILL_APP=chronicle    -- run dir "$ST_DIR"
  st_stub_refusal "run with DRILL_NET=construct_net"  DRILL_NET=construct_net -- run dir "$ST_DIR"
  st_stub_refusal "boot with DRILL_APP=chronicle"     DRILL_APP=chronicle    -- boot
  st_stub_refusal "teardown with DRILL_APP=chronicle" DRILL_APP=chronicle    -- teardown
  st_stub_refusal "teardown with DRILL_PG=postgres"   DRILL_PG=postgres      -- teardown
  st_stub_refusal "teardown with DRILL_NET=construct_net" DRILL_NET=construct_net -- teardown
  st_stub_refusal "scratch-up with DRILL_PG=postgres" DRILL_PG=postgres      -- scratch-up
  # And each destructive call guards itself, without main in front of it:
  # called directly, in-process, with the stub first on PATH.
  local d
  for d in "DRILL_APP=chronicle cmd_teardown" "DRILL_PG=postgres cmd_teardown" "DRILL_NET=construct_net cmd_teardown" \
           "DRILL_APP=chronicle cmd_boot" "DRILL_PG=postgres cmd_scratch_up"; do
    : >"$ST_STUB_LOG"
    # shellcheck disable=SC2086
    if ( eval "export PATH=\"$ST_DIR/stub:\$PATH\" STUB_LOG=\"$ST_STUB_LOG\"; ${d%% *}; ${d#* }" ) >/dev/null 2>&1; then
      log "  NOT REFUSED in-process: ${d#* } with ${d%% *}"; ST_BAD=$((ST_BAD + 1))
    elif grep -qE '(^| )(rm|network rm) ' "$ST_STUB_LOG"; then
      log "  NOT SAFE in-process: ${d#* } with ${d%% *} removed something"; ST_BAD=$((ST_BAD + 1))
    else
      log "  REFUSED, nothing removed, called directly: ${d#* } with ${d%% *}"
    fi
  done

  log "== clean run: every check must pass"
  st_fresh_restore || { log "  clean restore failed"; ST_BAD=$((ST_BAD + 1)); }
  cmd_check || { log "  CLEAN RUN FAILED a check"; ST_BAD=$((ST_BAD + 1)); }
  cmd_boot || { log "  CLEAN BOOT FAILED"; ST_BAD=$((ST_BAD + 1)); }
  cmd_serve_checks || { log "  CLEAN SERVE CHECKS FAILED"; ST_BAD=$((ST_BAD + 1)); }
  [[ -s "$WORK/loss.txt" ]] && { log "  measured loss reported: $(awk -F'\t' '{printf "%s=%s ", $3, $4}' "$WORK/loss.txt")"; } \
    || { log "  NO LOSS REPORTED for the rows written after the dump"; ST_BAD=$((ST_BAD + 1)); }
  [[ -s "$FAILS" ]] && { log "  clean run recorded failures:"; sed 's/^/    /' "$FAILS" >&2; }
  check_live_tree || { log "  CLEAN live-tree check failed"; ST_BAD=$((ST_BAD + 1)); }
  log "== the record this run would leave behind (printed before teardown)"
  DRILL_SOURCE=dir; record 2>&1 | sed 's/^/    /' >&2
  local one; one="$(find "$LIVE_AUDIO_DIR" -type f | head -1)"
  printf x >>"$one"
  : >"$FAILS"; check_live_tree >/dev/null 2>&1
  st_expect_red "a live recording changing size during the drill" live-audio-tree
  docker rm -fv "$DRILL_APP" >/dev/null 2>&1

  log "== damage 1 of 6: drop one guard trigger"
  st_fresh_restore && sp -d chronicle -c 'DROP TRIGGER memos_guard ON tier2.memos' && cmd_check shape content tier1 >/dev/null 2>&1
  st_expect_red "a dropped trigger" shape

  log "== damage 2 of 6: disable one guard trigger"
  st_fresh_restore && sp -d chronicle -c 'ALTER TABLE tier2.transcripts DISABLE TRIGGER transcripts_guard' && cmd_check shape content tier1 >/dev/null 2>&1
  st_expect_red "a disabled trigger" shape

  log "== damage 3 of 6: grant chronicle_tier1 SELECT on a tier-2 table"
  st_fresh_restore && sp -d chronicle -c 'GRANT SELECT ON tier2.notes TO chronicle_tier1' && cmd_check shape content tier1 >/dev/null 2>&1
  st_expect_red "an extra grant" shape
  st_expect_red "an extra grant" tier1-audit

  log "== damage 4 of 6: restore without --create"
  : >"$FAILS"; : >"$RESULTS"
  cmd_scratch_up >/dev/null
  DRILL_SELFTEST_NOCREATE=1 cmd_restore >/dev/null 2>&1
  cmd_check shape content tier1 >/dev/null 2>&1
  st_expect_red "a restore without --create" shape

  log "== damage 5 of 6: change one complete transcript's text"
  st_fresh_restore && sp -d chronicle -c "SET session_replication_role = replica; UPDATE tier2.transcripts SET text = text || ' (altered)' WHERE id = (SELECT id FROM tier2.transcripts WHERE NOT partial LIMIT 1)" && cmd_check shape content tier1 >/dev/null 2>&1
  st_expect_red "a changed transcript" content

  log "== damage 6 of 6: delete one row"
  st_fresh_restore && sp -d chronicle -c "SET session_replication_role = replica; DELETE FROM tier2.memo_arrivals WHERE id = (SELECT id FROM tier2.memo_arrivals LIMIT 1)" && cmd_check shape content tier1 >/dev/null 2>&1
  st_expect_red "a deleted row" content

  log
  if [[ $ST_BAD -eq 0 ]]; then
    log "selftest PASSED: the clean run is green, every damage is red, every refusal refuses"
    return 0
  fi
  log "selftest FAILED: $ST_BAD expectation(s) not met"
  return 1
}

# st_seed: the corpus the clean run restores. Memos and transcripts the way
# production holds them (no notes, no discussions yet: the drill creates those
# through the API itself), plus recordings on disk and reference files.
st_seed() {
  local owner h i file
  mkdir -p "$LIVE_AUDIO_DIR" "$REFERENCE_DIR"
  local -a hashes=()
  # The migrator seeds no owner on its own in every version; take it if there
  # is one, make it if not.
  owner="$(docker exec "$LIVE_PG" psql -U postgres -d chronicle -X -q -At -c "SELECT id FROM tier2.users WHERE is_owner")"
  if [[ -z "$owner" ]]; then
    owner="$(docker exec "$LIVE_PG" psql -U postgres -d chronicle -X -q -At -c \
      "INSERT INTO tier2.users (email, display_name, is_owner) VALUES ('owner@example.invalid', 'Owner', true) RETURNING id" | head -1)"
  fi
  for i in 1 2 3 4; do
    file="$ST_DIR/rec$i"; head -c $((2000 + i)) /dev/urandom >"$file"
    h="$(sha256sum <"$file" | cut -d' ' -f1)"
    hashes+=("$h")
    docker exec "$LIVE_PG" psql -U postgres -d chronicle -X -q -c \
      "INSERT INTO tier2.memos (author_id, content_hash, byte_size, created_at) VALUES ('$owner', '$h', $((2000 + i)), now() - interval '1 hour')"
    mkdir -p "$LIVE_AUDIO_DIR/$owner/${h:0:2}"
    mv "$file" "$LIVE_AUDIO_DIR/$owner/${h:0:2}/$h"
    [[ $i -le 2 ]] && printf 'reference text %s\n' "$i" >"$REFERENCE_DIR/$h.google-recorder.txt"
  done
  docker exec "$LIVE_PG" psql -U postgres -d chronicle -X -q -c "
    INSERT INTO tier2.memo_arrivals (memo_id, author_id, source, source_ref, arrived_at)
      SELECT id, author_id, 'upload', 'seed-' || id, now() - interval '1 hour' FROM tier2.memos;
    INSERT INTO tier2.transcripts (memo_id, text, partial, model, backend)
      SELECT id, 'transcript of ' || id, false, 'whisper.cpp/small.en', 'asrd' FROM tier2.memos ORDER BY captured_at, id LIMIT 3;
    INSERT INTO tier2.transcripts (memo_id, text, partial, model, backend)
      SELECT id, 'partial of ' || id, true, 'whisper.cpp/small.en', 'asrd' FROM tier2.memos ORDER BY captured_at DESC, id DESC LIMIT 1;" \
    || die "seed failed"
}

st_seed_after_dump() {
  local owner h
  owner="$(docker exec "$LIVE_PG" psql -U postgres -d chronicle -X -q -At -c "SELECT id FROM tier2.users WHERE is_owner")"
  h="$(openssl rand -hex 32)"
  docker exec "$LIVE_PG" psql -U postgres -d chronicle -X -q -c "
    INSERT INTO tier2.memos (author_id, content_hash, byte_size) VALUES ('$owner', '$h', 9);
    INSERT INTO tier2.memo_arrivals (memo_id, author_id, source, source_ref) SELECT id, author_id, 'upload', 'late-' || id FROM tier2.memos WHERE content_hash = '$h';
    "
  # Production's own pruner takes one recording after the dump: the memo is
  # marked pruned live and its file leaves the live tree. The restored copy
  # still expects the file, so the storage prediction must list it as missing
  # and the explanation must find it pruned in live state.
  local victim vh va
  read -r vh va < <(docker exec "$LIVE_PG" psql -U postgres -d chronicle -X -q -At -F ' ' -c \
    "SELECT content_hash, author_id FROM tier2.memos ORDER BY captured_at, id LIMIT 1")
  docker exec "$LIVE_PG" psql -U postgres -d chronicle -X -q -c \
    "UPDATE tier2.memos SET audio_pruned_at = now() WHERE content_hash = '$vh'"
  victim="$LIVE_AUDIO_DIR/$va/${vh:0:2}/$vh"
  rm -f "$victim"
}

# --- dispatch ----------------------------------------------------------------
main() {
  guard_names
  need docker
  case "${1:-}" in
    run)           shift; cmd_run "$@" ;;
    extract)       shift; cmd_extract "$@" ;;
    scratch-up)    cmd_scratch_up ;;
    restore)       cmd_restore ;;
    check)         shift; cmd_check "$@" ;;
    boot)          cmd_boot ;;
    serve-checks)  cmd_serve_checks ;;
    teardown)      cmd_teardown ;;
    selftest)      cmd_selftest ;;
    *)
      sed -n '2,24p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
      exit 2
      ;;
  esac
}
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then main "$@"; fi
