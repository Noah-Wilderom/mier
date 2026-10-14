import { useEffect, useRef, useState } from 'react'
import { Icon } from './Icon'
import { Markdown } from './Markdown'
import { hasNativeBackend, LSP_CONFIGURATION_CHANGED_EVENT, services, type LSPServerConfiguration, type LSPServerStatus } from './services'
import './LanguageServerSettings.css'

export interface LanguageServerSettingsProps { root?: string; available?: boolean }

function ServerConfiguration({ server, busy, onSave, onCancel }: {
  server: LSPServerStatus
  busy: boolean
  onSave: (configuration: LSPServerConfiguration) => Promise<void>
  onCancel: () => void
}) {
  const [command, setCommand] = useState(server.configuration.command)
  const [args, setArgs] = useState((server.configuration.args ?? []).join('\n'))
  const [typescriptSdkPath, setSDK] = useState(server.configuration.typescriptSdkPath ?? '')
  const [vuePluginPath, setPlugin] = useState(server.configuration.vuePluginPath ?? '')
  const customArgs = args.split(/\r?\n/).filter(argument => argument.length > 0)
  const invalidArgs = !command.trim() && customArgs.length > 0

  return <form className="language-server-form" onSubmit={event => {
    event.preventDefault()
    if (!busy && !invalidArgs) void onSave({ command: command.trim(), args: customArgs, typescriptSdkPath: typescriptSdkPath.trim(), vuePluginPath: vuePluginPath.trim() }).catch(() => { /* Parent displays the failure; inputs remain mounted. */ })
  }}>
    <label><span>Executable</span><input className="input" value={command} disabled={busy} onChange={event => setCommand(event.target.value)} placeholder={server.command || (server.language === 'php' ? 'phpactor' : server.language === 'vue' ? 'vue-language-server' : 'typescript-language-server')} spellCheck={false} aria-label={`${server.name} executable`} /></label>
    <p className="language-server-field-help">Leave empty to use M.I.E.R.’s installation, a project dependency, or your PATH.</p>
    <label><span>Arguments · one per line</span><textarea className="input" rows={3} value={args} disabled={busy} onChange={event => setArgs(event.target.value)} placeholder={server.language === 'php' ? 'language-server' : '--stdio'} spellCheck={false} aria-label={`${server.name} arguments`} /></label>
    <p className="language-server-field-help">Each line is passed as one argument. Include the server’s stdio argument when configuring an executable.</p>
    {invalidArgs && <p className="language-server-error" role="alert">Enter an executable to use custom arguments, or clear the arguments for automatic detection.</p>}
    {server.language !== 'php' && <><label><span>TypeScript SDK directory</span><input className="input" value={typescriptSdkPath} disabled={busy} onChange={event => setSDK(event.target.value)} placeholder="/path/to/node_modules/typescript/lib" spellCheck={false} aria-label={`${server.name} TypeScript SDK directory`} /></label><p className="language-server-field-help">Absolute path to the directory containing tsserver.js. Leave empty to detect it.</p></>}
    {server.language === 'vue' && <><label><span>Vue TypeScript plugin directory</span><input className="input" value={vuePluginPath} disabled={busy} onChange={event => setPlugin(event.target.value)} placeholder="/path/to/node_modules/@vue/typescript-plugin" spellCheck={false} aria-label="Vue TypeScript plugin directory" /></label><p className="language-server-field-help">Absolute path to the @vue/typescript-plugin package. Leave empty to detect it.</p></>}
    <div className="language-server-form-actions"><button className="btn" type="button" disabled={busy} onClick={() => {setCommand('');setArgs('');setSDK('');setPlugin('')}}>Use automatic detection</button><span /><button className="btn" type="button" disabled={busy} onClick={onCancel}>Cancel</button><button className="btn btn-primary" type="submit" disabled={busy || invalidArgs}>{busy ? 'Saving…' : 'Save configuration'}</button></div>
  </form>
}

