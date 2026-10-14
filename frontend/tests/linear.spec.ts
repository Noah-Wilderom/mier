import { test as base, expect } from '@playwright/test'
import { desktopFixture, openFixture, detail, summary } from './desktopFixture'
import type { TicketIssue, JiraStatus, LinearStatus, LinearTickets } from '../src/services'

const workspace = { id: 'workspace-a', name: 'Product team', url: 'https://linear.app/product-team' }
const status: LinearStatus = { configured: true, connected: true, clientId: 'fixture-linear', callbackPort: 43828, callbackUrl: 'http://localhost:43828/oauth/linear/callback', scopes: ['read'], state: 'idle', error: '', workspace, account: 'Reviewer' }
function issue(key = 'ENG-123'): TicketIssue {
  return { id: `issue-${key}`, key, summary: `Requirements for ${key}`, url: `${workspace.url}/issue/${key}`, type: 'Engineering · Review sprint', status: 'In progress', statusCategory: 'indeterminate', assignee: 'Developer', priority: 'High', labels: ['review'], parentKey: 'ENG-1', updatedAt: '2026-10-14T12:00:00Z', description: '## Requirements\n- [x] Keep the editor interactive.\n\n| Requirement | Result |\n| --- | --- |\n| Preserve drafts | Verified |\n\n```ts\nconst value = 1\n```\n\n[Unsafe link](javascript:alert(1))\n\n![Design attachment](https://private-image.example.test/diagram.png)', acceptanceCriteria: null, comments: [{ id: 'comment-1', author: 'Reviewer', createdAt: '2026-10-14T12:00:00Z', body: 'Please **verify** the requirements.' }], commentsError: '' }
}
function result(keys = ['ENG-123'], hidden: string[] = []): LinearTickets {
  return { workspace, preferences: { teams: ['ENG'] }, tickets: keys.map(key => ({ key, sources: ['branch', 'title'], issue: issue(key), error: '' })), hidden, truncated: false }
}
const test = base.extend<{ desktop: Awaited<ReturnType<typeof desktopFixture>> }>({
  desktop: async ({ page }, use) => {
    const fixture = await desktopFixture(page)
    fixture.prs = [{ ...summary(), title: 'ENG-123: improve reviews', head: { ...summary().head, ref: 'eng-123-review' } }]
    fixture.currentDetail = { ...detail(), pullRequest: fixture.prs[0] }
    fixture.handlers.set('main.LinearService.GetLinearStatus', () => status)
    fixture.handlers.set('main.LinearService.GetLinearTickets', () => result())
    fixture.handlers.set('main.LinearService.ReadLinearIssue', args => issue(args[0].key))
    await use(fixture)
    expect(fixture.unexpected).toEqual([])
    expect(fixture.pageErrors).toEqual([])
  },
})
async function openReview(desktop: Awaited<ReturnType<typeof desktopFixture>>) {
  await openFixture(desktop)
  await desktop.page.locator('.pr-list-item').first().click()
  await expect(desktop.page.getByRole('region', { name: 'Linear tickets', exact: true })).toContainText('Requirements for ENG-123')
}

test('own Linear app uses a client ID, browser authorization, cancellation, and disconnect', async ({ desktop, page }) => {
  let current: LinearStatus = { ...status, configured: false, connected: false, clientId: '', workspace: undefined, account: '' }
  desktop.handlers.set('main.LinearService.GetLinearStatus', () => current)
  let reject = true
  desktop.handlers.set('main.LinearService.ConfigureLinear', args => {
    if (reject) throw new Error('Configuration could not be saved. Retry.')
    expect(args[0]).toEqual({ clientId: 'fixture-linear', callbackPort: 43828 })
    current = { ...current, configured: true, clientId: args[0].clientId }
  })
  desktop.handlers.set('main.LinearService.BeginLinearConnection', () => { current = { ...current, state: 'authorizing' }; return 'https://linear.app/oauth/authorize?client_id=fixture-linear&code_challenge=fixture&code_challenge_method=S256&scope=read' })
  desktop.handlers.set('main.LinearService.CancelLinearConnection', () => { current = { ...current, state: 'idle' } })
  desktop.handlers.set('main.LinearService.DisconnectLinear', () => { current = { ...current, connected: false, workspace: undefined, account: '' } })
  await openFixture(desktop)
  await page.getByRole('button', { name: 'Settings', exact: true }).click()
  const app = page.getByRole('article', { name: 'Linear app integration' })
  await app.getByRole('button', { name: 'Configure Linear', exact: true }).click()
  await expect(app.getByLabel('Linear callback URL')).toHaveValue(status.callbackUrl)
  await expect(app).toContainText('PKCE')
  await expect(app.locator('input[type="password"]')).toHaveCount(0)
  await app.getByLabel('Linear client ID').fill('fixture-linear')
  await app.getByRole('button', { name: 'Save Linear app', exact: true }).click()
  await expect(app.getByRole('alert')).toContainText('could not be saved')
  await expect(app.getByLabel('Linear client ID')).toHaveValue('fixture-linear')
  reject = false
  await app.getByRole('button', { name: 'Save Linear app', exact: true }).click()
  await app.getByRole('button', { name: 'Authorize Linear', exact: true }).click()
  await expect(app.locator('.jira-authorization-status')).toContainText('Finish authorizing in your browser')
  await app.getByRole('button', { name: 'Cancel Linear authorization', exact: true }).click()
  await app.getByRole('button', { name: 'Authorize Linear', exact: true }).click()
  // The click starts an async RPC. Let the provider receive it before the
  // fixture completes consent, otherwise its handler can overwrite idle state.
  await expect(app.locator('.jira-authorization-status')).toContainText('Finish authorizing in your browser')
  current = { ...status }
  await expect(app.getByText('Connected', { exact: true })).toBeVisible()
  await expect(app).toContainText('Product team · Reviewer')
  await app.getByRole('button', { name: 'Disconnect Linear', exact: true }).click()
  await app.getByRole('button', { name: 'Disconnect Linear account', exact: true }).click()
  await expect(app.getByText('Connected', { exact: true })).not.toBeVisible()
  await expect(app.getByRole('button', { name: 'Authorize Linear', exact: true })).toBeVisible()
})

