import { test as base, expect, type Page } from '@playwright/test'
import { desktopFixture, openFixture } from './desktopFixture'
import type { LSPServerStatus, LSPToolStatus } from '../src/services'
import { catalogServers } from './languageServerFixture'

const test = base.extend<{ desktop: Awaited<ReturnType<typeof desktopFixture>> }>({
  desktop: async ({ page }, use) => {
    const fixture = await desktopFixture(page)
    await use(fixture)
    expect(fixture.unexpected, 'all native calls have explicit fixtures').toEqual([])
    expect(fixture.pageErrors, 'no uncaught application exceptions').toEqual([])
  },
})
function server(language: 'php' | 'typescript' | 'vue', managed: Partial<LSPToolStatus> = {}, overrides: Partial<LSPServerStatus> = {}): LSPServerStatus {
  return {
    language, name: { php: 'Phpactor', typescript: 'TypeScript / JavaScript', vue: 'Vue' }[language],
    available: false, command: '', args: [], source: '', setupUrl: 'https://example.test/server-docs', error: 'Server not installed',
    configuration: { command: '', args: [] },
    managed: { installedVersion: '', latestVersion: language === 'php' ? '2026.10.07.0' : language === 'vue' ? '3.3.13' : '6.0.2', ready: false, updateAvailable: false, dependencyUpdate: false, state: 'idle', progress: '', installError: '', checkError: '', prerequisiteError: '', checkedAt: '2026-10-14T10:00:00Z', ...managed },
    ...overrides,
  }
}
function finish(item: LSPServerStatus) {
  item.available = true
  item.source = 'managed'
  item.command = '/fixture/managed/node'
  item.args = ['/fixture/managed/server.js', '--stdio']
  item.error = ''
  item.managed = { ...item.managed, installedVersion: item.managed.latestVersion, ready: true, updateAvailable: false, dependencyUpdate: false, state: 'idle', progress: 'Installation ready.', installError: '' }
}
function withServers(desktop: Awaited<ReturnType<typeof desktopFixture>>, servers: LSPServerStatus[]) {
  desktop.handlers.set('main.LSPService.Servers', () => structuredClone(servers))
  desktop.handlers.set('main.LSPService.CheckServerUpdates', () => structuredClone(servers))
}
async function settings(page: Page) {
  await page.getByRole('button', { name: 'Settings', exact: true }).click()
  const section = page.getByRole('region', { name: 'Language intelligence' })
  await expect(section.getByRole('heading', { level: 4 }).first()).toBeVisible()
  await expect(section.getByRole('button', { name: 'Check for language server updates' })).toBeEnabled()
  return section
}

test('installation progress resumes after navigation and supports cancellation and retry', async ({ desktop, page }) => {
  const servers = [server('php'), server('typescript'), server('vue')]
  withServers(desktop, servers)
  desktop.handlers.set('main.LSPService.InstallServer', args => {
    const item = servers.find(server => server.language === args[0])!
    item.managed.state = 'installing'
    item.managed.progress = 'Installing server packages and dependencies…'
    return null
  })
  desktop.handlers.set('main.LSPService.CancelServerInstall', args => {
    const item = servers.find(server => server.language === args[0])!
    item.managed.state = 'cancelled'
    item.managed.progress = 'Installation cancelled.'
    return null
  })
  await openFixture(desktop)
  let section = await settings(page)
  const install = section.getByRole('button', { name: 'Install TypeScript / JavaScript', exact: true })
  await expect(install).toBeEnabled()
  await install.evaluate(button => { (button as HTMLButtonElement).click(); (button as HTMLButtonElement).click() })
  await expect.poll(() => desktop.calls.filter(call => call.name.endsWith('.InstallServer')).length).toBe(1)
  await expect(section.getByText('Installing server packages and dependencies…', { exact: true })).toBeVisible()
  await expect(section.getByRole('button', { name: 'Install Vue', exact: true })).toBeDisabled()
  await page.locator('.sidebar').getByRole('button', { name: /^History/ }).click()
  await page.getByRole('button', { name: 'Settings', exact: true }).click()
  section = page.getByRole('region', { name: 'Language intelligence' })
  await expect(section.getByText('Installing server packages and dependencies…', { exact: true })).toBeVisible()
  await section.getByRole('button', { name: 'Cancel TypeScript / JavaScript installation' }).click()
  await expect(section.getByText('Installation cancelled.', { exact: true })).toBeVisible()
  await expect(section.getByRole('button', { name: 'Install TypeScript / JavaScript', exact: true })).toBeEnabled()
  await section.getByRole('button', { name: 'Install TypeScript / JavaScript', exact: true }).click()
  await expect(section.getByText('Installing server packages and dependencies…', { exact: true })).toBeVisible()
  finish(servers[1])
  await expect(section.getByText('Installed 6.0.2', { exact: true })).toBeVisible()
  await expect(section.getByRole('button', { name: 'Installed TypeScript / JavaScript', exact: true })).toBeDisabled()
  await expect(section.getByText('Up to date', { exact: true })).toBeVisible()
})

