import type { CSSProperties } from 'react'
import type { GrammarState, HighlighterCore, LanguageRegistration, ThemeRegistration, ThemedToken } from 'shiki/types'
import type { ChangedFile, DiffLine } from './data'
import type { CodeThemeId } from './codeReviewPreferences'

export interface SyntaxToken { content: string; color?: string; fontStyle?: number; type?: number }
export interface HighlightedCode { lines: SyntaxToken[][]; foreground: string; background: string; highlighted: boolean }
export interface HighlightedDiff { old: Map<string, SyntaxToken[]>; new: Map<string, SyntaxToken[]>; foreground: string; background: string; highlighted: boolean }

const themes: Record<CodeThemeId, () => Promise<{ default: ThemeRegistration }>> = {
  'github-light': () => import('@shikijs/themes/github-light'),
  'light-plus': () => import('@shikijs/themes/light-plus'),
  'one-light': () => import('@shikijs/themes/one-light'),
  'catppuccin-latte': () => import('@shikijs/themes/catppuccin-latte'),
  'github-dark': () => import('@shikijs/themes/github-dark'),
  'dark-plus': () => import('@shikijs/themes/dark-plus'),
  'one-dark-pro': () => import('@shikijs/themes/one-dark-pro'),
  nord: () => import('@shikijs/themes/nord'),
}
const languages = {
  php: () => import('@shikijs/langs/php'),
  javascript: () => import('@shikijs/langs/javascript'),
  jsx: () => import('@shikijs/langs/jsx'),
  typescript: () => import('@shikijs/langs/typescript'),
  tsx: () => import('@shikijs/langs/tsx'),
  json: () => import('@shikijs/langs/json'),
  jsonc: () => import('@shikijs/langs/jsonc'),
  css: () => import('@shikijs/langs/css'),
  scss: () => import('@shikijs/langs/scss'),
  html: () => import('@shikijs/langs/html'),
  xml: () => import('@shikijs/langs/xml'),
  markdown: () => import('@shikijs/langs/markdown'),
  go: () => import('@shikijs/langs/go'),
  python: () => import('@shikijs/langs/python'),
  ruby: () => import('@shikijs/langs/ruby'),
  rust: () => import('@shikijs/langs/rust'),
  java: () => import('@shikijs/langs/java'),
  c: () => import('@shikijs/langs/c'),
  cpp: () => import('@shikijs/langs/cpp'),
  csharp: () => import('@shikijs/langs/csharp'),
  shellscript: () => import('@shikijs/langs/shellscript'),
  yaml: () => import('@shikijs/langs/yaml'),
  sql: () => import('@shikijs/langs/sql'),
  vue: () => import('@shikijs/langs/vue'),
  dockerfile: () => import('@shikijs/langs/dockerfile'),
  toml: () => import('@shikijs/langs/toml'),
  ini: () => import('@shikijs/langs/ini'),
  swift: () => import('@shikijs/langs/swift'),
  kotlin: () => import('@shikijs/langs/kotlin'),
} satisfies Record<string, () => Promise<{ default: LanguageRegistration[] }>>
type LanguageId = keyof typeof languages
const extensions: Record<string, LanguageId> = {
  php: 'php', phtml: 'php', php3: 'php', php4: 'php', php5: 'php',
  js: 'javascript', mjs: 'javascript', cjs: 'javascript', jsx: 'jsx', ts: 'typescript', mts: 'typescript', cts: 'typescript', tsx: 'tsx',
  json: 'json', jsonc: 'jsonc', css: 'css', scss: 'scss', html: 'html', htm: 'html', xml: 'xml', svg: 'xml', md: 'markdown', markdown: 'markdown',
  go: 'go', py: 'python', rb: 'ruby', rs: 'rust', java: 'java', c: 'c', h: 'c', cc: 'cpp', cpp: 'cpp', cxx: 'cpp', hpp: 'cpp', cs: 'csharp',
  sh: 'shellscript', bash: 'shellscript', zsh: 'shellscript', yaml: 'yaml', yml: 'yaml', sql: 'sql', vue: 'vue', toml: 'toml', ini: 'ini', swift: 'swift', kt: 'kotlin', kts: 'kotlin',
}
export function codeLanguage(path: string): LanguageId | undefined {
  const name = path.split('/').pop()?.toLowerCase() || ''
  if (name === 'dockerfile' || name.startsWith('dockerfile.')) return 'dockerfile'
  if (name === '.bashrc' || name === '.zshrc') return 'shellscript'
  return extensions[name.slice(name.lastIndexOf('.') + 1)]
}

