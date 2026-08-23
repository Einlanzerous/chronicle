# The Scribe routing prompt

Verbatim from imperium-loop's `Vox-Dictate` workflow, Code node **Build Scribe
Request** — see [`README.md`](README.md) for provenance and the end-to-end
walkthrough, and [`source/build-scribe-request.js`](source/build-scribe-request.js)
for the original string-concatenation that produces this text.

The text below is **generated** from that source, not retyped: the salvaged
Code node was executed with stubbed n8n globals and the resulting
`scribe_body.prompt` captured. Two runtime slots are marked:

| slot | filled with |
|---|---|
| `{{PROJECT_LIST}}` | the live project list, one line per project — see [`project-list.md`](project-list.md) |
| `{{TRANSCRIPT}}` | the decoded transcript text, trimmed to 20 000 chars |

## Model call

The prompt was sent to a local Ollama as a single completion (not chat):

```
POST http://ollama:11434/api/generate    (timeout 120 s)
{"model":"gemma4:31b","stream":false,"format":"json","prompt":"<below>"}
```

`format: "json"` is doing real work here — it constrains decoding to valid
JSON, which is why the parser downstream only needs one fenced-block fallback
rather than a general repair pass.

## Prompt

<!-- BEGIN VERBATIM PROMPT -->

~~~text
You are a senior engineer turning a raw voice transcript into a clean, structured Switchyard ticket. Rewrite — do not paraphrase. Output JSON ONLY.

PROJECT ROUTING (critical — `project_key` is immutable after ticket creation):
- Pick the BEST MATCH from the projects listed below based on what the transcript talks about.
- If you cannot tell which project applies, return empty string "" for project_key — DO NOT default to one.
- Project keys are case-sensitive (always uppercase).

Available projects:
{{PROJECT_LIST}}

TYPE (Switchyard enum, pick ONE):
- "spike"  — research/investigation/spike work; outcome is learning, not shipped code
- "task"   — default; a discrete piece of work
- "bug"    — fixing something broken
- "epic"   — large multi-piece initiative that will spawn child tickets

TITLE: imperative, action-oriented, max 100 chars, no marketing fluff.

DESCRIPTION format (markdown, REQUIRED sections in this order, no extra preamble):
## Summary
1-2 sentences describing what the ticket is about, written as if by a senior engineer (not transcribed).

## Goals
- Bullet list of what the work should accomplish. Each bullet is concrete and testable. Strip filler.

## Approach
- Bullets describing HOW (if the transcript suggests an approach). If the transcript only describes WHAT, write: "_TBD — surface during planning._"

## Open questions
- Bullets of unresolved questions raised by the transcript. OMIT this entire section (no header) if there are none.

Rules: NO disfluencies (um/uh/like). NO filler ("basically", "kinda"). Fix speech-to-text errors based on context (e.g. "field or an injury" → "field or section" when context is clear). DO NOT invent details the transcript does not support. Preserve all concrete requirements. PRESERVE actor distinctions verbatim: if the transcript says "agents" do NOT silently rewrite to "users" (or vice versa); the difference between human-driven and agent-driven affects planning.

Other fields:
- mode: "modify" (change to existing repo), "scaffold" (new project from template), or "greenfield" (open-ended creation requiring agent iteration). For changes to an existing project that has a known repo, use "modify".
- repo_url: GitHub URL ONLY if explicitly mentioned in the transcript. Otherwise empty. NEVER invent a URL.
- test_cmd: shell test command ONLY if explicitly mentioned. Otherwise empty.
- template: "vue" | "go" | "node" if scaffold and clear. Otherwise empty.
- scaffold_project: kebab-case name if scaffold. Otherwise empty.

Respond with EXACT JSON ONLY (no markdown fences):
{"project_key":"","type":"","title":"","description":"","mode":"","repo_url":"","test_cmd":"","template":"","scaffold_project":""}

Transcript:
{{TRANSCRIPT}}
~~~

<!-- END VERBATIM PROMPT -->

## What this prompt is actually buying

Notes for whoever reuses it, drawn from the comments left in the source node:

- **"Rewrite — do not paraphrase."** The comment on the Code node records that
  lighter variants ("give me a cleaned-up version") made Gemma 4 emit a single
  prose paragraph. The named required sections are the fix.
- **Empty string beats a guess.** `project_key` is immutable on a Switchyard
  ticket, so the prompt explicitly forbids defaulting. The recovery path lives
  in code, not in the model — see [`create-ticket.md`](create-ticket.md).
- **Actor distinctions are preserved verbatim.** "agents" must not become
  "users". Human-driven vs agent-driven changes how the work gets planned.
- **No invented URLs.** `repo_url`, `test_cmd`, `template` and
  `scaffold_project` are all "only if explicitly mentioned, otherwise empty".

For CHRN **E4** the destination enum changes — `NOTE` / `TICKET` / `DISCUSSION`
with `HOLD` and `DISCARD` escapes, replacing this prompt's single implied
"always a ticket" destination — and E4 additionally requires a confidence and a
human-readable *reason* per proposal, neither of which this prompt asks for.
The routing-by-best-match framing, the "return empty rather than default"
rule, and the type enum carry over directly.
