# Salvage: the Scribe, out of `imperium-loop/workflows/vox-dictate.json`

Recovered 2026-08-22 for **CHRN-11**, ahead of SWY-222 (revokes the
`n8n-vox-dictate` agent token, archives LOOP) and SERV-82 (removes the n8n
container). Once that container is gone the prompt goes with it, and IDEA-21's
"build on, not redo" quietly becomes "redo".

Three things were worth keeping, and all three are here:

| file | what it holds |
|---|---|
| [`routing-prompt.md`](routing-prompt.md) | the Scribe prompt, rendered to literal text with its two runtime slots marked |
| [`project-list.md`](project-list.md) | the live-project-list pattern — fetch once, render into the prompt, validate the answer against the same snapshot. Reused in CHRN **E4** |
| [`create-ticket.md`](create-ticket.md) | the Switchyard `POST /v1/tickets` call shape, its idempotency key, and the triage-comment escape hatch |
| [`source/`](source/) | the verbatim n8n export and the three Code nodes, unmodified |

---

## Provenance

Both copies were taken, as CHRN-11 asked, and diffed.

**Committed** — `github.com/Einlanzerous/imperium-loop`, `workflows/vox-dictate.json`
- repo `HEAD` `15ddfbc2c0e5d6e661e97186a22f94f636bc36b8`
- file blob `869affb7b45f6785f7a54ada5f5f26b8da2a2aaf`
- last touched by `6dc8145` *feat: upgrade Scribe + diff-gen models to gemma4:31b post-bakeoff*, 2026-05-17
- only one commit before that ever touched it: `9e77dae` *feat!: migrate pipeline from Vikunja to Switchyard*, 2026-05-17

**Live** — exported from the running `n8n` container
- `docker exec n8n n8n export:workflow --id=vox-dictate-001 --pretty`
- workflow `updatedAt` **2026-05-14T04:51:24Z**, `versionCounter` 65
- `active: true`, and `activeVersionId == versionId`
  (`b62e6ebe-7d1c-4fd3-8ff1-904482695091`) — so the export is the version that
  was actually running, not an unpublished draft
- saved here as [`source/vox-dictate.live.json`](source/vox-dictate.live.json),
  sha256 `78e9455757338908e881cf667a98ad0cc9c19e94b0430a3dbefef9941b49f500`

### Divergence: **none**

All ten nodes compare byte-identical between the two copies on a full
structural comparison — `parameters` (including every `jsCode` body), `type`,
`typeVersion`, `credentials`, node `id`s and canvas positions — as do
`connections`, `settings` and `tags`. The two files differ only in the
export wrapper n8n adds (`createdAt`, `versionId`, `shared`, `staticData`, and
friends) and in JSON formatting.

So the committed copy was fresh: the live workflow was last edited 2026-05-14
and exported to git on 2026-05-17. **Nothing was hand-tuned in the UI and left
unrecorded.** The files here are the live export, but the committed copy would
have served equally well — that is a finding, not an accident, and it is the
answer CHRN-11 asked for.

### One thing the live copy told us that git could not

`staticData` records the Google Drive trigger's last poll as
**2026-05-21T01:39:31Z**. The retained n8n event logs run from 2026-08-07 to
the moment of salvage and contain no Vox-Dictate activity at all — 688
workflow-tagged entries in that window, every one of them Cogitation Engine.

The workflow is flagged `active` but has not polled in roughly three months.
Whatever this prompt's production track record is, it was earned before late
May; nothing has been exercising it since. Worth knowing before treating it as
battle-tested.

---

## What the workflow actually did, end to end

Nine nodes, one straight line with a single conditional tail:

```
Google Drive Trigger  ──▶ Download Transcript ──▶ Extract Text
      ──▶ List Switchyard Projects ──▶ Build Scribe Request
      ──▶ The Scribe (Gemma 4) ──▶ Parse Scribe Output
      ──▶ Create Switchyard Ticket ──▶ Needs Triage? ──[true]──▶ Post Triage Note
```

1. **Google Drive Trigger** — polls the `Codegen Transcripts` folder
   (`1tWqTsfftxfTU_VSRDJgU1UH-5Vh981FH`) every minute for `fileCreated`. The
   pipeline's front door is a folder, not an API: a transcript lands there and
   the rest follows.
2. **Download Transcript** — Drive `download` by file id into binary property
   `data`.
3. **Extract Text** ([`source/extract-text.js`](source/extract-text.js)) —
   dispatches on mime type: `.docx` through `mammoth.extractRawText()`, `text/*`
   and `.txt`/`.md`/`.log` as UTF-8, anything else UTF-8 with a
   looks-like-binary heuristic that sets `decode_error`. Native Google Docs are
   explicitly detected and rejected with an instruction to switch the download
   node to Export. Output is trimmed and **capped at 20 000 chars**, alongside
   `decode_path` / `decode_error` for triage.
4. **List Switchyard Projects** — `GET /v1/projects`. See
   [`project-list.md`](project-list.md).
5. **Build Scribe Request**
   ([`source/build-scribe-request.js`](source/build-scribe-request.js)) —
   renders the project list into the prompt and assembles the Ollama body. See
   [`routing-prompt.md`](routing-prompt.md).
6. **The Scribe (Gemma 4)** — `POST http://ollama:11434/api/generate`,
   `gemma4:31b`, `stream: false`, `format: "json"`, 120 s timeout. Local model,
   single completion, no chat framing.
7. **Parse Scribe Output**
   ([`source/parse-scribe-output.js`](source/parse-scribe-output.js)) — parses
   the JSON (with a ```` ```json ```` fenced-block fallback), validates
   `project_key` against the live list, clamps `type` to the enum, assembles the
   ticket body, the triage comment, and the idempotency fingerprint.
8. **Create Switchyard Ticket** — `POST /v1/tickets` with an `Idempotency-Key`.
   See [`create-ticket.md`](create-ticket.md).
9. **Needs Triage? → Post Triage Note** — if routing fell back, comment on the
   new ticket saying so and why.

### The shape underneath it

Strip the n8n plumbing and the pipeline is: **decode → offer the model a live
choice set → constrain the output to JSON → validate the answer against the
same choice set → write idempotently → flag rather than block when unsure.**

Every judgement the model makes is checked against real data before it reaches
Switchyard, and nothing is trusted straight out of the model — not the project,
not the type, not the title length. That is the part worth inheriting. What E4
changes is the destination set (`NOTE` / `TICKET` / `DISCUSSION`, with `HOLD`
and `DISCARD`), and that a proposal now has to carry a confidence and a reason
instead of silently falling back to a junk-drawer project.

---

## Notes for whoever reuses this

- The three `source/*.js` files are **verbatim n8n Code nodes**. `$input`,
  `$()` and `this.helpers` are n8n runtime globals, not imports — these files
  do not run standalone. They are kept unmodified so the salvage is auditable
  against the export next to them; port them, do not import them.
- `mammoth` was available inside n8n only because the container set
  `NODE_FUNCTION_ALLOW_EXTERNAL=mammoth`. Any reimplementation needs a real
  dependency on it, or a different `.docx` path.
- Service URLs (`switchyard:4002`, `ollama:11434`) are container-network names
  from the imperium-loop compose stack and will not resolve elsewhere.
- Credentials are referenced, never contained: the Google Drive OAuth
  credential is an n8n credential id, and the Switchyard token is read from
  `$env.SWITCHYARD_TOKEN_N8N_VOX_DICTATE`. No secret values are in this
  directory. **SWY-222 revokes that token** — nothing here depends on it still
  working.
