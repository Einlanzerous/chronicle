<script setup lang="ts">
// The editing state of a triage row (CHRN-55, board 1e): the destination as a
// segmented control, the title, the page, the project and type a TICKET
// needs, and the line that says what an edit is recorded as. It only holds a
// draft; lib/triage.ts turns one into the override, and TriageView.vue sends
// it.
//
// A real <form> with real radios, so the keyboard needs no handling of its
// own here: ⏎ in a field submits, the arrow keys walk the destination, Tab
// moves between fields. The one addition is ⌘/Ctrl+⏎ from the prose field,
// where a bare ⏎ has to stay a newline. Esc is the view's.
//
// TWO FIELDS THE BOARD DOES NOT DRAW, both because the contract needs them:
// the prose field (an override is validated like a model's proposal, so a
// NOTE needs its body, a TICKET its description and a DISCUSSION its opening
// post) and, only for a proposal that acts on an existing note, the verb and
// the note it names.
import { onMounted, reactive, ref, watch } from 'vue'
import {
  DESTINATIONS,
  TICKET_TYPES,
  VERBS,
  textLabel,
  validateDraft,
  verbNeedsTarget,
  type Draft,
} from '@/lib/triage'

const props = defineProps<{
  memoId: string
  initial: Draft
  pagePaths: string[]
  projectKeys: string[]
  /** The proposal named an existing note: offer the verb and its target. */
  showVerb: boolean
  /** What the server said about the last attempt, if it refused one. */
  serverError: string | null
}>()

const emit = defineEmits<{ confirm: [draft: Draft]; cancel: [] }>()

const draft = reactive<Draft>({ ...props.initial })
const error = ref<string | null>(null)
const titleInput = ref<HTMLInputElement | null>(null)

onMounted(() => {
  titleInput.value?.focus()
  titleInput.value?.select()
})

watch(draft, () => {
  error.value = null
})

function submit(): void {
  const problem = validateDraft(draft)
  if (problem) {
    error.value = problem
    return
  }
  emit('confirm', { ...draft })
}
</script>

<template>
  <form class="ch-tri-editor" @submit.prevent="submit">
    <fieldset class="ch-tri-editor-group">
      <legend class="ch-tri-label">DESTINATION</legend>
      <div class="ch-tri-seg">
        <label v-for="dest in DESTINATIONS" :key="dest" class="ch-tri-seg-option">
          <input v-model="draft.destination" type="radio" :name="`dest-${memoId}`" :value="dest" />
          <span>{{ dest }}</span>
        </label>
      </div>
    </fieldset>

    <template v-if="draft.destination !== 'DISCARD'">
      <div class="ch-tri-editor-row">
        <label class="ch-tri-field ch-tri-field--wide">
          <span class="ch-tri-label">TITLE</span>
          <input ref="titleInput" v-model="draft.title" class="ch-tri-input" type="text" maxlength="200" autocomplete="off" />
        </label>
        <label class="ch-tri-field" :class="{ 'is-off': draft.destination !== 'NOTE' }">
          <span class="ch-tri-label">PAGE{{ draft.destination === 'NOTE' ? '' : ' · NOTE ONLY' }}</span>
          <input
            v-model="draft.pagePath"
            class="ch-tri-input ch-tri-input--mono"
            type="text"
            :list="`pages-${memoId}`"
            :disabled="draft.destination !== 'NOTE'"
            autocomplete="off"
            placeholder="estate/storage"
          />
          <datalist :id="`pages-${memoId}`">
            <option v-for="p in pagePaths" :key="p" :value="p" />
          </datalist>
          <span v-if="draft.destination === 'NOTE'" class="ch-tri-help">
            {{
              initial.pagePath
                ? 'NEAREST EXISTING PAGE · CHANGE IF WRONG'
                : pagePaths.length
                  ? 'PICK A PAGE · A NEW LEAF UNDER AN EXISTING ONE IS FINE'
                  : 'NO PAGES EXIST YET · CREATE ONE UNDER PAGES FIRST'
            }}
          </span>
        </label>
      </div>

      <div class="ch-tri-editor-row">
        <label class="ch-tri-field" :class="{ 'is-off': draft.destination !== 'TICKET' }">
          <span class="ch-tri-label">PROJECT · TICKET ONLY</span>
          <input
            v-model="draft.projectKey"
            class="ch-tri-input ch-tri-input--mono ch-tri-input--upper"
            type="text"
            :list="`projects-${memoId}`"
            :disabled="draft.destination !== 'TICKET'"
            autocomplete="off"
          />
          <datalist :id="`projects-${memoId}`">
            <option v-for="k in projectKeys" :key="k" :value="k" />
          </datalist>
        </label>
        <label class="ch-tri-field" :class="{ 'is-off': draft.destination !== 'TICKET' }">
          <span class="ch-tri-label">TYPE · TICKET ONLY</span>
          <select v-model="draft.ticketType" class="ch-tri-input ch-tri-input--mono" :disabled="draft.destination !== 'TICKET'">
            <option v-for="t in TICKET_TYPES" :key="t" :value="t">{{ t }}</option>
          </select>
        </label>
      </div>

      <div v-if="showVerb && draft.destination === 'NOTE'" class="ch-tri-editor-row">
        <label class="ch-tri-field">
          <span class="ch-tri-label">VERB</span>
          <select v-model="draft.verb" class="ch-tri-input ch-tri-input--mono">
            <option v-for="v in VERBS" :key="v" :value="v">{{ v }}</option>
          </select>
        </label>
        <label class="ch-tri-field" :class="{ 'is-off': !verbNeedsTarget(draft.verb) }">
          <span class="ch-tri-label">EXISTING NOTE</span>
          <input
            v-model="draft.targetNote"
            class="ch-tri-input ch-tri-input--mono"
            type="text"
            :disabled="!verbNeedsTarget(draft.verb)"
            placeholder="CHR-0311"
            autocomplete="off"
          />
        </label>
      </div>

      <label class="ch-tri-field ch-tri-field--block">
        <span class="ch-tri-label">{{ textLabel(draft.destination) }}</span>
        <textarea
          v-model="draft.text"
          class="ch-tri-input ch-tri-textarea"
          rows="5"
          @keydown.ctrl.enter.prevent="submit"
          @keydown.meta.enter.prevent="submit"
        ></textarea>
        <span class="ch-tri-help">⌘/CTRL + ⏎ CONFIRMS FROM HERE</span>
      </label>
    </template>

    <p v-else class="ch-tri-editor-discard">
      Nothing is written. The memo leaves triage; you get ten minutes on this screen to undo it.
    </p>

    <p v-if="error || serverError" class="ch-tri-editor-error" role="alert">{{ error || serverError }}</p>

    <div class="ch-tri-editor-foot">
      <span class="ch-tri-help">
        {{
          draft.destination === 'DISCARD'
            ? 'DISCARD · A DELIBERATE DECISION, NEVER PART OF ACCEPT ALL'
            : 'EDITED · RECORDED AS AGREED-AFTER-REWRITING, NOT AGREED'
        }}
      </span>
      <span class="ch-tri-editor-actions">
        <button type="button" class="ch-tri-quiet" @click="emit('cancel')">CANCEL ESC</button>
        <button type="submit" class="ch-tri-primary">CONFIRM <span class="ch-tri-primary-key">⏎</span></button>
      </span>
    </div>
  </form>
