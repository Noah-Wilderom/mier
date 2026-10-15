import { strict as assert } from 'node:assert'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import type { LSPServerStatus } from './services'
import { filterServers, installedServer, serverForFile } from './languageServerCatalog.ts'
import { languageServerLogo } from './languageServerLogos.ts'

const definitions = JSON.parse(readFileSync(new URL('../../internal/lspclient/catalog.json', import.meta.url), 'utf8'))
const catalog: LSPServerStatus[] = definitions.map((definition: any) => ({
  language: definition.id, name: definition.name, category: definition.category, description: definition.description,
  fileTypes: definition.files, requirements: definition.install.requires, installMethod: definition.install.method,
  available: false, configuration: { command: '', args: [] }, managed: {},
}))

test('all catalog file associations are understood by the editor', () => {
  assert.ok(catalog.length >= 40)
  for (const server of catalog) for (const file of server.fileTypes!) {
    const path = file.pattern.split('**/').join('').split('*').join('example').split('?').join('x')
    assert.equal(serverForFile(path, [server])?.language, server.language, path)
  }
})

test('every catalog entry has a bundled SVG logo and an attributed source', () => {
  const sources = JSON.parse(readFileSync(new URL('../public/language-servers/sources.json', import.meta.url), 'utf8'))
  for (const server of catalog) {
    const logo = languageServerLogo(server.language)
    assert.ok(logo, server.language)
    const svg = readFileSync(new URL(`../public${logo}`, import.meta.url), 'utf8')
    assert.match(svg, /<svg\s/)
    assert.match(sources[server.language], /^https:\/\/raw\.githubusercontent\.com\//)
    assert.doesNotMatch(svg, /<script|<foreignObject|\bonload\s*=|href="https?:/i)
  }
  assert.equal(languageServerLogo('unknown'), undefined)
})

test('dedicated configuration servers take priority and fall back to available YAML', () => {
  const cases = [
    ['Dockerfile', 'dockerfile'], ['build/Containerfile.dev', 'dockerfile'],
    ['.github/workflows/ci.yml', 'github-actions'], ['.github/actions/check/action.yaml', 'github-actions'],
    ['compose.override.yaml', 'docker-compose'], ['infra/docker-compose.yml', 'docker-compose'],
    ['infra/playbooks/deploy/main.yml', 'ansible'], ['roles/api/tasks/setup/main.yaml', 'ansible'],
    ['templates/deployment.yaml', 'helm'], ['deploy/charts/api/templates/nested/service.yml', 'helm'],
    ['azure-pipelines.yml', 'azure-pipelines'], ['settings.yaml', 'yaml'],
    ['main.cpp', 'cpp'], ['script.py', 'python'], ['main.go', 'go'], ['app.tsx', 'typescript'],
  ]
  for (const [path, server] of cases) assert.equal(serverForFile(path, catalog)?.language, server, path)
  const available = catalog.map(server => ({ ...server, available: server.language === 'yaml' }))
  assert.equal(serverForFile('.github/workflows/ci.yml', available)?.language, 'yaml')
  const both = available.map(server => ({ ...server, available: server.available || server.language === 'github-actions' }))
  assert.equal(serverForFile('.github/workflows/ci.yml', both)?.language, 'github-actions')
  assert.equal(serverForFile('.github/other/ci.yml', both)?.language, 'yaml')
})

test('routing accepts Windows paths, case variants, and opaque definition language IDs', () => {
  assert.equal(serverForFile('src\\Dockerfile.PROD', catalog)?.language, 'dockerfile')
  assert.equal(serverForFile('SRC/MAIN.CPP', catalog)?.language, 'cpp')
  assert.equal(serverForFile('mier-lsp://definition/opaque', catalog, 'cpp')?.language, 'cpp')
  assert.equal(serverForFile('mier-lsp://definition/opaque', catalog, 'javascriptreact')?.language, 'typescript')
  assert.equal(serverForFile('mier-lsp://definition/opaque', catalog, 'yaml.ansible')?.language, 'ansible')
  assert.equal(serverForFile('photo.png', catalog), undefined)
  assert.equal(serverForFile('photo.png', catalog, 'not-supported'), undefined)
})

test('a new status response invalidates cached unavailable routes', () => {
  assert.equal(serverForFile('compose.yml', catalog)?.language, 'docker-compose')
  const refreshed = catalog.map(server => ({ ...server, available: server.language === 'yaml' }))
  assert.equal(serverForFile('compose.yml', refreshed)?.language, 'yaml')
  assert.equal(serverForFile('compose.yml', catalog)?.language, 'docker-compose')
})

test('catalog search includes file names, descriptions, and prerequisites', () => {
  assert.ok(filterServers(catalog, 'docker', 'all', '').length >= 2)
  assert.deepEqual(filterServers(catalog, '.github/workflows', 'all', '').map(server => server.language), ['github-actions'])
  assert.deepEqual(filterServers(catalog, 'jdt jdk', 'all', '').map(server => server.language), ['java'])
  assert.ok(filterServers(catalog, '', 'all', 'Configuration').every(server => server.category === 'Configuration'))
  assert.equal(filterServers(catalog, 'not-a-language-server', 'all', '').length, 0)
})

test('installed and update filters include managed repairs and available SDKs accurately', () => {
  const statuses = catalog.map(server => ({ ...server, available: server.language === 'rust', managed: { ...server.managed, installedVersion: server.language === 'yaml' ? '1.0.0' : '', updateAvailable: server.language === 'yaml' } }))
  assert.deepEqual(filterServers(statuses, '', 'installed', '').map(server => server.language).sort(), ['rust', 'yaml'])
  assert.deepEqual(filterServers(statuses, '', 'updates', '').map(server => server.language), ['yaml'])
  assert.ok(installedServer(statuses.find(server => server.language === 'rust')!))
  assert.equal(filterServers(statuses, 'rust', 'updates', '').length, 0)
})

test('older server responses retain TypeScript, JavaScript, PHP, and Vue navigation', () => {
  const legacy = ['php', 'typescript', 'vue'].map(language => ({ language, available: true } as LSPServerStatus))
  for (const [path, language] of [['test.php', 'php'], ['app.tsx', 'typescript'], ['app.jsx', 'typescript'], ['App.vue', 'vue']]) assert.equal(serverForFile(path, legacy)?.language, language)
})
