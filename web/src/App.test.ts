import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import App from './App.vue'

// CHRN-171: a client-side navigation is silent to a screen reader, so the
// root announces each route's meta.title in a polite live region.
describe('App route announcer', () => {
  it('announces the page name on navigation', async () => {
    const stub = { template: '<div />' }
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/a', component: stub, meta: { title: 'Alpha' } },
        { path: '/b', component: stub, meta: { title: 'Beta' } },
      ],
    })
    await router.push('/a')
    const wrapper = mount(App, { global: { plugins: [router] } })
    const live = wrapper.get('[data-test="route-announcer"]')
    expect(live.attributes('aria-live')).toBe('polite')
    expect(live.text()).toBe('Alpha')
    await router.push('/b')
    await flushPromises()
    expect(live.text()).toBe('Beta')
  })
})
