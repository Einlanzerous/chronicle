# Chronicle — Android client

The capture end of Chronicle (E9). **CHRN-59** is the skeleton: the app
project, its auth against Chronicle's own tokens, and networking that works
on Tailscale at home and off the WAN outside. **CHRN-60** adds one-tap
capture — Ogg/Opus into a foreground service, and recovery that destroys
nothing. **CHRN-61** adds the durable offline queue this README's own
[queue section](#the-durable-offline-queue-chrn-61) describes.

The `DISCARD NOW / 30 DAYS / FOREVER` confirm is CHRN-62 and batch triage is
CHRN-63; the [triage section](#batch-triage-chrn-63) below describes the
latter.

## The toolkit, and why it was not a fresh decision

CHRN-59 says the toolkit "is a call for whoever starts this … because E9's
remaining five tickets inherit it." **The estate had already made that call.**
`argosy/mobile/argosy/` and `lyceum/mobile/lyceum/` are both Flutter with
Riverpod + go_router and a design-token port, and Lyceum's README states the
lineage outright: *"Built to match the Argosy mobile stack."* Argosy is the
reference implementation; this is the third copy of one decision rather than a
fourth opinion. The layout convention — `mobile/<name>/` inside the service
repo, not a repository of its own — comes from the same place.

## Setup — one line, no sudo

```sh
source ~/dev-tools/env.sh     # JAVA_HOME, ANDROID_HOME, flutter + sdk on PATH
```

Nothing is installed system-wide: the JDK (Temurin 17), Flutter 3.44.2 and
Android SDK 36 are unpacked under `~/dev-tools`. `flutter doctor` is green on
Flutter and the Android toolchain. **Do not check `command -v java`,
`/usr/lib/jvm` or apt** — all three say "absent", all three are the wrong
question, and that mistake once produced a bogus "needs sudo to install a JDK".

CI pins the same two versions (`.github/workflows/mobile.yml`).

```sh
flutter pub get
flutter analyze          # the lint gate -- run AFTER the last edit, not before
flutter test             # 420 tests, no hardware
flutter build apk --debug
```

## The API client is generated, not written

`packages/chronicle_api/` comes from the repo's `openapi.yaml`:

```sh
scripts/gen-dartapi.sh   # from the repo root
```

CHRN-53 requires it — *"an API client generated from the server's schema rather
than hand-written … Chronicle has three clients against one API (web, Android,
MCP)"* — and `scripts/gen-api.sh` names this client as one of them. CI
regenerates into a temp directory and compares the whole tree, so the generated
package cannot drift from the contract. **The workflow watches `openapi.yaml` as
well as `mobile/**`**, because a PR that edits only the contract is exactly the
one that makes the committed client stale.

The generator is pinned twice, and both pins are needed: the npm launcher in
`mobile/package.json` (through `mobile/bun.lock`) and the JAR version in
`mobile/openapitools.json`. It needs a JDK, which is why this guard lives in
`mobile.yml` and not in `verify.sh` — `verify.sh` is the one command a Go change
has to pass and should not need Java to check a Dart package.

One generator quirk is configured around rather than worked around:
`TriageDecision` has a property named `override`, and in Dart a field called
`override` shadows `dart:core`'s `@override` annotation inside its own class, so
the generated model fails `dart analyze`. `mobile/openapi-dart.yaml` renames the
**Dart field** and leaves the wire key alone. Renaming the property in
`openapi.yaml` would change a shipped contract the Go server, the web client and
CHRN-32's proposal decision all use, to suit one language's scoping rule.

### The server deploys first, on any contract change the client always sends

A generated model built by `openapi-generator`'s Dart target always emits
every property it declares on `toJson`, `null` where the app has no value —
`retention`, `original_filename` and (CHRN-118) `recorded_at` all round-trip
this way. Chronicle's own `decodeJSONLimit` (`internal/api/session.go`) uses
`DisallowUnknownFields`, which refuses *any* JSON key it does not recognise,
`null` value or not. Adding a field to `openapi.yaml` is therefore not safe to
ship independently on the two sides: an app build carrying a regenerated
`chronicle_api` that now sends a new key fails **every** upload — not just
one carrying the new field — against a Chronicle server that predates that
field. **The server release containing the contract change must be promoted
before the first APK built from a tree containing the regenerated package is
installed.**

## One server address, and never a second

Chronicle serves two hostnames off one backend and they are not interchangeable
(`deploy/README.md`):

| host | what it is | for |
|---|---|---|
| `chronicle-direct.…` | DNS-only A record to the WAN address, **no tunnel, no Access** | this app, and the MCP |
| `chronicle.…` | tunneled, **Cloudflare Access-gated** | browsers |

The app holds **one** base URL and never guesses another. It learns it by
scanning the sign-in code CHRN-106 renders in *Account → Add device*, which
encodes `<base>/sign-in?token=…` — one scan sets both the address and the
invite. The base comes from the server's `CHRONICLE_MOBILE_BASE_URL`, which its
own config comment calls *"the only origin a phone is told about"*, and
`internal/invite/url.go` exists so that no client builds the address itself:
*"Only the server knows which of its origins a phone can reach."*

So **"reaches the API from both networks" is a property of that one address** —
Tailscale at home, the WAN record outside — and not of failover logic in the
app. What the app owes instead is an honest answer when the address it holds does
not answer, which is `lib/api/reachability.dart`.

### Pointed at the wrong host, the app says so by name

Point it at the tunneled host and Cloudflare Access answers **302 to its own
login page**. Left to itself `http` follows that redirect and returns a 200 whose
body is a login page, which the generated client reports as a parse error —
blaming the server for the one thing that is actually wrong. So the transport
turns redirect-following off and refuses, and names Access when the redirect
target is Access. Refusing every redirect is safe by construction: `openapi.yaml`
declares **no 3xx on any of its 51 operations** (the two `304`s are cache
validators).

## The durable offline queue (CHRN-61)

CHRN-60 leaves a `ready`/`salvaged` capture on disk. CHRN-61 turns that into
"a memo the server has acknowledged," and its whole design rests on one
promise it must never break: **never say a memo was sent when it was not.**
`lib/queue/queue_record.dart:QueueStatus.acknowledged` is reachable only
through `lib/queue/ack.dart:verifyAck`, which checks the server's answer
against the capture's OWN recorded hash and size — a `complete` response is
not enough on its own, because it says nothing about *which* memo completed.

### What "survives a force-stop" actually means

Force-stop is a platform fact, not a design choice, and reading the ticket's
own `Done when` against it is what the approved plan settles:

| device state | what runs | what does not |
|---|---|---|
| **app open** | every in-app trigger (launch, a capture reaching `ready`, resume, backoff, manual retry) | — |
| **backgrounded, not force-stopped** | the WorkManager periodic task, on `NetworkType.connected` | — |
| **force-stopped** | **nothing** | no WorkManager job, no alarm, **not even across a reboot**, until the person launches the app again |
| **rebooted, never force-stopped** | WorkManager re-registers at boot and can drain unattended once the device is unlocked (credential-encrypted storage needs first-unlock) | — |

So "survives" means: nothing lost, nothing marked sent, and the queue
resumes on the next launch with no action beyond opening the app. A
force-stopped app draining in the background is not a bug this ticket left
in — it is a claim Android does not let any app make.

### The state machine, in one paragraph

`upload.json` sits beside `meta.json` in each capture directory, written and
read only by the queue engine (`lib/queue/engine.dart`), atomically, the same
temp-then-rename pattern as `CaptureDir.writeMeta`. There is **no persisted
`uploading`** — CHRN-60 already learned that lesson once (`state: recording`
on disk was the wrong oracle for "is this still being written"); `SENDING`
here is a live fact of a running pass (`QueueController.wake`'s
`onAttemptStart` callback), never a flag a killed engine can leave behind.
The four other screen labels — `QUEUED`, `AWAITING RETENTION`,
`SIGN IN TO SEND`, `NOT SENT — <reason>`, and `SENT` — are a pure function of
persisted state (`lib/queue/queue_label.dart`, unit-tested directly).

A 401 never touches the credential store from the queue. It raises an
in-memory `DeviceBlock` (`lib/queue/device_block.dart`) and asks the
EXISTING `/auth/me` path (`meProvider`) to arbitrate — a stale or
wrong-endpoint 401 must not be trusted over that path, which is the only
thing that has ever cleared a dead token here.

### Who wakes it

- **In-app**: launch (chained after capture recovery, never alongside it),
  a capture reaching `ready`, app resume, the per-capture backoff timer, and
  a manual "Try again" (`QueueController.retryCapture`).
- **Process-dead**: a WorkManager periodic task on `queueCallbackDispatcher`
  (`lib/queue/background.dart`) — the SAME Dart entrypoint the foreground
  drives, so there is one protocol implementation, not two. It reads the
  token and server address directly (no `ProviderScope`) and yields cleanly
  rather than crash-looping if they are unreadable.
- Two isolates (foreground + headless) coordinate through an
  `IsolateNameServer` presence check to avoid double-draining. It is not the
  safety property: `_writeUnlessAcknowledged` and
  `test/queue/engine_concurrent_test.dart` prove two engines racing the same
  capture cannot produce a second memo or an unverified ack even with the
  exclusion disabled. **Since CHRN-120 it is also no longer merely an
  optimisation**, because the prune pass below deletes: the other isolate can
  find a file gone mid-attempt, and the engine now reads that as "the file
  changed" rather than throwing (`test/queue/engine_prune_race_test.dart`).

### Verification status

`flutter analyze`, `flutter test` (420 tests, including a faithful in-Dart
fake Chronicle server, a kill harness over every I/O boundary of a
multi-chunk upload, and a genuine two-engine race) and `flutter build apk
--debug` are all green — see the commits on `chrn-61-durable-upload-queue`.
**Not yet run, and not runnable from this environment:** the real
`chronicle serve` pass (criterion 13) and the on-device pass below, both of
which need hardware/network access this session did not have.

### The device pass this ticket still needs

Follow the wireless-debugging setup in [Verifying it on a
device](#verifying-it-on-a-device) below, then:

1. **Ten memos, offline.** Airplane mode. Record ten short memos. Expect ten
   `QUEUED` rows, `0` acknowledged, no file removed.
2. **Force-stop, and confirm nothing runs.** `am force-stop dev.dodson.chronicle.dev`.
   Reconnect the network. Wait several minutes. Expect **all ten still
   `QUEUED`** — this is not a failure, it is the platform fact the plan's
   finding 7 names.
3. **Relaunch, still offline-adjacent state cleared.** Open the app. Expect
   the queue to start draining on its own (the launch trigger), with no
   further action.
4. **Reboot, no force-stop, in between.** From a fresh set of ten offline
   memos: reboot the device (no force-stop), leave the app closed, reconnect
   the network, unlock the device. Expect the WorkManager task to drain them
   **without opening the app** — the one case that can run unattended.
5. **Mid-upload interruption.** Cut connectivity partway through a
   multi-chunk memo (a longer recording, Wi-Fi off mid-`SENDING`). Expect
   the same memo to resume from the server's own offset once connectivity
   returns, landing as exactly one memo server-side.

Evidence for all five belongs on the CHRN-61 Switchyard ticket as a comment,
posted alongside the transition — never assumed from CI alone, per the
epic's own `Done when`.

## The retention choice (CHRN-62)

The epic puts retention "at the moment of decision, not in a settings screen":
the only time the person knows whether a memo's audio matters is right after
recording it. So when a recording stops, the capture screen shows a **THE AUDIO**
card with three chips, **DISCARD NOW · 30 DAYS · FOREVER**, and the rule
underneath: *transcript is the durable artefact, audio prunes at 30 days.* The
queue screen states the same rule at its foot.

### Why here, and not where the canvas draws it

Board 1a draws the `THE AUDIO` block on screen 06, after routing. It cannot live
there. By the time a memo is routed, the server already holds its declaration,
and the server's ratchet (`store.Arrival`) only ever **raises** retention. A
DISCARD NOW offered after the first send would be a button the server can never
honour. So the card comes straight after recording, before anything is sent. It
reuses screen 06's block: the title line, its caption, and the chips with 30 DAYS
selected.

### Three states, one gate

| the person… | `meta.json` | the queue |
|---|---|---|
| taps a chip, then **CONFIRM** | `retention` set | declares at once, carrying it |
| taps **SKIP** | `retention_skipped_at` set, `retention` null | declares at once with no opinion, which is the server's 30-day default |
| does neither (leaves the card, or never saw it) | both null | holds for **24 hours** from first sight, then declares with no opinion |

That third row is the "skipped versus not yet seen" distinction CHRN-61's plan
(ruling 3) left for this ticket. `retentionGrace` in `lib/queue/retention_gate.dart`
went from zero to 24 hours in the same change that added the card, and
`test/queue/retention_gate_test.dart` pins it. At zero, a capture was declared
the instant it was ready. That was correct while nothing could ask the question,
and it becomes wrong as soon as something can.

**30 DAYS is the fast path, and the other two are not the same tap.** A chip only
selects and CONFIRM commits, so keeping 30 days takes one tap and discarding or
pinning takes two. A single mis-tap can never throw audio away. The card never
covers the capture control, so a person can go straight into the next memo. The
one they leave undecided waits out its grace, and they can still decide it from
its queue row.

### The choice is made once

`retentionChoosable` (`lib/capture/capture_controller.dart`) is the single rule
behind both the card's visibility and the write's refusal. A capture can be
decided only while:

* it is `ready` or `salvaged`;
* it has no choice and no skip yet;
* the queue has made **no attempt** on it (`upload.json` is absent, or pending with
  zero attempts);
* its grace has more than `retentionChoiceCloses` (5 minutes) left.

The last rule exists because a drain pass reads `meta.json` once, at its start. A
choice written in the grace's final seconds could land after a pass had already
read "no opinion, grace over" and declared it. The server keeps a session's first
declaration, so the choice would appear made and never be honoured. Closing the
choice five minutes early means the two windows cannot overlap, and
`retention_gate_test.dart` checks every minute of the day for exactly that.

An undecided capture is held, so the queue also schedules a foreground wake for
the moment its grace ends. Otherwise it would wait for the next resume. With the
app closed, the periodic WorkManager task picks it up.

### Seeing the choice later

On the phone, each queue row carries `AUDIO · 30 DAYS` / `FOREVER` / `DISCARD
NOW`, or `DEFAULT (30 DAYS)` for a skip or an expired grace. On the web, the note's
provenance block already renders the server's own reading of it:
`PRUNES <date>`, `PINNED — KEPT`, or `PRUNES AT THE NEXT SWEEP`.

## Batch triage (CHRN-63)

Board 1b's B2, "Route decision, variation -- the lane": **Home -> Evening
triage** (`/triage`, behind the sign-in redirect -- triage reads and decides on
the server, so a device with no credential has nothing to show). A day's memos,
each with the Scribe's pick filled in across three lanes, `TICKET` / `NOTE` /
`DISC` (coral, vellum, `chDiscussion`), the other two dim, and one mono line
under it with where the pick goes. **One primary button is pinned to the foot
and its label is the pending outcome, `FILE 3 · DISCARD 1`: that is the commit.**
So the common case is zero taps per memo and one to file, and a wrong proposal
costs one tap on the right lane. A memo the Scribe would discard is dimmed and
shows a bordered `DISCARD` strip with its reason in place of the lanes.

Board 1a's frame 06 is the card an accepted ticket gets: `LINKED · NOT COPIED`, a
coral left rule, the key, the ticket's title, `OPEN ↗`.

The rules live in `lib/triage/triage_rows.dart`, beside `web/src/lib/triage.ts`
(CHRN-55), and say the same things; the first version of this screen was a port
of the web's rows and was reworked to the board.

- **Nothing is sent until FILE.** A lane tap stages the choice on the row; FILE
  sends every row it counts in one request -- as shown, or as the person's
  override -- and the discards enter their undo window.
- **Override is a tap on a different lane**, and an override is not a patch: the
  server builds the decision from the override alone and validates it like a
  model's. So a lane tap carries the title and text across, and what the new
  destination needs that the proposal did not carry (a note's page, a ticket's
  project key) is left blank: the row says `NEEDS INPUT · ...` and FILE leaves
  it alone until the row's own tap supplies it. Nothing is guessed.
- **A proposal the server is not confident about is not filed blind.**
  `pre_acceptable` is a hint for the default, never a licence; such a row says
  `LOW CONFIDENCE · TAP THE LANE TO CONFIRM` and joins FILE when its picked
  lane is tapped.
- **Tapping a row's title** opens the single-memo confirm: the whole transcript,
  the proposal in full, and `ACCEPT AS SHOWN` -- which sends that memo on its
  own, now, and is the only path that carries the per-item `confirm_edit` an
  append or supersede costs -- plus EDIT (title, project, page, text), HOLD and
  DISCARD. Neither FILE nor the header's `ACCEPT ALL` ever sets `confirm_edit`.
- **`ACCEPT ALL` (header)** commits the untouched pre-filled set exactly as it
  did before the rework; **FILE** commits the lanes as they stand (that set,
  plus confirmed, overridden and discarded rows). Whether the header action
  should instead reset every row to the Scribe's pick is an open question: the
  canvas draws both and does not say.
- **A failed item stays visibly pending.** Only `applied` takes a row out of the
  batch; `failed`, `refused`, `stale` and `needs_input` each leave it in place
  under `... · STILL PENDING` with the server's reason. There is no per-row
  RETRY: a failed row is still counted in FILE, which is the retry. A refused
  row waits for a changed decision. A network failure says to retry (a replay
  answers `applied` from the recorded decision).
- **A discard is held back, not recalled.** `discarded` is terminal on the
  server, so `UNDO 10 MIN` means the request is not sent until the window
  closes, the screen is left or the next batch loads. Kill the app inside the
  window and the memo is simply still waiting.
- **An accepted ticket is one tap from open.** The whole card is the tap; it
  opens the `ticket_url` the server answered with in the browser
  (`url_launcher`). The card carries the upstream's own state word and its age,
  and says `SWITCHYARD UNREACHABLE` rather than showing a confident stale value
  (CLAUDE.md invariant 2).
- **44 px.** The canvas draws the lanes 38 px tall; the epic's 44 px minimum tap
  target wins, so every lane, the title and `ACCEPT ALL` have a hit area of at
  least 44 px.
- **Capture times are read defensively.** A device clock can assert any value
  from year 100 to 9900 (CHRN-118), so a time before 2020 or more than a day
  ahead is drawn `--:--` and never as a date it was not.
- **Named deferrals:** the web's DEFERRED list (parked memos from earlier
  evenings) is not on the phone; a memo held here shows as held until the
  screen is left, and the web lists it. The discussion colour `chDiscussion`
  is ported from the canvas frame, not from `web/src/styles/tokens.css`, which
  has no token for it yet. Batches are the server's 25 at a time.

### The device pass for CHRN-63

Still owed -- the tests prove the behaviour on a 412 x 915 surface, not on the
phone. On a debug build against a server with a few transcribed memos and the
Scribe on:

1. **Home -> Evening triage.** Expect the sub-line, one row per memo with the
   Scribe's lane filled, and `FILE n · DISCARD m` pinned at the foot. Compare
   against board 1b's B2.
2. **FILE** once. Expect the rows to read `... CREATED` and a proposed discard
   to read `DISCARDED · UNDO 10 MIN`.
3. **A TICKET row:** tap the coral card. Expect the Switchyard ticket open in
   the browser, one tap from the row.
4. **Failure:** with Wi-Fi and data off, FILE. Expect `FAILED · STILL PENDING`
   under each row and the button still counting them; restore the network and
   FILE again.
5. **Override:** tap a different lane on a row. Expect `YOUR CHOICE`, and
   `NEEDS INPUT` if the destination wants something the proposal lacked; FILE
   it.

## Pruning the phone's copy (CHRN-120)

CHRN-61 shipped no deletion path on purpose: an acknowledged capture's audio
stayed on the phone forever. Storage runs about 15 MB an hour and CHRN-60's
free-space gate refuses to record rather than evict, so an install that never
prunes eventually holds every memo it ever recorded. CHRN-120 is the follow-up
that ruling promised, and it is the client's own prune of its own local copy —
separate from the server's (CHRN-22), and gated on it.

### It ships dark

`pruneLocalAudioEnabled` (`lib/queue/prune.dart`) is a compile-time constant that
**defaults to false**. A build that does not opt in never asks the server and
never deletes anything: the drain runs exactly as before and the prune pass
returns at its first line. To turn it on for a development build:

```sh
flutter run --dart-define=CHRONICLE_PRUNE_LOCAL_AUDIO=true
```

The reason is the backup gap. The shared Postgres has no backup (CHRN-68,
SERV-43), and the transcript lives in that same database. Under this gate the
phone deletes only after the server has deleted ITS audio, so from that moment a
lost database loses the memo outright — where today the phone's surviving copy
means it would only cost derived data. Until that is closed this ships
default-off: the code is landed, reviewed and tested, and what is
deferred is storage reclamation. `test/queue/prune_test.dart` pins the default,
so changing it fails CI; flipping it is the follow-up that closes the gap.

**Since CHRN-125 it is checked, not just defaulted.** A release APK is made by
`tool/build_release.sh` and nothing else, and `tool/check_release_build.sh`
fails the release if that path passes any `--dart-define` -- this one included
-- and then reads `pruneBuildMarker` back out of the built APK and requires
`chronicle: local-audio prune off` in every snapshot. See
[Releases](#releases-chrn-125).

### The gate: the server's own prune, and nothing else

The phone deletes a capture's sendable audio only after the server has deleted
its own: `GET /audio/{memo_id}` (asked for one byte, `Range: bytes=0-0`)
answering **`410` with code `audio_pruned`**. That answer is the server's whole
prune predicate evaluated by the server — `retention <> 'forever'`, the 30-day
window or `discard_now`, *and* a durable transcript — and `audio_pruned_at` is
only ever set by a compare-and-swap over all of it (`store.MarkAudioPruned`). So
`lib/queue/prune_gate.dart` holds no copy of any of it: no model floor, no
window, no retention rule that could drift out of step and delete audio the
server would still keep. CLAUDE.md invariant 1 (never prune audio for a memo
whose transcription never succeeded) is enforced by the server's predicate, once.

Every other answer is a refusal that changes nothing on disk. The table is in
`prune_gate.dart` and tested row by row in `test/queue/prune_gate_test.dart`:

| answer | outcome |
|---|---|
| `410`, `audio_pruned` | **the only delete** |
| `206`, or a bare `200` (Range ignored) | still there; ask again later |
| `410` with any other code or a non-JSON body | kept, logged at WARN |
| `500 audio_missing`, `404` | kept, logged at WARN — the server may have lost *its* copy, so this device may hold the only one |
| `429`, `503`, other `5xx` | kept; ends the pass |
| `401`, no answer, wrong host | ends the pass; nothing recorded |
| anything else | kept, logged at WARN |

Nothing local ever permits a delete: not the age of the capture, not a
`retention` value in `meta.json`, not how long ago it was acknowledged. A locally
declared `forever` skips the request as a saving — the server never prunes a
`forever` memo — and a `forever` memo therefore keeps its local audio.

### When it asks, and how often

The pass runs immediately after `drainPass` on every trigger the queue already
wakes on, in both the foreground and the WorkManager leg
(`runBackgroundQueuePass`, which exists so that leg has a test seam), and never
before a send. It is skipped under an active device block. It does not raise a
block itself: the send path's arbitration is the only writer of that state.

A poll floor bounds *when* the server is asked, never *whether* a delete happens:

- not before `startedAt + 30 days` (the server's window, a scheduling hint that
  can only ask early, never late), or from the acknowledgement for a capture
  declared `discard_now`;
- at most once per capture per 24 hours (`lastPolledAt` in `upload.json`, absent
  meaning "never asked");
- least recently asked first, at most `maxProbesPerPass` (10) per pass.

So removal happens at the **first poll after the server has pruned, and that is
at most daily**, not the moment transcription finishes. A `discard_now` capture
is asked once straight after its ack, gets a `206`, and is asked again a day
later.

### The order, and what survives

For a capture the gate permits: the `pruned` tombstone is written, then
`locallyPrunedAt` goes into `upload.json`, then the file is unlinked. A crash at
any point leaves at most a tombstone and/or a mark with the file still present,
and the next pass finishes the unlink **without asking the server again** — but
never deletes a file that has no tombstone.

Only the file the queue sent is ever eligible. `meta.json`, `upload.json` and the
tombstone are never deleted, and on a `salvaged` capture the untrimmed
`audio.opus` stays: its tail past the trim cut is bytes the server never
received. An `empty` capture is never touched.

**A lost `upload.json` no longer costs only a round trip.** The queue used to
read an absent or corrupt `upload.json` as "never queued", which was harmless
while the audio was there to re-open with. The tombstone carries `memoId`
independently, and `QueueDir.readOrEnqueue` (used by both scans) rebuilds an
`acknowledged` record from it instead of enqueuing a fresh `pending` one.

### What this does not do

- **No UI signal** that a capture's local audio is gone. `SENT` already says the
  server holds it, and no design brief exists for a distinct "pruned locally"
  chip.
- **It does not remove the extra copy today's phone accidentally holds.** With no
  prune the phone's copy survives even past the server's own, so a lost database
  loses only derived data. Once the server's audio is gone and this device has
  deleted its copy, the memo exists only in that database. That is the exposure
  the default-off flag defers.

### The device pass for CHRN-120

Run against a real server with a debug build carrying the define. The 410 is not
reachable quickly by ordinary use, because `captured_at` is immutable and only a
`discard_now` memo is pruned before 30 days, so the route is:

1. Record a memo and choose **DISCARD NOW** on the confirm (CHRN-62). Before
   CHRN-62 this step meant hand-setting `retention` in `meta.json` over
   `adb shell run-as` on the debug build.
2. Let it send: one `SENT` row, and one `206` probe straight after the ack.
3. On the test database make sure a durable transcript exists (model
   `whisper.cpp/small.en`, `partial` false), then run `chronicle prune` against
   it (or wait for the server's hourly sweep).
4. The once-per-24-hours cap now blocks the second probe, so clear
   `last_polled_at` in that capture's `upload.json` over
   `adb shell run-as dev.dodson.chronicle.dev` (a debug build is `.dev` since
   CHRN-125, and the released app refuses `run-as`) — or wait 24 hours.
5. Wake the queue (open the app). Expect `audio.opus` gone from
   `<filesDir>/captures/<id>/` while `meta.json`, `upload.json` and `pruned`
   remain, and one `chronicle: pruned local audio memo=… gate=audio_pruned`
   line in `adb logcat`.

Evidence belongs on the CHRN-120 Switchyard ticket as a comment. If hardware or
time does not allow the pass, say so there as a deviation rather than skipping it
silently.

## Releases (CHRN-125)

The app ships as a **signed APK attached to a GitHub Release** and is
sideloaded. There is one device and one user; a Play account, Play's own key
custody and review latency buy nothing here.

### Installing

Download `chronicle-<version>.apk` from the newest **`mobile-v*`** Release on
<https://github.com/Einlanzerous/chronicle/releases> -- the server's `v*`
Releases carry no APK -- and check it against the `.sha256` beside it. Then
either open it on the device (allow the installer once) or:

```sh
adb install -r chronicle-<version>.apk
```

A fresh install knows no server. Scan *Account -> Add device* on a signed-in
browser, as in the [device pass](#the-device-pass).

### Updating

**Obtainium**, pointed at `https://github.com/Einlanzerous/chronicle` with the
APK asset filter `^chronicle-.*\.apk$`. Obtainium has no tag filter; the asset
filter alone skips the server's releases, because they carry no matching file.
It checks on a schedule and notifies. The signature check is Android's own, not
Obtainium's: the installer accepts an update only when it is signed within the
app's lineage, the same rule as `adb install -r`.

The fallback with no app on the phone is GitHub's *Watch -> Custom ->
Releases* email, then install as above.

### How a release is cut

`mobile/chronicle` is its own release-please component, `mobile`, with its own
`CHANGELOG.md` and **`mobile-vX.Y.Z` tags** -- the way `asr/` is versioned
apart from the server (CHRN-82), and for the same reason: an Android-only
commit no longer cuts a server release, and a server-only one no longer
implies a new APK. The root package excludes `mobile/`.

Merging the `mobile` release PR pushes the tag, and the tag runs
`.github/workflows/mobile-release.yml`:

1. the guard's self-test, then its config check (below);
2. **it waits for magos's approval** -- the `mobile-release` environment has a
   required reviewer, because the environment's `mobile-v*` rule limits the
   ref's *name* and not who pushed it, and the key is the only thing between
   repo write access and code on the phone;
3. `tool/build_release.sh <version> <unix-time>` builds, signs and asserts;
4. the guard's artifact scan on that APK;
5. the APK and its `.sha256` go onto the Release.

`versionName` is the tag's version; `versionCode` is a Unix timestamp
(Lyceum's rule), so a re-dispatch of the same tag still counts as newer. To
rebuild a tag, run the workflow by hand **from that tag** (the *Use workflow
from* selector), or the environment refuses it.

**A contract change still deploys the server first.** A `mobile-v*` Release
built from a tree whose generated client sends a new field is installed only
after the server release carrying that field is promoted (see
[above](#the-server-deploys-first-on-any-contract-change-the-client-always-sends)).

### The guard: no `--dart-define` in a release

`CHRONICLE_BASE_URL` would point every install at whoever built it;
`CHRONICLE_PRUNE_LOCAL_AUDIO` turns on the phone's deletion of its own audio,
which ships off until the database has a backup. Neither may reach a release,
and nothing in the Dart source can hold that line -- it is a property of the
build command. `tool/check_release_build.sh` (a port of Lyceum's
`check_store_build.sh`) checks four things:

| check | what it catches |
|---|---|
| config | any `dart-define` (`--dart-define`, `--dart-define-from-file`, `-Pdart-defines`) in `mobile-release.yml`, `tool/build_release.sh`, `tool/release_lib.sh` or the Gradle files |
| hostnames | the estate's domain or `chronicle-direct.zerogravity…` in a shipped file or the APK |
| origins | any absolute URL in the Dart snapshot that is not on its allowlist -- a baked base URL, whatever it points at |
| marker | `pruneBuildMarker`: every `libapp.so` must say `chronicle: local-audio prune off` and none `…prune ON` |

The last exists because the prune define is a `bool`: const-folded, it leaves
no string in the snapshot, so no scan could tell a build with it from one
without. `pruneBuildMarker` is folded the same way, so exactly one of its two
literals survives, and `main()` logs it once at startup.

It runs **config-only in `mobile.yml` on every PR** -- advisory, since `main`
has no required checks -- and **in full before upload in `mobile-release.yml`**,
which is the hard stop. `--self-test` proves each check fails on what it exists
to catch; `tool/test_release_lib.sh` does the same for the signing assertions.

### Signing, and the key

The release key lives in **Signet**, project `chronicle`
(`ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`,
`ANDROID_KEY_PASSWORD`), synced to the **`mobile-release` environment only**:
no PR and no push to `main` can read it. No copy is in the repository or on
disk.

The app installed before CHRN-125 was a debug build, signed by this machine's
`~/.android/debug.keystore`. Android refuses an update across a signature
change -- and an uninstall would destroy every unsent capture, the session, and
the local audio that is the only second copy of each memo while the database has
no backup. So the release key was introduced by **key rotation**:
`android/signing/lineage.bin` (certificates and signatures, no private key)
records the debug key handing over to the release key, and every release is
signed by the release key **with that lineage**:

```sh
apksigner sign --ks <release> --lineage android/signing/lineage.bin \
  --rotation-min-sdk-version 30 --v1-signing-enabled false --v2-signing-enabled false …
```

That is APK Signature Scheme v3, release key alone. v1/v2 would need the
lineage's oldest signer -- the debug key -- in CI, and the minSdk-30 rotation
means every device the app supports sees the same rotated signer.
`tool/release_lib.sh` asserts, before upload, that the APK is signed by the
release key alone, **carries the debug -> release lineage** (read from the APK;
`apksigner verify` cannot see it), is `dev.dodson.chronicle` at the tag's
version, is not debuggable, and holds all three ABIs.

Consequences worth knowing:

* **Nothing signed by the debug key installs over the released app any more**
  (`INSTALL_FAILED_UPDATE_INCOMPATIBLE`, measured). That is why debug builds are
  `.dev`.
* The debug keystore's only role was writing the lineage. A copy is in Signet as
  `chronicle/ANDROID_DEBUG_KEYSTORE_BASE64` with no target, so the lineage can
  be reproduced; CI never sees it.
* **A lost release key costs this:** no further update can be signed. Recovery
  is a new key and an uninstall -- drain the queue first, and accept that the
  phone's local audio copies and the session go with it (a release build has no
  `run-as` to back them up from). Signet is the only copy of the key, and
  Signet's own store is inside the estate's open backup gap.

### Scratch releases for a device pass

`tool/build_rotcheck.sh` builds the same release, signs it the same way and
runs the same assertions and scan, as `dev.dodson.chronicle.rotcheck`
("Chronicle rotcheck"), together with a debug build of that package -- so a
pass can install the release OVER the debug build and walk the key rotation on
an app that holds nothing. The package change is made in a copy of the app;
the release path has no knob for it, and the script refuses to run in CI.

```sh
signet exec --secret chronicle/ANDROID_KEYSTORE_BASE64 \
  --secret chronicle/ANDROID_KEYSTORE_PASSWORD \
  --secret chronicle/ANDROID_KEY_ALIAS \
  --secret chronicle/ANDROID_KEY_PASSWORD -- tool/build_rotcheck.sh /tmp/rotcheck
```

## Verifying it on a device

`verify.sh` is *"every check that does not need hardware"*, and CHRN-59's
`Done when` is a hardware claim — E9 is the first epic whose evidence cannot come
from it. The split, agreed before the code was written:

* **CI** proves what needs no hardware: `flutter analyze`, `flutter test`, a
  debug APK, and the generated-client guard.
* **A real device over `adb`** proves the rest. A real device needs no sudo and
  is better evidence than an emulator, which has one NAT'd interface and cannot
  honestly tell Tailscale-at-home from the WAN outside. (An emulator would also
  need `magos` added to the `kvm` group — the only genuine sudo item in E9, and
  it is for the emulator alone, never for building.)

### The device pass

Wireless debugging is enough, and the connect port is not the pairing port --
let mDNS find it rather than reading it off the phone twice:

```sh
source ~/dev-tools/env.sh
adb pair <host>:<pairing-port> <code>   # from the phone's Wireless debugging screen
adb mdns services                        # _adb-tls-connect._tcp -> the connect port
adb devices                              # auto-connects
flutter install --debug                  # --debug: `flutter install` defaults to release
```

A debug build installs as **`dev.dodson.chronicle.dev`, "Chronicle dev"**,
beside the released app and with its own data -- it can never overwrite the real
install, and the real install refuses it anyway (see
[Releases](#releases-chrn-125)). A pass that needs the real app's own data works
on a released build, which is not debuggable, so `run-as` is not available on it.

The app logs nothing on purpose (`avoid_print`), so **screenshots are the
observability**: `adb exec-out screencap -p > shot.png`.

1. **Onboard.** On a signed-in device open *Account → Add device* and scan the
   QR. The app should land on the home screen showing the account and a green
   `Connected`, with a `CHECKED hh:mm` under it. Camera permission is asked for
   when the scanner opens, not at launch.
2. **On the home network.** Pull to refresh. Expect `Connected` and a newer
   `CHECKED`. **Not necessarily over Tailscale** -- see the note below.
3. **Off the home network** (Wi-Fi off, on cellular). Same address, now resolving
   to the WAN A record from outside. Pull to refresh. Expect `Connected`.
4. **Unreachable, and recovery.** Take the network away entirely -- airplane
   mode, or `svc wifi disable` **and** `svc data disable`, because either alone
   leaves a path open. Expect `Cannot reach the server` with its timestamp and a
   **Try again** button, **not** a spinner. Restore the network and pull to
   refresh: back to `Connected` **without restarting the app** and without
   signing in again, and **Try again** disappears. That is the "recovers cleanly"
   clause.
5. **The wrong host, once, on purpose.** Sign out, then paste the *tunneled*
   host's sign-in link. Expect *"That address is behind Cloudflare Access…"* and
   — the part worth checking — that the invite is **not** spent, because the app
   probes `/healthz` before it POSTs. The same code should still redeem against
   the direct host. (Cheaper alternative, since an invite is single-use: `curl`
   the tunneled host and check the `Location` against
   `NotChronicleException.isAccessGated`.)

Note what step 4 is and is not. **The app never uses the Cloudflare tunnel**: it
talks to the direct host, which has no tunnel ingress, so the tunnel going down
cannot affect it. `Done when`'s *"recovers cleanly when the tunnel is down"* is
therefore exercised as *the server is unreachable* (step 4) plus *pointed at the
Access-gated host* (step 5), which are the two ways this app can actually lose
the server.

### Two things that will mislead you, both learned the hard way

**"At home" does not mean "over Tailscale."** On the 2026-09-19 pass Tailscale
was installed but **inactive**, and the phone still reached the API: it sat on
the LAN, resolved `chronicle-direct…` to the **public WAN address**, and got
there in 24 ms by NAT hairpin. The clause is about reaching the API from each
network, and the app holds one address and does not care which path carries it
-- so do not record "Tailscale" unless you checked for a `100.x` address.

**`OUT_OF_SERVICE` on cellular does not mean there is no cellular.** While on
Wi-Fi the phone registers over Wi-Fi calling -- `getRilDataRadioTechnology=
18(IWLAN)`, `transportType=WLAN` -- and `dumpsys telephony.registry` reports
`mVoiceRegState=1` / `mDataRegState=1`. Drop Wi-Fi and it comes up on 5G within
seconds. Reading that as "no mobile-data path" once turned a run meant to test
step 4 into an accidental test of step 3.

### Result, 2026-09-19

Run on a **Pixel 9 Pro / Android 17** against the shipped `v1.19.0` debug APK.
Steps 1-4 **pass**: onboarded by QR; `Connected` on the home network; `Connected`
on 5G with Wi-Fi off; and `Cannot reach the server` + **Try again** with both
radios down, returning to `Connected` on one running instance with the session
intact. Step 5 was evidenced by the `curl` alternative rather than by spending a
second invite. The session list showed the device as **Pixel 9 Pro**, which is
`deviceLabel()` reading `ro.product.model`.

## Named deferrals

Deliberate, and recorded rather than left to be discovered (the APK release
track that used to be listed here is [Releases](#releases-chrn-125)):

* **The canvas's typefaces are not bundled.** Hanken Grotesk, JetBrains Mono and
  Newsreader come to the web client from `@fontsource`, which ships **woff2
  only**, and Flutter needs ttf/otf — so it is an asset-vendoring job, not a
  copy, and it buys nothing on the one screen this ticket ships. The mono role is
  kept as a role (`fontMono` in `lib/theme/tokens.dart`), so bundling the real
  faces later is one edit in one file. This follows CHRN-54, which left the type
  scale to "whichever ticket builds the first real screen and can see what sizes
  it actually needs."

## Layout

```
lib/
  api/
    server_url.dart    the one base URL, and why there is only one
    session.dart       the session token, in the keystore
    transport.dart     bearer + deadline + the redirect refusal
    reachability.dart  probe /healthz, classify, recover
    providers.dart     the generated client, rebuilt when either fact changes
  auth/
    sign_in_link.dart  parsing the QR payload
    auth_controller.dart
    device_label.dart
  capture/             CHRN-60: one-tap capture, recovery, the on-disk record
    capture_record.dart   meta.json, CaptureDir, the idempotency key, the prune tombstone
    capture_channel.dart  the Dart half of the seam to CaptureService
    capture_controller.dart  also CHRN-62's chooseRetention / skipRetention
    retention_choice.dart   CHRN-62: the three answers and their wire values
    recovery.dart      classify() and finalise() -- never guesses about a live recorder
    ogg.dart
  queue/               CHRN-61: the durable offline queue -- see the section above
    queue_record.dart  upload.json, QueueStatus, the queue's only write surface
    ack.dart           verifyAck -- the one door to `acknowledged`
    retention_gate.dart · backoff.dart · device_block.dart · failure.dart
    engine.dart        QueueEngine.drainPass -- one attempt, always the same shape
    upload_transport.dart · uploads_api_transport.dart
    prune.dart         CHRN-120: the pass that deletes the phone's copy -- default OFF
    prune_gate.dart    the one condition that permits it: the server's own 410
    audio_gate_transport.dart  GET /audio/{id}, one byte, status returned not thrown
    queue_controller.dart  the in-app triggers
    queue_label.dart   the screen label, as a pure function of persisted state
    background.dart    the WorkManager headless entrypoint
  triage/              CHRN-63: batch triage -- see the section below
    triage_rows.dart   the rules, as pure functions (a port of web/src/lib/triage.ts)
    triage_controller.dart  the batch, the decisions, the ticket cards
  theme/               tokens ported from web/src/styles/tokens.css
  features/
    triage/            board 1b B2 (the lanes, FILE), board 1a 06 (the ticket card)
    signin/            scan, or paste the link
    home/               account, address, connection state
    capture/           board 1a screens 01/02 -- idle, recording; CHRN-62's retention card
    queue/              board 1a screen 03's CHRN-61 slice
    shared/            the CAPTURE / QUEUE tab bar
packages/chronicle_api/  GENERATED -- do not edit
```
