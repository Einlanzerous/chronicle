// Salvaged verbatim from imperium-loop n8n workflow "Vox-Dictate"
// (vox-dictate-001), Code node 'Parse Scribe Output', typeVersion 2.
// Runs inside n8n: `$input`, `$()`, `this.helpers` are n8n globals,
// not imports. Kept unmodified on purpose — see ../README.md.

// Parse Gemma 4's JSON, validate project_key against the live project list,
// and build the Switchyard CreateTicket body. When project inference is
// uncertain we fall back to LOOP and flag the ticket so a human can triage —
// project_key is immutable, so we accept the cost of moving via delete+recreate
// in exchange for never blocking the pipeline.
const llmRaw = $input.first().json.response || '';
let parsed = {};
try {
  parsed = typeof llmRaw === 'string' ? JSON.parse(llmRaw) : llmRaw;
} catch (e) {
  const match = (llmRaw || '').match(/```json?\s*([\s\S]*?)```/);
  if (match) { try { parsed = JSON.parse(match[1]); } catch (e2) {} }
}

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

const allowedTypes = ['spike', 'task', 'bug', 'epic'];
let ticketType = (parsed.type || '').toLowerCase().trim();
if (!allowedTypes.includes(ticketType)) ticketType = 'task';
const title = (parsed.title || 'Untitled voice note').toString().substring(0, 500);
const mode = (parsed.mode || 'modify').toLowerCase();
const metadata = { mode };
if (parsed.repo_url) metadata.repo_url = parsed.repo_url;
if (parsed.test_cmd) metadata.test_cmd = parsed.test_cmd;
if (parsed.template) metadata.template = parsed.template;
if (parsed.scaffold_project) metadata.scaffold_project = parsed.scaffold_project;

const descriptionLines = [
  (parsed.description || '').toString(),
];
if (parsed.repo_url) descriptionLines.push('repo: ' + parsed.repo_url);
if (parsed.test_cmd) descriptionLines.push('test_cmd: ' + parsed.test_cmd);
if (parsed.template) descriptionLines.push('template: ' + parsed.template);
if (parsed.scaffold_project) descriptionLines.push('project: ' + parsed.scaffold_project);
const description = descriptionLines.filter(Boolean).join('\n\n');

const ticketBody = JSON.stringify({
  project_key: projectKey,
  type: ticketType,
  title,
  description,
  metadata,
});

const triageBody = projectFallback
  ? JSON.stringify({
      body:
        ':warning: **Project routing fell back to LOOP** — needs human triage.\n\n' +
        fallbackReason + '\n\n' +
        'If this ticket belongs to a different project, please delete it and recreate manually ' +
        '(project_key is immutable on Switchyard tickets). Re-train the Scribe by clarifying the transcript next time.',
    })
  : '';


// Idempotency-Key fingerprint: same Drive file + same inferred routing +
// same title → same ticket (legitimate retry replays). Re-parsing the same
// file with a different project_key (e.g. after fixing decode) treats it
// as a fresh request and creates a new ticket.
const slugify = (s) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').substring(0, 40);
let _h = 0;
const _fpInput = projectKey + '|' + title + '|' + mode;
for (let i = 0; i < _fpInput.length; i++) _h = ((_h << 5) - _h + _fpInput.charCodeAt(i)) | 0;
const fingerprint = Math.abs(_h).toString(36) + '-' + slugify(title).substring(0, 20);

return [{
  json: {
    ticket_body: ticketBody,
    triage_body: triageBody,
    project_key: projectKey,
    project_fallback: projectFallback,
    parsed_title: title,
    parsed_mode: mode,
    parsed_type: ticketType,
    idempotency_fingerprint: fingerprint,
  },
}];
