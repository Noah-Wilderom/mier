import type { LSPServerStatus } from './services'

export type ServerFilter = 'all' | 'installed' | 'updates'

// Preserve navigation when a pre-catalog backend or test fixture supplies the
// original three servers without association metadata.
const legacyFiles: Record<string, { pattern: string; languageId: string }[]> = {
  php: ['php', 'phtml'].map(extension => ({ pattern: `*.${extension}`, languageId: 'php' })),
  typescript: ['ts', 'tsx', 'mts', 'cts', 'js', 'jsx', 'mjs', 'cjs'].map(extension => ({ pattern: `*.${extension}`, languageId: 'typescript' })),
  vue: [{ pattern: '*.vue', languageId: 'vue' }],
}
const expressions = new Map<string, RegExp>()
const selectedServers = new WeakMap<LSPServerStatus[], Map<string, LSPServerStatus | undefined>>()

function matches(pattern: string, filename: string): boolean {
  let expression = expressions.get(pattern)
  if (!expression) {
    let source = ''
    const normalized = pattern.toLowerCase()
    for (let index = 0; index < normalized.length; index++) {
      const character = normalized[index]
      if (normalized.slice(index).startsWith('**/')) { source += '(?:[^/]+/)*'; index += 2 }
      else source += character === '*' ? '[^/]*' : character === '?' ? '[^/]' : character.replace(/[\\^$.*+?()[\]{}|]/g, '\\$&')
    }
    expression = new RegExp(`^${source}$`)
    expressions.set(pattern, expression)
  }
  const candidate = pattern.includes('/') ? filename : filename.split('/').pop() || ''
  return expression.test(candidate)
}

export function serverForFile(path: string, servers: LSPServerStatus[], suppliedLanguage?: string): LSPServerStatus | undefined {
  const filename = path.replace(/\\/g, '/').toLowerCase()
  let cache = selectedServers.get(servers)
  if (!cache) { cache = new Map(); selectedServers.set(servers, cache) }
  const key = `${filename}\0${suppliedLanguage || ''}`
  if (cache.has(key)) return cache.get(key)
  const candidates = servers.flatMap(server => {
    const associations = server.fileTypes ?? legacyFiles[server.language] ?? []
    const scores = associations.filter(file => matches(file.pattern, filename)).map(file => file.pattern.replace(/[*?]/g, '').length + (file.pattern.includes('/') ? 1000 : 0))
    return scores.length ? [{ server, score: Math.max(...scores) }] : []
  }).sort((a, b) => b.score - a.score)
  let selected: LSPServerStatus | undefined
  if (candidates.length) selected = candidates.find(candidate => candidate.server.available)?.server ?? candidates[0].server
  else if (suppliedLanguage) {
    const normalized = ['javascript', 'typescriptreact', 'javascriptreact'].includes(suppliedLanguage) ? 'typescript' : suppliedLanguage
    const supplied = servers.filter(server => server.language === normalized || server.fileTypes?.some(file => file.languageId === suppliedLanguage))
    selected = supplied.find(server => server.available) ?? supplied[0]
  }
  if (cache.size >= 512) cache.clear()
  cache.set(key, selected)
  return selected
}

export function installedServer(server: LSPServerStatus): boolean {
  return server.available || !!server.managed?.installedVersion
}

export function filterServers(servers: LSPServerStatus[], query: string, filter: ServerFilter, category: string): LSPServerStatus[] {
  const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean)
  return servers.filter(server => {
    if (filter === 'installed' && !installedServer(server)) return false
    if (filter === 'updates' && !server.managed?.updateAvailable) return false
    if (category && (server.category || 'Languages') !== category) return false
    const search = [server.name, server.language, server.description, server.category, server.requirements, ...(server.fileTypes ?? []).map(file => file.pattern)].join(' ').toLowerCase()
    return terms.every(term => search.includes(term))
  })
}
