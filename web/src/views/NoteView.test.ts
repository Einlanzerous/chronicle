import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { api } from '@/api/client'
import NoteView from './NoteView.vue'
import type { components } from '@/api/schema.d.ts'

// CHRN-110: `loadNote` bumps a module-level `loadSeq`, captures it, and
// drops its own answer if a later navigation has superseded it. This test
// exercises the same guard on the loaders it fans out to -- `loadBacklinks`
// here, chosen because it is the simplest one to control the timing of --
// which is the part of the file that had NO guard before this ticket. Run
// against the pre-fix code (drop the `seq` parameter and the `seq !==
// loadSeq` check in `loadBacklinks`), this test fails: the previous note's
// slow backlinks answer lands last and overwrites the aside under the
// current note's URL.

type Note = components['schemas']['Note']

function note(ref: string, overrides: Partial<Note> = {}): Note {
  return {
    ref,
    title: `Title for ${ref}`,
    page: 'decisions/example',
    created_at: '2026-09-16T19:07:00Z',
    updated_at: '2026-09-16T19:07:00Z',
    body: 'Body text.',
    html: '<p>Body text.</p>',
    references: [],
    revision: {
      id: '11111111-1111-4111-8111-111111111111',
      seq: 1,
      created_at: '2026-09-16T19:07:00Z',
      author_id: '22222222-2222-4222-8222-222222222222',
    },
    resolved_from: [],
    ...overrides,
  }
}

function backlinkPage(items: Array<{ ref: string; title: string; page: string }>) {
  return {
    items,
    generated: { tier: 1 as const, source: 'chronicle' as const, regenerable: true as const, notice: 'Regenerated from Chronicle’s own corpus.' },
  }
}

/** A promise this test resolves by hand, to control arrival order. */
function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

function buildRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/notes/:ref', name: 'note', component: NoteView },
      { path: '/:pathMatch(.*)*', component: { template: '<div />' } },
    ],
  })
}

describe('NoteView -- loadSeq guards on the secondary loaders', () => {
  it('drops a slow loadBacklinks answer for the previous note once a newer note has already loaded', async () => {
    const router = buildRouter()

    const backlinksRequests: Record<string, ReturnType<typeof deferred<unknown>>> = {}

    const getSpy = vi.spyOn(api, 'GET').mockImplementation(((url: string, opts: any): Promise<any> => {
      if (url === '/notes/{ref}') {
        // Resolves immediately: this test's race is about the SECONDARY
        // loader, not the note fetch `loadNote` itself already guards.
        return Promise.resolve({ data: note(opts.params.path.ref), error: undefined, response: { status: 200 } })
      }
      if (url === '/notes/{ref}/backlinks') {
        const ref = opts.params.path.ref as string
        const d = deferred<any>()
        backlinksRequests[ref] = d
        return d.promise
      }
      // provenance / tier1 / revisions -- an honest, immediate 404. None of
      // this test's assertions touch them.
      return Promise.resolve({ data: undefined, error: undefined, response: { status: 404 } })
    }) as any)

    await router.push('/notes/CHR-0001')
    await router.isReady()

    const wrapper = mount(NoteView, {
      global: {
        plugins: [router],
        stubs: { Tier1Pane: true, DiscussionThread: true, ReferenceCard: true },
      },
    })

    await flushPromises()
    expect(backlinksRequests['CHR-0001']).toBeDefined()

    // Navigate to a second note WHILE the first note's backlinks answer is
    // still in flight.
    await router.push('/notes/CHR-0002')
    await flushPromises()
    expect(backlinksRequests['CHR-0002']).toBeDefined()

    // The newer note's own backlinks answer arrives first, as the ordinary
    // (non-race) case would.
    backlinksRequests['CHR-0002'].resolve({
      data: backlinkPage([{ ref: 'CHR-0009', title: 'Current note backlink', page: 'decisions/example' }]),
      error: undefined,
      response: { status: 200 },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Current note backlink')

    // The FIRST note's slow backlinks answer resolves last -- the race this
    // ticket is about.
    backlinksRequests['CHR-0001'].resolve({
      data: backlinkPage([{ ref: 'CHR-0000', title: 'Stale backlink from the previous note', page: 'decisions/example' }]),
      error: undefined,
      response: { status: 200 },
    })
    await flushPromises()

    // The guard must have dropped it: the current note's own backlinks stay
    // on screen, and the stale answer never renders under this URL.
    expect(wrapper.text()).toContain('Current note backlink')
    expect(wrapper.text()).not.toContain('Stale backlink from the previous note')

    getSpy.mockRestore()
  })
})
