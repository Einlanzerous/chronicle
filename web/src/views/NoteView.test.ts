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

// CHRN-128: the pin, from the provenance block. One action moves the memo
// from `PRUNES <date>` to `PINNED — KEPT`; a caller who cannot read the audio
// sees no control; and a refusal is shown rather than swallowed.
describe('NoteView -- keeping the recording', () => {
  type MemoProvenance = components['schemas']['MemoProvenance']
  const MEMO = '33333333-3333-4333-8333-333333333333'

  function provenance(overrides: Partial<MemoProvenance> = {}) {
    const item: MemoProvenance = {
      revision_seq: 1,
      revision_id: '11111111-1111-4111-8111-111111111111',
      memo_id: MEMO,
      captured_at: '2026-08-30T12:55:00Z',
      recorded_at: null,
      duration_ms: 104000,
      duration_source: 'transcript',
      audio_readable: true,
      retention_status: 'scheduled',
      prunes_at: '2026-09-29T12:55:00Z',
      audio_pruned_at: null,
      transcript: { present: true, readable: true, model: 'whisper.cpp/small.en', partial: false },
      ...overrides,
    }
    return { items: [item] }
  }

  async function mountNote(entry: Partial<MemoProvenance>, put: (opts: any) => Promise<any>) {
    let current = provenance(entry)
    vi.spyOn(api, 'GET').mockImplementation(((url: string, opts: any): Promise<any> => {
      if (url === '/notes/{ref}') return Promise.resolve({ data: note(opts.params.path.ref), error: undefined, response: { status: 200 } })
      if (url === '/notes/{ref}/provenance') return Promise.resolve({ data: current, error: undefined, response: { status: 200 } })
      return Promise.resolve({ data: undefined, error: undefined, response: { status: 404 } })
    }) as any)
    const putSpy = vi.spyOn(api, 'PUT').mockImplementation(((_url: string, opts: any): Promise<any> => put(opts)) as any)

    const router = buildRouter()
    await router.push('/notes/CHR-0001')
    await router.isReady()
    const wrapper = mount(NoteView, {
      global: { plugins: [router], stubs: { Tier1Pane: true, DiscussionThread: true, ReferenceCard: true } },
    })
    await flushPromises()
    return { wrapper, putSpy, setProvenance: (e: Partial<MemoProvenance>) => (current = provenance(e)) }
  }

  const pinButton = (wrapper: ReturnType<typeof mount>) =>
    wrapper.findAll('button').find((b) => b.text() === 'KEEP FOREVER')

  it('one click moves PRUNES <date> to PINNED — KEPT, and the control goes away', async () => {
    const { wrapper, putSpy } = await mountNote({}, () =>
      Promise.resolve({
        data: { memo_id: MEMO, retention: 'forever', retention_status: 'pinned', prunes_at: null },
        error: undefined,
        response: { status: 200 },
      }),
    )
    expect(wrapper.text()).toContain('PRUNES 2026-09-29')

    await pinButton(wrapper)!.trigger('click')
    await flushPromises()

    expect(putSpy).toHaveBeenCalledWith('/audio/{memo_id}/retention', {
      params: { path: { memo_id: MEMO } },
      body: { retention: 'forever' },
    })
    expect(wrapper.text()).toContain('PINNED — KEPT')
    expect(wrapper.text()).not.toContain('PRUNES 2026-09-29')
    expect(pinButton(wrapper)).toBeUndefined()
    vi.restoreAllMocks()
  })

  it('a caller who cannot read the audio sees no control', async () => {
    const { wrapper, putSpy } = await mountNote(
      { audio_readable: false, transcript: { present: true, readable: false, partial: false } },
      () => Promise.reject(new Error('must not be called')),
    )
    expect(pinButton(wrapper)).toBeUndefined()
    expect(wrapper.text()).not.toContain('PLAY SOURCE AUDIO')
    expect(putSpy).not.toHaveBeenCalled()
    vi.restoreAllMocks()
  })

  it('an already-pinned memo offers nothing to do', async () => {
    const { wrapper } = await mountNote({ retention_status: 'pinned', prunes_at: null }, () => Promise.reject(new Error('no')))
    expect(wrapper.text()).toContain('PINNED — KEPT')
    expect(pinButton(wrapper)).toBeUndefined()
    vi.restoreAllMocks()
  })

  it('a pin that lost to the sweep says so, and the row re-reads as pruned', async () => {
    const harness = await mountNote({}, () => {
      harness.setProvenance({ retention_status: 'pruned', prunes_at: null, audio_pruned_at: '2026-09-29T03:00:00Z' })
      return Promise.resolve({
        data: undefined,
        error: { code: 'audio_pruned', message: 'this recording was already deleted by policy, so there is nothing left to keep; the transcript remains' },
        response: { status: 410 },
      })
    })
    await pinButton(harness.wrapper)!.trigger('click')
    await flushPromises()

    expect(harness.wrapper.text()).toContain('TRANSCRIPT KEPT · AUDIO PRUNED 2026-09-29')
    expect(harness.wrapper.text()).not.toContain('PINNED — KEPT')
    expect(pinButton(harness.wrapper)).toBeUndefined()
    vi.restoreAllMocks()
  })
})