test('pending updates preserve the installed version after failure and can be retried', async ({ desktop, page }) => {
  const item = server('typescript', { installedVersion: '6.0.1', ready: true, updateAvailable: true }, { available: true, source: 'managed', error: '' })
  withServers(desktop, [item])
  desktop.handlers.set('main.LSPService.InstallServer', () => { item.managed.state = 'updating'; item.managed.progress = 'Downloading the update…'; item.managed.installError = ''; return null })
  await openFixture(desktop)
  const section = await settings(page)
  await expect(section.getByText('Update available', { exact: true })).toBeVisible()
  await expect(section.getByText('Installed 6.0.1', { exact: true })).toBeVisible()
  await section.getByRole('button', { name: 'Update TypeScript / JavaScript', exact: true }).click()
  await expect(section.getByRole('button', { name: 'Updating TypeScript / JavaScript', exact: true })).toBeDisabled()
  await expect(section.getByText('Downloading the update…', { exact: true })).toBeVisible()
  item.managed.state = 'failed'
  item.managed.installError = 'Download failed'
  item.managed.progress = ''
  await expect(section.getByRole('alert')).toContainText('Your previous installation is preserved')
  await expect(section.getByText('Installed 6.0.1', { exact: true })).toBeVisible()
  await expect(section.getByText('Update available', { exact: true })).toBeVisible()
  await section.getByRole('button', { name: 'Retry update TypeScript / JavaScript', exact: true }).click()
  await expect(section.getByText('Downloading the update…', { exact: true })).toBeVisible()
  finish(item)
  await expect(section.getByText('Installed 6.0.2', { exact: true })).toBeVisible()
  await expect(section.getByText('Update available', { exact: true })).not.toBeVisible()
})

test('servers can be set up without a repository and prerequisites and repair are explicit', async ({ desktop, page }) => {
  const php = server('php', { prerequisiteError: 'Install PHP 8.2 or newer with mbstring, tokenizer, and posix, then check again' })
  const typescript = server('typescript', { installedVersion: '6.0.2', ready: true }, { available: true, source: 'managed', error: '' })
  const vue = server('vue', { installedVersion: '3.3.13', ready: false, installError: 'The M.I.E.R. installation is incomplete. Reinstall this server to repair it.' })
  withServers(desktop, [php, typescript, vue])
  await page.goto('/')
  const section = await settings(page)
  await expect(section).toContainText('You can set up servers before opening a repository')
  await expect(section.getByRole('button', { name: 'Install Phpactor', exact: true })).toBeDisabled()
  await expect(section).toContainText('Install PHP 8.2 or newer')
  await expect(section.getByRole('button', { name: 'Installed TypeScript / JavaScript', exact: true })).toBeDisabled()
  await expect(section.getByRole('button', { name: 'Reinstall Vue', exact: true })).toBeEnabled()
  expect(desktop.calls.filter(call => call.name.endsWith('.InstallServer'))).toHaveLength(0)
  expect(desktop.calls.find(call => call.name.endsWith('.Servers'))?.args).toEqual([''])
})

