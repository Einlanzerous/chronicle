// Salvaged verbatim from imperium-loop n8n workflow "Vox-Dictate"
// (vox-dictate-001), Code node 'Build Scribe Request', typeVersion 2.
// Runs inside n8n: `$input`, `$()`, `this.helpers` are n8n globals,
// not imports. Kept unmodified on purpose — see ../README.md.

// Build the Gemma 4 prompt for the Scribe. Asks for a STRUCTURED ticket:
// markdown description with named sections, an inferred ticket type from
// Switchyard's enum, and extracted custom-field hints. Lighter prompt
// variants (e.g. "cleaned-up version") cause Gemma 4 to produce a
// single prose paragraph — that's the failure mode we're fixing here.
const extract = $('Extract Text').first().json;
const transcript = extract.transcript || '';
const projects = ($('List Switchyard Projects').first().json.items) || [];

const projectList = projects.map((p) =>
  '- ' + p.key + ' — ' + p.name + (p.description ? ': ' + p.description.substring(0, 150) : '')
).join('\n');

const prompt =
  'You are a senior engineer turning a raw voice transcript into a clean, structured Switchyard ticket. Rewrite — do not paraphrase. Output JSON ONLY.\n\n' +
  'PROJECT ROUTING (critical — `project_key` is immutable after ticket creation):\n' +
  '- Pick the BEST MATCH from the projects listed below based on what the transcript talks about.\n' +
  '- If you cannot tell which project applies, return empty string "" for project_key — DO NOT default to one.\n' +
  '- Project keys are case-sensitive (always uppercase).\n\n' +
  'Available projects:\n' + projectList + '\n\n' +
  'TYPE (Switchyard enum, pick ONE):\n' +
  '- "spike"  — research/investigation/spike work; outcome is learning, not shipped code\n' +
  '- "task"   — default; a discrete piece of work\n' +
  '- "bug"    — fixing something broken\n' +
  '- "epic"   — large multi-piece initiative that will spawn child tickets\n\n' +
  'TITLE: imperative, action-oriented, max 100 chars, no marketing fluff.\n\n' +
  'DESCRIPTION format (markdown, REQUIRED sections in this order, no extra preamble):\n' +
  '## Summary\n' +
  '1-2 sentences describing what the ticket is about, written as if by a senior engineer (not transcribed).\n\n' +
  '## Goals\n' +
  '- Bullet list of what the work should accomplish. Each bullet is concrete and testable. Strip filler.\n\n' +
  '## Approach\n' +
  '- Bullets describing HOW (if the transcript suggests an approach). If the transcript only describes WHAT, write: "_TBD — surface during planning._"\n\n' +
  '## Open questions\n' +
  '- Bullets of unresolved questions raised by the transcript. OMIT this entire section (no header) if there are none.\n\n' +
  'Rules: NO disfluencies (um/uh/like). NO filler ("basically", "kinda"). Fix speech-to-text errors based on context (e.g. "field or an injury" → "field or section" when context is clear). DO NOT invent details the transcript does not support. Preserve all concrete requirements. PRESERVE actor distinctions verbatim: if the transcript says "agents" do NOT silently rewrite to "users" (or vice versa); the difference between human-driven and agent-driven affects planning.\n\n' +
  'Other fields:\n' +
  '- mode: "modify" (change to existing repo), "scaffold" (new project from template), or "greenfield" (open-ended creation requiring agent iteration). For changes to an existing project that has a known repo, use "modify".\n' +
  '- repo_url: GitHub URL ONLY if explicitly mentioned in the transcript. Otherwise empty. NEVER invent a URL.\n' +
  '- test_cmd: shell test command ONLY if explicitly mentioned. Otherwise empty.\n' +
  '- template: "vue" | "go" | "node" if scaffold and clear. Otherwise empty.\n' +
  '- scaffold_project: kebab-case name if scaffold. Otherwise empty.\n\n' +
  'Respond with EXACT JSON ONLY (no markdown fences):\n' +
  '{"project_key":"","type":"","title":"","description":"","mode":"","repo_url":"","test_cmd":"","template":"","scaffold_project":""}\n\n' +
  'Transcript:\n' + transcript;

const scribeBody = JSON.stringify({
  model: 'gemma4:31b',
  stream: false,
  format: 'json',
  prompt,
});

return [{ json: { scribe_body: scribeBody, transcript_length: transcript.length } }];
