import { describe, expect, it } from 'vitest'
import { tier1RoutePath } from './tier1Link'

describe('tier1RoutePath', () => {
  it('rewrites a root-absolute in-corpus link under /tier1', () => {
    expect(tier1RoutePath('/services/postgres')).toBe('/tier1/services/postgres')
    expect(tier1RoutePath('/repos/chronicle')).toBe('/tier1/repos/chronicle')
  })

  it('leaves a protocol-relative link untouched', () => {
    expect(tier1RoutePath('//host/x')).toBeNull()
  })

  it('leaves an external link untouched', () => {
    expect(tier1RoutePath('https://github.com/Einlanzerous/chronicle')).toBeNull()
  })

  it('leaves a mailto: link untouched', () => {
    expect(tier1RoutePath('mailto:someone@example.com')).toBeNull()
  })

  it('leaves a same-page fragment untouched', () => {
    expect(tier1RoutePath('#section')).toBeNull()
  })
})
