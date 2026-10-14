import { test as base, expect } from '@playwright/test'
import { desktopFixture, openFixture, summary, detail, headSHA, changedSHA, repository } from './desktopFixture'

const test = base.extend<{ desktop: Awaited<ReturnType<typeof desktopFixture>> }>({
  desktop: async ({ page }, use) => {
    const fixture = await desktopFixture(page)
    await use(fixture)
    expect(fixture.unexpected).toEqual([])
    expect(fixture.pageErrors).toEqual([])
  },
})
const pageCalls = (desktop: Awaited<ReturnType<typeof desktopFixture>>) => desktop.calls.filter(call => call.name.endsWith('.ListPullRequestsPage'))

test('opening the inbox loads one page, shows server totals, and appends the next page on demand', async ({ desktop, page }, testInfo) => {
  desktop.prs = Array.from({ length: 60 }, (_, index) => summary(headSHA, index + 1))
  await openFixture(desktop)
  const inbox = page.getByRole('region', { name: 'Pull request inbox' })
  await expect(inbox.locator('.pr-list-item')).toHaveCount(25)
  await expect(inbox.locator('.list-section-label')).toContainText('25 of 60')
  await expect(inbox.getByRole('button', { name: /^Open/ })).toContainText('60')
  expect(pageCalls(desktop)).toHaveLength(1)
  expect(pageCalls(desktop)[0].args[3]).toEqual({ state: 'open', view: 'inbox', query: '', cursor: '', pageSize: 25 })
  await page.screenshot({ path: testInfo.outputPath('paginated-inbox.png') })
  const loadMore = inbox.getByRole('button', { name: 'Load more pull requests', exact: true })
  await loadMore.scrollIntoViewIfNeeded()
  await page.screenshot({ path: testInfo.outputPath('load-more.png') })
  await loadMore.focus()
  await page.keyboard.press('Enter')
  await expect(inbox.locator('.pr-list-item')).toHaveCount(50)
  expect(pageCalls(desktop)[1].args[3].cursor).toBe('25')
  await desktop.emit({ pullRequests: [summary(headSHA, 1)] })
  await expect(inbox.locator('.pr-list-item')).toHaveCount(50)
})

test('overlapping pages preserve the selected review revision and filters keep its details open', async ({ desktop, page }) => {
  desktop.prs = Array.from({ length: 30 }, (_, index) => summary(headSHA, index + 1))
  await openFixture(desktop)
  const inbox = page.getByRole('region', { name: 'Pull request inbox' })
  await expect(inbox.locator('.pr-list-item')).toHaveCount(25)
  await inbox.getByRole('button').filter({ hasText: 'Review 12' }).click()
  await expect(page.locator('.review-code-text').filter({ hasText: 'const value = 2' }).first()).toBeVisible()
  desktop.handlers.set('main.GitHubService.ListPullRequestsPage', args => ({ items: args[3].cursor ? [summary(changedSHA, 12), summary(headSHA, 26)] : [], nextCursor: '', hasNextPage: false, totalCount: args[3].cursor ? 26 : 0, openCount: 30 }))
  await inbox.getByRole('button', { name: 'Load more pull requests', exact: true }).click()
  await expect(inbox.locator('.pr-list-item')).toHaveCount(26)
  await expect(page.getByText('New commits are available', { exact: true })).toBeVisible()
  await expect(page.locator('.review-code-text').filter({ hasText: 'const value = 2' }).first()).toBeVisible()
  await inbox.getByRole('button', { name: 'Closed', exact: true }).click()
  await expect(inbox.locator('.pr-list-item')).toHaveCount(0)
  await expect(page.locator('.review-code-text').filter({ hasText: 'const value = 2' }).first()).toBeVisible()
})

test('initial page failures retry cleanly and successful refreshes clear transient errors', async ({ desktop, page }) => {
  desktop.handlers.set('main.GitHubService.ListPullRequestsPage', () => { throw new Error('First page failed') })
  await openFixture(desktop)
  const inbox = page.getByRole('region', { name: 'Pull request inbox' })
  await expect(inbox.getByRole('alert')).toContainText('First page failed')
  await expect(inbox.locator('.pr-list-item')).toHaveCount(0)
  desktop.handlers.delete('main.GitHubService.ListPullRequestsPage')
  await inbox.getByRole('button', { name: 'Retry loading pull requests', exact: true }).click()
  await expect(inbox.locator('.pr-list-item')).toHaveCount(1)
  await expect(inbox.getByRole('alert')).toHaveCount(0)
  await desktop.emit({ error: 'Old refresh deadline exceeded' })
  await expect(page.getByText('Old refresh deadline exceeded', { exact: true })).toBeVisible()
  await desktop.emit()
  await expect(page.getByText('Old refresh deadline exceeded', { exact: true })).toHaveCount(0)
})