let highlighterPromise: Promise<HighlighterCore> | undefined
const loaded = new Map<string, Promise<void>>()
function getHighlighter(): Promise<HighlighterCore> {
  if (!highlighterPromise) highlighterPromise = Promise.all([import('shiki/core'), import('shiki/engine/oniguruma')]).then(async ([core, engine]) => core.createHighlighterCore({ themes: [], langs: [], engine: engine.createOnigurumaEngine(import('shiki/wasm')) })).catch(error => { highlighterPromise = undefined; loaded.clear(); throw error })
  return highlighterPromise
}
async function loadTheme(theme: CodeThemeId): Promise<HighlighterCore> {
  const highlighter = await getHighlighter()
  const key = `theme:${theme}`
  if (!loaded.has(key)) loaded.set(key, themes[theme]().then(module => highlighter.loadTheme(module.default)).catch(error => { loaded.delete(key); throw error }))
  await loaded.get(key)
  return highlighter
}
async function loadLanguage(language: LanguageId, highlighter: HighlighterCore) {
  const key = `language:${language}`
  if (!loaded.has(key)) loaded.set(key, languages[language]().then(module => highlighter.loadLanguage(module.default)).catch(error => { loaded.delete(key); throw error }))
  await loaded.get(key)
}

const MAX_HIGHLIGHT_LINES = 6000
const MAX_HIGHLIGHT_CHARACTERS = 300_000
const MAX_CACHE_CHARACTERS = 1_000_000
const codeCache = new Map<string, { size: number; value: Promise<HighlightedCode> }>()
let cachedCharacters = 0
const plainTokens = (lines: string[]): SyntaxToken[][] => lines.map(content => content ? [{ content }] : [])

export function syntaxTokenStyle(token: SyntaxToken): CSSProperties {
  return { color: token.color, fontStyle: token.fontStyle && (token.fontStyle & 1) ? 'italic' : undefined, fontWeight: token.fontStyle && (token.fontStyle & 2) ? 600 : undefined, textDecoration: token.fontStyle && (token.fontStyle & 4) ? 'underline' : undefined }
}

async function tokenize(path: string, lines: string[], theme: CodeThemeId): Promise<HighlightedCode> {
  const fallback: HighlightedCode = { lines: plainTokens(lines), foreground: 'var(--text)', background: 'var(--surface)', highlighted: false }
  try {
    const highlighter = await loadTheme(theme)
    const palette = highlighter.getTheme(theme)
    fallback.foreground = palette.fg || fallback.foreground
    fallback.background = palette.bg || fallback.background
    const language = codeLanguage(path)
    if (!language || lines.length > MAX_HIGHLIGHT_LINES || lines.reduce((count, line) => count + line.length, 0) > MAX_HIGHLIGHT_CHARACTERS) return fallback
    await loadLanguage(language, highlighter)
    const output: SyntaxToken[][] = []
    let grammarState: GrammarState | undefined
    const phpContext = language === 'php' && !lines.some(line => /<\?(?:php\b|=)/i.test(line)) ? '<?php\n' : undefined
    for (let start = 0; start < lines.length; start += 200) {
      // Yield before each bounded chunk so settings/file navigation remain responsive.
      await new Promise<void>(resolve => setTimeout(resolve, 0))
      const chunk = lines.slice(start, start + 200)
      const result = highlighter.codeToTokens(chunk.join('\n'), { lang: language, theme, grammarState, grammarContextCode: grammarState ? undefined : phpContext, includeExplanation: 'tokenType', tokenizeMaxLineLength: 10_000, tokenizeTimeLimit: 10 })
      grammarState = result.grammarState
      for (let index = 0; index < chunk.length; index++) {
        const tokens = result.tokens[index] as ThemedToken[] | undefined
        output.push(tokens && tokens.map(token => token.content).join('') === chunk[index] ? tokens.map(({ content, color, fontStyle, type }) => ({ content, color, fontStyle, type })) : plainTokens([chunk[index]])[0])
      }
    }
    return { lines: output, foreground: fallback.foreground, background: fallback.background, highlighted: true }
  } catch { return fallback }
}

