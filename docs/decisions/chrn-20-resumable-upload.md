# CHRN-20 — the resumable upload path

**Status:** implemented · **Mode:** A (evidence) · **Written:** 2026-08-27

CHRN-20 is tier `sonnet`, so the working agreement reviews it on its `Done when`
and green CI rather than on a written decision. This exists anyway, for the same
reason `chrn-23-audio-layout.md` does: the ticket settles *what* the endpoint
does and leaves five things open that the code had to decide, and one of them is
a tier call. §6 is what CHRN-22 inherits.

---

## 1 · The offset is the staging file's size, and is never also a column

Everything else falls out of this.

The obvious design records the offset in the session row and appends to the
file. That gives **two accounts of one fact**, and a crash between the write and
the commit puts them permanently out of step — in one direction the server
believes it holds bytes it does not (the memo finalises short, and the hash
catches it only after the whole remaining transfer), in the other it re-writes
bytes it already has (the memo is corrupt, and the hash catches that too, just
as late). Both failures are recoverable and both cost the client the rest of the
upload.

Reading the size back from the filesystem cannot disagree with the filesystem.
So `tier1.memo_uploads` holds the **declaration** — hash, length, key, retention
— and nothing that changes as bytes arrive except `last_activity_at`.

The consequence worth naming: **the ledger is not load-bearing for
correctness.** Delete the row and the client re-opens with its key; delete the
file and the offset is zero. Neither loses a memo. That is the same property
CHRN-19's seen-ledger has, and it is what makes both of them honestly tier 1.

## 2 · The client declares the content hash before it sends anything

CHRN-18 made the SHA-256 of the arriving bytes the memo's identity, and a hash
is only knowable after the last byte. Asking for it up front buys two things:

- **Re-delivery costs one round trip instead of one transfer.** A phone whose
  queue flushed but whose acknowledgement was lost is the ordinary case, and E2's
  exit criterion — *"re-delivery is a no-op"* — is otherwise only true after
  forty minutes of audio has crossed a mobile link a second time.
- **A truncated or mangled transfer is caught**, and no memo is ever written
  from bytes that did not match what was promised.

It is **declared, never trusted**. Nothing believes the client's hash; the
finalise hashes what actually arrived and compares.

### The gate that makes the shortcut safe

The shortcut is conditional on **the file being on disk**, not on a memo row
existing. Without that gate a client could declare a hash it does not have the
bytes for and mint a memo whose audio is missing — CHRN-23's `missing`, the one
reconciliation state that means something irreplaceable is gone. Asserted in
`TestAlreadyHeldRequiresTheRecordingToBeOnDisk`, which is the test to keep if
any others are dropped.

Size is compared, not content. Re-hashing a recording on every open would make
the cheap path as expensive as the transfer it exists to avoid, and a size
disagreement is itself sufficient reason to fall back to sending the bytes.

## 3 · Staging lives inside the audio root

`os.Rename` is atomic only within a filesystem. A separately configured
`CHRONICLE_UPLOAD_DIR` could be pointed at another mount, at which point
finalisation silently degrades from an atomic rename to a copy — and the
property that there is never a half-written file at a recording's path, which
both CHRN-22 and the player depend on, is gone with no error anywhere. CHRN-19
relies on exactly the same thing for its temp files and says so.

The cost is a reserved name, `audio.StagingDir` = `.uploads`. The leading dot is
load-bearing: every other entry under the root is an author's UUID and no UUID
starts with one, so the name cannot collide with a directory the layout writes.
`Scan` skips it by name and reports it as its own category.

**Not strays, and not corpus.** Counted as corpus it would inflate what the
memos cost. Counted as strays, every phone mid-upload would read as "a file
under the root we cannot name" — which is a warning, and a stalled upload would
sit in that warning indefinitely, teaching whoever reads the storage report to
ignore the field that exists to be alarming.

## 4 · The session table is tier 1

The one call here that is not obvious, and the argument is in
`0005_memo_uploads.up.sql` in full.

The test is not "are these bytes precious" — a partial upload is absolutely
somebody's recording. It is *"is this regenerable from a source of truth that
lives outside Chronicle"*, and it plainly is: **the phone still holds the file.**
A client does not delete its local copy until the server acknowledges a memo, so
dropping the table costs the bytes already transferred and nothing else.

Losing this costs bandwidth. Losing tier 2 costs the corpus. That asymmetry is
the whole test, and 0004 applied it the same way to `tier1.watch_seen`, where the
cost was a re-hash.

Consequences taken deliberately, both following 0004's precedent:

- **No foreign key on `author_id`.** A tier-1 table referencing tier 2 is the
  cross-schema write path the doctrine forbids. So deleting an account leaves its
  in-flight uploads behind rather than cascading them — they are unreachable
  (nothing can authenticate as that account) and the sweep collects them.
  `TestDeletingAnAccountDoesNotCascadeIntoUploads` pins both halves.
- **No `REVOKE`.** That pattern documents intent at the tier-2 boundary; this
  table is one `chronicle_tier1` is *supposed* to reach.

## 5 · Two kinds of failure, treated differently