test('an account lookup failure survives local refreshes and prevents unauthenticated page requests', async ({ desktop, page }) => {
  desktop.handlers.set('main.CredentialService.AccountStatus', () => { throw new Error('GitHub CLI token lookup timed out') })
  await openFixture(desktop)
  await expect(page.getByText('GitHub CLI token lookup timed out', { exact: true })).toBeVisible()
  await desktop.emit()
  await expect(page.getByText('GitHub CLI token lookup timed out', { exact: true })).toBeVisible()
  expect(pageCalls(desktop)).toHaveLength(0)
})

test('creating a PR clears a restrictive search and retains its selected details before listing catches up', async ({ desktop, page }) => {
  desktop.handlers.set('main.GitHubService.CreatePullRequest', args => {
    const created = { ...summary(headSHA, 99), title: args[3].title }
    desktop.currentDetail = { ...detail(), pullRequest: created }
    return created
  })
  await openFixture(desktop)
  const inbox = page.getByRole('region', { name: 'Pull request inbox' })
  await page.getByLabel('Search pull requests').fill('Review 12')
  await expect.poll(() => pageCalls(desktop).at(-1)?.args[3].query).toBe('Review 12')
  await page.getByRole('button', { name: 'New pull request', exact: true }).click()
  await page.getByLabel('Title', { exact: true }).fill('New paginated review')
  await page.getByRole('button', { name: 'Create pull request', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Pull request 99' })).toContainText('New paginated review')
  await expect(page.getByLabel('Search pull requests')).toHaveValue('')
  await expect(inbox).not.toContainText('New paginated review')
  await page.getByRole('tab', { name: /Changes/ }).click()
  await expect(page.locator('.review-code-text').filter({ hasText: 'const value = 2' }).first()).toBeVisible()
})

test('background membership changes remove nonmatching rows while retaining later pages', async ({ desktop, page }) => {
  desktop.prs = Array.from({ length: 30 }, (_, index) => summary(headSHA, index + 1))
  await openFixture(desktop)
  const inbox = page.getByRole('region', { name: 'Pull request inbox' })
  await expect(inbox.locator('.pr-list-item')).toHaveCount(25)
  await inbox.getByRole('button', { name: 'Load more pull requests', exact: true }).click()
  await expect(inbox.locator('.pr-list-item')).toHaveCount(30)
  await desktop.emit({ pullRequests: [{ ...summary(headSHA, 1), status: 'closed' }], openPullRequestCount: 29 })
  await expect(inbox.locator('.pr-list-item')).toHaveCount(29)
  await expect(inbox.getByRole('button').filter({ hasText: 'Review 30' })).toBeVisible()
  await expect(inbox.getByRole('button', { name: /^Open/ })).toContainText('29')
  await page.locator('.sidebar').getByRole('button', { name: 'Review requested', exact: true }).click()
  await expect(inbox.locator('.pr-list-item')).toHaveCount(25)
  await desktop.emit({ pullRequests: [{ ...summary(headSHA, 2), reviewers: [] }] })
  await expect(inbox.locator('.pr-list-item')).toHaveCount(24)
  await expect(inbox.getByRole('button').filter({ hasText: 'Review 25' })).toBeVisible()
})

test('filters and search fetch matching PRs beyond the first page with a fresh cursor', async ({ desktop, page }) => {
  desktop.prs = Array.from({ length: 60 }, (_, index) => summary(headSHA, index + 1))
  desktop.prs[49] = { ...desktop.prs[49], status: 'merged', merged: true, title: 'Archived pagination fix' }
  desktop.prs[59] = { ...desktop.prs[59], author: { ...desktop.prs[59].author, login: 'reviewer' }, title: 'My distant change' }
  await openFixture(desktop)
  const inbox = page.getByRole('region', { name: 'Pull request inbox' })
  await expect(inbox.locator('.pr-list-item')).toHaveCount(25)
  await inbox.getByRole('button', { name: 'Merged', exact: true }).click()
  await expect(inbox.locator('.pr-list-item')).toHaveCount(1)
  await expect(inbox).toContainText('Archived pagination fix')
  expect(pageCalls(desktop).at(-1)!.args[3]).toMatchObject({ state: 'merged', cursor: '' })
  await inbox.getByRole('button', { name: /^Open/ }).click()
  await page.getByLabel('Search pull requests').fill('distant')
  await expect(inbox.locator('.pr-list-item')).toHaveCount(1)
  await expect(inbox).toContainText('My distant change')
  expect(pageCalls(desktop).at(-1)!.args[3]).toMatchObject({ query: 'distant', cursor: '' })
  await page.getByLabel('Search pull requests').fill('')
  await page.locator('.sidebar').getByRole('button', { name: 'My pull requests', exact: true }).click()
  await expect(inbox.locator('.pr-list-item')).toHaveCount(1)
  expect(pageCalls(desktop).at(-1)!.args[3]).toMatchObject({ view: 'mine', cursor: '' })
})

test('a failed next page retains loaded rows and retries the same cursor while duplicate clicks coalesce', async ({ desktop, page }) => {
  desktop.prs = Array.from({ length: 30 }, (_, index) => summary(headSHA, index + 1))
  await openFixture(desktop)
  const inbox = page.getByRole('region', { name: 'Pull request inbox' })
  await expect(inbox.locator('.pr-list-item')).toHaveCount(25)
  let reject!: () => void
  desktop.handlers.set('main.GitHubService.ListPullRequestsPage', async () => {
    await new Promise<void>(resolve => { reject = resolve })
    throw new Error('GitHub is temporarily unavailable')
  })
  await inbox.getByRole('button', { name: 'Load more pull requests', exact: true }).evaluate(button => { (button as HTMLButtonElement).click(); (button as HTMLButtonElement).click() })
  await expect.poll(() => typeof reject).toBe('function')
  expect(pageCalls(desktop)).toHaveLength(2)
  reject()
  await expect(inbox.getByRole('alert')).toContainText('GitHub is temporarily unavailable')
  await expect(inbox.locator('.pr-list-item')).toHaveCount(25)
  desktop.handlers.delete('main.GitHubService.ListPullRequestsPage')
  await inbox.getByRole('button', { name: 'Retry loading more pull requests', exact: true }).click()
  await expect(inbox.locator('.pr-list-item')).toHaveCount(30)
  expect(pageCalls(desktop).at(-1)!.args[3].cursor).toBe('25')
  await expect(inbox.getByRole('button', { name: 'Load more pull requests', exact: true })).toHaveCount(0)
})

test('slow pages cannot overwrite a newer filter or repository', async ({ desktop, page }) => {
  desktop.prs = Array.from({ length: 30 }, (_, index) => summary(headSHA, index + 1))
  await openFixture(desktop)
  const inbox = page.getByRole('region', { name: 'Pull request inbox' })
  await expect(inbox.locator('.pr-list-item')).toHaveCount(25)
  let release!: () => void
  desktop.handlers.set('main.GitHubService.ListPullRequestsPage', async args => {
    if (args[3].cursor) {
      await new Promise<void>(resolve => { release = resolve })
      return { items: [{ ...summary(headSHA, 99), title: 'Stale page result' }], nextCursor: '', hasNextPage: false, totalCount: 30, openCount: 30 }
    }
    return { items: [{ ...summary(headSHA, 80), title: 'Current merged result', status: 'merged', merged: true }], nextCursor: '', hasNextPage: false, totalCount: 1, openCount: 30 }
  })
  await inbox.getByRole('button', { name: 'Load more pull requests', exact: true }).click()
  await expect.poll(() => typeof release).toBe('function')
  await inbox.getByRole('button', { name: 'Merged', exact: true }).click()
  await expect(inbox).toContainText('Current merged result')
  release()
  await expect(inbox).not.toContainText('Stale page result')
  const second = '/fixture/second-repository'
  desktop.repositories.set(second, repository(second))
  desktop.selected = second
  let finishOld!: () => void
  desktop.handlers.set('main.GitHubService.ListPullRequestsPage', async args => {
    if (args[3].state === 'closed') {
      await new Promise<void>(resolve => { finishOld = resolve })
      return { items: [{ ...summary(headSHA, 99), title: 'Old repository result', status: 'closed' }], nextCursor: '', hasNextPage: false, totalCount: 1, openCount: 30 }
    }
    return { items: [{ ...summary(headSHA, 81), title: 'New repository result' }], nextCursor: '', hasNextPage: false, totalCount: 1, openCount: 1 }
  })
  await inbox.getByRole('button', { name: 'Closed', exact: true }).click()
  await expect.poll(() => typeof finishOld).toBe('function')
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('mag-ik-een-review:command', { detail: { action: 'open-repository' } })))
  await expect.poll(() => desktop.calls.filter(call => call.name.endsWith('.Open')).at(-1)?.args[0]).toBe(second)
  await inbox.getByRole('button', { name: /^Open/ }).click()
  await expect(inbox).toContainText('New repository result')
  finishOld()
  await expect(inbox).not.toContainText('Old repository result')
})
