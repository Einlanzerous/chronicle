import { beforeEach, describe, expect, it, vi } from 'vitest'
import { recoverFromStaleChunk, router } from './router'

// CHRN-171: a stale lazy chunk reloads the target once. The guard must clear
// after a good navigation, or a second release in the same tab strands it.
const stale = new TypeError('error loading dynamically imported module: /app/assets/TriageView-old.js')

describe('stale chunk recovery', () => {
  beforeEach(() => sessionStorage.clear())

  it('reloads once per target and ignores other errors', () => {
    const reload = vi.fn()
    expect(recoverFromStaleChunk(new Error('boom'), '/triage', reload)).toBe(false)
    expect(recoverFromStaleChunk(stale, '/triage', reload)).toBe(true)
    expect(recoverFromStaleChunk(stale, '/triage', reload)).toBe(false)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('a successful navigation re-arms the guard for the next release', async () => {
    const reload = vi.fn()
    expect(recoverFromStaleChunk(stale, '/triage', reload)).toBe(true)
    await router.push('/sign-in').catch(() => {})
    await router.isReady().catch(() => {})
    expect(sessionStorage.getItem('ch-chunk-reload')).toBeNull()
    expect(recoverFromStaleChunk(stale, '/triage', reload)).toBe(true)
    expect(reload).toHaveBeenCalledTimes(2)
  })
})
