import { test as base, expect } from '@playwright/test'
import { desktopFixture, openFixture, detail, summary } from './desktopFixture'
import type { JiraIssue, JiraStatus, JiraTickets } from '../src/services'

const site = { id: 'site-a', name: 'Team Jira', url: 'https://team.atlassian.net' }
const otherSite = { id: 'site-b', name: 'Other Jira', url: 'https://other.atlassian.net' }
const status: JiraStatus = { configured: true, connected: true, clientId: 'fixture-client', callbackPort: 43827, callbackUrl: 'http://localhost:43827/oauth/jira/callback', scopes: ['read:jira-work', 'offline_access'], state: 'idle', error: '', sites: [site, otherSite], defaultSiteId: site.id }
const text = (value: string) => ({ type: 'paragraph', content: [{ type: 'text', text: value }] })
function issue(key = 'FAN-2187'): JiraIssue {
  return { id: `issue-${key}`, key, summary: `Review requirements for ${key}`, url: `${site.url}/browse/${key}`, type: 'Story', status: 'In Progress', statusCategory: 'indeterminate', assignee: 'Developer', priority: 'High', labels: ['review'], parentKey: 'FAN-100', updatedAt: '2026-10-14T12:00:00Z', description: { type: 'doc', content: [text('Keep the original source unchanged.'), { type: 'paragraph', content: [{ type: 'text', text: '<script>literal markup</script>' }, { type: 'text', text: 'Unsafe link', marks: [{ type: 'link', attrs: { href: 'javascript:alert(1)' } }] }] }, { type: 'bulletList', content: [{ type: 'listItem', content: [text('Hover and definitions must work.')] }] }, { type: 'table', content: [{ type: 'tableRow', content: [{ type: 'tableHeader', content: [text('Requirement')] }, { type: 'tableCell', content: [text('Verified')] }] }] }] }, acceptanceCriteria: { type: 'doc', content: [{ type: 'taskList', content: [{ type: 'taskItem', attrs: { state: 'DONE' }, content: [{ type: 'text', text: 'Read-only definitions' }] }] }] }, comments: [{ id: 'comment-1', author: 'Reviewer', createdAt: '2026-10-14T12:00:00Z', body: { type: 'doc', content: [text('Please verify the requirements.')] } }], commentsError: '' }
}
const test = base.extend<{ desktop: Awaited<ReturnType<typeof desktopFixture>> }>({
  desktop: async ({ page }, use) => {
    const fixture = await desktopFixture(page)
    fixture.prs = [{ ...summary(), title: 'FAN-2187: review improvement', head: { ...summary().head, ref: 'FAN-2187-update-calcasa' } }]
    fixture.currentDetail = { ...detail(), pullRequest: fixture.prs[0] }
    fixture.handlers.set('main.IntegrationService.GetJiraStatus', () => status)
    fixture.handlers.set('main.IntegrationService.GetJiraTickets', () => ({ site, preferences: { siteId: site.id, projects: ['FAN'], acceptanceField: 'customfield_10001' }, tickets: [{ key: 'FAN-2187', sources: ['branch', 'title'], issue: issue(), error: '' }], hidden: [], truncated: false } satisfies JiraTickets))
    fixture.handlers.set('main.IntegrationService.ReadJiraIssue', args => issue(args[0].key))
    await use(fixture)
    expect(fixture.unexpected).toEqual([])
    expect(fixture.pageErrors).toEqual([])
  },
})
async function openReview(desktop: Awaited<ReturnType<typeof desktopFixture>>) {
  await openFixture(desktop)
  await desktop.page.locator('.pr-list-item').first().click()
  await expect(desktop.page.getByRole('region', { name: 'Jira tickets', exact: true })).toContainText('Review requirements for FAN-2187')
}

