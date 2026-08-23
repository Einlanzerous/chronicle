// Salvaged verbatim from imperium-loop n8n workflow "Vox-Dictate"
// (vox-dictate-001), Code node 'Extract Text', typeVersion 2.
// Runs inside n8n: `$input`, `$()`, `this.helpers` are n8n globals,
// not imports. Kept unmodified on purpose — see ../README.md.

// Extract text from the file Google Drive downloaded. Dispatches on mime
// type:
//   - .docx (wordprocessingml.document) → mammoth.extractRawText() unzips
//     the OOXML archive and pulls the paragraph text out cleanly.
//   - text/* or .txt/.md → UTF-8 decode of the raw buffer.
//   - anything else → UTF-8 attempt with a decode_error flag for triage.
// n8n's binary storage may be filesystem-backed, so we go through the
// helpers.getBinaryDataBuffer accessor regardless of storage mode.
const item = $input.first();
const meta = (item.binary && item.binary.data) || {};
const mime = (meta.mimeType || '').toLowerCase();
const fileName = meta.fileName || '';
const ext = (meta.fileExtension || fileName.split('.').pop() || '').toLowerCase();

let text = '';
let decodeError = '';
let decodePath = '';

try {
  const buf = await this.helpers.getBinaryDataBuffer(0, 'data');
  if (mime.includes('wordprocessingml.document') || ext === 'docx') {
    const mammoth = require('mammoth');
    const result = await mammoth.extractRawText({ buffer: buf });
    text = result.value || '';
    decodePath = 'mammoth';
    if ((result.messages || []).length > 0) {
      decodeError = result.messages.map((m) => m.message).join('; ');
    }
  } else if (mime.startsWith('text/') || ['txt','md','markdown','log'].includes(ext)) {
    text = buf.toString('utf-8');
    decodePath = 'utf-8 (text)';
  } else if (mime.includes('vnd.google-apps.document')) {
    decodeError = 'Source is a native Google Doc — change Download Transcript to use the Export operation with text/plain mime type.';
    decodePath = 'unsupported-google-doc';
  } else {
    text = buf.toString('utf-8');
    decodePath = 'utf-8 (fallback)';
    if (/[\u0000-\u0008\u000b\u000c\u000e-\u001f]/.test(text.substring(0, 500))) {
      decodeError = 'Unknown mime ' + mime + ' (ext=' + ext + ') — decoded as UTF-8 but result looks binary. Add a handler for this format.';
    }
  }
} catch (e) {
  decodeError = e && e.message || String(e);
}

text = text.replace(/\u0000/g, '').trim().substring(0, 20000);
const source = item.json || {};
return [{
  json: {
    transcript: text,
    transcript_length: text.length,
    decode_path: decodePath,
    decode_error: decodeError,
    source_file_id: source.id,
    source_file_name: source.name,
    source_mime_type: meta.mimeType || '',
  },
}];
