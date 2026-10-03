// The sidebar's TRIAGE badge and the triage screen's header count are one
// number (CHRN-55). It lives here, as a module-level ref, because the two
// that show it are a layout and a route inside it: AppShell.vue reads the
// batch once per session for the badge, and TriageView.vue is what drives the
// number down -- so the screen writes this after every decision and the badge
// follows without a reload (magos's review of PR #97).
import { ref } from 'vue'

export interface TriageWaiting {
  count: number
  /** The batch came back full. GET /triage/batch hands over one screen
   *  (DefaultLimit == MaxLimit == 25) and no total, so a full batch means "at
   *  least this many" -- shown as `25+`, never as a confident 25. */
  more: boolean
}

export const triageWaiting = ref<TriageWaiting | null>(null)

export function setTriageWaiting(count: number, more: boolean): void {
  triageWaiting.value = { count, more }
}

export function formatTriageWaiting(w: TriageWaiting): string {
  return `${w.count}${w.more ? '+' : ''}`
}

/** getTriageBatch scopes to the actor for a member and to everyone for the
 *  owner (internal/triage/batch.go), so the number is right but different per
 *  person. ONE wording, used by the badge's title and the screen's header. */
export function triageScopeLabel(isOwner: boolean): string {
  return isOwner ? 'all memos awaiting a decision' : 'your memos awaiting a decision'
}
