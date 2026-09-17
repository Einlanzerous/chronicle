// The injection keys AppShell.vue provides its already-fetched page-path
// list under, so /app/pages/ (no path -- "the root page list") can read the
// tree's roots without a second GET /pages. A separate module so a child
// view can import the keys without importing the whole shell component.
import type { InjectionKey, Ref } from 'vue'

export const PAGE_PATHS_KEY: InjectionKey<Ref<string[] | null>> = Symbol('ch-page-paths')

// Rides alongside PAGE_PATHS_KEY: true when the shell's GET /pages failed
// (or the backend was unreachable), so a consumer can tell "the corpus
// really is empty" from "the read never succeeded" -- an empty array reads
// the same either way, and only one of those is honestly "no pages yet".
export const PAGE_PATHS_ERROR_KEY: InjectionKey<Ref<boolean>> = Symbol('ch-page-paths-error')
