import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { api } from '@/api/client'
import { currentUser } from '@/auth'
import AccountView from './AccountView.vue'

// CHRN-171: the display-name field used to save on `@keyup.enter`. An IME
// user commits a composition with Enter, which fires that keyup, so a
// half-typed name was saved. The field now lives in a <form>, and the browser
// does not implicit-submit during composition. jsdom has no IME, so this pins
// the two halves of the contract: a keyup Enter alone never saves, and a
// form submit does.

const me = {
  id: '33333333-3333-4333-8333-333333333333',
  email: 'someone@example.invalid',
  display_name: 'Original',
  kind: 'person' as const,
  is_owner: false,
}

function stubApi() {
  const patch = vi.spyOn(api, 'PATCH').mockResolvedValue({ data: { ...me, display_name: 'Changed' }, response: { status: 200 } } as any)
  vi.spyOn(api, 'GET').mockResolvedValue({ data: undefined, error: undefined, response: { status: 404 } } as any)
  vi.spyOn(api, 'POST').mockResolvedValue({ data: undefined, error: undefined, response: { status: 404 } } as any)
  return patch
}

afterEach(() => {
  vi.restoreAllMocks()
  currentUser.value = null
})

describe('AccountView display-name form', () => {
  it('does not save on a bare keyup Enter (the IME commit)', async () => {
    currentUser.value = me
    const patch = stubApi()
    const wrapper = mount(AccountView, { global: { stubs: { InviteReveal: true } } })
    await flushPromises()

    const input = wrapper.get('input.ch-account-name-input')
    await input.setValue('Changed')
    await input.trigger('keyup', { key: 'Enter' })
    await flushPromises()

    expect(patch).not.toHaveBeenCalled()
  })

  it('saves once on form submit', async () => {
    currentUser.value = me
    const patch = stubApi()
    const wrapper = mount(AccountView, { global: { stubs: { InviteReveal: true } } })
    await flushPromises()

    await wrapper.get('input.ch-account-name-input').setValue('Changed')
    await wrapper.get('form.ch-account-name-row').trigger('submit')
    await flushPromises()

    expect(patch).toHaveBeenCalledTimes(1)
    expect(patch.mock.calls[0][1]).toEqual({ body: { display_name: 'Changed' } })
  })
})
