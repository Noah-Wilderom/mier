// @ts-nocheck -- Node's TypeScript runner exercises the actual hover formatter.
import test from 'node:test'
import assert from 'node:assert/strict'
import { highlightSymbolSignature, prepareSymbolSignature } from './symbolSignature.ts'
import { highlightCodeLines } from './syntaxHighlighter.ts'
import { DARK_CODE_THEMES, LIGHT_CODE_THEMES } from './codeReviewPreferences.ts'

// Phpactor's outline uses inferred return types and has no method bodies or
// terminators. Highlighting this as a PHP source file made later methods white.
const outline = `<?php class HomeValuationService {
    public function __construct(Ⓒ CalcasaService $calcasaService)
    public function createOrder(Ⓒ UserEntity $user, Ⓒ Address $address): Ⓒ HomeValuationOrderEntity
    public function findById(int $id): ?HomeValuationOrderEntity
    public function findOneByAddress(Ⓒ UserEntity $user, Ⓒ Address $address): ?HomeValuationOrderEntity
    public function getOrdersAwaitingReport(): HomeValuationOrderEntity[]
    public function fetchAndCompleteReport(Ⓒ HomeValuationOrderEntity $order): Collection<string, HomeValuationOrderEntity>
    public function completeOrder(Ⓒ HomeValuationOrderEntity $order): void
}`
const content = tokens => tokens.map(token => token.content).join('')

test('PHP class hovers hide scaffolding and badges with consistent member colors in every theme', async () => {
  const display = prepareSymbolSignature(outline, 'php')
  assert.equal(display.lines[0], 'class HomeValuationService {')
  assert.ok(display.lines.every(line => !line.includes('Ⓒ') && !line.includes('<?php')))
  assert.ok(display.lines.some(line => line.includes('HomeValuationOrderEntity[]')))
  assert.ok(display.lines.some(line => line.includes('Collection<string, HomeValuationOrderEntity>')))
  for (const { id } of [...LIGHT_CODE_THEMES, ...DARK_CODE_THEMES]) {
    const code = await highlightSymbolSignature('symbol.php', outline, 'php', id)
    assert.equal(code.highlighted, true, id)
    assert.deepEqual(code.lines.map(content), display.lines, id)
    const keyword = code.lines[0].find(token => token.content === 'class')
    assert.ok(keyword, `Missing class token in ${id}`)
    for (const [index, line] of display.lines.entries()) {
      if (!line.includes('public function')) continue
      for (const word of ['public', 'function']) {
        const token = code.lines[index].find(token => token.content === word)
        assert.equal(token?.color, keyword.color, `${id}: ${word} lost keyword coloring on line ${index + 1}`)
      }
    }
  }
})

test('PHP hover cleanup retains quoted, commented and heredoc text and multiline declarations', async () => {
  const source = `<?php
class Example {
    /* A documentation example:
    public function untouched(Ⓒ Example $value)
    */
    public function greet(
        Ⓒ Example $value,
        string $message = 'Ⓒ literal <?php',
        string $escaped = 'can\\'t remove Ⓘ here',
        string $double = "Ⓔ text",
    ): Ⓘ Iterator
    public function body(): void {
        $text = <<<'TEXT'
public function literal(Ⓒ Example $value)
<?php inside text
TEXT; // kept
        // Ⓒ comment <?php
        # Ⓘ another comment
        $code = '<?php echo "hello";';
    }
    public function later(): Ⓣ Example|Ⓔ State
}`
  const result = prepareSymbolSignature(source, 'PHP')
  assert.ok(result.lines.includes('    public function untouched(Ⓒ Example $value)'))
  assert.ok(result.lines.includes('public function literal(Ⓒ Example $value)'))
  assert.ok(result.lines.includes('<?php inside text'))
  assert.ok(result.lines.includes("        string $message = 'Ⓒ literal <?php',"))
  assert.ok(result.lines.includes("        string $escaped = 'can\\'t remove Ⓘ here',"))
  assert.ok(result.lines.includes('        string $double = "Ⓔ text",'))
  assert.ok(result.lines.includes('        // Ⓒ comment <?php'))
  assert.ok(result.lines.includes('        # Ⓘ another comment'))
  assert.ok(result.lines.includes('    public function later(): Example|State'))
  assert.deepEqual(result.starts.map(start => result.lines[start]), ['class Example {', '    public function greet(', '    public function body(): void {', '    public function later(): Example|State'])
  const code = await highlightSymbolSignature('symbol.php', source, 'php', 'github-dark')
  assert.deepEqual(code.lines.map(content), result.lines)
  assert.ok(code.lines[2].every(token => token.type === 1), 'A declaration inside documentation must stay a comment')
  const literal = result.lines.indexOf('public function literal(Ⓒ Example $value)')
  assert.ok(code.lines[literal].every(token => token.type === 2), 'A declaration inside a nowdoc must stay a string')
})

test('hover formatting keeps other languages and actual PHP source untouched', async () => {
  const source = 'const tag = "<?php Ⓒ Literal"\n/* public function text() */'
  assert.deepEqual(prepareSymbolSignature(source, 'typescript'), { lines: source.split('\n'), starts: [0] })
  const php = ['<?php', 'class ActualSource {}', "$message = 'Ⓒ literal';"]
  const code = await highlightCodeLines('ActualSource.php', php, 'github-dark')
  assert.deepEqual(code.lines.map(content), php)
})

test('oversized hover outlines keep the overall highlighting limit', async () => {
  const source = '<?php\n' + Array.from({ length: 6001 }, (_, index) => `public function method${index}(): void`).join('\n')
  const code = await highlightSymbolSignature('symbol.php', source, 'php', 'github-dark')
  assert.equal(code.highlighted, false)
  assert.equal(code.lines.length, 6001)
  assert.equal(content(code.lines[6000]), 'public function method6000(): void')
})