test('Linear renders Markdown beside the editor and links attachments without fetching them', async ({ desktop, page }) => {
  const requestedImages: string[] = []
  await page.route('https://private-image.example.test/**', route => { requestedImages.push(route.request().url()); return route.abort() })
  await openReview(desktop)
  await page.getByRole('tab', { name: /Changes/ }).click()
  await page.getByRole('button', { name: /^ENG-123 Requirements/ }).click()
  const panel = page.getByRole('region', { name: 'Linear ticket ENG-123', exact: true })
  await expect(panel.getByRole('heading', { name: 'Requirements', exact: true })).toBeVisible()
  await expect(panel.locator('input[type="checkbox"]')).toBeChecked()
  await expect(panel.getByRole('table')).toContainText('Preserve drafts')
  await expect(panel.getByRole('link', { name: 'Attachment: Design attachment', exact: true })).toHaveAttribute('href', 'https://private-image.example.test/diagram.png')
  await expect(panel.locator('img')).toHaveCount(0)
  await expect(panel.locator('a[href^="javascript:"]')).toHaveCount(0)
  await expect(panel.locator('strong')).toContainText(['ENG-123', 'Reviewer', 'verify'])
  await expect(panel.getByRole('button', { name: 'Open in Linear', exact: true })).toBeVisible()
  const code = (await page.locator('.review-diff-scroll').boundingBox())!, ticket = (await panel.boundingBox())!
  expect(code.x + code.width).toBeLessThanOrEqual(ticket.x + 1)
  expect(requestedImages).toEqual([])
  await panel.getByRole('button', { name: 'Close Linear ticket' }).press('Escape')
  await expect(panel).not.toBeVisible()
  await expect(page.getByRole('button', { name: /^ENG-123 Requirements/ })).toBeFocused()
})

