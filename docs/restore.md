# Restoring Chronicle, and the record of the drill

Chronicle builds no backup machinery of its own. Its `chronicle` database
(and the `asr` database beside it) rides construct-server's nightly pipeline
(`construct-server/docs/backups.md`, SERV-43): a `pg_dump -Fc` per database plus
`globals.sql`, into a restic hub on the box and, from there, a copy at `desk`.
What is Chronicle's is the answer to *does the application come back, not only
the bytes*, which is `scripts/restore-drill.sh` and this record. The decision
is CHRN-68's approved plan (rev 2); the drill is CHRN-132 (the script) and
CHRN-133 (the runs).

## The rulings, as picked

| | Ruling | Picked |
|---|---|---|
| ⚖ 0 | Accepted loss window | **Nightly, as built.** Revisit when notes and discussion turns are being written on most days: that is when a lost day is a day of text nobody can retype. |
| ⚖ 1 | Which copy the drill restores from | **Two stages: the hub now, `desk` when it is healthy.** The ticket closes after stage 2. |
| ⚖ 2 | Where CHRN-68 ends | **On the Postgres drill.** The volume tier is a follow-on blocked by SERV-166. |
| ⚖ 3 | Which recordings go off-box | **Pinned (`forever`) recordings only**, at the text tier beside the eval reference. |

## Running it

On `imperial-construct`, never in an isolated runner (it needs the host's Docker
daemon), and never between 02:25 and 03:20 CDT or while `construct-backup.service`
is active.

```
scripts/restore-drill.sh selftest      # shows every check red before any run is believed
scripts/restore-drill.sh run hub       # stage 1: from the local hub
scripts/restore-drill.sh run desk      # stage 2: from the desktop copy; needs a person at the password prompt
```

The self-test starts its own throwaway containers and never touches `chronicle`,
`chronicle_test` or `postgres-dev`. Nothing runs the script in CI; run the
self-test by hand before each drill, and add a migration's new guard to the
guards check when it lands.

## Stage 1 — from the hub: PASSED, 2026-10-25

Run at 15:20 CDT, `construct-backup.service` inactive, on `main` plus the audio
copy fix of #176. The first stage-1 attempt the same day failed its `storage`
check because the script copied the root-owned audio tree as an unprivileged user
and did not treat the skipped files as fatal; that was a defect in the script, not
in the restore, and #176 fixes it. The numbers below are the passing run.

Source: hub snapshot `609748f5`, made by the 02:30 timer on 2026-10-25 (exit 0).
Dump time (archive header): `2026-10-25T07:30:55Z`. Corpus: a 441,251-byte
`chronicle.dump`; 48 memos, 48 transcripts; 37 recordings, 15,883,869 bytes.

| Phase | Elapsed |
|---|---|
| Extraction (hub, read-only) | 2.9 s |
| Scratch server accepting connections | 3.7 s |
| Restore (`pg_restore --create`, both databases) | 0.8 s |
| Manifests | 1.8 s |
| Boot to `/readyz` 200 | 0.7 s |
| API checks | 6.5 s |
| **Extraction start to `/readyz` 200** | **9.8 s** |

Every check passed: restore (no warnings); shape (448 `chronicle` lines and 51
`asr` lines identical, live and restored); content (208 restored rows, each equal
to live or legitimately changed; none listed for a person); tier-1 audit (clean; a
deliberate widening is exactly one finding; `chronicle_tier1` reads `memos` and is
refused with `42501` on `notes` and on insert); audio copy (37 files, equal to the
inventory); boot of the production image digest, disarmed, `schema_migrations`
unchanged; 48/48 transcripts served with the stored text digest; one recording
served with SHA-256 equal to its `content_hash`; storage (endpoint counts equal the
prediction, `missing=0 orphans=0 mismatched=0`); the guards `CH002 CH005 CH003
CH040 CH090 CH092` each refused with their own SQLSTATE on rows made through the
scratch API; the 20 eval reference files each join a restored memo; and the live
audio tree unchanged. All 12 guard triggers were present and enabled.

A rerun on the final script (after review fixes to the copy helper) passed the same twelve checks in 17.8 s; the spread is the figure's noise at this size. The hub's snapshot list was identical before and after (32 and 32). The restored
instance had no outward variable set and `/data/chronicle` was not mounted into
it; it was torn down within the 30-minute bound.

What this figure excludes: rebuilding the host, recovering secrets (SERV-211),
the other databases (SERV-217), DNS and the edge. It is a figure for 48 memos and
says the path works and is fast when small; it is not a recovery-time promise.

