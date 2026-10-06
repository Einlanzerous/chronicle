import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { api } from '@/api/client'
import { triageWaiting } from '@/lib/triageCount'
import TriageView from './TriageView.vue'
import type { components } from '@/api/schema.d.ts'

// CHRN-55's Done-when, driven the way it is worded: forty memos triaged with
// nothing but keydown events, against a fake server that behaves like the
// real one in the two ways that matter here -- it hands over ONE SCREEN (25)
// at a time, and it answers per item, so one memo can fail in a batch that
// otherwise lands.

type BatchItem = components['schemas']['BatchItem']
type TriageDecision = components['schemas']['TriageDecision']
type TriageResult = components['schemas']['TriageResult']

const LIMIT = 25

function memo(i: number, overrides: Partial<BatchItem> = {}): BatchItem {
  return {
    memo_id: `00000000-0000-4000-8000-${String(i).padStart(12, '0')}`,
    captured_at: '2026-10-03T20:16:00-05:00',
    duration_ms: 41000,
    excerpt: `Memo ${i} said something worth keeping.`,
    proposer: 'ollama/gemma4:e4b',
    generation: 1,
    status: 'valid',
    pre_acceptable: true,
    proposal: {
      generated: { tier: 1, source: 'chronicle', regenerable: true, notice: 'Regenerated.' } as never,
      destination: 'NOTE',
      confidence: 0.9,
      reason: 'Records a decision',
      nearest_page: null,
      title: `Title ${i}`,
      body: 'Body.',
      verb: 'create',
      page_path: 'estate',
    },
    ...overrides,
  }
}

/** The server's side: what is still untriaged, and every decision it took. */
function fakeServer(memos: BatchItem[], answer: (d: TriageDecision) => TriageResult['status'] = () => 'applied') {
  const untriaged = [...memos]
  const decisions: TriageDecision[] = []
  const held: string[] = []
  const drop = (id: string) => {
    const at = untriaged.findIndex((m) => m.memo_id === id)
    if (at >= 0) untriaged.splice(at, 1)
  }
  const ok = (data: unknown) => Promise.resolve({ data, error: undefined, response: { status: 200 } })

  vi.spyOn(api, 'GET').mockImplementation(((url: string): Promise<any> => {
    if (url === '/triage/batch') return ok({ items: untriaged.slice(0, LIMIT), limit: LIMIT })
    if (url === '/triage/deferred') return ok({ items: [], limit: LIMIT })
    return Promise.resolve({ data: undefined, error: undefined, response: { status: 404 } })
  }) as any)

  vi.spyOn(api, 'POST').mockImplementation(((url: string, opts: any): Promise<any> => {
    if (url === '/triage/accept') {
      const results = (opts.body.items as TriageDecision[]).map((d) => {
        decisions.push(d)
        const status = answer(d)
        if (status === 'applied') drop(d.memo_id)
        return { memo_id: d.memo_id, status, note_ref: status === 'applied' ? 'CHR-0001' : undefined, reason: status === 'applied' ? undefined : 'switchyard timed out' }
      })
      return ok({ results })
    }
    if (url === '/triage/hold') {
      held.push(opts.body.memo_id)
      drop(opts.body.memo_id)
      return ok({ memo_id: opts.body.memo_id, captured_at: 'x', held_by: 'u', held_at: 'x', age_seconds: 0 })
    }
    return Promise.resolve({ data: undefined, error: undefined, response: { status: 404 } })
  }) as any)

  return { untriaged, decisions, held }
}

let mounted: VueWrapper | null = null

async function mountView(): Promise<VueWrapper> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
  })
  await router.push('/triage')
  await router.isReady()
  mounted = mount(TriageView, { global: { plugins: [router] }, attachTo: document.body })
  await flushPromises()
  return mounted
}

async function press(key: string): Promise<void> {
  window.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }))
  await flushPromises()
}

afterEach(() => {
  // A test that failed before its own unmount must not leave its window
  // listener deciding rows in the next one.
  mounted?.unmount()
  mounted = null
  vi.restoreAllMocks()
  triageWaiting.value = null
  document.body.innerHTML = ''
})

