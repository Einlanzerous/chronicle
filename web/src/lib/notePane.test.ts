import { describe, expect, it } from 'vitest'
import { isValidTier1Path, tier1PathForNotePage } from './notePane'

describe('isValidTier1Path', () => {
  it('accepts an ordinary relative, slash-separated path', () => {
    expect(isValidTier1Path('estate/storage/amber')).toBe(true)
  })

  it('accepts a single-segment path', () => {
    expect(isValidTier1Path('estate')).toBe(true)
  })

  it('rejects an empty path', () => {
    expect(isValidTier1Path('')).toBe(false)
  })

  it('rejects a leading slash', () => {
    expect(isValidTier1Path('/estate/storage')).toBe(false)
  })

  it('rejects a trailing slash', () => {
    expect(isValidTier1Path('estate/storage/')).toBe(false)
  })

  it('rejects an empty segment (a doubled slash)', () => {
    expect(isValidTier1Path('estate//storage')).toBe(false)
  })

  it('rejects a dot-prefixed segment, including .. (matching fs.ValidPath)', () => {
    expect(isValidTier1Path('estate/../storage')).toBe(false)
    expect(isValidTier1Path('.hidden/storage')).toBe(false)
  })

  it('rejects a .md suffix -- the corpus\'s own address never carries the extension', () => {
    expect(isValidTier1Path('estate/storage.md')).toBe(false)
  })
})

describe('tier1PathForNotePage', () => {
  it('returns the trimmed path when valid', () => {
    expect(tier1PathForNotePage('  estate/storage/amber  ')).toBe('estate/storage/amber')
  })

  it('returns null for a path the corpus could never hold', () => {
    expect(tier1PathForNotePage('/estate/storage')).toBeNull()
  })
})
