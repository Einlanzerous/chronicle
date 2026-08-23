# The Switchyard ticket-creation call shape

Salvaged from imperium-loop's `Vox-Dictate` — the third of the three things
worth keeping, because it is the concrete record of *how a machine wrote into
Switchyard and stayed safe to retry*.

## The call

```
POST http://switchyard:4002/v1/tickets                    (timeout 15000 ms)
Authorization:   Bearer {{ $env.SWITCHYARD_TOKEN_N8N_VOX_DICTATE }}
Content-Type:    application/json
Idempotency-Key: voxdictate-<drive_file_id>-<fingerprint>
```

```json
{
  "project_key": "SWY",
  "type": "task",
  "title": "…",
  "description": "…",
  "metadata": { "mode": "modify", "repo_url": "…", "test_cmd": "…",
                "template": "…", "scaffold_project": "…" }
}
```

Five fields, flat. The response carries the new ticket's `key` at the top
level — the triage node reads `$('Create Switchyard Ticket').first().json.key`
directly.

## How each field is built

Assembled in `Parse Scribe Output`, never handed straight from the model:

| field | rule |
|---|---|
| `project_key` | validated against the live list, `LOOP` on failure — see [`project-list.md`](project-list.md) |
| `type` | must be one of `spike` / `task` / `bug` / `epic`; anything else silently becomes `task` |
| `title` | `parsed.title` or `"Untitled voice note"`, truncated to **500** chars |
| `description` | the model's markdown, plus trailing `repo:` / `test_cmd:` / `template:` / `project:` lines for whichever of those the model returned |
| `metadata` | `{ mode }` always (default `"modify"`), plus the same four optional keys when non-empty |

Two things to fix rather than copy:

- **The title limit disagrees with the prompt.** The prompt asks for ≤100
  chars; the code truncates at 500. Neither number is Switchyard's actual
  limit. Pick one and enforce it in one place.
- **`repo_url` / `test_cmd` / `template` / `scaffold_project` are written
  twice** — once as structured `metadata` and again as plain-text lines glued
  onto the end of the description. The description copy is the one that goes
  stale; the metadata copy is the one anything downstream should read.

## Idempotency

The interesting part, and the reason this file exists.

```js
const slugify = (s) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').substring(0, 40);
let _h = 0;
const _fpInput = projectKey + '|' + title + '|' + mode;
for (let i = 0; i < _fpInput.length; i++) _h = ((_h << 5) - _h + _fpInput.charCodeAt(i)) | 0;
const fingerprint = Math.abs(_h).toString(36) + '-' + slugify(title).substring(0, 20);
```

`Idempotency-Key = 'voxdictate-' + source_file_id + '-' + fingerprint`, where
the fingerprint is a 32-bit string hash of `project_key|title|mode` plus a
slug of the title.

The deliberate consequence, recorded in the node's own comment: **the same
Drive file re-parsed to the same routing replays onto the same ticket, but the
same Drive file re-parsed to a *different* `project_key` creates a new one.**
A retry after a network blip is a no-op. A re-run after fixing a decode bug —
which produces different routing — is treated as a genuinely new request.

That is a real design choice and not an obvious one, so: the identity of a
ticket here is *(source file, routing decision)*, not *(source file)*. If E4
wants "one memo, one ticket, forever", the drive file id alone is the key and
the fingerprint has to go.

The hash itself is `h * 31 + c` folded to int32 and then `Math.abs`'d —
adequate for a dozen memos a day, not a content hash. It does not cover the
description, so a re-run where only the description text changed is idempotent
and the first version wins.

## The triage escape hatch

When routing fell back, an IF node (`Needs Triage?`, testing
`project_fallback === true`) posts a comment on the freshly created ticket:

```
POST http://switchyard:4002/v1/tickets/<key>/comments        (timeout 15000 ms)
{"body": ":warning: **Project routing fell back to LOOP** — needs human triage.\n\n…"}
```

The body is prebuilt in `Parse Scribe Output` and carries the `fallbackReason`,
plus the instruction to delete-and-recreate because `project_key` is immutable.
When routing succeeded the body is `''` and the IF's false branch has nothing
attached, so nothing is posted.

The workflow accepted a real cost here — a misrouted ticket has to be destroyed
and rebuilt by hand — in exchange for never blocking the pipeline on an
uncertain answer. Written down because E4 gets to make that trade differently:
`HOLD` lets an uncertain memo wait without a wrong ticket existing first.
