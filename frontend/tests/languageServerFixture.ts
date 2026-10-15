import { readFileSync } from 'node:fs'
import type { LSPServerStatus } from '../src/services'

export function catalogServers(): LSPServerStatus[] {
  const definitions = JSON.parse(readFileSync(new URL('../../internal/lspclient/catalog.json', import.meta.url), 'utf8'))
  return definitions.map((definition: any) => ({
    language: definition.id, name: definition.name, label: definition.label, category: definition.category,
    description: definition.description, fileTypes: definition.files, installMethod: definition.install.method,
    requirements: definition.install.requires, defaultCommand: definition.program, defaultArgs: definition.args,
    available: false, command: '', args: [], source: '', setupUrl: definition.homepage, error: 'Server not installed',
    configuration: { command: '', args: [] },
    managed: { installedVersion: '', latestVersion: '', ready: false, updateAvailable: false, dependencyUpdate: false, state: 'idle', progress: '', installError: '', checkError: '', prerequisiteError: '', checkedAt: '' },
  }))
}
