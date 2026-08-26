# CHRN-23 — Where a recording lives on disk (decision)

Status: **implemented in PR #11.** Written up after the fact at the reviewer's
request, because the consumer of these constraints is CHRN-22 and this is where
CHRN-22 will look for them.
Ticket: CHRN-23 (Phase P1, parent CHRN-2). Tier `haiku`, so Mode A — this
document exists despite that, for the reason in the next paragraph.
Read by: **CHRN-22** (retention pruner, Mode C), CHRN-19, CHRN-20, CHRN-21.

## Why a haiku ticket has a decision record

Two of the three choices below are binding on CHRN-22, which is Mode C
*precisely because it can destroy authored data*. `REVIEW.md` names
`docs/decisions/` as where decisions live, and CHRN-18 set the pattern. A
constraint recorded only in a code comment and a merged PR body is a constraint
the next person meets after they have designed around its absence.

## 1 · The path

```
<root>/<author_id>/<first two characters of content_hash>/<content_hash>
```

`root` is `CHRONICLE_AUDIO_DIR`, absolute, and the service refuses to boot if it
is not an existing directory.

Both components are immutable in `tier2.memos` — 0003's trigger raises `CH002`
on an `UPDATE` that moves `author_id`, `content_hash`, `byte_size` or
`captured_at`. That is what makes the path stable for the life of the memo, and
it is why CHRN-18 was right to add no `audio_path` column: the path is derivable,
so storing it would be a second source of truth for one fact.

What this rules out, and why: a path built from the original filename, the state,
the retention class or the author's display name **moves** when a memo is renamed
or reclassified. A file the pruner cannot find is a file the pruner cannot delete
and the player cannot play.

## 2 · The author scope, which is the decision

**The conventional choice — content-addressing alone — is wrong here, and the
reason only surfaces in CHRN-22.**

`memos_author_content` is `UNIQUE (author_id, content_hash)`, *not* unique on
`content_hash`. 0003 states the intent plainly: the same bytes arriving under a
second author produce "a second memo under the second author rather than silently
re-attributing the first."

So under a hash-only layout, two memo rows share one file. Pruning either one
must then either refcount the other or delete audio that a live memo still needs.
**A refcount inside the pruner is exactly where an irreversible-deletion bug would
live**, and `CLAUDE.md` calls pruning audio that should have been kept the single
worst thing this system can do.

Scoped by author, each memo's file is its own and **CHRN-22 is an unlink, never
an arithmetic problem.** The cost is duplicate bytes when two authors hold
byte-identical audio — a case a personal corpus does not produce, and one the
sizing has three orders of magnitude of headroom for (4.1 GB over 812 memos on a
volume with 256 GB free).

`TestSameBytesUnderTwoAuthorsAreTwoFiles` is named for this and fails if the
scope is ever removed.

## 3 · No extension

`.opus` / `.wav` would be derived from `codec`, which is `NULL` until CHRN-21 runs
and is rewritten when it does. **A path that only becomes knowable after
normalisation is not derivable from the memo row alone**, which is the one
property this layout exists to have. The bytes are self-describing and the row
records the codec.

## 4 · Three reconciliation states, not two

| | |
|---|---|
| **orphan** | on disk, no unpruned memo expects it — what a prune whose unlink did not happen looks like. **The only list safe to act on.** |
| **missing** | a memo expects its audio and the file is gone. It emits an `ERROR` log line whether or not anyone reads the response. |
| **mismatched** | present, but not the size ingest recorded. |

Collapsing the third into either of the others gives the wrong prompt: reported as
an orphan it invites deletion, reported as missing it invites panic.

**Strays** — anything under the root this layout did not write — are counted and
named but never treated as corpus and never offered as orphans. A file we cannot
name is a file we do not understand, and "delete what you do not understand" is
the wrong default for a directory holding the only copy of somebody's recording.

## What CHRN-22 inherits

- **Deletion is `os.Remove(store.Path(ref))` and nothing else.** No refcount, by
  construction (§2).
- **The orphan list is the only one it may act on.** Never `missing`, never
  `mismatched`, never a stray.
- **`audio.ProjectionWindow` is the 30 days.** One constant, so a label that
  promises a prune date and a job that acts on one cannot disagree — reuse it
  rather than re-declaring it.
- **The clock is `captured_at`**, which is immutable (decision CHRN-18 §4). Not
  `updated_at`, not the latest arrival.
- **Pruned is `audio_pruned_at IS NOT NULL`**, never a nulled path — there is no
  path column. `AudioInventory` omits pruned memos, which is what makes a leftover
  file read as an orphan rather than as agreement.

## What CHRN-21 inherits, and must settle before it normalises anything

**`byte_size` is immutable and the layout gives a memo exactly one path.** If
Chronicle ever rewrites the audio in place, the file at `RelPath(ref)` stops
matching `byte_size`, `byte_size` cannot be corrected (`CH002`), and **every
successfully normalised memo becomes a permanent `mismatched`** — the state that
means "something corrupted your audio" would be the steady state. That is the
exact false alarm §4 exists to prevent.

Three exits, and they are not equivalent:

1. a separate `normalised_byte_size` column,
2. excluding normalised memos from the size comparison,
3. **the decode not happening in Chronicle at all** — in which case the stored
   bytes stay the arrival bytes, `byte_size` stays true, and `mismatched` keeps
   meaning what it says.

CHRN-21 is currently stopped for sign-off on precisely that placement question,
and option 3 there is the one that leaves this design intact. Noted so the two
tickets are decided together rather than in sequence.

## Configuration

| variable | |
|---|---|
| `CHRONICLE_AUDIO_DIR` | Absolute, or absent. Absent → `GET /admin/storage` answers **503 naming the variable**, not an empty corpus: "not configured" and "no audio yet" are different facts. Set but not an existing directory → the service **refuses to boot** rather than creating it. |

A directory that springs into existence on a typo is how tier-2 audio lands on the
container's writable layer instead of the NVMe — which looks like it works until
the next redeploy takes the corpus with it.

## What this does not decide

- **Whether Chronicle decodes audio at all.** CHRN-21, stopped for sign-off.
- **When the pruner runs, or what it does.** CHRN-22.
- **Where the Copyparty inbox sits.** CHRN-19. It is a different directory under
  the same `/data` root, and the watcher observes it rather than moving files out
  of it (CHRN-18 §3).
