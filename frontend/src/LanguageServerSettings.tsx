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
    <p className="language-server-field-help">Leave empty to detect the server in this project or your PATH.</p>
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
  const [refresh, setRefresh] = useState(0)
  const mounted = useRef(true)
  const pending = useRef(false)
  const selectedRoot = useRef(root)
  selectedRoot.current = root
  const enabled = (available ?? nativeAvailable) && !!root

  useEffect(() => {
    mounted.current = true
    const detect = () => setNativeAvailable(hasNativeBackend())
    window.addEventListener('wails:runtime-config-ready', detect)
    return () => {mounted.current = false;window.removeEventListener('wails:runtime-config-ready', detect)}
  }, [])

  useEffect(() => {
    let cancelled = false
    setError('')
    setStatus('')
    setEditing(undefined)
    setServers([])
    if (!enabled || !root) {setLoading(false);return}
    setLoading(true)
    void services.lsp.servers(root).then(value => {if (!cancelled) setServers(value)}).catch(issue => {
      if (!cancelled) setError(issue instanceof Error ? issue.message : 'Language server availability could not be checked.')
    }).finally(() => {if (!cancelled) setLoading(false)})
    return () => {cancelled = true}
  }, [root, enabled, refresh])

  const save = async (language: string, configuration: LSPServerConfiguration) => {
    if (!enabled || !root || pending.current) return
    const requestedRoot = root
    pending.current = true
    setSaving(true)
    setError('')
    setStatus('')
    let applied = false
    try {
      await services.lsp.configureServer(language, configuration)
      applied = true
      window.dispatchEvent(new CustomEvent(LSP_CONFIGURATION_CHANGED_EVENT))
      const updated = await services.lsp.servers(requestedRoot)
      if (mounted.current && selectedRoot.current === requestedRoot) {
        setServers(updated)
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

  return <section className="language-server-settings" aria-labelledby="language-server-settings-heading">
    <div className="language-server-settings-heading"><div><h3 id="language-server-settings-heading">Language intelligence</h3><p>Use installed language servers for symbol information and go to definition.</p></div><button className="btn" type="button" disabled={!enabled || loading || saving} onClick={() => setRefresh(value => value + 1)}><Icon name="refresh" size={14} />Check again</button></div>
    {!nativeAvailable && available !== true ? <p className="language-server-intro">Language servers run in the desktop app.</p> : !root ? <p className="language-server-intro">Open a repository to check PHP, TypeScript / JavaScript, and Vue servers.</p> : <>
      <p className="language-server-intro">Servers are detected from project dependencies and your PATH. Code navigation uses the version shown in your diff.</p>
      {loading ? <div className="language-server-skeleton" role="status" aria-label="Checking language servers"><span className="sr-only">Checking language servers…</span>{[0, 1, 2].map(index => <div key={index} aria-hidden="true"><span className="skeleton" /><span className="skeleton" /></div>)}</div> : <div className="language-server-list" aria-busy={saving}>{servers.map(server => <article className="language-server-entry" key={server.language}>
        <div className="language-server-summary"><Icon name="code" size={18} /><div><strong>{server.name}</strong><span className={server.available ? 'text-green' : 'muted'}><Icon name={server.available ? 'check-circle' : 'alert-circle'} size={13} />{server.available ? `Available · ${server.source === 'project' ? 'Project dependency' : server.source === 'configured' ? 'Configured executable' : 'PATH'}` : 'Setup required'}</span></div><button className="btn" type="button" disabled={saving} aria-expanded={editing === server.language} aria-controls={`language-server-config-${server.language}`} onClick={() => {setEditing(value => value === server.language ? undefined : server.language);setError('');setStatus('')}}>{editing === server.language ? 'Hide configuration' : 'Configure'}</button></div>
        {server.command && <code className="language-server-command" title={server.command}>{server.command}{server.args.length > 0 && ` · ${server.args.join(' ')}`}</code>}
        {server.error && <p className="language-server-availability-error">{server.error}</p>}
        {server.setupUrl && <Markdown className="language-server-docs">{`[${server.available ? 'Server documentation' : 'Setup instructions'}](${server.setupUrl})`}</Markdown>}
        {editing === server.language && <div id={`language-server-config-${server.language}`}><ServerConfiguration server={server} busy={saving} onSave={configuration => save(server.language, configuration)} onCancel={() => {setEditing(undefined);setError('')}} /></div>}
      </article>)}</div>}
      <p className="language-server-status" role="status">{status}</p>
      {error && <p className="language-server-error" role="alert">{error}</p>}
    </>}
  </section>
}
