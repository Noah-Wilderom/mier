import { useEffect, useMemo, useRef, useState } from 'react'
import { Icon } from './Icon'
import { repositoryName, type RepositoryCard } from './RepositorySelector'
import { hasNativeBackend, services, type NotificationPreferences } from './services'
import './NotificationSettings.css'

export interface NotificationSettingsProps {
  repositoryCards: Record<string, RepositoryCard>
  recent: string[]
  activePath?: string
  available?: boolean
}

type PreferenceKey = keyof NotificationPreferences
const eventOptions: { key: PreferenceKey; label: string; description: string; icon: string }[] = [
  { key: 'opened', label: 'Pull request opened', description: 'New pull requests and pull requests that are reopened.', icon: 'pull-request' },
  { key: 'closed', label: 'Pull request closed', description: 'Pull requests closed without being merged.', icon: 'close' },
  { key: 'merged', label: 'Pull request merged', description: 'Pull requests merged into their base branch.', icon: 'git-merge' },
  { key: 'reviewRequested', label: 'Review requested for you', description: 'You are added as a requested reviewer.', icon: 'message-circle' },
]

export function notificationPreferenceKey(card: RepositoryCard): string | undefined {
  if (!card.host || !card.owner || !card.repo) return undefined
  return `${card.host}/${card.owner}/${card.repo}`.toLowerCase()
}

