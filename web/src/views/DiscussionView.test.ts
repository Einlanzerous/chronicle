import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { api } from '@/api/client'
import DiscussionView from './DiscussionView.vue'
import type { components } from '@/api/schema.d.ts'

// CHRN-111: every mutation handler reloads after its write. It must reload
// the discussion the ROUTE now names, not the one that was on screen when
// the handler started. `load` is guarded by `loadSeq`, so a reload of the
// captured ref takes a newer seq than the new route's own load and the
// guard then drops the right answer and keeps the wrong one. Run against
// the pre-fix code (`load(thread.value.discussion.ref)` in `submitReply`)
// this test fails: DSC-0001's title is left on screen under /discussions/DSC-0002.

type Thread = components['schemas']['Thread']

function thread(ref: string): Thread {
  return {
    discussion: { ref, title: `Title for ${ref}`, created_at: '2026-10-01T12:00:00Z' },
    turns: [],
    participants: [],
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

function ok<T>(data: T) {
  return { data, error: undefined, response: { status: 200 } }
}

function buildRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/discussions/:ref', name: 'discussion', component: DiscussionView },
      { path: '/:pathMatch(.*)*', component: { template: '<div />' } },
    ],
  })
}

describe('DiscussionView -- a reload after a mutation follows the live route', () => {
  it('leaves the newer discussion on screen when the operator navigates while a reply is in flight', async () => {
    const router = buildRouter()

    // The first GET for each ref is held by the test when `hold` names it;
    // every other GET answers at once.
    const held: Record<string, ReturnType<typeof deferred<unknown>>> = {}
    const hold = new Set<string>(['DSC-0002'])
    const getCalls: string[] = []
    const getSpy = vi.spyOn(api, 'GET').mockImplementation(((url: string, opts: any): Promise<any> => {
      if (url !== '/discussions/{ref}') throw new Error(`unexpected GET ${url}`)
      const ref = opts.params.path.ref as string
      getCalls.push(ref)
      if (hold.has(ref)) {
        hold.delete(ref)
        const d = deferred<unknown>()
        held[ref] = d
        return d.promise
      }
      return Promise.resolve(ok(thread(ref)))
    }) as any)

    const reply = deferred<unknown>()
    const postSpy = vi.spyOn(api, 'POST').mockImplementation(((url: string): Promise<any> => {
      if (url !== '/discussions/{ref}/turns') throw new Error(`unexpected POST ${url}`)
      return reply.promise
    }) as any)

    await router.push('/discussions/DSC-0001')
    await router.isReady()
    const wrapper = mount(DiscussionView, {
      global: { plugins: [router], stubs: { DiscussionThread: true } },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Title for DSC-0001')

    // Post a reply on DSC-0001 and leave it in flight.
    await wrapper.find('textarea').setValue('A reply to the first thread.')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(postSpy).toHaveBeenCalledTimes(1)

    // Navigate to DSC-0002 while the reply is still in flight; its own load
    // is held.
    await router.push('/discussions/DSC-0002')
    await flushPromises()
    expect(held['DSC-0002']).toBeDefined()

    // The reply lands, and its handler reloads.
    reply.resolve(ok({}))
    await flushPromises()

    // DSC-0002's original load answers last; whichever load the guard keeps,
    // the screen must show the discussion the URL names.
    held['DSC-0002'].resolve(ok(thread('DSC-0002')))
    await flushPromises()

    expect(getCalls.filter((r) => r === 'DSC-0001')).toHaveLength(1)
    expect(wrapper.text()).toContain('Title for DSC-0002')
    expect(wrapper.text()).not.toContain('Title for DSC-0001')

    getSpy.mockRestore()
    postSpy.mockRestore()
  })
})
