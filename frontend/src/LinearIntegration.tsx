import { useEffect, useState } from 'react'
import { Icon } from './Icon'
import { openWebURL } from './native'
import { services } from './services'
import { linearChanged, useLinearConnection } from './useLinearConnection'
import './JiraIntegration.css'

export function LinearIntegration() {
  const { native, status, error: statusError, refresh } = useLinearConnection()
  const [open, setOpen] = useState(false), [clientId, setClientId] = useState(''), [port, setPort] = useState(43828)
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState(''), [confirmDisconnect, setConfirmDisconnect] = useState(false)
  useEffect(() => { if (status && !open) { setClientId(status.clientId); setPort(status.callbackPort) } }, [status, open])
  const authorizing = status?.state === 'authorizing' || status?.state === 'verifying'
  const run = async (action: () => Promise<void>) => {
    if (busy) return
    setBusy(true); setError(''); setNotice('')
    try { await action() } catch (issue) { setError(issue instanceof Error ? issue.message : 'Linear setup could not be updated.') }
    finally { setBusy(false); linearChanged() }
  }
  const authorize = () => run(async () => {
    const address = await services.linear.authorize()
    try { await openWebURL(address) } catch (issue) { await services.linear.cancelAuthorization(); throw issue }
  })
  const callback = `http://localhost:${port}/oauth/linear/callback`
  return <article className="jira-integration" aria-label="Linear app integration">
    <img className="jira-integration-logo" src="/integrations/linear.svg" alt="" width={36} height={36}/>
    <div className="jira-integration-details">
      <div className="jira-integration-heading"><h3>Linear</h3>{status?.connected && <span className="badge text-green">Connected</span>}</div>
      <p className="jira-integration-description">View issues and requirements alongside your pull requests. Connect using an OAuth app you control.</p>
      {!native && <p className="jira-integration-help">Open the desktop app to connect Linear.</p>}
      {(error || statusError || status?.error) && <p className="jira-integration-error" role="alert">{error || statusError || status?.error}{statusError && <button className="btn-ghost" type="button" onClick={refresh}>Retry</button>}</p>}
      {notice && <p role="status" className="jira-integration-status">{notice}</p>}
      {authorizing && <div className="jira-authorization-status" role="status"><span className="jira-install-spinner"/>{status?.state === 'verifying' ? 'Verifying Linear access…' : 'Finish authorizing in your browser…'}<button className="btn-ghost" disabled={busy} onClick={() => void run(() => services.linear.cancelAuthorization())}>Cancel Linear authorization</button></div>}
      {status?.configured && (!status.connected || authorizing || status.error) && !open && <details className="jira-authorization-help"><summary>Browser authorization failed?</summary><p>Check that your Linear OAuth app registers <code>{status.callbackUrl}</code> exactly. Keep M.I.E.R. open until the browser returns here. Cancel the current attempt before retrying.</p></details>}
      {status?.connected && <p className="jira-integration-help">{status.workspace?.name} · {status.account}</p>}
      {open && <form className="jira-setup-form" onSubmit={event => { event.preventDefault(); void run(async () => { await services.linear.configure({ clientId, callbackPort: port }); setOpen(false); setNotice('App configuration saved. Authorize Linear to finish connecting.'); }) }}>
        <div className="jira-registration-guide">
          <strong>1. Register your OAuth app</strong>
          <p>In Linear, open <strong>Settings → Administration → API</strong> and create an OAuth application. This requires workspace admin access.</p>
          <a href="https://linear.app/settings/api" target="_blank" rel="noopener noreferrer" onClick={event => { event.preventDefault(); void openWebURL(event.currentTarget.href).catch(() => setError('Linear app settings could not be opened.')) }}>Open Linear app settings <Icon name="external" size={12}/></a>
          <p>Register this exact callback URL:</p>
          <label>Callback URL<input aria-label="Linear callback URL" value={callback} readOnly onFocus={event => event.target.select()}/></label>
          <p>M.I.E.R. requests read access using PKCE. A client secret is not needed.</p>
        </div>
        <strong>2. Enter your app client ID</strong>
        <label>Client ID<input aria-label="Linear client ID" required maxLength={256} autoComplete="off" value={clientId} disabled={busy} onChange={event => setClientId(event.target.value)}/></label>
        <p className="jira-integration-help">Authorization tokens stay in your OS keychain. Changing the client ID or callback port requires authorization again.</p>
        <details><summary>Callback port</summary><label>Port<input aria-label="Linear callback port" type="number" min={1024} max={65535} required value={port} disabled={busy} onChange={event => setPort(Number(event.target.value))}/></label><p>Change this only if the port is in use, and update the registered callback URL too.</p></details>
        <div className="jira-setup-actions"><button className="btn btn-primary" disabled={busy}>{busy ? 'Saving…' : 'Save Linear app'}</button><button className="btn" type="button" disabled={busy} onClick={() => { setOpen(false); setError('') }}>Cancel</button></div>
      </form>}
      {confirmDisconnect && <div className="jira-disconnect-confirm"><p>Disconnect Linear? Your client ID and ticket selections will be kept for reconnecting.</p><button className="btn" disabled={busy} onClick={() => void run(async () => { await services.linear.disconnect(); setConfirmDisconnect(false) })}>Disconnect Linear account</button><button className="btn-ghost" disabled={busy} onClick={() => setConfirmDisconnect(false)}>Keep connected</button></div>}
    </div>
    <div className="jira-install-action">
      <button className="btn jira-install-button" disabled={!native || busy || authorizing || !status} onClick={() => { setOpen(value => !value); setError('') }}>{status?.configured ? 'Linear app settings' : 'Configure Linear'}</button>
      {status?.configured && !authorizing && <button className="btn btn-primary" disabled={busy || open} onClick={() => void authorize()}>{status.connected ? 'Authorize Linear again' : 'Authorize Linear'}</button>}
      {status?.connected && <button className="btn-ghost" disabled={busy || authorizing} onClick={() => setConfirmDisconnect(true)}>Disconnect Linear</button>}
    </div>
  </article>
}