## Stage 2 — from `desk`: NOT RUN

It waits on, in order: the desktop receiver serving snapshot listings (an
authenticated `GET /construct/snapshots/` hung from 2026-10-02), one clean
timer-fired run whose copy to `desk` is confirmed by readback, and SERV-216 (the
desktop repository's password), ideally in SERV-217's session. A person types that
password; the script reads it once with `read -rs`.

## The loss window, three ways

1. **By design.** `construct-backup.timer` is `OnCalendar=*-*-* 02:30:00` with
   `RandomizedDelaySec=5min`: one run a day. A hub restore loses at most about 24
   hours 5 minutes; a night on which the hub half itself fails doubles that.
2. **Off-box, as observed.** At stage 1 the newest copy at `desk` is the
   2026-10-25 02:31 CDT run (`state.json`: `last_success=2026-10-25T07:31:20Z`,
   `consecutive_misses=0`; the journal logs `snapshot a3dca7f2 saved` at `desk`),
   about 13 hours old at drill time. The 2026-10-24 run also logged a save at
   `desk`. This is `state.json` and the journal, not a listing read from `desk`;
   stage 2 reads the destination itself. Between 2026-10-17 and 2026-10-23 the
   service failed every night at preflight (`argosy-db-1: pg_isready failing`,
   seven failures recorded), so the schedule is not a guarantee, and nothing
   alarms on it until SERV-224 and SERV-218 exist.
3. **Measured.** Live-only rows after the dump: none. The drill ran about 13 hours after
   the dump; the figure grows with the snapshot's age.

## The volume gap

As of the drill, **no file under `/data/chronicle` is in any backup**.

| Loss | Database | Volume files | What Chronicle keeps |
|---|---|---|---|
| `/` only (Postgres volume) | restore from the hub, at most one night old | intact | everything but the window's rows; recordings newer than the snapshot sit on disk as orphans |
| `/data` only | intact | **all gone**, and so is the hub | every transcript and note; no recording, no eval reference |
| the box | restore from `desk`, as old as the last copy that landed | all gone | transcripts and notes as of that copy |

Until SERV-166 provides a volume source, the second and third rows lose every
recording and the eval control. Chronicle's requirements are on SERV-166:
`/data/chronicle/eval/reference/` at the text tier by name, pinned `forever`
recordings (a list Chronicle's database produces, because a recording's path
encodes neither retention nor state), and nothing else off-box. The drill proves
the database half of the reference control only: all 20 reference files name a
`content_hash` present in the restored `tier2.memos`; the files themselves are in
no backup.

## Runbook: five rules that are particular to Chronicle

1. **Restore whole, with `--create`, after the globals.** Never replay rows: the
   guard triggers (`CH092`, `CH041`) refuse it, and a restore without `--create`
   loses the database's ACL.
2. **Start a restored instance disarmed**, exactly as the drill does: the
   retention pruner is built whenever `CHRONICLE_AUDIO_DIR` is set and unlinks
   files, so a restored database pointed at the real audio directory prunes real
   recordings. Give it a copy of the tree, an explicit tier-1 DSN, and none of
   the inbox, ASR, Scribe, Switchyard, Amber or Access variables.
3. **After a restore, an orphan is not garbage.** `GET /admin/storage` lists
   recordings no memo expects; `docs/decisions/chrn-23-audio-layout.md` §4 calls
   that the only list safe to act on, and after a restore it is the opposite:
   those are the window's recordings, and the only copy the server has.
4. **A pin made in the window is gone.** A memo raised to `forever` after the
   snapshot comes back as `days_30`, and the pruner would delete its audio within
   the hour if it is past its window. Run `chronicle prune --dry-run` against the
   restored database and read the list before `serve` starts with the audio
   directory set.
5. **A session revoked in the window is live again.**

## What the drill does not prove

Stated for CHRN-124, which asks what evidence bar the drill meets:

- That the backups keep working: one drill is one day, and the 2026-10-17 to
  2026-10-23 failures passed with the miss counter at zero (SERV-224, SERV-218).
- That a transcript is in a backup before its audio is pruned: a transcript that
  lands late can be pruned on within the hour and before the next 02:30 run.
- Anything about the recordings: none is in any backup.
- A restore's effect on a phone that already pruned: a memo pruned in the window
  returns with `audio_pruned_at` unset and no file.

Half of what the guards protect (notes, revisions, discussions, turns) is empty
in production, so the guards check proves those triggers on rows the drill makes
in scratch, not on restored history. Cross-database consistency (Switchyard) and
what Chronicle does about an ASR job the restored `asr` database no longer knows
are not exercised.
