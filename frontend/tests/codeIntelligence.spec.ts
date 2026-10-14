import { test as base, expect, type Page } from '@playwright/test'
import { desktopFixture, openFixture, diff, root, headSHA } from './desktopFixture'

const snapshot = 'd'.repeat(64)
const test = base.extend<{ desktop: Awaited<ReturnType<typeof desktopFixture>> }>({
  desktop: async ({ page }, use) => {
    const fixture = await desktopFixture(page)
    fixture.handlers.set('main.LSPService.Servers', () => [{ language: 'php', name: 'Phpactor', available: true, source: 'managed', command: '/fixture/php', args: [], configuration: {}, managed: {} }])
    fixture.repositories.get(root)!.changes![0].path = 'src/use.php'
    fixture.handlers.set('main.RepositoryService.Diff', () => diff('src/use.php', "$message = $greeter->greet('PHP');"))
    await use(fixture)
    expect(fixture.unexpected).toEqual([])
    expect(fixture.pageErrors).toEqual([])
  },
})

async function changes(desktop: Awaited<ReturnType<typeof desktopFixture>>, page: Page) {
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button', { name: /^Changes/ }).click()
  await expect(page.locator('.local-code-text[data-code-side="new"]')).toContainText('greet')
}

async function point(page: Page, part = 1) {
  const code = page.locator('.local-code-text[data-code-side="new"]')
  const location = await code.evaluate((element, part) => {
    const index = element.textContent!.indexOf('greet(') + part
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT)
    let remaining = index
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      if (remaining < node.textContent!.length) {
        const range = document.createRange()
        range.setStart(node, remaining)
        range.setEnd(node, remaining + 1)
        const box = range.getBoundingClientRect()
        return { x: box.x + box.width / 2, y: box.y + box.height / 2 }
      }
      remaining -= node.textContent!.length
    }
    throw new Error('Symbol was not found in the code DOM')
  }, part)
  await page.mouse.move(location.x, location.y)
}

test('visible source starts preparation before hover and renders highlighted documentation and definitions', async ({ desktop, page }) => {
  let finish!: () => void
  desktop.handlers.set('main.LSPService.Warmup', async () => { await new Promise<void>(resolve => { finish = resolve }); return null })
  desktop.handlers.set('main.LSPService.Hover', () => ({ language: 'php', revision: snapshot, kind: 'markdown', contents: "### Greeter::greet\n\n```php\npublic function greet(string $name): string\n```\n\nReturns a **greeting**. [Documentation](https://example.test/docs)." }))
  desktop.handlers.set('main.LSPService.Definition', () => [{ id: 'definition', uri: 'mier-lsp://definition/fixture', path: 'src/Greeter.php', language: 'php', revision: snapshot, external: false, range: { start: { line: 1, character: 0 }, end: { line: 1, character: 5 } } }])
  desktop.handlers.set('main.LSPService.ReadDefinition', () => ({ uri: 'mier-lsp://definition/fixture', path: 'src/Greeter.php', language: 'php', revision: snapshot, external: false, range: { start: { line: 1, character: 0 }, end: { line: 1, character: 5 } }, text: '<?php\nfunction greet(string $name): string {}\n' }))
  await changes(desktop, page)
  await expect.poll(() => typeof finish).toBe('function')
  expect(desktop.calls.filter(call => call.name.endsWith('.Hover'))).toHaveLength(0)
  await expect(page.locator('.code-intelligence-hint')).toContainText('Preparing code analysis')
  finish()
  await expect(page.locator('.code-intelligence-hint')).toContainText('Hover symbols')
  await point(page)
  const popup = page.getByRole('dialog', { name: 'Symbol information' })
  await expect(popup.getByLabel('Symbol signature')).toContainText('public function greet(string $name): string')
  await expect(popup.locator('.code-hover-location')).toHaveText('use.php:1')
  await expect.poll(async () => popup.getByLabel('Symbol signature').locator('span[style*="color"]').count()).toBeGreaterThan(2)
  await expect(popup.getByRole('link', { name: /Documentation/ })).toHaveAttribute('rel', 'noopener noreferrer')
  await popup.getByRole('button', { name: /Go to definition/ }).click()
  await expect(page.getByRole('region', { name: 'Read-only definition src/Greeter.php' })).toContainText('function greet')
  expect(desktop.calls.find(call => call.name.endsWith('.Warmup'))!.args[0]).toMatchObject({ root, revision: 'working', path: 'src/use.php', line: 0, character: 0 })
})

