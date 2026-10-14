import { useEffect, useId, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import type { PullRequest } from './data'
import { Icon } from './Icon'
import { openWebURL } from './native'
import { services, type JiraIssue, type JiraPullRequest, type JiraTickets as TicketResult } from './services'
import { jiraChanged, useJiraConnection } from './useJiraConnection'
import { JiraRichText } from './JiraRichText'
import './JiraIntegration.css'

const sourceNames: Record<string, string> = { branch: 'Branch', title: 'PR title', description: 'Description', manual: 'Manually added' }
type JiraReviewProps = { pr: PullRequest; onOpenSettings: () => void; children: ReactNode }
export function JiraTickets({ enabled, ...props }: JiraReviewProps & { enabled: boolean }) {
  return enabled && props.pr.provider ? <JiraReview {...props}/> : <>{props.children}</>
}
function JiraReview({ pr, onOpenSettings, children }: JiraReviewProps) {
  const readerId = useId()
  const { status, native, error: statusError, revision, refresh: refreshConnection } = useJiraConnection()
  const request = useMemo<JiraPullRequest | undefined>(() => pr.provider ? { repository: { host: pr.provider.host, owner: pr.provider.owner, repo: pr.provider.repo }, number: pr.number, branch: pr.head, title: pr.title, body: pr.body } : undefined, [pr.provider?.host, pr.provider?.owner, pr.provider?.repo, pr.number, pr.head, pr.title, pr.body])
  const [result, setResult] = useState<TicketResult>(), [error, setError] = useState(''), [loading, setLoading] = useState(false), [attempt, setAttempt] = useState(0)
  const [selected, setSelected] = useState<string>(), [issue, setIssue] = useState<JiraIssue>(), [issueError, setIssueError] = useState(''), [issueLoading, setIssueLoading] = useState(false), [issueAttempt, setIssueAttempt] = useState(0)
  const [searchOpen, setSearchOpen] = useState(false), [query, setQuery] = useState(''), [matches, setMatches] = useState<JiraIssue[]>([]), [searchError, setSearchError] = useState(''), [searching, setSearching] = useState(false)
  const [mappingOpen, setMappingOpen] = useState(false), [site, setSite] = useState(''), [projects, setProjects] = useState(''), [field, setField] = useState(''), [saving, setSaving] = useState(false)
  const panel = useRef<HTMLElement>(null), restoreFocus = useRef<HTMLElement>(), focusFirst = useRef(false)
  const layout = useRef<HTMLDivElement>(null), resizeOrigin = useRef({ x: 0, width: 360 })
  const [layoutWidth, setLayoutWidth] = useState(0), [paneWidth, setPaneWidth] = useState(360), [resizing, setResizing] = useState(false)
  const context = request ? `${request.repository.host}/${request.repository.owner}/${request.repository.repo}#${request.number}` : ''
  const compact = layoutWidth > 0 && layoutWidth < 740
  const maxPaneWidth = Math.max(280, Math.min(560, Math.floor(layoutWidth - 425)))
  const visiblePaneWidth = Math.min(paneWidth, maxPaneWidth)
  useEffect(() => {
    if (!native || !layout.current) return
    const observer = new ResizeObserver(entries => setLayoutWidth(entries[0].contentRect.width))
    observer.observe(layout.current)
    return () => observer.disconnect()
  }, [native, context])
  useEffect(() => { if (compact || !selected) setResizing(false) }, [compact, selected])
  useEffect(() => { setResult(undefined); setSelected(undefined); setIssue(undefined); setIssueError(''); setSearchOpen(false); setQuery(''); setMatches([]); setMappingOpen(false); setError('') }, [context])
  useEffect(() => {
    if (!request || !status?.connected) { setResult(undefined); setSelected(undefined); setLoading(false); return }
    const abort = new AbortController(); let stale = false
    setLoading(true); setError('')
    void services.jira.tickets({ ...request, refresh: attempt > 0 }, abort.signal).then(value => { if (!stale) { setResult(value); setSite(value.preferences.siteId || value.site.id); setProjects(value.preferences.projects.join(', ')); setField(value.preferences.acceptanceField) } }).catch(failure => { if (!stale) setError(failure instanceof Error ? failure.message : 'Jira tickets could not be loaded.') }).finally(() => { if (!stale) setLoading(false) })
    return () => { stale = true; abort.abort() }
  }, [request, status?.connected, status?.defaultSiteId, revision, attempt])
  useEffect(() => {
    if (!selected || !request || !status?.connected) return
    const abort = new AbortController(); let stale = false
    setIssue(undefined); setIssueError(''); setIssueLoading(true)
    void services.jira.issue({ repository: request.repository, key: selected, refresh: issueAttempt > 0 }, abort.signal).then(value => { if (!stale) setIssue(value) }).catch(failure => { if (!stale) setIssueError(failure instanceof Error ? failure.message : 'The Jira ticket could not be opened.') }).finally(() => { if (!stale) setIssueLoading(false) })
    return () => { stale = true; abort.abort() }
  }, [selected, request?.repository, status?.connected, revision, issueAttempt])
  useEffect(() => {
    if (!searchOpen || !request || query.trim().length < 2) { setMatches([]); setSearchError(''); setSearching(false); return }
    const abort = new AbortController(); let stale = false
    setSearching(true); setSearchError(''); setMatches([])
    const timer = setTimeout(() => { void services.jira.search({ repository: request.repository, query: query.trim() }, abort.signal).then(value => { if (!stale) setMatches(value) }).catch(failure => { if (!stale) setSearchError(failure instanceof Error ? failure.message : 'Jira search failed.') }).finally(() => { if (!stale) setSearching(false) }) }, 300)
    return () => { stale = true; clearTimeout(timer); abort.abort() }
  }, [searchOpen, query, context, revision])
  const close = () => { setSelected(undefined); setIssue(undefined); restoreFocus.current?.focus({ preventScroll: true }) }
  useEffect(() => {
    if (!selected || !panel.current) return
    if (focusFirst.current) { panel.current.querySelector<HTMLElement>('button')?.focus(); focusFirst.current = false }
    const keydown = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close() } }
    const reader = panel.current
    reader.addEventListener('keydown', keydown)
    return () => reader.removeEventListener('keydown', keydown)
  }, [selected])
  const choose = (key: string, trigger: HTMLButtonElement) => {
    restoreFocus.current = trigger
    if (selected === key) { panel.current?.querySelector<HTMLElement>('button')?.focus(); return }
    focusFirst.current = true; setIssue(undefined); setIssueAttempt(0); setSelected(key)
  }
  const select = async (key: string, action: 'pin' | 'hide' | 'restore') => {
    if (!request || saving) return
    setSaving(true); setError('')
    try { await services.jira.select(request, key, action); if (action === 'pin') { setSearchOpen(false); setQuery('') } setAttempt(value => value + 1) }
    catch (failure) { setError(failure instanceof Error ? failure.message : 'Ticket selection could not be saved.') }
    finally { setSaving(false) }
  }
  if (!native || !request) return <>{children}</>
  return <div ref={layout} className={`jira-review-layout${selected ? ' has-ticket' : ''}${compact ? ' is-compact' : ''}${resizing ? ' is-resizing-ticket' : ''}`} style={{ '--jira-ticket-width': `${visiblePaneWidth}px` } as CSSProperties}>
    <div className="jira-review-main">
    <section className="jira-pr-tickets" aria-label="Jira tickets">
    <div className="jira-ticket-toolbar"><strong><img src="/integrations/jira.svg" width={14} height={14} alt=""/>Tickets {result && <span className="badge">{result.tickets.length}</span>}</strong><div>
      {!status?.connected ? <button className="btn-ghost" onClick={onOpenSettings}>Connect Jira</button> : <><span className="jira-ticket-site muted">{result?.site.name}</span><button className="btn-ghost" disabled={saving} onClick={() => setSearchOpen(value => !value)}>Add ticket</button><button className="icon-button" aria-label="Refresh Jira tickets" disabled={loading} onClick={() => setAttempt(value => value + 1)}><Icon name="refresh" size={13}/></button><button className="icon-button" aria-label="Jira repository settings" onClick={() => setMappingOpen(value => !value)}><Icon name="settings" size={13}/></button></>}
    </div></div>
    {statusError && <p className="jira-ticket-error" role="alert">{statusError}<button className="btn-ghost" onClick={refreshConnection}>Retry</button></p>}
    {error && <p className="jira-ticket-error" role="alert">{error}<button className="btn-ghost" disabled={loading} onClick={() => setAttempt(value => value + 1)}>Retry</button></p>}
    {mappingOpen && request && <form className="jira-repository-form" onSubmit={event => { event.preventDefault(); if (saving) return; setSaving(true); setError(''); void services.jira.saveRepository(request.repository, { siteId: site, projects: projects.split(/[,\n]/).map(value => value.trim()).filter(Boolean), acceptanceField: field }).then(() => { setMappingOpen(false); setResult(undefined); setSelected(undefined); setIssue(undefined); setMatches([]); jiraChanged() }).catch(failure => setError(failure instanceof Error ? failure.message : 'Repository settings could not be saved.')).finally(() => setSaving(false)) }}>
      <label>Jira site<select aria-label="Repository Jira site" value={site} disabled={saving} required onChange={event => setSite(event.target.value)}>{status?.sites.map(value => <option key={value.id} value={value.id}>{value.name}</option>)}</select></label>
      <label>Project keys<input aria-label="Jira project keys" placeholder="FAN, APP · blank includes all projects" value={projects} disabled={saving} onChange={event => setProjects(event.target.value)}/></label>
      <label>Acceptance criteria field (optional)<input aria-label="Acceptance criteria field" placeholder="customfield_10001" value={field} disabled={saving} onChange={event => setField(event.target.value)}/></label>
      <div><button className="btn" disabled={saving}>Save repository settings</button><button className="btn-ghost" type="button" disabled={saving} onClick={() => setMappingOpen(false)}>Cancel</button></div>
    </form>}
    {searchOpen && <div className="jira-ticket-search"><label>Find a Jira ticket<input aria-label="Find a Jira ticket" placeholder="Issue key or words from its title" value={query} onChange={event => setQuery(event.target.value)} autoFocus maxLength={120}/></label><p className="muted">Manual selections are saved in M.I.E.R. for this pull request.</p>{searching && <p role="status">Searching Jira…</p>}{searchError && <p role="alert" className="jira-ticket-error">{searchError}</p>}{!searching && !searchError && query.trim().length >= 2 && !matches.length && <p>No matching tickets.</p>}<ul>{matches.map(value => <li key={value.id}><button disabled={saving} onClick={() => void select(value.key, 'pin')}><code>{value.key}</code><span>{value.summary}</span><Icon name="plus" size={14}/></button></li>)}</ul></div>}
    {loading && !result && <p className="jira-ticket-loading" role="status">Loading related Jira tickets…</p>}
    {!!result?.tickets.length && <div className="jira-ticket-list">{result.tickets.map(ticket => <article key={ticket.key} className={`jira-ticket-card${selected === ticket.key ? ' is-selected' : ''}`}><button className="jira-ticket-open" aria-expanded={selected === ticket.key} aria-controls={selected === ticket.key ? readerId : undefined} onClick={event => choose(ticket.key, event.currentTarget)}><code>{ticket.issue?.key || ticket.key}</code><span className="jira-ticket-summary">{ticket.issue?.summary || ticket.error || 'Ticket unavailable'}</span>{ticket.issue && <span className={`badge jira-status-${ticket.issue.statusCategory}`}>{ticket.issue.status}</span>}</button><div className="jira-ticket-card-meta"><span>{ticket.sources.map(source => sourceNames[source] || source).join(' · ')}</span>{ticket.issue?.assignee && <span>{ticket.issue.assignee}</span>}<button className="btn-ghost" disabled={saving} aria-label={`Hide ${ticket.key}`} onClick={() => void select(ticket.key, 'hide')}>Hide</button></div></article>)}</div>}
    {status?.connected && !loading && result && !result.tickets.length && <p className="jira-no-tickets">No related tickets found. Add a ticket or include its key in the branch, title, or description.</p>}
    {result?.truncated && <p className="muted jira-no-tickets">Showing the first 25 matching tickets. Narrow the repository's Jira projects to refine the list.</p>}
    {!!result?.hidden.length && <details className="jira-hidden-tickets"><summary>Hidden tickets ({result.hidden.length})</summary>{result.hidden.map(key => <button className="btn-ghost" key={key} disabled={saving} onClick={() => void select(key, 'restore')}>Restore {key}</button>)}</details>}
    </section>
    {children}
    </div>
    {selected && !compact && <div className="jira-ticket-resizer" role="separator" aria-label="Resize Jira ticket pane" aria-orientation="vertical" aria-valuemin={280} aria-valuemax={maxPaneWidth} aria-valuenow={visiblePaneWidth} aria-valuetext={`${visiblePaneWidth} pixels`} tabIndex={0} title="Drag to resize Jira ticket pane" onPointerDown={event => {
      resizeOrigin.current = { x: event.clientX, width: visiblePaneWidth }
      event.currentTarget.setPointerCapture(event.pointerId)
      setResizing(true)
      event.preventDefault()
    }} onPointerMove={event => {
      if (resizing) setPaneWidth(Math.max(280, Math.min(maxPaneWidth, resizeOrigin.current.width + resizeOrigin.current.x - event.clientX)))
    }} onPointerUp={event => { event.currentTarget.releasePointerCapture(event.pointerId); setResizing(false) }} onPointerCancel={() => setResizing(false)} onLostPointerCapture={() => setResizing(false)} onKeyDown={event => {
      if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
      event.preventDefault()
      setPaneWidth(event.key === 'Home' ? 280 : event.key === 'End' ? maxPaneWidth : Math.max(280, Math.min(maxPaneWidth, visiblePaneWidth + (event.key === 'ArrowLeft' ? 10 : -10))))
    }}/>}
    {selected && <aside id={readerId} ref={panel} className="jira-ticket-reader" role="region" aria-label={`Jira ticket ${selected}`}><header><strong>{issue?.key || selected}</strong><button className="icon-button" aria-label="Close Jira ticket" onClick={close}><Icon name="close" size={14}/></button></header><div className="jira-ticket-reader-scroll" tabIndex={0}>
      {issueLoading && <p role="status">Loading ticket details…</p>}
      {issueError && <p className="jira-ticket-error" role="alert">{issueError}<button className="btn" onClick={() => setIssueAttempt(value => value + 1)}>Retry ticket</button></p>}
      {issue && <><h2>{issue.summary}</h2><div className="jira-issue-metadata"><span className={`badge jira-status-${issue.statusCategory}`}>{issue.status}</span><span>{issue.type}</span>{issue.priority && <span>Priority: {issue.priority}</span>}<span>Assignee: {issue.assignee || 'Unassigned'}</span>{issue.parentKey && <span>Parent: {issue.parentKey}</span>}</div><h3>Description</h3>{issue.description ? <JiraRichText value={issue.description}/> : <p className="muted">No description provided.</p>}{!!issue.acceptanceCriteria && <><h3>Acceptance criteria</h3><JiraRichText value={issue.acceptanceCriteria}/></>}{!!issue.labels?.length && <div className="jira-issue-labels">{issue.labels.map(label => <span key={label} className="label-chip">{label}</span>)}</div>}<h3>Recent comments</h3>{issue.commentsError && <p className="jira-ticket-error">{issue.commentsError}</p>}{!issue.commentsError && !issue.comments?.length && <p className="muted">No comments yet.</p>}{issue.comments?.map(comment => <article className="jira-issue-comment" key={comment.id}><strong>{comment.author}</strong><time>{new Date(comment.createdAt).toLocaleString()}</time><JiraRichText value={comment.body}/></article>)}</>}
    </div>{issue && <footer><button className="btn" onClick={() => void openWebURL(issue.url).catch(() => setIssueError('The ticket could not be opened in your browser.'))}><Icon name="external" size={13}/>Open in Jira</button><button className="btn-ghost" disabled={issueLoading} onClick={() => setIssueAttempt(value => value + 1)}>Refresh</button></footer>}</aside>}
  </div>
}