export default function LanguageServerSettings({ root, available }: LanguageServerSettingsProps) {
  const [nativeAvailable, setNativeAvailable] = useState(hasNativeBackend)
  const [servers, setServers] = useState<LSPServerStatus[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editing, setEditing] = useState<string>()
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const [checking, setChecking] = useState(false)
  const [starting, setStarting] = useState<string>()
  const [cancelling, setCancelling] = useState(false)
  const mounted = useRef(true)
  const pending = useRef(false)
  const selectedRoot = useRef(root ?? '')
  const requestSequence = useRef(0)
  const previousServers = useRef<LSPServerStatus[]>([])
  selectedRoot.current = root ?? ''
  const enabled = available ?? nativeAvailable
  const busy = !!starting || servers.some(server => isInstalling(server.managed?.state))

  const applyServers = (value: LSPServerStatus[]) => {
    const changed = value.some(server => {
      const before = previousServers.current.find(item => item.language === server.language)?.managed
      return before && (before.installedVersion !== server.managed?.installedVersion || (isInstalling(before.state) && !isInstalling(server.managed?.state)))
    })
    previousServers.current = value
    setServers(value)
    if (changed) window.dispatchEvent(new CustomEvent(LSP_CONFIGURATION_CHANGED_EVENT))
  }
  const readServers = async (requestedRoot: string) => {
    const sequence = ++requestSequence.current
    const updated = await services.lsp.servers(requestedRoot)
    if (mounted.current && selectedRoot.current === requestedRoot && sequence === requestSequence.current) applyServers(updated)
    return updated
  }

  useEffect(() => {
    mounted.current = true
    const detect = () => setNativeAvailable(hasNativeBackend())
    window.addEventListener('wails:runtime-config-ready', detect)
    return () => {mounted.current = false;window.removeEventListener('wails:runtime-config-ready', detect)}
  }, [])

  useEffect(() => {
    let cancelled = false
    const requestedRoot = root ?? ''
    const sequence = ++requestSequence.current
    setError('')
    setStatus('')
    setEditing(undefined)
    previousServers.current = []
    setServers([])
    setChecking(false)
    if (!enabled) {setLoading(false);return}
    setLoading(true)
    void services.lsp.servers(requestedRoot).then(async value => {
      if (cancelled || sequence !== requestSequence.current) return
      applyServers(value)
      setLoading(false)
      setChecking(true)
      const updated = await services.lsp.checkServerUpdates(requestedRoot)
      if (!cancelled && sequence === requestSequence.current) applyServers(updated)
    }).catch(issue => {
      if (!cancelled && sequence === requestSequence.current) setError(issue instanceof Error ? issue.message : 'Language server availability could not be checked.')
    }).finally(() => {if (!cancelled) {setLoading(false);setChecking(false)}})
    return () => {cancelled = true}
  }, [root, enabled])

  useEffect(() => {
    if (!enabled || !busy) return
    let cancelled = false
    let timer: ReturnType<typeof setTimeout>
    const requestedRoot = root ?? ''
    const poll = async () => {
      try { await readServers(requestedRoot) }
      catch (issue) { if (!cancelled && mounted.current && selectedRoot.current === requestedRoot) setError(issue instanceof Error ? issue.message : 'Installation status could not be checked. Trying again…') }
      if (!cancelled) timer = setTimeout(() => {void poll()}, 1000)
    }
    timer = setTimeout(() => {void poll()}, 1000)
    return () => {cancelled = true;clearTimeout(timer)}
  }, [enabled, root, busy])

  useEffect(() => {
    if (!enabled) return
    const requestedRoot = root ?? ''
    return services.lsp.onChanged(() => {
      void readServers(requestedRoot).catch(issue => {
        if (mounted.current && selectedRoot.current === requestedRoot) setError(issue instanceof Error ? issue.message : 'Language server status could not be refreshed.')
      })
    })
  }, [enabled, root])

  const save = async (language: string, configuration: LSPServerConfiguration) => {
    if (!enabled || pending.current || busy) return
    const requestedRoot = root ?? ''
    pending.current = true
    ++requestSequence.current
    setSaving(true)
    setError('')
    setStatus('')
    let applied = false
    try {
      await services.lsp.configureServer(language, configuration)
      applied = true
      window.dispatchEvent(new CustomEvent(LSP_CONFIGURATION_CHANGED_EVENT))
      await readServers(requestedRoot)
      if (mounted.current && selectedRoot.current === requestedRoot) {
        setEditing(undefined)
        setStatus('Language server configuration saved.')
      }
    } catch (issue) {
      const message = issue instanceof Error ? issue.message : 'The language server configuration could not be saved.'
      if (mounted.current && selectedRoot.current === requestedRoot) setError(applied ? `Configuration saved, but availability could not be checked: ${message}` : message)
      throw issue
    } finally {
      pending.current = false
      if (mounted.current) setSaving(false)
    }
  }

  const checkUpdates = async () => {
    if (!enabled || checking || pending.current || busy) return
    const requestedRoot = root ?? '', sequence = ++requestSequence.current
    setChecking(true)
    setError('')
    try {
      const updated = await services.lsp.checkServerUpdates(requestedRoot, true)
      if (mounted.current && selectedRoot.current === requestedRoot && sequence === requestSequence.current) applyServers(updated)
    } catch (issue) {
      if (mounted.current && selectedRoot.current === requestedRoot && sequence === requestSequence.current) setError(issue instanceof Error ? issue.message : 'Updates could not be checked. Try again.')
    } finally {
      if (mounted.current && selectedRoot.current === requestedRoot) setChecking(false)
    }
  }
  const install = async (language: string) => {
    if (!enabled || pending.current || busy || checking) return
    const requestedRoot = root ?? ''
    pending.current = true
    ++requestSequence.current
    setStarting(language)
    setEditing(undefined)
    setError('')
    setStatus('')
    let started = false
    try {
      await services.lsp.installServer(language)
      started = true
      if (mounted.current && selectedRoot.current === requestedRoot) await readServers(requestedRoot)
    } catch (issue) {
      const message = issue instanceof Error ? issue.message : 'Installation could not be started.'
      if (mounted.current && selectedRoot.current === requestedRoot) setError(started ? `Installation started, but its status could not be loaded: ${message}. Check again before retrying.` : message)
    } finally {
      pending.current = false
      if (mounted.current) setStarting(undefined)
    }
  }
  const cancelInstall = async (language: string) => {
    if (cancelling) return
    const requestedRoot = root ?? ''
    setCancelling(true)
    try {
      await services.lsp.cancelServerInstall(language)
      if (mounted.current && selectedRoot.current === requestedRoot) await readServers(requestedRoot)
    } catch (issue) {
      if (mounted.current && selectedRoot.current === requestedRoot) setError(issue instanceof Error ? issue.message : 'Installation could not be cancelled.')
    } finally {
      if (mounted.current) setCancelling(false)
    }
  }

  return <section className="language-server-settings" aria-labelledby="language-server-settings-heading">
    <div className="language-server-settings-heading"><div><h3 id="language-server-settings-heading">Language intelligence</h3><p>Symbol information and go to definition for PHP, TypeScript, and Vue.</p></div><button className="btn" type="button" aria-label="Check for language server updates" disabled={!enabled || loading || saving || checking || busy} onClick={() => {void checkUpdates()}}><Icon name="refresh" size={14} className={checking ? 'is-spinning' : undefined} />{checking ? 'Checking…' : 'Check for updates'}</button></div>
    {!enabled ? <p className="language-server-intro">Language servers run in the desktop app.</p> : <>
      <p className="language-server-intro">Install servers for M.I.E.R. without changing your project dependencies. Existing custom executables remain available.{!root && ' You can set up servers before opening a repository.'}</p>
      {loading ? <div className="language-server-skeleton" role="status" aria-label="Checking language servers"><span className="sr-only">Checking language servers…</span>{[0, 1, 2].map(index => <div key={index} aria-hidden="true"><span className="skeleton" /><span className="skeleton" /></div>)}</div> : <div className="language-server-list" aria-busy={saving || busy}>{servers.map(server => {
        const tool = server.managed
        const installing = starting === server.language || isInstalling(tool?.state)
        const action = !tool?.installedVersion ? (tool?.installError ? 'Retry install' : 'Install') : !tool.ready ? 'Reinstall' : tool.updateAvailable ? (tool.installError ? 'Retry update' : 'Update') : 'Installed'
        const blocked = !!tool?.prerequisiteError
        return <article className="language-server-entry" key={server.language} aria-labelledby={`language-server-title-${server.language}`} data-language={server.language}>
          <div className="language-server-summary"><Icon name="code" size={18} /><div><h4 id={`language-server-title-${server.language}`}>{server.name}</h4><span className={server.available ? 'text-green' : 'muted'}><Icon name={server.available ? 'check-circle' : 'alert-circle'} size={13} />{server.available ? `Available · ${server.source === 'managed' ? 'M.I.E.R. installation' : server.source === 'project' ? 'Project dependency' : server.source === 'configured' ? 'Configured executable' : 'PATH'}` : 'Setup required'}</span></div>
            <div className="language-server-actions"><button className={`btn${action === 'Install' || tool?.updateAvailable ? ' btn-primary' : ''}`} type="button" aria-label={`${installing ? tool?.installedVersion ? 'Updating' : 'Installing' : action} ${server.name}`} title={blocked ? tool.prerequisiteError : undefined} disabled={saving || busy || checking || blocked || action === 'Installed'} onClick={() => {void install(server.language)}}>{installing && <Icon name="refresh" size={13} className="is-spinning" />}{installing ? tool?.installedVersion ? 'Updating…' : 'Installing…' : action}</button><button className="btn" type="button" disabled={saving || busy} aria-label={`Configure ${server.name}`} aria-expanded={editing === server.language} aria-controls={`language-server-config-${server.language}`} onClick={() => {setEditing(value => value === server.language ? undefined : server.language);setError('');setStatus('')}}>{editing === server.language ? 'Hide configuration' : 'Configure'}</button></div>
          </div>
          <div className="language-server-installation" aria-live="polite">
            {tool?.installedVersion && <span className="language-server-version">Installed {tool.installedVersion}</span>}
            {tool?.latestVersion && <span className="language-server-version">Latest {tool.latestVersion}</span>}
            {tool?.updateAvailable ? <span className="language-server-update-badge"><Icon name="alert-circle" size={12} />{tool.dependencyUpdate ? 'Dependency update available' : 'Update available'}</span> : tool?.installedVersion && tool.checkedAt && !tool.checkError && tool.ready && <span className="language-server-current">Up to date</span>}
            {(installing || tool?.state === 'cancelled' || tool?.progress) && <div className="language-server-progress" role="status"><span>{tool?.progress || 'Starting installation…'}</span>{installing && <button type="button" className="btn-ghost" aria-label={`Cancel ${server.name} installation`} disabled={cancelling || tool?.state === 'cancelling'} onClick={() => {void cancelInstall(server.language)}}>{tool?.state === 'cancelling' ? 'Cancelling…' : 'Cancel'}</button>}</div>}
            {tool?.prerequisiteError && <p className="language-server-availability-error">{tool.prerequisiteError}</p>}
            {tool?.checkError && <p className="language-server-availability-error">Could not check for updates: {tool.checkError}</p>}
            {tool?.installError && <p className="language-server-error" role="alert">{tool.installError}{tool.installedVersion && ' Your previous installation is preserved.'}</p>}
            {tool?.ready && server.source !== 'managed' && <button className="btn language-server-use-installed" type="button" disabled={saving || busy} aria-label={`Use installed ${server.name}`} onClick={() => {void save(server.language, {command:'',args:[]}).catch(() => {})}}>Use installed version</button>}
          </div>
          {server.command && <code className="language-server-command" title={server.command}>{server.command}{server.args.length > 0 && ` · ${server.args.join(' ')}`}</code>}
          {server.error && <p className="language-server-availability-error">{server.error}</p>}
          {server.setupUrl && <Markdown className="language-server-docs">{`[${server.available ? 'Server documentation' : 'Setup instructions'}](${server.setupUrl})`}</Markdown>}
          {editing === server.language && <div id={`language-server-config-${server.language}`}><ServerConfiguration server={server} busy={saving || busy} onSave={configuration => save(server.language, configuration)} onCancel={() => {setEditing(undefined);setError('')}} /></div>}
        </article>
      })}</div>}
      <p className="language-server-status" role="status">{status}</p>
      {error && <p className="language-server-error" role="alert">{error}</p>}
    </>}
  </section>
}

function isInstalling(state?: string) {
  return state === 'installing' || state === 'updating' || state === 'cancelling'
}
