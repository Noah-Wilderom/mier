import { test as base, expect } from '@playwright/test'
import { desktopFixture, openFixture, root, repository, diff, detail, summary, baseSHA, changedSHA } from './desktopFixture'

const test = base.extend<{ desktop: Awaited<ReturnType<typeof desktopFixture>> }>({
  desktop: async ({ page }, use) => {
    const fixture = await desktopFixture(page)
    await use(fixture)
    expect(fixture.unexpected, 'all native calls have explicit fixtures').toEqual([])
    expect(fixture.pageErrors, 'no uncaught application exceptions').toEqual([])
  },
})

test('branch creation preserves fields after rejection, suppresses duplicates, and restores dialog focus', async ({ desktop, page }) => {
  await openFixture(desktop)
  const button = page.getByTitle('Manage branches')
  await button.click()
  const dialog = page.getByRole('dialog', { name: 'Manage branches' })
  await expect(dialog.getByLabel('New branch name')).toBeFocused()
  await dialog.getByLabel('New branch name').fill('feature/retained')
  let reject!: () => void
  desktop.handlers.set('main.RepositoryService.CreateAndCheckoutBranch', async () => { await new Promise<void>(resolve => { reject = resolve }); throw new Error('A Git lock is held by another process') })
  await dialog.getByRole('button', { name: 'Create branch', exact: true }).evaluate(button => { (button as HTMLButtonElement).click(); (button as HTMLButtonElement).click() })
  await expect.poll(() => desktop.calls.filter(call => call.name.endsWith('.CreateAndCheckoutBranch')).length).toBe(1)
  reject()
  await expect(dialog.getByRole('alert')).toContainText('Git lock')
  await expect(dialog.getByLabel('New branch name')).toHaveValue('feature/retained')
  desktop.handlers.delete('main.RepositoryService.CreateAndCheckoutBranch')
  await dialog.getByRole('button', { name: 'Create branch', exact: true }).click()
  await expect(dialog.getByRole('button', { name: 'Switch to feature/retained' })).toBeDisabled()
  await expect(dialog.getByLabel('New branch name')).toHaveValue('')
  await page.keyboard.press('Escape')
  await expect(dialog).not.toBeVisible()
  await expect(button).toBeFocused()
})

test('Git toolbar respects tracking remotes and reports network failure without a success state', async ({ desktop, page }) => {
  await openFixture(desktop)
  const toolbar = page.getByRole('region', { name: 'Repository Git actions' })
  await expect(toolbar.getByLabel('Git remote')).toHaveValue('upstream')
  await toolbar.getByRole('button', { name: 'Fetch', exact: true }).click()
  await expect.poll(() => desktop.calls.find(call => call.name.endsWith('.Fetch'))?.args).toEqual([root, 'upstream'])
  await toolbar.getByLabel('Git remote').selectOption('origin')
  await expect(toolbar.getByRole('button', { name: 'Pull', exact: true })).toBeDisabled()
  desktop.handlers.set('main.RepositoryService.Fetch', () => { throw new Error('Network is offline') })
  await toolbar.getByRole('button', { name: 'Fetch', exact: true }).click()
  await expect(toolbar.getByRole('alert')).toContainText('Network is offline')
  await expect(toolbar.getByRole('button', { name: 'Fetch', exact: true })).toBeEnabled()
})

