// Pure note-revision logic (CHRN-56), kept out of NoteView.vue so it is unit
// testable without mounting anything, following CHRN-57/58's lib precedent
// (discussionThread.ts, pageTree.ts).
import type { components } from '@/api/schema.d.ts'
import { formatTimestamp } from './format'

export type Revision = components['schemas']['Revision']
export type RevisionMeta = components['schemas']['RevisionMeta']
export type AppendRevisionRequest = components['schemas']['AppendRevisionRequest']

/**
 * `listNoteRevisions` answers oldest-first, append-only (openapi.yaml:
 * "seq... only ever grows"). HISTORY reads newest-first -- the revision
 * somebody is about to restore is usually the one from a minute ago, not the
 * one from three months back -- so this is the one place that order is
 * inverted, done once here rather than trusted to every `v-for` that needs it.
 */
export function sortRevisionsNewestFirst(revisions: readonly Revision[]): Revision[] {
  return [...revisions].sort((a, b) => b.seq - a.seq)
}

/** The mono `REVISION n · <when>` banner the history read view and its rows both use. */
export function revisionLabel(revision: Pick<Revision, 'seq' | 'created_at'>): string {
  return `REVISION ${revision.seq} · ${formatTimestamp(revision.created_at)}`
}

/**
 * Restore, held to CHRN-56's ticket comment exactly: "appending an old body
 * as a new revision, never rewriting one." `AppendRevisionRequest` carries no
 * `restored_from` field -- there is nothing else in the contract for a
 * restore to send -- so this reproduces the old revision's title and body
 * and lets `appendRevision` treat it as an ordinary edit whose text happens
 * to match something already in history.
 */
export function restorePayloadFor(revision: Pick<Revision, 'title' | 'body'>): AppendRevisionRequest {
  return { title: revision.title, body: revision.body }
}

/**
 * Board 1c's own footer line, `N REVISIONS`. `RevisionMeta.seq` is "1 for
 * the first revision. Only ever grows" over a strictly append-only history
 * with no branching, so the CURRENT revision's own `seq` already IS the
 * total revision count -- no second call to `listNoteRevisions` is needed
 * just to put a number in the header.
 */
export function revisionsCountLabel(count: number): string {
  return `${count} REVISION${count === 1 ? '' : 'S'}`
}
