// @ts-nocheck -- Node's native TypeScript runner exercises the production tokenization module.
import test from 'node:test'
import assert from 'node:assert/strict'
import { codeLanguage, highlightCodeLines, highlightDiff, syntaxLineKey } from './syntaxHighlighter.ts'
import { DARK_CODE_THEMES, LIGHT_CODE_THEMES } from './codeReviewPreferences.ts'

const content = tokens => tokens.map(token => token.content).join('')
test('PHP fragments retain PHPDoc comment scope and distinguish real keywords and strings', async () => {
  const lines = ['/**', ' * @return array{name: string}', ' */', 'public function pages(): array', '{', "\treturn ['home', 'about'];", '}']
  const code = await highlightCodeLines('app/ManageSitemaps.php', lines, 'github-light')
  assert.equal(code.highlighted, true)
  assert.deepEqual(code.lines.map(content), lines)
  assert.ok(code.lines[1].every(token => token.type === 1), 'PHPDoc types must remain inside comment grammar scope')
  assert.ok(code.lines[3].some(token => token.content === 'public' && token.type === 0))
  assert.ok(code.lines[5].some(token => token.content === "'home'" && token.type === 2))
  assert.ok(code.lines[5].some(token => token.content === '];' && token.type === 0), 'Closing quotes must end the string before array punctuation')
  assert.notEqual(code.lines[1].find(token => token.content === ': string}').color, code.lines[3].find(token => token.content === 'public').color)
})

test('multiline comments and escaped strings preserve state across async chunks and empty lines', async () => {
  const lines = ['/*', ...Array.from({ length: 205 }, () => 'return null inside a block comment'), '*/', '$message = "first', 'second \\"quoted\\"', 'third";', '', '\treturn "😀";', '  ', '']
  const code = await highlightCodeLines('multi.php', lines, 'github-dark')
  assert.deepEqual(code.lines.map(content), lines)
  assert.ok(code.lines[201].every(token => token.type === 1), 'Chunk boundaries must retain the open comment state')
  assert.ok(code.lines[208].every(token => token.type === 2), 'Multiline escaped string lost its grammar state')
  assert.deepEqual(code.lines[210], [])
  assert.deepEqual(code.lines.at(-1), [])
})

test('old/new diff sequences have independent grammar state and reset across omitted hunks', async () => {
  const lines = [
    { hunk: 0, kind: 'context', oldLine: 1, newLine: 1, text: '/*' },
    { hunk: 0, kind: 'remove', oldLine: 2, text: 'old comment' },
    { hunk: 0, kind: 'remove', oldLine: 3, text: '*/' },
    { hunk: 0, kind: 'add', newLine: 2, text: 'new comment' },
    { hunk: 0, kind: 'add', newLine: 3, text: 'still comment' },
    { hunk: 0, kind: 'context', oldLine: 4, newLine: 4, text: '$value = 1;' },
    { hunk: 0, kind: 'add', newLine: 5, text: '*/' },
    { hunk: 0, kind: 'context', oldLine: 5, newLine: 6, text: "return 'end';" },
    { hunk: 1, kind: 'context', oldLine: 20, newLine: 21, text: "return 'fresh';" },
  ]
  const before = structuredClone(lines)
  const file = { path: 'renamed.php', previousPath: 'original.php', lines }
  const code = await highlightDiff(file, 'github-light')
  assert.deepEqual(lines, before)
  for (const line of lines) for (const side of ['old', 'new']) {
    if ((side === 'old' ? line.oldLine : line.newLine) !== undefined) assert.equal(content(code[side].get(syntaxLineKey(line, side))), line.text)
  }
  assert.ok(code.old.get('0:4').some(token => token.type === 0), 'Old side should have closed its comment')
  assert.ok(code.new.get('0:4').every(token => token.type === 1), 'New side should still be inside its comment')
  assert.ok(code.old.get('1:20').some(token => token.content === 'return' && token.type === 0))
  assert.ok(code.new.get('1:21').some(token => token.content === 'return' && token.type === 0))
})

test('all eight selected theme IDs load and produce actual PHP token colors', async () => {
  for (const theme of [...LIGHT_CODE_THEMES, ...DARK_CODE_THEMES]) {
    const code = await highlightCodeLines('preview.php', ["return ['home'];"], theme.id)
    assert.equal(code.highlighted, true, theme.id)
    assert.match(code.background, /^#/)
    assert.ok(new Set(code.lines[0].map(token => token.color)).size > 1, theme.id)
  }
})

test('unsupported and oversized sources use exact plain text rather than guessing a grammar', async () => {
  const lines = ['', '\tunknown <script> & 😀', '  ', '']
  const code = await highlightCodeLines('file.unsupported', lines, 'nord')
  assert.equal(code.highlighted, false)
  assert.deepEqual(code.lines.map(content), lines)
  const large = Array.from({ length: 6001 }, () => 'return "text";')
  const fallback = await highlightCodeLines('large.php', large, 'nord')
  assert.equal(fallback.highlighted, false)
  assert.deepEqual(fallback.lines.map(content), large)
  assert.equal(codeLanguage('component.tsx'), 'tsx')
  assert.equal(codeLanguage('Dockerfile'), 'dockerfile')
})
