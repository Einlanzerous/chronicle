// Pure page-tree logic, kept out of the component so it can be unit tested
// without mounting anything (CHRN-58). `listPages` answers a flat, sorted
// list of every page's current path -- "estate is the parent of
// estate/conventions" is a property of the strings themselves (openapi.yaml,
// PageTree's description) -- and this turns that list into the nested shape
// the sidebar's TIER 2 · AUTHORED tree draws with indentation, board 1c.

export interface PageTreeNode {
  /** The full path, e.g. `estate/storage/amber`. */
  path: string
  /** The last path segment -- what a tree row actually shows. */
  label: string
  /** 0 for a root page, 1 for its direct child, and so on -- the tree's own indent unit. */
  depth: number
  children: PageTreeNode[]
}

/**
 * Nests a flat, sorted path list into a tree. `createPage` requires every
 * ancestor segment to already exist (openapi.yaml), so in the ordinary case
 * every parent path is itself a row in `paths` -- but a tree renders
 * honestly rather than dropping a row, so a path whose parent is missing
 * from the list still appears, at the root, rather than vanishing.
 */
export function buildPageTree(paths: readonly string[]): PageTreeNode[] {
  const sorted = [...paths].sort()
  const byPath = new Map<string, PageTreeNode>()
  const roots: PageTreeNode[] = []

  for (const path of sorted) {
    if (path === '') continue
    const segments = path.split('/')
    const node: PageTreeNode = {
      path,
      label: segments[segments.length - 1],
      depth: segments.length - 1,
      children: [],
    }
    byPath.set(path, node)

    if (segments.length === 1) {
      roots.push(node)
      continue
    }
    const parentPath = segments.slice(0, -1).join('/')
    const parent = byPath.get(parentPath)
    if (parent) {
      parent.children.push(node)
    } else {
      // Defensive: an ancestor is missing from the list. Should not happen
      // per the contract above, but a dropped row is worse than a
      // mis-nested one.
      roots.push(node)
    }
  }
  return roots
}

/**
 * Flattens a tree back into the sidebar's own render order -- depth-first,
 * parent before children -- because board 1c draws indentation as left
 * padding on a flat list of rows, not as a collapsible tree with disclosure
 * triangles.
 */
export function flattenPageTree(nodes: readonly PageTreeNode[]): PageTreeNode[] {
  const out: PageTreeNode[] = []
  const visit = (list: readonly PageTreeNode[]): void => {
    for (const node of list) {
      out.push(node)
      visit(node.children)
    }
  }
  visit(nodes)
  return out
}

/**
 * The one-line mono notice `/app/pages/:path*` shows when `listNotes`
 * answers `moved_from` (openapi.yaml's `NoteList.moved_from`): the path
 * asked for was a redirect left by a page move, and the client should say
 * so rather than silently rendering the new page under the old URL.
 */
export function formatMovedNotice(movedFrom: string, canonicalPath: string): string {
  return `This page moved from ${movedFrom} — now at ${canonicalPath}.`
}
