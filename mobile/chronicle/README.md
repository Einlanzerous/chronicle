# Chronicle — Android client

The capture end of Chronicle (E9). **CHRN-59** is the skeleton: the app
project, its auth against Chronicle's own tokens, and networking that works
on Tailscale at home and off the WAN outside. **CHRN-60** adds one-tap
capture — Ogg/Opus into a foreground service, and recovery that destroys
nothing. **CHRN-61** adds the durable offline queue this README's own
[queue section](#the-durable-offline-queue-chrn-61) describes.

The `DISCARD NOW / 30 DAYS / FOREVER` confirm is CHRN-62 and batch triage is
CHRN-63 — both `review_mode: decision`, so they owe a written decision before
any code, same as CHRN-60 and CHRN-61 did.

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
flutter test             # 238 tests, no hardware
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
declares **no 3xx on any of its 50 operations** (the two `304`s are cache
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

`flutter analyze`, `flutter test` (238 tests, including a faithful in-Dart
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
2. **Force-stop, and confirm nothing runs.** `am force-stop dev.dodson.chronicle`.
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

**This is a default, not a lock.** Chronicle has no APK release track yet, so
nothing rejects a release build that passes the define. Lyceum's
`tool/check_store_build.sh` fails on any `--dart-define` in its release workflow;
the ticket that adds Chronicle's release track (see `server_url.dart`) owns the
equivalent, and it must reject this define too.

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
reachable by ordinary use, because `captured_at` is immutable and declared
retention is null on every capture today, so the route is:

1. Record a memo. Before its first send, hand-set `retention` to `discard_now` in
   its `meta.json` (`adb shell run-as dev.dodson.chronicle`).
2. Let it send: one `SENT` row, and one `206` probe straight after the ack.
3. On the test database make sure a durable transcript exists (model
   `whisper.cpp/small.en`, `partial` false), then run `chronicle prune` against
   it (or wait for the server's hourly sweep).
4. The once-per-24-hours cap now blocks the second probe, so clear
   `last_polled_at` in that capture's `upload.json` with the same adb edit as
   step 1 — or wait 24 hours.
5. Wake the queue (open the app). Expect `audio.opus` gone from
   `<filesDir>/captures/<id>/` while `meta.json`, `upload.json` and `pruned`
   remain, and one `chronicle: pruned local audio memo=… gate=audio_pruned`
   line in `adb logcat`.

Evidence belongs on the CHRN-120 Switchyard ticket as a comment. If hardware or
time does not allow the pass, say so there as a deviation rather than skipping it
silently.

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

Both are deliberate, and both are recorded rather than left to be discovered:

* **The canvas's typefaces are not bundled.** Hanken Grotesk, JetBrains Mono and
  Newsreader come to the web client from `@fontsource`, which ships **woff2
  only**, and Flutter needs ttf/otf — so it is an asset-vendoring job, not a
  copy, and it buys nothing on the one screen this ticket ships. The mono role is
  kept as a role (`fontMono` in `lib/theme/tokens.dart`), so bundling the real
  faces later is one edit in one file. This follows CHRN-54, which left the type
  scale to "whichever ticket builds the first real screen and can see what sizes
  it actually needs."
* **No APK release track.** CI builds a debug APK and stops. A signed release
  needs a keystore this repo does not have, and the ticket that adds one also
  owns Lyceum's `tool/check_store_build.sh` guard — the one that fails a store
  build carrying any `--dart-define`, so that no installer ships pointed at
  whoever built it.

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
    capture_controller.dart
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
  theme/               tokens ported from web/src/styles/tokens.css
  features/
    signin/            scan, or paste the link
    home/               account, address, connection state
    capture/           board 1a screens 01/02 -- idle, recording
    queue/              board 1a screen 03's CHRN-61 slice
    shared/            the CAPTURE / QUEUE tab bar
packages/chronicle_api/  GENERATED -- do not edit
```