test('moving within a symbol preserves pending hover and dismissal cancels delayed PHP refreshes', async ({ desktop, page }) => {
  let respond!: () => void
  desktop.handlers.set('main.LSPService.Hover', async () => { await new Promise<void>(resolve => { respond = resolve }); return { language: 'php', revision: snapshot, kind: 'markdown', contents: '```php\nfunction greet(string $name): string\n```' } })
  await changes(desktop, page)
  await point(page, 0)
  await expect.poll(() => typeof respond).toBe('function')
  await point(page, 3)
  await page.waitForTimeout(600)
  expect(desktop.calls.filter(call => call.name.endsWith('.Hover'))).toHaveLength(1)
  respond()
  const popup = page.getByRole('dialog', { name: 'Symbol information' })
  await expect(popup.getByLabel('Symbol signature')).toContainText('greet')
  await popup.getByRole('button', { name: 'Close symbol information' }).click()
  await page.waitForTimeout(1500)
  await expect(popup).not.toBeVisible()
  expect(desktop.calls.filter(call => call.name.endsWith('.Hover'))).toHaveLength(1)
})

test('PHP results refresh beyond one second, fit a small viewport, and plaintext stays literal', async ({ desktop, page }) => {
  let calls = 0
  desktop.handlers.set('main.LSPService.Hover', () => ({ language: 'php', revision: snapshot, kind: calls++ < 2 ? 'markdown' : 'plaintext', contents: calls <= 2 ? 'method greet' : 'List<T> contains **literal** markers' }))
  await changes(desktop, page)
  await point(page)
  const popup = page.getByRole('dialog', { name: 'Symbol information' })
  await expect(popup).toContainText('method greet')
  await expect(popup.locator('.code-hover-plaintext')).toHaveText('List<T> contains **literal** markers')
  await expect(popup.locator('strong')).toHaveCount(0)
  expect(desktop.calls.filter(call => call.name.endsWith('.Hover')).slice(1).every(call => call.args[0].revision === snapshot)).toBe(true)
  await page.setViewportSize({ width: 720, height: 520 })
  await expect.poll(async () => {
    const box = await popup.boundingBox()
    return !!box && box.x >= 0 && box.y >= 0 && box.x + box.width <= 720 && box.y + box.height <= 520
  }).toBe(true)
  await popup.hover()
  // Later PHP responses must keep the placement calculated after the resize.
  await expect.poll(() => desktop.calls.filter(call => call.name.endsWith('.Hover')).length).toBeGreaterThanOrEqual(4)
  const box = await popup.boundingBox()
  expect(box!.x + box!.width).toBeLessThanOrEqual(720)
  expect(box!.y + box!.height).toBeLessThanOrEqual(520)
  await popup.getByRole('button', { name: 'Close symbol information' }).click()
})

test('failed preparation retries and abandoned views ignore late preparation responses', async ({ desktop, page }) => {
  desktop.handlers.set('main.LSPService.Warmup', () => { throw new Error('Temporary source fetch failure') })
  await changes(desktop, page)
  await expect(page.locator('.code-intelligence-hint')).toContainText('Code analysis could not be prepared')
  let finish!: () => void
  desktop.handlers.set('main.LSPService.Warmup', async () => { await new Promise<void>(resolve => { finish = resolve }); return null })
  await page.locator('.code-intelligence-hint').getByRole('button', { name: 'Retry', exact: true }).click()
  await expect.poll(() => typeof finish).toBe('function')
  await page.locator('.sidebar').getByRole('button', { name: /^History/ }).click()
  finish()
  await expect(page.locator('.local-workspace')).not.toBeVisible()
  await expect(page.getByRole('dialog', { name: 'Symbol information' })).not.toBeVisible()
  expect(desktop.calls.filter(call => call.name.endsWith('.Warmup'))).toHaveLength(2)
  expect(desktop.repositories.get(root)!.headSha).toBe(headSHA)
})
