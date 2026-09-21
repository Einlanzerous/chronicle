/// <reference types="node" />
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import faviconSvg from '../../public/favicon.svg?raw'
import indexHtml from '../../index.html?raw'

// CHRN-115. The favicon is a standalone SVG document, so it cannot read the
// page's custom properties: its two colours are literals. That is the one place
// a colour is written outside tokens.css, and scripts/check-tokens.sh only
// scans web/src, so neither the script nor the compiler would notice the
// favicon going stale when a token moves. This is what does.

// Read with fs, not `?raw`: vitest replaces every CSS module with an empty
// string unless css processing is switched on, `?raw` and `?inline` included,
// and switching it on would push tokens.css's @fontsource imports through
// Vite's CSS pipeline for the sake of one regex. An empty read fails loudly in
// token() below rather than passing on nothing.
const tokensCss = readFileSync(resolve(import.meta.dirname, '../styles/tokens.css'), 'utf8')

function token(name: string): string {
  const found = new RegExp(`${name}:\\s*(#[0-9a-fA-F]{6})\\b`).exec(tokensCss)
  if (!found) throw new Error(`${name} is not a six-digit hex token in tokens.css`)
  return found[1].toLowerCase()
}

// Comments carry prose, and prose can mention a colour.
const svg = faviconSvg.replace(/<!--[\s\S]*?-->/g, '')

describe('favicon.svg', () => {
  it('draws the plate in the base token and the glyph in the signal token', () => {
    const plate = /<path\s+fill="(#[0-9a-fA-F]{6})"/.exec(svg)?.[1].toLowerCase()
    const glyph = /<g\s+fill="(#[0-9a-fA-F]{6})"/.exec(svg)?.[1].toLowerCase()

    expect(plate).toBe(token('--ch-base'))
    expect(glyph).toBe(token('--ch-signal'))
  })

  it('uses no colour beyond those two', () => {
    const used = new Set((svg.match(/#[0-9a-fA-F]{6}\b/g) ?? []).map((c) => c.toLowerCase()))

    expect([...used].sort()).toEqual([token('--ch-base'), token('--ch-signal')].sort())
  })

  it('is square, because a tab and a touch icon both are', () => {
    expect(faviconSvg).toContain('viewBox="0 0 32 32"')
  })
})

describe('index.html icon links', () => {
  // Keys only: nothing is loaded, so binary files cost nothing.
  const inPublic = Object.keys(import.meta.glob('../../public/*', { query: '?raw' })).map((p) =>
    p.replace('../../public', ''),
  )

  const links = [...indexHtml.matchAll(/<link\b[^>]*>/g)]
    .map((m) => m[0])
    .filter((tag) => /\brel="[^"]*icon[^"]*"/.test(tag))
    .map((tag) => /\bhref="([^"]+)"/.exec(tag)?.[1] ?? '')

  it('declares the SVG, its PNG fallback and the touch icon', () => {
    expect(links.sort()).toEqual(['/apple-touch-icon.png', '/favicon-32.png', '/favicon.svg'])
  })

  it('points every one at a file that exists in public/', () => {
    // A link to a file that is not there does not 404 under /app/: the bundle
    // falls back to index.html for any unknown path, so the tab would quietly
    // show a blank icon and nothing anywhere would say why.
    for (const href of links) {
      expect(inPublic, `${href} is linked from index.html but missing from web/public`).toContain(href)
    }
  })
})
