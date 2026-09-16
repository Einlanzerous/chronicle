import { describe, expect, it } from 'vitest'
import { buildPageTree, flattenPageTree, formatMovedNotice } from './pageTree'

describe('buildPageTree', () => {
  it('nests children under their parent path', () => {
    const tree = buildPageTree([
      'estate',
      'estate/conventions',
      'estate/storage',
      'estate/storage/amber',
      'estate/storage/amber/index-rebuild',
      'estate/storage/copyparty',
    ])

    expect(tree).toHaveLength(1)
    const estate = tree[0]
    expect(estate.path).toBe('estate')
    expect(estate.label).toBe('estate')
    expect(estate.depth).toBe(0)
    expect(estate.children.map((c) => c.path)).toEqual(['estate/conventions', 'estate/storage'])

    const storage = estate.children.find((c) => c.path === 'estate/storage')!
    expect(storage.depth).toBe(1)
    expect(storage.children.map((c) => c.path)).toEqual([
      'estate/storage/amber',
      'estate/storage/copyparty',
    ])

    const amber = storage.children.find((c) => c.path === 'estate/storage/amber')!
    expect(amber.children).toHaveLength(1)
    expect(amber.children[0].path).toBe('estate/storage/amber/index-rebuild')
    expect(amber.children[0].label).toBe('index-rebuild')
    expect(amber.children[0].depth).toBe(3)
  })

  it('sorts unsorted input before nesting, so render order does not depend on API order', () => {
    const tree = buildPageTree(['estate/storage', 'estate', 'estate/conventions'])
    expect(tree[0].children.map((c) => c.label)).toEqual(['conventions', 'storage'])
  })

  it('supports more than one root', () => {
    const tree = buildPageTree(['estate', 'services'])
    expect(tree.map((n) => n.path)).toEqual(['estate', 'services'])
  })

  it('does not drop a row whose parent is missing from the list', () => {
    // Defends against a corpus that disagrees with its own contract rather
    // than assuming it cannot happen.
    const tree = buildPageTree(['estate/storage/amber'])
    expect(tree).toHaveLength(1)
    expect(tree[0].path).toBe('estate/storage/amber')
  })

  it('ignores an empty path', () => {
    expect(buildPageTree([''])).toEqual([])
  })
})

describe('flattenPageTree', () => {
  it('walks depth-first, parent before children, matching the sidebar row order', () => {
    const tree = buildPageTree([
      'estate',
      'estate/conventions',
      'estate/storage',
      'estate/storage/amber',
      'services',
    ])
    const flat = flattenPageTree(tree)
    expect(flat.map((n) => n.path)).toEqual([
      'estate',
      'estate/conventions',
      'estate/storage',
      'estate/storage/amber',
      'services',
    ])
  })
})

describe('formatMovedNotice', () => {
  it('names both the old and the canonical path', () => {
    expect(formatMovedNotice('estate/old-name', 'estate/new-name')).toBe(
      'This page moved from estate/old-name — now at estate/new-name.',
    )
  })
})