</template>

<style scoped>
.ch-tri-editor {
  max-width: 760px;
}

.ch-tri-editor-group {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
}

.ch-tri-label {
  display: block;
  padding: 0;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.13em;
  color: var(--ch-text-meta);
}

.ch-tri-help {
  display: block;
  margin-top: 7px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.11em;
  color: var(--ch-text-meta);
}

.ch-tri-seg {
  margin-top: 8px;
  display: flex;
  gap: 1px;
}

.ch-tri-seg-option {
  flex: 1;
  position: relative;
  cursor: pointer;
}

.ch-tri-seg-option input {
  position: absolute;
  inset: 0;
  opacity: 0;
  margin: 0;
  cursor: pointer;
}

.ch-tri-seg-option span {
  height: 34px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: var(--ch-font-mono);
  font-size: 10px;
  letter-spacing: 0.11em;
  color: var(--ch-text-2);
  background: var(--ch-base);
  border: 1px solid var(--ch-line);
}

.ch-tri-seg-option input:checked + span {
  font-weight: 600;
  color: var(--ch-base);
  background: var(--ch-signal);
  border-color: var(--ch-signal);
}

.ch-tri-seg-option input:focus-visible + span {
  outline: 1px solid var(--ch-signal);
  outline-offset: 2px;
}

.ch-tri-editor-row {
  margin-top: 16px;
  display: flex;
  gap: 16px;
}

.ch-tri-field {
  flex: 1;
  min-width: 0;
  display: block;
}

.ch-tri-field--wide {
  flex: 1.4;
}

.ch-tri-field--block {
  margin-top: 16px;
}

.ch-tri-field.is-off {
  opacity: 0.4;
}

.ch-tri-input {
  margin-top: 8px;
  width: 100%;
  height: 38px;
  padding: 0 12px;
  border: 1px solid var(--ch-line);
  border-radius: 0;
  background: var(--ch-base);
  color: var(--ch-text);
  font-family: var(--ch-font-sans);
  font-size: 14px;
  caret-color: var(--ch-signal);
}

.ch-tri-input:focus-visible {
  outline: none;
  border-color: var(--ch-signal);
}

.ch-tri-input--mono {
  font-family: var(--ch-font-mono);
  font-size: 12.5px;
}

.ch-tri-input--upper {
  text-transform: uppercase;
}

.ch-tri-textarea {
  height: auto;
  padding: 10px 12px;
  line-height: 1.5;
  resize: vertical;
}

.ch-tri-editor-discard {
  margin: 18px 0 0;
  font-size: 13px;
  line-height: 1.5;
  color: var(--ch-text-2);
}

.ch-tri-editor-error {
  margin: 14px 0 0;
  font-size: 12.5px;
  line-height: 1.5;
  color: var(--ch-text);
  border-left: 2px solid var(--ch-text-meta);
  padding-left: 10px;
}

.ch-tri-editor-foot {
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid var(--ch-line);
  display: flex;
  align-items: center;
  gap: 16px;
}

.ch-tri-editor-foot .ch-tri-help {
  margin-top: 0;
}

.ch-tri-editor-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 12px;
  flex: none;
}

.ch-tri-quiet {
  border: 0;
  background: none;
  padding: 0;
  cursor: pointer;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: 0.11em;
  color: var(--ch-text-2);
}

.ch-tri-quiet:hover,
.ch-tri-quiet:focus-visible {
  color: var(--ch-signal);
  outline: none;
}

.ch-tri-primary {
  height: 34px;
  padding: 0 16px;
  border: 0;
  border-radius: 0;
  background: var(--ch-signal);
  color: var(--ch-base);
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 9px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  font-weight: 600;
  letter-spacing: 0.12em;
}

.ch-tri-primary:focus-visible {
  outline: 1px solid var(--ch-text);
  outline-offset: 2px;
}

.ch-tri-primary-key {
  font-weight: 400;
}
</style>
