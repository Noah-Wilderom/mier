import { highlightCodeLines, MAX_HIGHLIGHT_CHARACTERS, MAX_HIGHLIGHT_LINES, type HighlightedCode } from './syntaxHighlighter.ts'
import type { CodeThemeId } from './codeReviewPreferences'

type PHPState = { quote?: string; comment?: boolean; heredoc?: string }
const typeBadges = new Set(['Ⓒ', 'Ⓘ', 'Ⓣ', 'Ⓔ', 'ⓘ'])
const declaration = /^\s*(?:(?:abstract|final|static|readonly|public|protected|private)\s+)*(?:function|class|interface|trait|enum)\b/

/** Phpactor's PHP hover is a declaration outline, not an executable PHP file. */
export function prepareSymbolSignature(source: string, language: string) {
  if (language.toLowerCase() !== 'php') return { lines: source.split('\n'), starts: [0] }
  // Only remove the opening scaffold, never a tag in a string or documentation.
  source = source.replace(/^\s*<\?php(?=\s|$)[ \t]*(?:\r?\n)?/i, '')
  const starts = [0], state: PHPState = {}
  const lines = source.split(/\r?\n/).map((line, index) => {
    const outside = !state.quote && !state.comment && !state.heredoc
    const display = cleanPHPLine(line, state)
    // An inferred type (e.g. Entity[] or Collection<T>) must not leave the
    // following member inside an unfinished PHP return-type grammar scope.
    if (index > 0 && outside && declaration.test(display)) starts.push(index)
    return display
  })
  return { lines, starts }
}

function cleanPHPLine(line: string, state: PHPState): string {
  if (state.heredoc) {
    const end = line.trimStart()
    if (!end.startsWith(state.heredoc) || /[a-zA-Z0-9_]/.test(end[state.heredoc.length] || '')) return line
    state.heredoc = undefined
  }
  let result = ''
  for (let index = 0; index < line.length; index++) {
    const char = line[index], next = line[index + 1]
    if (state.comment) {
      result += char
      if (char === '*' && next === '/') { result += next; index++; state.comment = false }
    } else if (state.quote) {
      result += char
      if (char === '\\' && next !== undefined) { result += next; index++ }
      else if (char === state.quote) state.quote = undefined
    } else if (char === '/' && next === '/' || char === '#' && next !== '[') {
      return result + line.slice(index)
    } else if (char === '/' && next === '*') {
      result += '/*'; index++; state.comment = true
    } else if (char === "'" || char === '"' || char === '`') {
      result += char; state.quote = char
    } else if (line.startsWith('<<<', index)) {
      const marker = line.slice(index).match(/^<<<[ \t]*(?:'([a-zA-Z_][a-zA-Z0-9_]*)'|"([a-zA-Z_][a-zA-Z0-9_]*)"|([a-zA-Z_][a-zA-Z0-9_]*))/)
      if (marker) { state.heredoc = marker[1] || marker[2] || marker[3]; return result + line.slice(index) }
      result += char
    } else if (typeBadges.has(char) && /\s/.test(next || '') && (index === 0 || !/[\p{L}\p{N}_$]/u.test(line[index - 1]))) {
      // These glyphs describe a reflected class/interface/trait/enum. The
      // ordinary type name conveys that information without the decoration.
      while (index + 1 < line.length && /[ \t]/.test(line[index + 1])) index++
    } else result += char
  }
  return result
}

export async function highlightSymbolSignature(path: string, source: string, language: string, theme: CodeThemeId): Promise<HighlightedCode> {
  const { lines, starts } = prepareSymbolSignature(source, language)
  if (lines.length > MAX_HIGHLIGHT_LINES || lines.reduce((size, line) => size + line.length, 0) > MAX_HIGHLIGHT_CHARACTERS) return highlightCodeLines(path, lines, theme, 'signature')
  let result: HighlightedCode | undefined
  // Keep PHP declaration groups independent while preserving multiline
  // parameters, comments, and literals within each group.
  for (const [index, start] of starts.entries()) {
    const part = await highlightCodeLines(path, lines.slice(start, starts[index + 1] ?? lines.length), theme, 'signature')
    if (!result) result = { ...part, lines: [] }
    result.lines.push(...part.lines)
    result.highlighted &&= part.highlighted
  }
  return result!
}