/** Full-file highlighting also supports read-only language-server definition documents. */
export function highlightCodeLines(path: string, lines: string[], theme: CodeThemeId): Promise<HighlightedCode> {
  const source = lines.join('\n')
  if (source.length > MAX_HIGHLIGHT_CHARACTERS || lines.length > MAX_HIGHLIGHT_LINES) return tokenize(path, lines, theme)
  const key = `${theme}\0${path}\0${source}`
  const existing = codeCache.get(key)
  if (existing) { codeCache.delete(key); codeCache.set(key, existing); return existing.value }
  const entry = { size: source.length + path.length, value: tokenize(path, lines, theme) }
  codeCache.set(key, entry)
  cachedCharacters += entry.size
  void entry.value.then(result => {
    // A transient chunk/engine load failure must not become a permanent plain-text cache entry.
    if (!result.highlighted && codeLanguage(path) && codeCache.get(key) === entry) {
      codeCache.delete(key)
      cachedCharacters -= entry.size
    }
  })
  while (codeCache.size > 24 || cachedCharacters > MAX_CACHE_CHARACTERS) {
    const oldest = codeCache.keys().next().value!
    cachedCharacters -= codeCache.get(oldest)!.size
    codeCache.delete(oldest)
  }
  return entry.value
}

export function syntaxLineKey(line: DiffLine, side: 'old' | 'new'): string { return `${line.hunk ?? 0}:${side === 'old' ? line.oldLine : line.newLine}` }
export async function highlightDiff(file: ChangedFile, theme: CodeThemeId): Promise<HighlightedDiff> {
  const result: HighlightedDiff = { old: new Map(), new: new Map(), foreground: 'var(--text)', background: 'var(--surface)', highlighted: false }
  const tooLarge = file.lines.length > MAX_HIGHLIGHT_LINES || file.lines.reduce((length, line) => length + line.text.length, 0) > MAX_HIGHLIGHT_CHARACTERS
  if (tooLarge) {
    const palette = await highlightCodeLines(file.path, [], theme)
    result.foreground = palette.foreground
    result.background = palette.background
    for (const line of file.lines) for (const side of ['old', 'new'] as const) {
      if ((side === 'old' ? line.oldLine : line.newLine) !== undefined) result[side].set(syntaxLineKey(line, side), plainTokens([line.text])[0])
    }
    return result
  }
  for (const side of ['old', 'new'] as const) {
    const groups: DiffLine[][] = []
    let previous: DiffLine | undefined
    for (const line of file.lines) {
      const number = side === 'old' ? line.oldLine : line.newLine
      if (number === undefined || (side === 'old' ? line.kind === 'add' : line.kind === 'remove')) continue
      const previousNumber = previous && (side === 'old' ? previous.oldLine : previous.newLine)
      if (!previous || previous.hunk !== line.hunk || previousNumber === undefined || number !== previousNumber + 1) groups.push([])
      groups[groups.length - 1].push(line)
      previous = line
    }
    for (const group of groups) {
      const code = await highlightCodeLines(side === 'old' ? file.previousPath || file.path : file.path, group.map(line => line.text), theme)
      result.foreground = code.foreground
      result.background = code.background
      result.highlighted ||= code.highlighted
      group.forEach((line, index) => result[side].set(syntaxLineKey(line, side), code.lines[index]))
    }
  }
  return result
}
