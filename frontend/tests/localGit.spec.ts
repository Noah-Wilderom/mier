import { test as base, expect } from '@playwright/test'
import { desktopFixture, openFixture, root, headSHA, diff } from './desktopFixture'

const test = base.extend<{ desktop: Awaited<ReturnType<typeof desktopFixture>> }>({
  desktop: async ({ page }, use) => {
    const fixture = await desktopFixture(page)
    await use(fixture)
    expect(fixture.unexpected).toEqual([])
    expect(fixture.pageErrors).toEqual([])
  },
})

test('selecting an added line stages only that row and a stale response keeps the selection', async ({ desktop, page }) => {
  desktop.handlers.set('main.RepositoryService.StageHunk', args => {
    expect(args).toEqual([root, { path: 'src/feature.ts', staged: false, hunk: 0, expectedPatch: diff().rawPatch, lines: [1] }])
    throw new Error('This diff changed. Refresh it before staging a section')
  })
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button', { name: /Changes/ }).click()
  await page.getByLabel('Select added line 1', { exact: true }).check()
  await page.getByRole('button', { name: 'Stage 1 selected lines', exact: true }).click()
  await expect(page.locator('.local-action-error')).toContainText('This diff changed')
  await expect(page.getByLabel('Select added line 1', { exact: true })).toBeChecked()
})

test('discard waits for a preview and passes the preview token after confirmation', async ({ desktop, page }) => {
  desktop.handlers.set('main.RepositoryService.PreviewDiscard', () => ({ path: 'src/feature.ts', token: 'file-version-1', diff: diff(), untracked: false }))
  desktop.handlers.set('main.RepositoryService.Discard', args => {
    expect(args).toEqual([root, { path: 'src/feature.ts', token: 'file-version-1' }])
    desktop.repositories.get(root)!.changes = []
  })
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button', { name: /Changes/ }).click()
  await page.getByRole('button', { name: 'Discard file…' }).click()
  const dialog = page.getByRole('dialog', { name: 'Discard changes' })
  await expect(dialog).toContainText('const value = 2')
  expect(desktop.calls.filter(call => call.name.endsWith('.Discard'))).toHaveLength(0)
  await dialog.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.getByText('Working tree is clean')).toBeVisible()
})

test('stashes show their preview and dropping requires a separate confirmation', async ({ desktop, page }) => {
  let dropped = false
  desktop.handlers.set('main.RepositoryService.Stashes', () => dropped ? [] : [{ sha: headSHA, ref: 'stash@{0}', message: 'WIP on feature', createdAt: '2026-10-15T12:00:00Z' }])
  desktop.handlers.set('main.RepositoryService.StashDiff', () => diff().rawPatch)
  desktop.handlers.set('main.RepositoryService.MutateStash', args => {
    expect(args).toEqual([root, { sha: headSHA, action: 'drop', restoreIndex: false, expectedHead: headSHA }])
    dropped = true
  })
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button', { name: /Changes/ }).click()
  await page.getByRole('button', { name: 'Stashes…' }).click()
  const dialog = page.getByRole('dialog', { name: 'Stashes', exact: true })
  await expect(dialog).toContainText('const value = 2')
  await dialog.getByRole('button', { name: 'Drop…', exact: true }).click()
  expect(desktop.calls.filter(call => call.name.endsWith('.MutateStash'))).toHaveLength(0)
  await dialog.getByRole('button', { name: 'Drop stash', exact: true }).click()
  await expect(dialog).toContainText('No saved stashes.')
})

test('amending a message does not require staged files and carries the displayed head', async ({ desktop, page }) => {
  desktop.repositories.get(root)!.changes = []
  desktop.handlers.set('main.RepositoryService.Amend', args => {
    expect(args).toEqual([root, { message: 'fix: revised message', expectedHead: headSHA }])
    return 'd'.repeat(40)
  })
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button', { name: /Changes/ }).click()
  await page.getByLabel('Amend last commit', { exact: true }).check()
  await page.getByLabel('Commit message', { exact: true }).fill('fix: revised message')
  await page.getByRole('button', { name: 'Amend last commit…', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Amend last commit', exact: true })
  await dialog.getByRole('button', { name: 'Amend commit', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.getByLabel('Commit message', { exact: true })).toHaveValue('')
})