export default function NotificationSettings({ repositoryCards, recent, activePath, available }: NotificationSettingsProps) {
  const [nativeAvailable, setNativeAvailable] = useState(hasNativeBackend)
  const [selectedPath, setSelectedPath] = useState(activePath ?? '')
  const [query, setQuery] = useState('')
  const [preferences, setPreferences] = useState<NotificationPreferences>()
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const [retry, setRetry] = useState(0)
  const pending = useRef(false)
  const mounted = useRef(true)
  const currentKey = useRef<string>()
  const enabled = available ?? nativeAvailable

  const repositories = useMemo(() => {
    const paths = [...new Set([...(activePath ? [activePath] : []), ...recent, ...Object.keys(repositoryCards)])]
    const identities = new Set<string>()
    return paths.map(path => repositoryCards[path] ?? { path, name: repositoryName(path) }).filter(card => {
      const identity = notificationPreferenceKey(card)
      if (!identity) return true
      if (identities.has(identity)) return false
      identities.add(identity)
      return true
    })
  }, [repositoryCards, recent, activePath])
  const selected = repositories.find(card => card.path === selectedPath)
  const key = selected && notificationPreferenceKey(selected)
  currentKey.current = key
  const filtered = repositories.filter(card => `${card.name} ${card.host ?? ''} ${card.owner ?? ''} ${card.repo ?? ''} ${card.path}`.toLowerCase().includes(query.toLowerCase()))

  useEffect(() => {
    mounted.current = true
    const detect = () => setNativeAvailable(hasNativeBackend())
    window.addEventListener('wails:runtime-config-ready', detect)
    return () => {
      mounted.current = false
      window.removeEventListener('wails:runtime-config-ready', detect)
    }
  }, [])

  useEffect(() => {
    if (!selected) {
      setSelectedPath(repositories.find(card => notificationPreferenceKey(card))?.path ?? repositories[0]?.path ?? '')
    }
  }, [repositories, selected])

  useEffect(() => {
    let cancelled = false
    setPreferences(undefined)
    setError('')
    setSaved(false)
    if (!enabled || !key) {
      setLoading(false)
      return
    }
    setLoading(true)
    void services.notifications.getPreferences(key).then(value => {
      if (!cancelled) setPreferences(value)
    }).catch(issue => {
      if (!cancelled) setError(issue instanceof Error ? issue.message : String(issue))
    }).finally(() => {
      if (!cancelled) setLoading(false)
    })
    return () => { cancelled = true }
  }, [key, enabled, retry])

  const update = async (field: PreferenceKey, checked: boolean) => {
    if (!key || !preferences || pending.current || !enabled) return
    const identity = key
    const previous = preferences
    const next = { ...previous, [field]: checked }
    pending.current = true
    setSaving(true)
    setSaved(false)
    setError('')
    setPreferences(next)
    try {
      if (field === 'desktopEnabled' && checked) await services.notifications.requestDesktopPermission()
      await services.notifications.savePreferences(identity, next)
      if (mounted.current && currentKey.current === identity) setSaved(true)
    } catch (issue) {
      if (mounted.current && currentKey.current === identity) {
        setPreferences(previous)
        setError(issue instanceof Error ? issue.message : String(issue))
      }
    } finally {
      pending.current = false
      if (mounted.current) setSaving(false)
    }
  }

  const toggle = (field: PreferenceKey, label: string, description: string, icon: string) => <label className="notification-option" key={field}>
    <Icon name={icon} size={18} className="notification-option-icon" />
    <span className="notification-option-copy"><strong>{label}</strong><span>{description}</span></span>
    <input type="checkbox" role="switch" aria-label={label} checked={preferences?.[field] ?? false} disabled={!preferences || saving || loading || !enabled} onChange={event => void update(field, event.target.checked)} />
    <span className="notification-switch" aria-hidden="true" />
  </label>

  return <section className="notification-settings" aria-labelledby="notification-settings-heading">
    <div className="notification-settings-heading"><Icon name="bell" size={20} /><div><h2 id="notification-settings-heading">Notifications</h2><p>Choose pull request updates for each repository.</p></div></div>
    <div className="notification-repository-controls">
      <label className="notification-repository-search"><Icon name="search" size={16} /><input className="input" type="search" value={query} disabled={saving} onChange={event => setQuery(event.target.value)} placeholder="Find a repository…" aria-label="Find a repository for notification settings" /></label>
      <label className="notification-repository-choice"><span>Repository</span><select className="input" value={selectedPath} disabled={saving || !repositories.length} onChange={event => {setSelectedPath(event.target.value);setQuery('')}}>
        {selected && !filtered.some(card => card.path === selected.path) && <option value={selected.path}>{selected.owner && `${selected.owner}/`}{selected.repo ?? selected.name}{selected.host && ` · ${selected.host}`}</option>}
        {filtered.map(card => <option key={card.path} value={card.path} disabled={!notificationPreferenceKey(card)}>{card.owner && `${card.owner}/`}{card.repo ?? card.name}{card.host ? ` · ${card.host}` : ' · GitHub remote not loaded'}</option>)}
        {!repositories.length && <option value="">Open a repository first</option>}
      </select></label>
    </div>
    {query && !filtered.length && <p className="notification-settings-note">No repositories match “{query}”.</p>}
    {!enabled ? <p className="notification-settings-note">Notification preferences are available in the desktop app.</p> : !key ? <p className="notification-settings-note">Open a repository with a GitHub or GitHub Enterprise remote to configure its notifications.</p> : <>
      <p className="notification-repository-identity">{selected?.owner}/{selected?.repo}<span> · {selected?.host}</span></p>
      <p className="notification-settings-note">These preferences also apply to other local clones of this repository.</p>
      {loading ? <div className="notification-settings-skeleton" role="status" aria-label="Loading notification preferences"><span className="sr-only">Loading notification preferences…</span><div aria-hidden="true">{Array.from({ length: 6 }, (_, index) => <div className="notification-skeleton-row" key={index}><span className="skeleton" /><span className="skeleton" /></div>)}</div></div> : preferences && <div aria-busy={saving}>
        <fieldset className="notification-options"><legend>Where to notify you</legend>
          {toggle('toastsEnabled', 'In-app toasts', 'Show updates inside M.I.E.R. Enabled by default.', 'message-circle')}
          {toggle('desktopEnabled', 'Desktop notifications', 'Use your operating system notifications. Permission is requested when enabled.', 'monitor')}
        </fieldset>
        <fieldset className="notification-options"><legend>Pull request events</legend>{eventOptions.map(option => toggle(option.key, option.label, option.description, option.icon))}</fieldset>
        <p className="notification-settings-note">All pull request events are enabled by default. Updates arrive while M.I.E.R. is running and this repository is being watched.</p>
      </div>}
      <p className="notification-save-status" role="status">{saving ? 'Saving notification preferences…' : saved ? 'Preferences saved.' : ''}</p>
      {error && <div className="notification-save-error" role="alert"><p>{error}</p>{!preferences && <button className="btn" type="button" disabled={loading} onClick={() => setRetry(value => value + 1)}>Try again</button>}</div>}
    </>}
  </section>
}
