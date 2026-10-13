import { useEffect, useRef, useState } from 'react'
import { hasNativeBackend, services } from './services'
import './JiraIntegration.css'

// Original Jira_icon.svg from Atlassian's official logo library:
// https://atlassian.design/assets/700bfde52eb7/logos/jira_app.zip
export function JiraIntegration({ available }: { available?: boolean }) {
  const [nativeAvailable, setNativeAvailable] = useState(hasNativeBackend)
  const [installing, setInstalling] = useState(false)
  const [error, setError] = useState('')
  const [completed, setCompleted] = useState(false)
  const pending = useRef(false)
  const mounted = useRef(true)
  const enabled = available ?? nativeAvailable

  useEffect(() => {
    mounted.current = true
    const detect = () => setNativeAvailable(hasNativeBackend())
    window.addEventListener('wails:runtime-config-ready', detect)
    return () => {
      mounted.current = false
      window.removeEventListener('wails:runtime-config-ready', detect)
    }
  }, [])

  const install = async () => {
    if (!enabled || pending.current) return
    pending.current = true
    setInstalling(true)
    setError('')
    setCompleted(false)
    try {
      await services.integrations.installJira()
      if (mounted.current) setCompleted(true)
    } catch (issue) {
      if (mounted.current) setError(issue instanceof Error ? issue.message : 'The Jira setup request failed. Try again.')
    } finally {
      pending.current = false
      if (mounted.current) setInstalling(false)
    }
  }

  const disabled = !enabled || installing
  return <article className="jira-integration" aria-label="Jira app integration">
    <img className="jira-integration-logo" src="/integrations/jira.svg" alt="Jira" width={36} height={36} />
    <div className="jira-integration-details">
      <div className="jira-integration-heading"><h3>Jira</h3><span className="jira-integration-preview">Preview</span></div>
      <p className="jira-integration-description">Try the app installation flow.</p>
      {!enabled && <p className="jira-integration-help">Available in the desktop app.</p>}
      <p className="jira-integration-status" role="status">{installing ? 'Installing Jira…' : completed ? 'Installation preview completed.' : ''}</p>
      {error && <p className="jira-integration-error" role="alert">{error}</p>}
    </div>
    <div className="jira-install-action"><button type="button" className={`btn jira-install-button${disabled ? ' is-disabled' : ''}${installing ? ' is-installing' : ''}`} disabled={disabled} aria-disabled={disabled} aria-busy={installing} onClick={() => void install()}>
      {installing && <span className="jira-install-spinner" aria-hidden="true" />}
      <span>{installing ? 'Installing…' : 'Install Jira app'}</span>
    </button></div>
  </article>
}
