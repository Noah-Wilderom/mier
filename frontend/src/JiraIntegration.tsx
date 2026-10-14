import { useEffect, useState } from 'react'
import { Icon } from './Icon'
import { openWebURL } from './native'
import { services } from './services'
import { jiraChanged, useJiraConnection } from './useJiraConnection'
import './JiraIntegration.css'

export function JiraIntegration() {
  const { native, status, error: statusError, refresh } = useJiraConnection()
  const [open, setOpen] = useState(false), [clientId, setClientId] = useState(''), [secret, setSecret] = useState(''), [port, setPort] = useState(43827)
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState(''), [confirmDisconnect, setConfirmDisconnect] = useState(false)
  useEffect(() => { if (status && !open) { setClientId(status.clientId); setPort(status.callbackPort) } }, [status, open])
  const authorizing = status?.state === 'authorizing' || status?.state === 'verifying'
  const run = async (action: () => Promise<void>) => {
    if (busy) return
    setBusy(true); setError(''); setNotice('')
    try { await action() } catch (issue) { setError(issue instanceof Error ? issue.message : 'Jira setup could not be updated.') }
    finally { setBusy(false); jiraChanged() }
  }
  const authorize = () => run(async () => {
    const address = await services.jira.authorize()
    try { await openWebURL(address) } catch (issue) { await services.jira.cancelAuthorization(); throw issue }
  })
  const callback = `http://localhost:${port}/oauth/jira/callback`
  return <article className="jira-integration" aria-label="Jira app integration">
    <img className="jira-integration-logo" src="/integrations/jira.svg" alt="" width={36} height={36}/>
    <div className="jira-integration-details">
      <div className="jira-integration-heading"><h3>Jira Cloud</h3>{status?.connected && <span className="badge text-green">Connected</span>}</div>
      <p className="jira-integration-description">View tickets and requirements alongside your pull requests. Connect using an OAuth app you control.</p>
      {!native && <p className="jira-integration-help">Open the desktop app to connect Jira.</p>}
      {(error || statusError || status?.error) && <p className="jira-integration-error" role="alert">{error || statusError || status?.error}{statusError && <button className="btn-ghost" type="button" onClick={refresh}>Retry</button>}</p>}
      {notice && <p role="status" className="jira-integration-status">{notice}</p>}
      {authorizing && <div className="jira-authorization-status" role="status"><span className="jira-install-spinner"/>{status?.state === 'verifying' ? 'Verifying Jira access…' : 'Finish authorizing in your browser…'}<button className="btn-ghost" disabled={busy} onClick={() => void run(() => services.jira.cancelAuthorization())}>Cancel authorization</button></div>}
      {status?.configured && !open && (!status.connected || authorizing || status.error) && <details className="jira-authorization-help">
        <summary>Browser authorization failed?</summary>
        <p>If Atlassian says “Your app doesn’t have any APIs”, open your app’s <strong>Permissions</strong>, add the <strong>Jira API</strong>, and configure the classic <code>read:jira-work</code> scope (“View Jira issue data”). Save the changes, cancel the current authorization attempt, and select <strong>Authorize Jira</strong> again.</p>
        <p>The registered callback must match <code>{status.callbackUrl}</code>. If authorization still fails, expand the owner information on Atlassian’s error page to see the specific cause.</p>
      </details>}
      {status?.connected && <label className="jira-site-setting">Default Jira site<select aria-label="Default Jira site" value={status.defaultSiteId} disabled={busy || authorizing} onChange={event => void run(() => services.jira.defaultSite(event.target.value))}>{status.sites.map(site => <option key={site.id} value={site.id}>{site.name} · {site.url}</option>)}</select></label>}
      {open && <form className="jira-setup-form" onSubmit={event => { event.preventDefault(); void run(async () => { await services.jira.configure({ clientId, clientSecret: secret, callbackPort: port }); setSecret(''); setOpen(false); setNotice('App credentials saved. Authorize Jira to finish connecting.'); }) }}>
        <div className="jira-registration-guide">
          <strong>1. Register your OAuth app</strong>
          <p>In the Atlassian developer console, create an OAuth 2.0 integration.</p>
          <a href="https://developer.atlassian.com/console/myapps/" target="_blank" rel="noopener noreferrer" onClick={event => { event.preventDefault(); void openWebURL(event.currentTarget.href).catch(() => setError('The developer console could not be opened.')) }}>Open developer console <Icon name="external" size={12}/></a>
          <ol>
            <li>Open <strong>Permissions</strong> and select <strong>Add</strong> next to the <strong>Jira API</strong>.</li>
            <li>Select <strong>Configure</strong> for the Jira API. Add the classic <code>read:jira-work</code> scope (“View Jira issue data”) and save your changes.</li>
            <li>Under <strong>Authorization → OAuth 2.0</strong>, register this exact callback URL.</li>
          </ol>
          <label>Callback URL<input aria-label="Jira callback URL" value={callback} readOnly onFocus={event => event.target.select()}/></label>
          <p>M.I.E.R. adds <code>offline_access</code> to the sign-in request automatically to keep your connection active. This is separate from the Jira API permission configured above.</p>
        </div>
        <strong>2. Enter your app credentials</strong>
        <label>Client ID<input aria-label="Jira client ID" required autoComplete="off" value={clientId} disabled={busy} onChange={event => setClientId(event.target.value)}/></label>
        <label>Client secret<input aria-label="Jira client secret" type="password" required={!status?.configured || clientId !== status.clientId} autoComplete="new-password" value={secret} disabled={busy} onChange={event => setSecret(event.target.value)} placeholder={status?.configured ? 'Leave blank to keep your saved secret' : 'From your OAuth app settings'}/></label>
        <p className="jira-integration-help">The secret and authorization tokens are stored in your OS keychain.</p>
        <details><summary>Callback port</summary><label>Port<input aria-label="Jira callback port" type="number" min={1024} max={65535} required value={port} disabled={busy} onChange={event => setPort(Number(event.target.value))}/></label><p>Change this only if the port is in use, and update the registered callback URL too.</p></details>
        <div className="jira-setup-actions"><button className="btn btn-primary" disabled={busy}>{busy ? 'Saving…' : 'Save app credentials'}</button><button className="btn" type="button" disabled={busy} onClick={() => { setOpen(false); setSecret(''); setError('') }}>Cancel</button></div>
      </form>}
      {confirmDisconnect && <div className="jira-disconnect-confirm"><p>Disconnect Jira? Your app credentials will be kept for reconnecting.</p><button className="btn" disabled={busy} onClick={() => void run(async () => { await services.jira.disconnect(); setConfirmDisconnect(false) })}>Disconnect account</button><button className="btn-ghost" disabled={busy} onClick={() => setConfirmDisconnect(false)}>Keep connected</button></div>}
    </div>
    <div className="jira-install-action">
      <button className="btn jira-install-button" disabled={!native || busy || authorizing || !status} onClick={() => { setOpen(value => !value); setSecret(''); setError('') }}>{status?.configured ? 'App credentials' : 'Configure Jira'}</button>
      {status?.configured && !authorizing && <button className="btn btn-primary" disabled={busy || open} onClick={() => void authorize()}>{status.connected ? 'Authorize again' : 'Authorize Jira'}</button>}
      {status?.connected && <button className="btn-ghost" disabled={busy || authorizing} onClick={() => setConfirmDisconnect(true)}>Disconnect</button>}
    </div>
  </article>
}