test('offline checks retain update state and custom executables switch only when requested', async ({ desktop, page }) => {
  const item = server('typescript', { installedVersion: '6.0.1', ready: true, updateAvailable: true, checkError: 'Provider offline' }, { available: true, source: 'configured', command: '/my/server', error: '', configuration: { command: '/my/server', args: ['--stdio'] } })
  withServers(desktop, [item])
  desktop.handlers.set('main.LSPService.ConfigureServer', args => {
    expect(args).toEqual(['typescript', { command: '', args: [] }])
    item.source = 'managed'
    item.configuration = { command: '', args: [] }
    return null
  })
  await openFixture(desktop)
  const section = await settings(page)
  await expect(section.getByText('Update available', { exact: true })).toBeVisible()
  await expect(section).toContainText('Could not check for updates: Provider offline')
  await expect(section).toContainText('Configured executable')
  expect(desktop.calls.filter(call => call.name.endsWith('.ConfigureServer'))).toHaveLength(0)
  item.managed.checkError = ''
  await section.getByRole('button', { name: 'Check for language server updates' }).click()
  await expect(section.getByText('Could not check for updates: Provider offline', { exact: true })).not.toBeVisible()
  expect(desktop.calls.filter(call => call.name.endsWith('.CheckServerUpdates')).at(-1)?.args[1]).toBe(true)
  await section.getByRole('button', { name: 'Use installed TypeScript / JavaScript' }).click()
  await expect(section).toContainText('M.I.E.R. installation')
  expect(desktop.calls.filter(call => call.name.endsWith('.InstallServer'))).toHaveLength(0)
})

test('a background installation refreshes code navigation without reopening settings', async ({ desktop, page }) => {
  const item = server('typescript')
  withServers(desktop, [item])
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button', { name: /^Changes/ }).click()
  await expect(page.locator('.code-intelligence-hint')).toContainText('Language servers need setup')
  finish(item)
  await page.evaluate(() => (window as any)._wails.dispatchWailsEvent({ name: 'lsp:changed', data: {} }))
  await expect(page.locator('.code-intelligence-hint')).toContainText('Hover symbols · Ctrl/Cmd click or F12 for definition')
})

test('the full catalog searches file types, categories, installed SDKs, and pending updates', async ({ desktop, page }) => {
  const servers = catalogServers()
  const yaml = servers.find(server => server.language === 'yaml')!
  yaml.available = true
  yaml.source = 'managed'
  Object.assign(yaml.managed, { installedVersion: '1.0.0', latestVersion: '1.1.0', ready: true, updateAvailable: true })
  servers.find(server => server.language === 'rust')!.available = true
  withServers(desktop, servers)
  await openFixture(desktop)
  const section = await settings(page)
  await expect(section.locator('article:visible')).toHaveCount(servers.length)
  expect(servers.length).toBeGreaterThanOrEqual(40)
  await expect(section.locator('.language-server-mark img')).toHaveCount(servers.length)
  const search = section.getByRole('searchbox', { name: 'Search language servers' })
  await search.fill('docker')
  await expect(section.getByRole('heading', { name: 'Dockerfile', exact: true })).toBeVisible()
  await expect.poll(() => section.locator('[data-language="dockerfile"] img').evaluate(image => (image as HTMLImageElement).naturalWidth)).toBeGreaterThan(0)
  await expect(section.getByRole('heading', { name: 'Docker Compose', exact: true })).toBeVisible()
  await search.fill('.github/workflows')
  await expect(section.locator('article:visible')).toHaveCount(1)
  await expect(section.getByRole('heading', { name: 'GitHub Actions', exact: true })).toBeVisible()
  await section.getByRole('button', { name: 'Clear language server search' }).click()
  await section.getByRole('combobox', { name: 'Language server category' }).selectOption('Configuration')
  await expect(section.locator('article:visible')).toHaveCount(servers.filter(server => server.category === 'Configuration').length)
  await section.getByRole('combobox', { name: 'Language server category' }).selectOption('')
  await section.getByRole('button', { name: /^Installed/ }).click()
  await expect(section.locator('article:visible')).toHaveCount(2)
  const rust = section.locator('article[data-language="rust"]')
  await expect(rust.getByRole('button', { name: /^Install/ })).toHaveCount(0)
  await expect(rust.getByRole('button', { name: 'Configure Rust · rust-analyzer' })).toBeVisible()
  await section.getByRole('button', { name: /^Updates/ }).click()
  await expect(section.locator('article:visible')).toHaveCount(1)
  await expect(section.getByRole('button', { name: 'Update YAML', exact: true })).toBeEnabled()
  await search.fill('not-a-language-server')
  await expect(section.getByText('No matching language servers', { exact: true })).toBeVisible()
  await section.getByRole('button', { name: 'Show all servers', exact: true }).click()
  await expect(section.locator('article:visible')).toHaveCount(servers.length)
})

