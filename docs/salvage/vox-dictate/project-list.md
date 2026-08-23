# The live-project-list pattern

Salvaged from imperium-loop's `Vox-Dictate`. This is the piece CHRN **E4**
reuses more or less verbatim, so it is written up as a pattern rather than as
a transcript of two nodes.

The shape is: **fetch the project list once, render it into the prompt, then
validate the model's answer against that same snapshot.** One fetch, two
readers. That is the whole idea, and it is what stops the router from routing
to a project that does not exist.

## 1 · Fetch — HTTP Request node `List Switchyard Projects`

```
GET http://switchyard:4002/v1/projects
Authorization: Bearer {{ $env.SWITCHYARD_TOKEN_N8N_VOX_DICTATE }}
timeout: 15000 ms
```

Both downstream consumers read `.json.items`, so the endpoint returns
`{"items": [ { "key", "name", "description", ... } ]}`.

The token is a per-workflow Switchyard agent token supplied through the n8n
container's environment — **SWY-222 revokes exactly this token**, which is one
of the reasons this ticket had to land first. Nothing here depends on the token
value; only on there being a scoped read token for `GET /v1/projects`.

## 2 · Render — inside `Build Scribe Request`

```js
const projects = ($('List Switchyard Projects').first().json.items) || [];

const projectList = projects.map((p) =>
  '- ' + p.key + ' — ' + p.name + (p.description ? ': ' + p.description.substring(0, 150) : '')
).join('\n');
```

which produces, for the prompt's `Available projects:` block:

```text
- SWY — Switchyard: Ticket tracker and the system of record for all work.
- LOOP — Imperium Loop: n8n automation pipelines.
- CTN — Catenary
```

Worth knowing before reusing it:

- Descriptions are hard-truncated at **150 chars**, mid-word, with no ellipsis.
- A project with no description renders as just `- KEY — Name`.
- The list is **unfiltered and unordered** — whatever the endpoint returns, in
  whatever order. Nothing excludes archived or empty projects, and nothing caps
  the list length, so prompt size grows linearly with the project count.

## 3 · Validate — inside `Parse Scribe Output`

The same node result is read a second time, and the model's answer is checked
against it rather than against a hardcoded list:

```js
const projects = ($('List Switchyard Projects').first().json.items) || [];
const allowed = projects.map((p) => p.key);

let projectKey = (parsed.project_key || '').toUpperCase().trim();
let projectFallback = false;
let fallbackReason = '';
if (!projectKey) {
  projectKey = 'LOOP';
  projectFallback = true;
  fallbackReason = 'Scribe returned empty project_key (transcript context was unclear).';
} else if (!allowed.includes(projectKey)) {
  fallbackReason = 'Scribe returned project_key="' + projectKey + '" which is not in the live project list.';
  projectKey = 'LOOP';
  projectFallback = true;
}
```

Two distinct failures, one recovery: **the model declined to choose** and **the
model chose something that does not exist** both land in `LOOP` with
`projectFallback = true` and a `fallbackReason` string that says which. The
reason string is preserved into the triage comment, so the two cases stay
distinguishable after the fact.

`.toUpperCase().trim()` is the only normalisation. A model answer of
`"swy "` is accepted; `"Switchyard"` is not.

## Carrying this into E4

What transfers unchanged:

- **One fetch, two readers.** The list the model saw *is* the list it is
  validated against. Re-fetching for validation would open a window where a
  project appears in the prompt and is gone by the time the answer is checked.
- **Validate against live data, never a hardcoded enum.** The workflow survived
  the Vikunja→Switchyard migration (`9e77dae`) without the routing logic
  needing to know the project set had changed.
- **Two failure modes, kept apart in the reason string.**

What does not transfer:

- **The `LOOP` fallback constant.** LOOP is being archived by SWY-222. E4 has
  a real destination for "cannot tell" — `HOLD`, the inbox — and does not need
  to write into a junk-drawer project to avoid blocking.
- **Silent recovery.** Fallback here is invisible until someone reads the
  triage comment. E4 attaches a confidence and a reason to *every* proposal,
  so "unsure" is a first-class value on the triage screen rather than a
  side effect discovered later.
- **The 150-char truncation.** It exists because the prompt was assembled by
  string concatenation with no budget; anything with a real token budget should
  make that a deliberate number.
