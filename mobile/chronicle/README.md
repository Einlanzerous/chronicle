# Chronicle — Android client

The capture end of Chronicle (E9). This ticket, **CHRN-59**, is the skeleton
under it: the app project, its auth against Chronicle's own tokens, and
networking that works on Tailscale at home and off the WAN outside.

Recording is **not** here. One-tap capture is CHRN-60, the durable queue is
CHRN-61, the `DISCARD NOW / 30 DAYS / FOREVER` confirm is CHRN-62 and batch
triage is CHRN-63 — the first two are `review_mode: decision`, so they owe a
written decision before any code.

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
flutter analyze          # the lint gate
flutter test             # 43 tests, no hardware
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
  theme/               tokens ported from web/src/styles/tokens.css
  features/signin/     scan, or paste the link
  features/home/       account, address, connection state
packages/chronicle_api/  GENERATED -- do not edit
```