test('provider switching keeps the editor and draft mounted', async ({ desktop, page }) => {
  const jiraSite = { id: 'site-a', name: 'Jira workspace', url: 'https://team.atlassian.net' }
  const jira: JiraStatus = { configured: true, connected: true, clientId: 'jira-client', callbackPort: 43827, callbackUrl: 'http://localhost:43827/oauth/jira/callback', scopes: ['read:jira-work', 'offline_access'], state: 'idle', error: '', sites: [jiraSite], defaultSiteId: jiraSite.id }
  desktop.handlers.set('main.IntegrationService.GetJiraStatus', () => jira)
  desktop.handlers.set('main.IntegrationService.GetJiraTickets', () => ({ site: jiraSite, preferences: { siteId: jiraSite.id, projects: [], acceptanceField: '' }, tickets: [{ key: 'JIRA-1', sources: ['manual'], issue: { ...issue('JIRA-1'), description: 'Jira requirements' }, error: '' }], hidden: [], truncated: false }))
  desktop.handlers.set('main.IntegrationService.ReadJiraIssue', () => ({ ...issue('JIRA-1'), description: 'Jira requirements' }))
  await openFixture(desktop)
  await page.locator('.pr-list-item').first().click()
  await expect(page.getByRole('region', { name: 'Jira tickets', exact: true })).toContainText('JIRA-1')
  await page.getByRole('tab', { name: /Changes/ }).click()
  await page.getByRole('button', { name: 'Comment on changed line 1 in src/feature.ts', exact: true }).click()
  const draft = page.getByLabel('Review comment', { exact: true })
  await draft.fill('Preserve this draft across ticket providers.')
  await draft.evaluate(element => { (window as any).__linearDraftElement = element })
  await page.getByRole('button', { name: /^JIRA-1 Requirements/ }).click()
  await expect(page.getByRole('region', { name: 'Jira ticket JIRA-1', exact: true })).toContainText('Jira requirements')
  await page.getByRole('group', { name: 'Ticket provider' }).getByRole('button', { name: 'Linear', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Jira ticket JIRA-1', exact: true })).not.toBeVisible()
  await page.getByRole('button', { name: /^ENG-123 Requirements/ }).click()
  await expect(page.getByRole('region', { name: 'Linear ticket ENG-123', exact: true })).toContainText('Preserve drafts')
  await expect(draft).toHaveValue('Preserve this draft across ticket providers.')
  expect(await draft.evaluate(element => element === (window as any).__linearDraftElement)).toBe(true)
  await page.getByRole('separator', { name: 'Resize Linear ticket pane' }).press('Home')
  await expect(draft).toHaveValue('Preserve this draft across ticket providers.')
  await page.getByRole('button', { name: 'Close Linear ticket' }).click()
  expect(await draft.evaluate(element => element === (window as any).__linearDraftElement)).toBe(true)
})

test('Linear search and local selections respect repository team settings', async ({ desktop, page }) => {
  let keys = ['ENG-123'], hidden: string[] = []
  desktop.handlers.set('main.LinearService.GetLinearTickets', () => result(keys, hidden))
  desktop.handlers.set('main.LinearService.SearchLinearIssues', () => [issue('ENG-7')])
  desktop.handlers.set('main.LinearService.SelectLinearTicket', args => {
    const { key, action } = args[0]
    if (action === 'pin' || action === 'restore') { keys = [...new Set([...keys, key])]; hidden = hidden.filter(value => value !== key) }
    else { keys = keys.filter(value => value !== key); hidden.push(key) }
  })
  desktop.handlers.set('main.LinearService.SaveLinearRepositoryPreferences', args => { expect(args).toEqual([{ host: 'github.com', owner: 'fork', repo: 'app' }, { teams: ['ENG', 'APP'] }]) })
  await openReview(desktop)
  const tickets = page.getByRole('region', { name: 'Linear tickets', exact: true })
  await tickets.getByRole('button', { name: 'Add ticket', exact: true }).click()
  await tickets.getByLabel('Find a Linear ticket').fill('requirements')
  await tickets.getByRole('button', { name: /^ENG-7 Requirements/ }).click()
  await expect(tickets.locator('.jira-ticket-card')).toHaveCount(2)
  await tickets.getByRole('button', { name: 'Hide ENG-7', exact: true }).click()
  await expect(tickets.locator('.jira-ticket-card')).toHaveCount(1)
  await tickets.getByText('Hidden tickets (1)').click()
  await tickets.getByRole('button', { name: 'Restore ENG-7', exact: true }).click()
  await expect(tickets.locator('.jira-ticket-card')).toHaveCount(2)
  await tickets.getByRole('button', { name: 'Linear repository settings', exact: true }).click()
  await tickets.getByLabel('Linear team keys').fill('ENG, APP')
  await tickets.getByRole('button', { name: 'Save repository settings', exact: true }).click()
  await expect(tickets.getByLabel('Linear team keys')).not.toBeVisible()
})

test('failed refresh preserves cards and late details cannot reopen a closed reader', async ({ desktop, page }) => {
  await openReview(desktop)
  const tickets = page.getByRole('region', { name: 'Linear tickets', exact: true })
  desktop.handlers.set('main.LinearService.GetLinearTickets', () => { throw new Error('Linear is limiting requests. Wait before retrying.') })
  await tickets.getByRole('button', { name: 'Refresh Linear tickets' }).click()
  await expect(tickets.getByRole('alert')).toContainText('limiting requests')
  await expect(tickets).toContainText('Requirements for ENG-123')
  let finish: (() => void) | undefined
  desktop.handlers.set('main.LinearService.ReadLinearIssue', () => new Promise(resolve => { finish = () => resolve(issue()) }))
  await tickets.getByRole('button', { name: /^ENG-123 Requirements/ }).click()
  await expect.poll(() => typeof finish).toBe('function')
  await page.getByRole('button', { name: 'Close Linear ticket', exact: true }).click()
  finish!()
  await expect(page.getByRole('region', { name: 'Linear ticket ENG-123', exact: true })).not.toBeVisible()
})