This is the one place the code is deliberately asymmetric, and it reads like an
inconsistency unless the reason is written down.

**A transfer cut by the network keeps whatever landed.** Those bytes are the
right bytes — the connection died, the content did not — and discarding them
throws away exactly the progress this ticket exists to preserve. The next request
resumes from the new offset.

**A client that sends more than it declared has the chunk discarded** and the
file truncated back to where the request found it. A client whose byte count is
wrong is a client whose bytes there is no reason to believe, so the hash would
reject them at the end anyway — after the rest of the transfer. Refusing at the
point of the mistake costs one chunk instead of the remainder of the file.

The handler refuses the common case from `Content-Length` before reading
anything, which is also why a chunked body is a 411: without a length there is no
way to see an oversized chunk in advance, and the service's backstop read would
have nothing to end on.

## 6 · What CHRN-22 inherits

**The sweep is not the pruner, and the difference is the whole of CHRN-22's
reason for being Mode C.**

| | sweep (`internal/upload/sweep.go`) | pruner (CHRN-22) |
|---|---|---|
| deletes | abandoned partial uploads | the audio of finished memos |
| regenerable? | **yes** — the phone still holds it | **no** |
| gate | idle for `DefaultTTL` (7 days) | a durable transcript, never the calendar alone |
| can name | `tier1.memo_uploads`, `StagingDir` | an author's recordings |

`TestSweepNeverReachesAFinishedRecording` asserts the boundary rather than
leaving it to the comment. Nothing in the sweep reads a memo row or walks an
author's directory.

**Three specific things:**

1. **`audio.StagingDir` is not corpus and is not the pruner's business.** The
   storage report counts it separately (`disk.staging` / `disk.staging_bytes`);
   the pruner should ignore the directory entirely.
2. **Re-upload of a pruned memo is unresolved and is CHRN-22's to settle.** If a
   person re-uploads a memo whose audio was pruned, the file lands and the row
   still carries `audio_pruned_at`, so the storage report counts the recording as
   an orphan. Chronicle **logs a warning naming the memo** and does not touch the
   column — clearing `audio_pruned_at` is a retention policy decision, not
   something an upload handler should invent. CHRN-22 decides whether a
   re-delivery un-prunes.
3. **Expiry is measured from activity, never from creation.** A slow upload is
   not an abandoned one, and the case this endpoint exists for is precisely a
   long recording over a poor link. The same distinction applies to the pruner
   from the other end: `captured_at` is the clock, and it is immutable so that a
   re-delivery cannot move a prune deadline onto today.

## 7 · What CHRN-75 constrained, and how it was honoured

§7 of `chrn-75-proxy-trust-and-the-signin-limiter.md` gave this ticket four
things. All four held without argument:

- **Nothing durable is keyed on an IP address.** A session is keyed on the
  account and the client's idempotency key. A phone moving between wifi and
  mobile data mid-upload changes address and nothing notices, which is the case
  resumability exists for.
- **`clientIP()` stays the single answer to "who is this request from."** This
  path does not consult it and does not grow a second notion of client identity.
- **The edge rate limit stays off the upload path.** No Traefik change was needed:
  `chronicle-public` already carries `/memos` unthrottled and
  `chronicle-public-auth` is a separate `PathPrefix(/auth/)` router, which
  `traefik-chronicle.yml` says it did for this ticket by name. What bounds this
  surface instead is `DefaultMaxOpen` (32 sessions per account) and
  `DefaultMaxBytes` (1 GiB per declaration) — both authenticated-caller bounds,
  which is the right shape for an authenticated endpoint.
- **`X-Chronicle-Proxy-Secret` is not consulted.** `requireUser` is the
  authorisation, and the secret decides one thing in one function.

## 8 · Deliberately not tus

The offset semantics are tus-shaped and the header is spelled `Upload-Offset`,
because that is the obvious name and the client is one we write. It is not an
implementation of tus: no `Tus-Resumable`, no `OPTIONS` discovery, no extensions.
A real tus client fails fast on the missing version header rather than
misbehaving, which is the right failure. Adopting tus properly would mean a
protocol surface — and probably a dependency — for interoperability with clients
that do not exist.

## 9 · What is single-instance about this

`Service` serialises appends to one session with an in-process mutex. Chronicle
runs as a single container and there is no story here for two of them, so this is
adequate — and it is called out because "adequate under an assumption" should be
written where the assumption is.

If that ever changes, the offset itself is already safe: it is the file's size,
and two instances reading it would agree. What would be needed is a Postgres
advisory lock keyed on the session id — the same shape `IngestMemo` already uses
to serialise same-key arrivals.

## 10 · Open, and not this ticket's to close

**Copyparty's inbox exposure is still unresolved** and CHRN-19 raised it: were
`/data/chronicle/inbox` published under `copyparty.conf`'s `accs: rwmd: *`,
anyone on the tailnet could attribute a memo to any account. This endpoint is the
alternative that does not have that problem — it is authenticated, and the author
comes from the session rather than from a directory name — but it does not make
the question go away, because the Copyparty path is what a phone's own recorder
app uses.
