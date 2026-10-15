import { test, expect } from '@playwright/test'
import { desktopFixture, openFixture, root, diff } from './desktopFixture'
import { catalogServers } from './languageServerFixture'

for (const example of [
  { path: 'Dockerfile', server: 'dockerfile', protocol: 'dockerfile', text: 'FROM node:22' },
  { path: '.github/workflows/ci.yml', server: 'github-actions', protocol: 'yaml', text: 'name: Build' },
  { path: 'src/main.go', server: 'go', protocol: 'go', text: 'var value = 42' },
]) test(`catalog routes hover and definitions for ${example.path}`, async ({ page }) => {
  const desktop = await desktopFixture(page), servers = catalogServers(), revision = 'd'.repeat(64)
  servers.find(server => server.language === example.server)!.available = true
  desktop.repositories.get(root)!.changes![0].path = example.path
  desktop.handlers.set('main.LSPService.Servers', () => servers)
  desktop.handlers.set('main.RepositoryService.Diff', () => diff(example.path, example.text))
  desktop.handlers.set('main.LSPService.Hover', args => { expect(args[0].path).toBe(example.path); return { contents: `Catalog symbol for ${example.server}`, kind: 'markdown', revision, language: example.protocol } })
  desktop.handlers.set('main.LSPService.Definition', args => { expect(args[0].path).toBe(example.path); return [{ id: 'definition', uri: 'mier-lsp://definition/catalog', path: example.path, language: example.protocol, revision, external: false, range: { start: { line: 0, character: 0 }, end: { line: 0, character: 3 } } }] })
  desktop.handlers.set('main.LSPService.ReadDefinition', () => ({ uri: 'mier-lsp://definition/catalog', path: example.path, language: example.protocol, revision, external: false, range: { start: { line: 0, character: 0 }, end: { line: 0, character: 3 } }, text: example.text+'\n' }))
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button', { name: /^Changes/ }).click()
  const code = page.locator('.local-code-text[data-code-side="new"]')
  await expect(code).toHaveText(example.text)
  await expect.poll(() => desktop.calls.filter(call => call.name.endsWith('.Warmup')).length).toBeGreaterThan(0)
  const position = await code.evaluate(element => {
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT), first = walker.nextNode()!
    const range = document.createRange(); range.setStart(first, 1); range.setEnd(first, 2)
    const box = range.getBoundingClientRect()
    return { x: box.x+box.width/2, y: box.y+box.height/2 }
  })
  await page.mouse.move(position.x, position.y)
  await expect(page.getByRole('dialog', { name: 'Symbol information' })).toContainText(`Catalog symbol for ${example.server}`)
  await page.getByRole('dialog', { name: 'Symbol information' }).getByRole('button', { name: /Go to definition/ }).click()
  await expect(page.locator('.code-definition-reader')).toBeVisible()
  expect(desktop.unexpected).toEqual([])
  expect(desktop.pageErrors).toEqual([])
})