describe('TriageView -- forty memos at a keyboard', () => {
  it('triages forty memos across two screens with keydown events alone', async () => {
    const server = fakeServer(Array.from({ length: 40 }, (_, i) => memo(i + 1)))
    const wrapper = await mountView()

    // One screen, and the count says it is not the whole of it.
    expect(wrapper.findAll('.ch-tri-row')).toHaveLength(25)
    expect(wrapper.find('.ch-tri-counts').text()).toBe('25+ MEMOS · 25 PRE-FILLED · 0 NEED INPUT · 0 FAILED')
    expect(triageWaiting.value).toEqual({ count: 25, more: true })

    // A: the whole first screen in one request -- which then refills itself.
    await press('a')
    expect(server.decisions).toHaveLength(25)
    expect(server.decisions.every((d) => d.override === undefined && d.confirm_edit === undefined)).toBe(true)
    expect(wrapper.findAll('.ch-tri-row.is-prefilled')).toHaveLength(15)

    // The second screen one row at a time: ⏎ ×13, then H, then D. Focus
    // advances on its own after each decision -- no J in between.
    for (let i = 0; i < 13; i++) await press('Enter')
    await press('h')
    await press('d')

    expect(server.decisions).toHaveLength(38)
    expect(server.held).toHaveLength(1)
    expect(wrapper.find('.ch-tri-foot-counts').text()).toBe('38 ACCEPTED · 0 EDITED · 1 HELD · 1 DISCARDED · 0 REMAINING')
    expect(triageWaiting.value).toEqual({ count: 0, more: false })

    // The discard is inside its undo window: drawn, not yet sent.
    expect(wrapper.find('.ch-tri-row.is-discarding').text()).toContain('DISCARDED · UNDO R · 10 MIN')
    expect(server.untriaged).toHaveLength(1)

    // Leaving the screen closes the window and sends it.
    wrapper.unmount()
    await flushPromises()
    expect(server.decisions).toHaveLength(39)
    expect(server.decisions[38].override).toEqual({ destination: 'DISCARD' })
    expect(server.untriaged).toHaveLength(0)
  })

  it('R takes a discard back before anything is sent', async () => {
    const server = fakeServer([memo(1)])
    const wrapper = await mountView()
    await press('d')
    expect(wrapper.find('.ch-tri-row').classes()).toContain('is-discarding')
    await press('r')
    expect(wrapper.find('.ch-tri-row').classes()).toContain('is-prefilled')
    wrapper.unmount()
    await flushPromises()
    expect(server.decisions).toHaveLength(0)
  })

  it('a failed item is visibly still pending, and ACCEPT ALL does not sweep it up again', async () => {
    const failing = memo(2).memo_id
    const server = fakeServer([memo(1), memo(2), memo(3)], (d) => (d.memo_id === failing ? 'failed' : 'applied'))
    const wrapper = await mountView()

    await press('a')

    const rows = wrapper.findAll('.ch-tri-row')
    expect(rows[0].classes()).toContain('is-accepted')
    expect(rows[2].classes()).toContain('is-accepted')
    expect(rows[1].classes()).toContain('is-problem')
    expect(rows[1].classes()).toContain('is-stuck')
    expect(rows[1].text()).toContain('FAILED · STILL PENDING')
    expect(rows[1].text()).toContain('switchyard timed out')
    expect(rows[1].text()).toContain('RETRY')
    expect(wrapper.find('.ch-tri-counts').text()).toBe('1 MEMO · 0 PRE-FILLED · 0 NEED INPUT · 1 FAILED')
    expect(wrapper.find('.ch-tri-foot-counts').text()).toBe('2 ACCEPTED · 0 EDITED · 0 HELD · 0 DISCARDED · 1 REMAINING')
    // The sidebar badge follows without a reload.
    expect(triageWaiting.value).toEqual({ count: 1, more: false })

    // A second A is a no-op for it; ⏎ on the row is the retry.
    await press('a')
    expect(server.decisions).toHaveLength(3)
    wrapper.unmount()
  })

  it('a row the server already refused says ACCEPT tries it again, and ⏎ sends it unchanged', async () => {
    const refused = memo(1, {
      link: {
        destination: 'TICKET',
        state: 'refused',
        decided_at: '2026-10-04T09:00:00-05:00',
        refused_status: 403,
        refused_reason: 'requires scope(s): tickets:write',
        refused_at: '2026-10-04T09:00:01-05:00',
      } as never,
    })
    const server = fakeServer([refused])
    const wrapper = await mountView()

    const row = wrapper.find('.ch-tri-row')
    expect(row.text()).toContain('REFUSED 403 · STILL PENDING')
    expect(row.text()).toContain('requires scope(s): tickets:write ACCEPT tries it again.')
    expect(row.text()).not.toContain('the same decision is refused the same way')

    await press('Enter')
    expect(server.decisions).toHaveLength(1)
    expect(server.decisions[0].memo_id).toBe(refused.memo_id)
    expect(server.decisions[0].override).toBeUndefined()
    wrapper.unmount()
  })

  it('a row refused in this session is retried unchanged by ⏎, not sent to the editor', async () => {
    let fixed = false
    const server = fakeServer([memo(1)], () => (fixed ? 'applied' : 'refused'))
    const wrapper = await mountView()

    await press('Enter')
    const row = wrapper.find('.ch-tri-row')
    expect(row.text()).toContain('REFUSED · STILL PENDING')
    expect(row.text()).toContain('RETRY')

    fixed = true
    await press('Enter')
    expect(server.decisions).toHaveLength(2)
    expect(server.decisions[1].override).toBeUndefined()
    expect(wrapper.find('.ch-tri-row').classes()).toContain('is-accepted')
    wrapper.unmount()
  })

  it('E opens the editor, keys typed into it decide nothing, and ⏎ sends a whole override', async () => {
    const server = fakeServer([
      memo(1, { status: 'needs_input', pre_acceptable: false, proposal: { ...memo(1).proposal!, page_path: undefined } }),
      memo(2),
    ])
    const wrapper = await mountView()

    // ⏎ cannot accept a proposal that needs input: it opens the editor.
    await press('Enter')
    expect(server.decisions).toHaveLength(0)
    const title = wrapper.find<HTMLInputElement>('.ch-tri-editor input[type="text"]')
    expect(document.activeElement).toBe(title.element)

    // "h" and "d" typed into the title are letters, not HOLD and DISCARD.
    title.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'h', bubbles: true }))
    title.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'd', bubbles: true }))
    await flushPromises()
    expect(server.held).toHaveLength(0)
    expect(wrapper.find('.ch-tri-editor').exists()).toBe(true)

    // No page yet: refused client-side, in words, and nothing is sent.
    await wrapper.find('.ch-tri-editor').trigger('submit')
    expect(wrapper.find('.ch-tri-editor-error').text()).toMatch(/page/)
    expect(server.decisions).toHaveLength(0)

    await title.setValue('A better title')
    await wrapper.findAll<HTMLInputElement>('.ch-tri-editor input[type="text"]')[1].setValue('estate / storage')
    await wrapper.find('.ch-tri-editor').trigger('submit')
    await flushPromises()

    expect(server.decisions).toHaveLength(1)
    expect(server.decisions[0].override).toEqual({
      destination: 'NOTE',
      title: 'A better title',
      verb: 'create',
      body: 'Body.',
      page_path: 'estate/storage',
    })
    expect(wrapper.findAll('.ch-tri-row')[0].text()).toContain('· EDITED')
    expect(wrapper.find('.ch-tri-foot-counts').text()).toMatch(/^0 ACCEPTED · 1 EDITED/)
    wrapper.unmount()
  })

  it('a held key never decides: auto-repeat accepts one row, not a run of them', async () => {
    const server = fakeServer([memo(1), memo(2), memo(3)])
    const wrapper = await mountView()
    await press('Enter')
    for (let i = 0; i < 5; i++) {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', repeat: true, bubbles: true }))
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'd', repeat: true, bubbles: true }))
    }
    await flushPromises()
    expect(server.decisions).toHaveLength(1)
    expect(wrapper.findAll('.ch-tri-row.is-prefilled')).toHaveLength(2)
    wrapper.unmount()
  })

  it('esc cancels an edit and sends nothing', async () => {
    const server = fakeServer([memo(1)])
    const wrapper = await mountView()
    await press('e')
    expect(wrapper.find('.ch-tri-editor').exists()).toBe(true)
    await press('Escape')
    expect(wrapper.find('.ch-tri-editor').exists()).toBe(false)
    expect(server.decisions).toHaveLength(0)
    wrapper.unmount()
  })
})
