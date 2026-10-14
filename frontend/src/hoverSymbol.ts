/** Caret offsets and RegExp indexes both use UTF-16, matching LSP coordinates. */
export function hoverSymbolSpan(line: string, character: number): [number, number] | undefined {
  for (const match of line.matchAll(/[$_\p{L}][$_\p{L}\p{N}\p{M}]*/gu)) {
    const start = match.index!
    const end = start + match[0].length
    if (character >= start && character < end) return [start, end]
  }
  return undefined
}
