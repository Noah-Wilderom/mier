import { useCallback, useEffect, useState } from 'react'
import { hasNativeBackend, LINEAR_CHANGED_EVENT, services, type LinearStatus } from './services'

export function linearChanged() { window.dispatchEvent(new Event(LINEAR_CHANGED_EVENT)) }
export function useLinearConnection() {
  const [native, setNative] = useState(hasNativeBackend)
  const [status, setStatus] = useState<LinearStatus>()
  const [error, setError] = useState('')
  const [revision, setRevision] = useState(0)
  const refresh = useCallback(() => setRevision(value => value + 1), [])
  useEffect(() => {
    const detect = () => setNative(hasNativeBackend())
    window.addEventListener('wails:runtime-config-ready', detect)
    window.addEventListener(LINEAR_CHANGED_EVENT, refresh)
    return () => { window.removeEventListener('wails:runtime-config-ready', detect); window.removeEventListener(LINEAR_CHANGED_EVENT, refresh) }
  }, [refresh])
  useEffect(() => {
    if (!native) return
    let stale = false, timer: ReturnType<typeof setTimeout> | undefined
    const check = async () => {
      try {
        const value = await services.linear.status()
        if (stale) return
        setStatus(value); setError('')
        if (value.state === 'authorizing' || value.state === 'verifying') timer = setTimeout(() => void check(), 1000)
      } catch (issue) { if (!stale) setError(issue instanceof Error ? issue.message : 'Linear setup could not be checked.') }
    }
    void check()
    return () => { stale = true; clearTimeout(timer) }
  }, [native, revision])
  return { native, status, error, refresh, revision }
}
