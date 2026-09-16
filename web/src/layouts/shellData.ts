// The injection key AppShell.vue provides its already-fetched page-path
// list under, so /app/pages/ (no path -- "the root page list") can read the
// tree's roots without a second GET /pages. A separate module so a child
// view can import the key without importing the whole shell component.
import type { InjectionKey, Ref } from 'vue'

export const PAGE_PATHS_KEY: InjectionKey<Ref<string[] | null>> = Symbol('ch-page-paths')