test('a slow branch comparison cannot overwrite a newer selection', async ({ desktop, page }) => {
  let release!: () => void
  desktop.handlers.set('main.RepositoryService.Compare', async args => {
    if (args[1] === 'main') { await new Promise<void>(resolve => { release = resolve }); return [diff('old.ts', 'const stale = true')] }
    return [diff('latest.ts', 'const selected = true')]
  })
  await openFixture(desktop)
  await page.getByRole('button', { name: 'Compare branches', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Compare branches' })
  await expect.poll(() => typeof release).toBe('function')
  await dialog.getByLabel('Base revision').selectOption('origin/main')
  await expect(dialog.getByLabel('Diff for latest.ts')).toContainText('const selected = true')
  release()
  await expect(dialog.getByLabel('Compared file')).toHaveValue('latest.ts')
  await expect(dialog).not.toContainText('const stale = true')
})

test('local diff failures preserve the commit draft through navigation and refresh', async ({ desktop, page }) => {
  desktop.handlers.set('main.RepositoryService.Diff', () => { throw new Error('Diff temporarily unavailable') })
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button', { name: /^Changes/ }).click()
  await expect(page.getByRole('alert').filter({ hasText: 'Could not load this diff' })).toContainText('Diff temporarily unavailable')
  desktop.handlers.delete('main.RepositoryService.Diff')
  await page.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect(page.locator('.local-code')).toContainText('const value = 2')
  await page.getByLabel('Commit message').fill('fix: preserve my draft')
  await page.getByRole('button', { name: 'Stage src/feature.ts', exact: true }).click()
  desktop.handlers.set('main.RepositoryService.Commit', () => { throw new Error('Commit hook rejected the change') })
  await page.getByRole('button', { name: 'Commit 1 staged files', exact: true }).click()
  await expect(page.getByRole('alert').filter({ hasText: 'Commit hook rejected' })).toBeVisible()
  await expect(page.getByLabel('Commit message')).toHaveValue('fix: preserve my draft')
  await page.locator('.sidebar').getByRole('button', { name: /^History/ }).click()
  await page.locator('.sidebar').getByRole('button', { name: /^Changes/ }).click()
  await expect(page.getByLabel('Commit message')).toHaveValue('fix: preserve my draft')
  await page.reload()
  await page.getByRole('region', { name: 'Repository Git actions' }).waitFor()
  await page.locator('.sidebar').getByRole('button', { name: /^Changes/ }).click()
  await expect(page.getByLabel('Commit message')).toHaveValue('fix: preserve my draft')
})

test('PR creation previews committed files, pushes its own remote, and keeps failed creation drafts', async ({ desktop, page }) => {
  desktop.handlers.set('main.GitHubService.CreatePullRequest', () => { throw new Error('GitHub denied creation') })
  await openFixture(desktop)
  await page.getByRole('button', { name: 'New pull request', exact: true }).click()
  await page.getByLabel('Title', { exact: true }).fill('feat: reliable review')
  await page.getByRole('textbox', { name: 'Description', exact: true }).fill('Keep this context')
  await expect(page.getByLabel('Diff for src/feature.ts')).toContainText('const value = 2')
  await page.getByRole('button', { name: 'Push branch', exact: true }).click()
  await expect.poll(() => desktop.calls.find(call => call.name.endsWith('.Push'))?.args).toEqual([root, 'origin'])
  await page.getByRole('button', { name: 'Create pull request', exact: true }).click()
  await expect(page.getByRole('alert').filter({ hasText: 'GitHub denied creation' })).toBeVisible()
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('feat: reliable review')
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  await page.getByRole('button', { name: 'New pull request', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Description', exact: true })).toHaveValue('Keep this context')
})

test('history inspects the selected commit and never leaves the previous commit diff visible', async ({ desktop, page }) => {
  desktop.handlers.set('main.RepositoryService.CommitDiff', args => [diff(args[1] === baseSHA ? 'initial.ts' : 'feature.ts', args[1] === baseSHA ? 'const initial = true' : 'const feature = true')])
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button', { name: /^History/ }).click()
  await expect(page.getByLabel('Diff for feature.ts')).toContainText('const feature = true')
  await page.getByText('Initial commit', { exact: true }).click()
  await expect(page.getByLabel('Diff for initial.ts')).toContainText('const initial = true')
  await expect(page.getByLabel('Diff for feature.ts')).not.toBeVisible()
})

test('PR commit inspection uses the selected immutable SHA and communicates missing patches', async ({ desktop, page }) => {
  desktop.handlers.set('main.GitHubService.GetCommitDiff', args => ({ sha: args[3], files: [{ path: 'large.bin', status: 'added', additions: 0, deletions: 0, patch: '' }], warnings: [] }))
  await openFixture(desktop)
  await page.getByRole('button').filter({ hasText: 'Review 12' }).click()
  await page.getByRole('tab', { name: /Commits/ }).click()
  await page.getByRole('button', { name: 'Inspect feat: reviewed change' }).click()
  const dialog = page.getByRole('dialog', { name: 'feat: reviewed change' })
  await expect(dialog).toContainText('did not supply a text patch')
  await expect.poll(() => desktop.calls.find(call => call.name.endsWith('.GetCommitDiff'))?.args[3]).toBe(desktop.currentDetail.commits![0].sha)
})

test('new PR revisions preserve the reviewed diff until the reviewer accepts them', async ({ desktop, page }) => {
  await openFixture(desktop)
  await page.getByRole('button').filter({ hasText: 'Review 12' }).click()
  await expect(page.locator('.review-code-text').filter({ hasText: 'const value = 2' }).first()).toBeVisible()
  desktop.currentDetail = detail(changedSHA)
  desktop.prs = [summary(changedSHA)]
  await desktop.emit({ pullRequest: desktop.currentDetail })
  await expect(page.getByText('New commits are available', { exact: true })).toBeVisible()
  await expect(page.locator('.review-code-text').filter({ hasText: 'const value = 2' }).first()).toBeVisible()
  await expect(page.locator('.review-code-text').filter({ hasText: 'const value = 3' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Refresh changes', exact: true }).click()
  await expect(page.locator('.review-code-text').filter({ hasText: 'const value = 3' }).first()).toBeVisible()
})

test('repository switches isolate pending requests and commit drafts', async ({ desktop, page }) => {
  const second = '/fixture/another-repository'
  desktop.repositories.set(second, repository(second))
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button', { name: /^Changes/ }).click()
  await page.getByLabel('Commit message').fill('Draft for the first repository')
  let release!: () => void
  desktop.handlers.set('main.RepositoryService.Fetch', async () => { await new Promise<void>(resolve => { release = resolve }); throw new Error('Old repository failure') })
  await page.getByRole('button', { name: 'Fetch', exact: true }).click()
  await expect.poll(() => typeof release).toBe('function')
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('mag-ik-een-review:command', { detail: { action: 'open-repository' } })))
  desktop.selected = second
  release()
  await expect(page.getByTitle('Manage branches')).toBeEnabled()
  await expect.poll(() => desktop.calls.filter(call => call.name.endsWith('.Open')).at(-1)?.args[0]).toBe(second)
  await expect(page.getByLabel('Commit message')).toHaveValue('')
  await expect(page.locator('.main-workspace')).not.toContainText('Old repository failure')
  await page.getByLabel('Commit message').fill('Second repository draft')
  desktop.selected = root
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('mag-ik-een-review:command', { detail: { action: 'open-repository' } })))
  await expect(page.getByLabel('Commit message')).toHaveValue('Draft for the first repository')
})

test('browser settings require the desktop app for Jira and recover invalid appearance preferences', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('mag-ik-een-review.appearance', 'invalid'))
  await page.goto('/')
  await page.getByRole('button', { name: 'Settings', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Configure Jira', exact: true })).toBeDisabled()
  await expect(page.locator('html')).toHaveAttribute('data-theme', /^(light|dark)$/)
})

test('removing a saved account uses its confirmed host rather than the editable host field', async ({ desktop, page }) => {
  desktop.handlers.set('main.CredentialService.AccountStatus', args=>({host:args[0],login:'reviewer',authenticated:true,source:'saved',name:'Reviewer',avatarUrl:'',url:'https://github.com/reviewer',error:''}))
  desktop.handlers.set('main.CredentialService.DeleteToken', ()=>null)
  await openFixture(desktop)
  await page.getByRole('button',{name:'Settings',exact:true}).click()
  await page.getByRole('textbox',{name:'Host',exact:true}).fill('github.other-company.test')
  await page.getByRole('button',{name:'Remove saved token',exact:true}).click()
  await expect(page.getByText('Remove the saved token for github.com?',{exact:true})).toBeVisible()
  await page.getByRole('button',{name:'Remove token',exact:true}).click()
  await expect.poll(()=>desktop.calls.find(call=>call.name.endsWith('.DeleteToken'))?.args).toEqual(['github.com'])
})

test('a commit completing after navigation clears its saved composer draft', async ({ desktop, page }) => {
  let complete!: () => void
  desktop.handlers.set('main.RepositoryService.Commit', async () => {
    await new Promise<void>(resolve => { complete = resolve })
    desktop.repositories.get(root)!.changes = []
    return changedSHA
  })
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button', { name: /^Changes/ }).click()
  await page.getByLabel('Commit message').fill('fix: complete the operation')
  await page.getByRole('button', { name: 'Stage src/feature.ts', exact: true }).click()
  await page.getByRole('button', { name: 'Commit 1 staged files', exact: true }).click()
  await expect.poll(() => typeof complete).toBe('function')
  await page.locator('.sidebar').getByRole('button', { name: /^History/ }).click()
  complete()
  await expect(page.getByTitle('Manage branches')).toBeEnabled()
  await page.locator('.sidebar').getByRole('button', { name: /^Changes/ }).click()
  await expect(page.getByLabel('Commit message')).toHaveValue('')
  await expect(page.getByText('Working tree is clean', { exact: true })).toBeVisible()
})
