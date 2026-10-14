import { useCallback, useEffect, useState } from 'react'
import { hasNativeBackend, JIRA_CHANGED_EVENT, services, type JiraStatus } from './services'

export function jiraChanged() { window.dispatchEvent(new Event(JIRA_CHANGED_EVENT)) }
export function useJiraConnection() {
  const [native, setNative] = useState(hasNativeBackend)
  const [status, setStatus] = useState<JiraStatus>()
  const [error, setError] = useState('')
  const [revision, setRevision] = useState(0)
  const refresh = useCallback(() => setRevision(value => value + 1), [])
  useEffect(() => {
    const detect = () => setNative(hasNativeBackend())
    window.addEventListener('wails:runtime-config-ready', detect)
    window.addEventListener(JIRA_CHANGED_EVENT, refresh)
    return () => { window.removeEventListener('wails:runtime-config-ready', detect); window.removeEventListener(JIRA_CHANGED_EVENT, refresh) }
  }, [refresh])
  useEffect(() => {
    if (!native) return
    let stale = false, timer: ReturnType<typeof setTimeout> | undefined
    const check = async () => {
      try {
        const value = await services.jira.status()
        if (stale) return
        setStatus(value); setError('')
        if (value.state === 'authorizing' || value.state === 'verifying') timer = setTimeout(() => void check(), 1000)
      } catch (issue) { if (!stale) setError(issue instanceof Error ? issue.message : 'Jira setup could not be checked.') }
    }
    void check()
    return () => { stale = true; clearTimeout(timer) }
  }, [native, revision])
  return { native, status, error, refresh, revision }
}