test('own app setup preserves failed entries, authorizes, selects a site, and disconnects', async ({ desktop, page }) => {
  let current: JiraStatus = { ...status, configured: false, connected: false, clientId: '', sites: [], defaultSiteId: '' }
  desktop.handlers.set('main.IntegrationService.GetJiraStatus', () => current)
  let reject = true
  desktop.handlers.set('main.IntegrationService.ConfigureJira', args => {
    if (reject) throw new Error('Unlock your OS keychain and retry.')
    current = { ...current, configured: true, clientId: args[0].clientId, callbackPort: args[0].callbackPort }
    return null
  })
  desktop.handlers.set('main.IntegrationService.BeginJiraConnection', () => { current = { ...current, state: 'authorizing' }; return 'https://auth.atlassian.com/authorize?client_id=fixture-client' })
  desktop.handlers.set('main.IntegrationService.SetJiraDefaultSite', args => { current = { ...current, defaultSiteId: args[0] }; return null })
  desktop.handlers.set('main.IntegrationService.DisconnectJira', () => { current = { ...current, connected: false, sites: [], defaultSiteId: '' }; return null })
  await openFixture(desktop)
  await page.getByRole('button', { name: 'Settings', exact: true }).click()
  await page.getByRole('button', { name: 'Configure Jira', exact: true }).click()
  await expect(page.getByLabel('Jira callback URL')).toHaveValue(status.callbackUrl)
  await page.getByLabel('Jira client ID').fill('fixture-client')
  await page.getByLabel('Jira client secret').fill('fixture-secret-only')
  await expect(page.getByLabel('Jira client secret')).toHaveAttribute('type', 'password')
  await page.getByRole('button', { name: 'Save app credentials' }).click()
  await expect(page.getByRole('alert')).toContainText('Unlock your OS keychain')
  await expect(page.getByLabel('Jira client secret')).toHaveValue('fixture-secret-only')
  reject = false
  await page.getByRole('button', { name: 'Save app credentials' }).click()
  await page.getByRole('button', { name: 'Authorize Jira', exact: true }).click()
  await expect(page.locator('.jira-authorization-status')).toContainText('Finish authorizing in your browser')
  current = { ...status }
  await expect(page.getByText('Connected', { exact: true })).toBeVisible()
  await page.getByLabel('Default Jira site').selectOption(otherSite.id)
  await expect.poll(() => current.defaultSiteId).toBe(otherSite.id)
  await page.getByRole('button', { name: 'App credentials', exact: true }).click()
  await expect(page.getByLabel('Jira client secret')).toHaveValue('')
  expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain('fixture-secret-only')
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await page.getByRole('button', { name: 'Disconnect account', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Authorize Jira', exact: true })).toBeVisible()
  await expect(page.getByText('Connected', { exact: true })).not.toBeVisible()
})

test('related tickets remain available while reviewing changes and render genuine rich text', async ({ desktop, page }) => {
  await openReview(desktop)
  const tickets = page.getByRole('region', { name: 'Jira tickets', exact: true })
  await expect(tickets).toContainText('Branch · PR title')
  await page.getByRole('tab', { name: /Changes/ }).click()
  await expect(page.locator('.review-workspace')).toBeVisible()
  const open = tickets.getByRole('button', { name: /^FAN-2187 Review requirements/ })
  await open.click()
  const panel = page.getByRole('dialog', { name: 'Jira ticket FAN-2187', exact: true })
  await expect(panel).toContainText('Keep the original source unchanged.')
  await expect(panel).toContainText('<script>literal markup</script>')
  await expect(panel.locator('script')).toHaveCount(0)
  await expect(panel.getByRole('link', { name: 'Unsafe link' })).toHaveCount(0)
  await expect(panel.getByRole('table')).toContainText('RequirementVerified')
  await expect(panel).toContainText('Read-only definitions')
  await expect(panel).toContainText('Please verify the requirements.')
  await expect(panel.getByRole('button', { name: 'Close Jira ticket' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(panel).not.toBeVisible()
  await expect(open).toBeFocused()
  await expect(page.locator('.review-workspace')).toBeVisible()
  expect(desktop.calls.find(call => call.name.endsWith('.GetJiraTickets'))!.args[0]).toMatchObject({ number: 12, branch: 'FAN-2187-update-calcasa', repository: { host: 'github.com', owner: 'fork', repo: 'app' } })
})

test('manual search, hide and restore, and repository mapping refresh the selected PR', async ({ desktop, page }) => {
  const hidden: string[] = [], pinned: string[] = []
  desktop.handlers.set('main.IntegrationService.GetJiraTickets', () => ({ site, preferences: { siteId: site.id, projects: ['FAN'], acceptanceField: '' }, tickets: [...pinned, 'FAN-2187'].filter(key => !hidden.includes(key)).map(key => ({ key, sources: [key === 'FAN-2187' ? 'branch' : 'manual'], issue: issue(key), error: '' })), hidden, truncated: false }))
  desktop.handlers.set('main.IntegrationService.SearchJiraIssues', () => [issue('FAN-7')])
  desktop.handlers.set('main.IntegrationService.SelectJiraTicket', args => { const value = args[0]; if (value.action === 'pin') pinned.push(value.key); if (value.action === 'hide') hidden.push(value.key); if (value.action === 'restore') hidden.splice(hidden.indexOf(value.key), 1); return null })
  desktop.handlers.set('main.IntegrationService.SaveJiraRepositoryPreferences', () => null)
  await openReview(desktop)
  const tickets = page.getByRole('region', { name: 'Jira tickets', exact: true })
  await tickets.getByRole('button', { name: 'Add ticket', exact: true }).click()
  await page.getByLabel('Find a Jira ticket').fill('requirements')
  await page.getByRole('button', { name: /FAN-7 Review requirements/ }).click()
  await expect(tickets).toContainText('Manually added')
  await tickets.getByRole('button', { name: 'Hide FAN-7', exact: true }).click()
  await expect(tickets.getByRole('button', { name: 'Hide FAN-7', exact: true })).not.toBeVisible()
  await tickets.getByText('Hidden tickets (1)', { exact: true }).click()
  await tickets.getByRole('button', { name: 'Restore FAN-7', exact: true }).click()
  await expect(tickets.getByRole('button', { name: 'Hide FAN-7', exact: true })).toBeVisible()
  await tickets.getByRole('button', { name: 'Jira repository settings' }).click()
  await page.getByLabel('Repository Jira site').selectOption(otherSite.id)
  await page.getByLabel('Jira project keys').fill('FAN, APP')
  await page.getByLabel('Acceptance criteria field').fill('customfield_10001')
  await page.getByRole('button', { name: 'Save repository settings' }).click()
  await expect.poll(() => desktop.calls.find(call => call.name.endsWith('.SaveJiraRepositoryPreferences'))?.args).toEqual([{ host: 'github.com', owner: 'fork', repo: 'app' }, { siteId: otherSite.id, projects: ['FAN', 'APP'], acceptanceField: 'customfield_10001' }])
})

test('failed refresh retains tickets and dismissed details cannot reopen from a late response', async ({ desktop, page }) => {
  await openReview(desktop)
  const tickets = page.getByRole('region', { name: 'Jira tickets', exact: true })
  desktop.handlers.set('main.IntegrationService.GetJiraTickets', () => { throw new Error('Jira is unavailable. Retry shortly.') })
  await tickets.getByRole('button', { name: 'Refresh Jira tickets' }).click()
  await expect(tickets.getByRole('alert')).toContainText('Jira is unavailable')
  await expect(tickets).toContainText('Review requirements for FAN-2187')
  let finish!: () => void
  desktop.handlers.set('main.IntegrationService.ReadJiraIssue', async () => { await new Promise<void>(resolve => { finish = resolve }); return issue() })
  await tickets.getByRole('button', { name: /^FAN-2187 Review requirements/ }).click()
  await expect.poll(() => typeof finish).toBe('function')
  const panel = page.getByRole('dialog', { name: 'Jira ticket FAN-2187', exact: true })
  await panel.getByRole('button', { name: 'Close Jira ticket' }).click()
  finish()
  await expect(panel).not.toBeVisible()
  await page.getByRole('tab', { name: /Changes/ }).click()
  await expect(page.locator('.review-workspace')).toBeVisible()
})