test('custom configuration survives filtering and does not require a TypeScript SDK for other servers', async ({ desktop, page }) => {
  const servers = catalogServers()
  withServers(desktop, servers)
  desktop.handlers.set('main.LSPService.ConfigureServer', args => {
    expect(args[0]).toBe('dockerfile')
    expect(args[1]).toMatchObject({ command: '/custom/docker-langserver', args: ['--stdio'], typescriptSdkPath: '', vuePluginPath: '' })
    return null
  })
  await openFixture(desktop)
  const section = await settings(page)
  await section.getByRole('searchbox').fill('dockerfile')
  await section.getByRole('button', { name: 'Configure Dockerfile', exact: true }).click()
  await section.getByRole('textbox', { name: 'Dockerfile executable', exact: true }).fill('/custom/docker-langserver')
  await expect(section.getByRole('textbox', { name: 'Dockerfile arguments', exact: true })).toHaveValue('--stdio')
  await expect(section.getByRole('textbox', { name: /TypeScript SDK/ })).toHaveCount(0)
  await section.getByRole('searchbox').fill('go')
  await expect(section.getByRole('textbox', { name: 'Dockerfile executable', exact: true })).not.toBeVisible()
  await section.getByRole('searchbox').fill('dockerfile')
  await expect(section.getByRole('textbox', { name: 'Dockerfile executable', exact: true })).toHaveValue('/custom/docker-langserver')
  await section.getByRole('button', { name: 'Save configuration', exact: true }).click()
  await expect(section).toContainText('Language server configuration saved.')
})

test('filtering cannot hide cancellation of a running catalog installation', async ({ desktop, page }) => {
  const servers = catalogServers(), item = servers.find(server => server.language === 'dockerfile')!
  withServers(desktop, servers)
  desktop.handlers.set('main.LSPService.InstallServer', () => { item.managed.state = 'installing'; item.managed.progress = 'Installing Dockerfile…'; return null })
  desktop.handlers.set('main.LSPService.CancelServerInstall', args => { expect(args).toEqual(['dockerfile']); item.managed.state = 'cancelled'; item.managed.progress = 'Installation cancelled.'; return null })
  await openFixture(desktop)
  const section = await settings(page)
  await section.getByRole('searchbox').fill('dockerfile')
  await section.getByRole('button', { name: 'Install Dockerfile', exact: true }).click()
  await expect(section.getByText('Installing Dockerfile…', { exact: true })).toBeVisible()
  await section.getByRole('searchbox').fill('nginx')
  await expect(section.locator('.language-server-active-install')).toContainText('Installing Dockerfile…')
  await section.getByRole('button', { name: 'Cancel Dockerfile installation', exact: true }).click()
  await expect(section.locator('.language-server-active-install')).toHaveCount(0)
  await section.getByRole('searchbox').fill('dockerfile')
  await expect(section.getByText('Installation cancelled.', { exact: true })).toBeVisible()
})
