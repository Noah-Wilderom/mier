import { useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import type { CSSProperties, ReactNode } from 'react'
import { Icon } from './Icon'
import { allowsReview } from './reviewCapabilities'
import { currentUser, latestReview, submitReview } from './data'
import type { ChangedFile, DiffLine, PendingComment, PullRequest, ReviewDecision, Thread } from './data'
import './review.css'
import { useReviewSessionState, useReviewSessionRef } from './ReviewSession'
import { Markdown } from './Markdown'
import FileChangeIcon from './FileChangeIcon'
import { ActorName, Avatar } from './Avatar'
import { codeReviewStyleVariables, DEFAULT_CODE_REVIEW_PREFERENCES, effectiveCodeTheme, getCodeAppearance, getCodeReviewPreferences, subscribeCodeAppearance, subscribeCodeReviewPreferences } from './codeReviewPreferences'
import { highlightDiff, syntaxLineKey, syntaxTokenStyle, type HighlightedDiff, type SyntaxToken } from './syntaxHighlighter'

type ReviewProps = {
  pr: PullRequest
  onUpdate: (pr: PullRequest) => void | Promise<void>
  onNotify: (message: string) => void
  busy?: boolean
}

export type ReviewLocation = { path: string; threadId?: string; line?: number; side?: 'old' | 'new' }
export type ReviewWorkspaceProps = ReviewProps & {
  focusMode: boolean
  onToggleFocus: () => void
  requestedLocation?: ReviewLocation
}

type LineLocation = { line: number; side: 'old' | 'new' }
type SplitRow = { old?: DiffLine; next?: DiffLine; key: string }

const decisionLabels: Record<ReviewDecision, string> = {
  approved: 'Approved',
  changes_requested: 'Changes requested',
  commented: 'Commented',
}

function shortTime(value: string) {
  const time = new Date(value).getTime()
  if (!Number.isFinite(time)) return value
  const minutes = Math.max(1, Math.floor((Date.now() - time) / 60_000))
  if (minutes < 60) return `${minutes}m ago`
  if (minutes < 1_440) return `${Math.floor(minutes / 60)}h ago`
  return `${Math.floor(minutes / 1_440)}d ago`
}

function uniqueId(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

function tokenLine(tokens: SyntaxToken[] | undefined, text: string): ReactNode {
  return tokens ? tokens.map((token, index) => <span style={syntaxTokenStyle(token)} data-syntax-type={token.type === 1 ? 'comment' : token.type === 2 ? 'string' : undefined} key={index}>{token.content}</span>) : text
}

function alignLines(lines: DiffLine[], ignoreWhitespace: boolean): SplitRow[] {
  const rows: SplitRow[] = []
  for (let index = 0; index < lines.length;) {
    const line = lines[index]
    if (line.kind === 'context') {
      rows.push({ old: line, next: line, key: `context-${index}` })
      index += 1
      continue
    }
    const removed: DiffLine[] = []
    const added: DiffLine[] = []
    const start = index
    while (index < lines.length && lines[index].kind !== 'context') {
      const changed = lines[index]
      if (changed.hunk !== line.hunk) break
      ;(changed.kind === 'remove' ? removed : added).push(changed)
      index += 1
    }
    for (let offset = 0; offset < Math.max(removed.length, added.length); offset += 1) {
      const old = removed[offset]
      const next = added[offset]
      const isWhitespaceOnly = ignoreWhitespace && old && next && old.text.replace(/\s/g, '') === next.text.replace(/\s/g, '')
      rows.push({
        old: isWhitespaceOnly ? { ...old, kind: 'context' } : old,
        next: isWhitespaceOnly ? { ...next, kind: 'context' } : next,
        key: `change-${start}-${offset}`,
      })
    }
  }
  return rows
}

function PersonAvatar({ author }: { author: string }) {
  return <Avatar user={author} size={25} />
}

function ReviewThread({ thread, pr, onUpdate, onNotify, busy = false }: ReviewProps & { thread: Thread }) {
  const [reply, setReply] = useReviewSessionState(`thread.${thread.id}.reply`, '')
  const [replyOpen, setReplyOpen] = useReviewSessionState(`thread.${thread.id}.replyOpen`, false)
  const [expanded, setExpanded] = useReviewSessionState(`thread.${thread.id}.expanded`, !thread.resolved)
  const [saving, setSaving] = useReviewSessionState(`thread.${thread.id}.saving`, false)
  const [error, setError] = useReviewSessionState(`thread.${thread.id}.error`, '')
  const editable = ['open', 'draft'].includes(pr.status) && pr.provider?.detailLoaded !== false && allowsReview(pr,'comments')
  const canResolve = editable && allowsReview(pr,'resolveThreads') && (thread.resolved ? thread.canUnresolve !== false : thread.canResolve !== false)

  const toggleResolved = async () => {
    if (!canResolve || busy || saving) return
    setSaving(true)
    setError('')
    try {
      await onUpdate({ ...pr, threads: pr.threads.map(item => item.id === thread.id ? { ...item, resolved: !item.resolved } : item) })
      setExpanded(thread.resolved)
      onNotify(thread.resolved ? 'Conversation reopened' : 'Conversation resolved')
    } catch (issue) {
      setError(issue instanceof Error ? issue.message : 'The conversation could not be updated.')
    } finally { setSaving(false) }
  }

  const sendReply = async () => {
    const body = reply.trim()
    if (!body || !editable || busy || saving) return
    setSaving(true)
    setError('')
    try {
      await onUpdate({ ...pr, threads: pr.threads.map(item => item.id === thread.id ? {
        ...item,
        comments: [...item.comments, { id: uniqueId('reply'), author: currentUser, body, createdAt: new Date().toISOString() }],
      } : item) })
      setReply('')
      setReplyOpen(false)
      onNotify('Reply added')
    } catch (issue) {
      setError(issue instanceof Error ? issue.message : 'The reply could not be posted. Your text has been kept.')
    } finally { setSaving(false) }
  }

  return (
    <section id={`review-thread-${thread.id}`} className={`review-thread ${thread.resolved ? 'is-resolved' : ''}`} aria-label={`Conversation on line ${thread.line}`}>
      <div className="review-thread-top">
        <span className="review-thread-state"><Icon name={thread.resolved ? 'check-circle' : 'message-circle'} size={13} />{thread.resolved ? 'Resolved conversation' : 'Review conversation'}</span>
        <div className="review-thread-actions">
          {thread.resolved && <button className="btn-ghost review-text-button" onClick={() => setExpanded(!expanded)} aria-expanded={expanded}>{expanded ? 'Hide' : `Show ${thread.comments.length} ${thread.comments.length === 1 ? 'comment' : 'comments'}`}</button>}
          <button className="btn-ghost review-text-button" onClick={toggleResolved} disabled={!canResolve || busy || saving}>{thread.resolved ? 'Reopen' : 'Resolve'}</button>
        </div>
      </div>
      {expanded && <>
        {thread.comments.map(comment => <div className="review-comment" key={comment.id}>
          <PersonAvatar author={comment.author} />
          <div className="review-comment-content">
            <div className="review-comment-meta"><strong><ActorName user={comment.author} author={pr.author} /></strong><time dateTime={comment.createdAt}>{shortTime(comment.createdAt)}</time></div>
            <Markdown>{comment.body}</Markdown>
          </div>
        </div>)}
        {!thread.resolved && editable && <div className="review-reply-area">
          {replyOpen ? <form onSubmit={event => { event.preventDefault(); sendReply() }}>
            <label className="sr-only" htmlFor={`reply-${thread.id}`}>Reply to this conversation</label>
            <textarea id={`reply-${thread.id}`} className="input review-textarea" placeholder="Reply to this conversation…" value={reply} onChange={event => setReply(event.target.value)} disabled={saving} autoFocus rows={2} />
            <div className="review-composer-actions"><span className="muted">Replies are posted immediately.</span><button type="button" className="btn btn-ghost" disabled={saving} onClick={() => setReplyOpen(false)}>Cancel</button><button className="btn btn-primary" disabled={!reply.trim() || busy || saving}>{saving ? 'Posting…' : 'Reply'}</button></div>
          </form> : <button className="review-reply-trigger" disabled={busy || saving} onClick={() => setReplyOpen(true)}><Icon name="reply" size={13} /> Reply to this conversation…</button>}
        </div>}
      </>}
      {error && <p className="review-thread-error" role="alert">{error}</p>}
    </section>
  )
}

function PendingThread({ comment, pr, onUpdate, onNotify, busy = false }: ReviewProps & { comment: PendingComment }) {
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const remove = async () => {
    if (busy || saving) return
    setSaving(true)
    setError('')
    try {
      await onUpdate({ ...pr, pendingComments: pr.pendingComments.filter(item => item.id !== comment.id) })
      onNotify('Pending comment removed')
    } catch (issue) { setError(issue instanceof Error ? issue.message : 'The pending comment could not be removed.') }
    finally { setSaving(false) }
  }
  return <section id={`review-thread-${comment.id}`} className="review-thread review-pending-thread" aria-label={`Pending comment on line ${comment.line}`}>
    <div className="review-comment">
      <PersonAvatar author={currentUser} />
      <div className="review-comment-content">
        <div className="review-comment-meta"><strong><ActorName user={currentUser} author={pr.author} /></strong><span className="review-pending-label">{comment.ai?'AI suggestion · '+comment.ai.severity:'Pending review'}</span><button className="icon-button review-delete-comment" disabled={busy || saving} title="Delete pending comment" aria-label={`Delete pending comment on line ${comment.line}`} onClick={remove}><Icon name="trash" size={13} /></button></div>
        <Markdown>{comment.body}</Markdown>
      </div>
    </div>
    {error && <p className="review-thread-error" role="alert">{error}</p>}
  </section>
}

export default function ReviewWorkspace({ pr, onUpdate, onNotify, focusMode, onToggleFocus, requestedLocation, busy = false }: ReviewWorkspaceProps) {
  const codePreferences = useSyncExternalStore(subscribeCodeReviewPreferences, getCodeReviewPreferences, () => DEFAULT_CODE_REVIEW_PREFERENCES)
  const appearance = useSyncExternalStore(subscribeCodeAppearance, getCodeAppearance, () => 'light' as const)
  const syntaxTheme = effectiveCodeTheme(appearance, codePreferences)
  const [highlighted, setHighlighted] = useState<{ source: string; theme: string; result: HighlightedDiff } | null>(null)
  const [activePath, setActivePath] = useReviewSessionState('workspace.activePath', () => requestedLocation && pr.files.some(item => item.path === requestedLocation.path) ? requestedLocation.path : pr.files[0]?.path ?? '')
  const [query, setQuery] = useReviewSessionState('workspace.query', '')
  const [diffMode, setDiffMode] = useReviewSessionState<'split' | 'unified'>('workspace.diffMode', 'split')
  const [ignoreWhitespace, setIgnoreWhitespace] = useReviewSessionState('workspace.ignoreWhitespace', false)
  const [wrapLines, setWrapLines] = useReviewSessionState('workspace.wrapLines', true)
  const [filePaneWidth, setFilePaneWidth] = useReviewSessionState('workspace.filePaneWidth', 218)
  const [resizingFiles, setResizingFiles] = useState(false)
  const [commentAt, setCommentAt] = useReviewSessionState<LineLocation | null>('workspace.commentAt', null)
  const [commentBody, setCommentBody] = useReviewSessionState('workspace.commentBody', '')
  const [composerHeadSha, setComposerHeadSha] = useReviewSessionState<string | null>('workspace.composerHeadSha', null)
  const [composerPath, setComposerPath] = useReviewSessionState<string | null>('workspace.composerPath', null)
  const [saving, setSaving] = useState(false)
  const [mutationError, setMutationError] = useState('')
  const diffScrollRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const resizeOrigin = useRef({ x: 0, width: 218 })
  const lastRequestedLocation = useReviewSessionRef<ReviewLocation | undefined>('workspace.lastLocation', undefined)
  const requestedScrollPending = useRef(false)
  const scrollPositions = useReviewSessionRef('workspace.scroll', new Map<string, {top: number; left: number}>())
  const file = pr.files.find(item => item.path === activePath) ?? pr.files[0]
  const syntaxSource = useMemo(() => file ? JSON.stringify([file.path, file.previousPath, file.lines]) : '', [file])
  const syntax = highlighted?.source === syntaxSource && highlighted?.theme === syntaxTheme ? highlighted.result : undefined
  useEffect(() => {
    let canceled = false
    if (file) void highlightDiff(file, syntaxTheme).then(result => { if (!canceled) setHighlighted({ source: syntaxSource, theme: syntaxTheme, result }) })
    return () => { canceled = true }
  }, [syntaxSource, syntaxTheme])
  const visibleFiles = useMemo(() => pr.files.filter(item => item.path.toLowerCase().includes(query.toLowerCase())), [pr.files, query])
  const groups = useMemo(() => {
    const folders = new Map<string, ChangedFile[]>()
    for (const item of visibleFiles) {
      const folder = item.path.includes('/') ? item.path.slice(0, item.path.lastIndexOf('/')) : 'Repository root'
      folders.set(folder, [...(folders.get(folder) ?? []), item])
    }
    return [...folders.entries()]
  }, [visibleFiles])
  const navigableFiles = useMemo(() => groups.flatMap(([, files]) => files), [groups])
  const splitRows = useMemo(() => alignLines(file?.lines ?? [], ignoreWhitespace), [file, ignoreWhitespace])
  const hunkHeaders = useMemo(() => [...(file?.rawPatch ?? '').matchAll(/^@@ .+? @@.*$/gm)].map(match => match[0]), [file])
  const unifiedLines = useMemo(() => {
    const lines = file?.lines ?? []
    if (!ignoreWhitespace) return lines
    const oldRows = new Map<string, SplitRow>()
    const newRows = new Map<string, SplitRow>()
    for (const row of splitRows) {
      if (row.old) oldRows.set(syntaxLineKey(row.old, 'old'), row)
      if (row.next) newRows.set(syntaxLineKey(row.next, 'new'), row)
    }
    return lines.flatMap(line => {
      if (line.kind === 'context') return [line]
      const paired = line.kind === 'remove' ? oldRows.get(syntaxLineKey(line, 'old')) : newRows.get(syntaxLineKey(line, 'new'))
      if (paired?.old?.kind !== 'context' || paired.next?.kind !== 'context') return [line]
      return line.kind === 'remove' ? [] : [{ ...line, kind: 'context' as const, oldLine: paired.old.oldLine }]
    })
  }, [file, splitRows, ignoreWhitespace])
  const editable = ['open', 'draft'].includes(pr.status) && pr.provider?.detailLoaded !== false && allowsReview(pr,'inlineComments')
  const composerOutdated = composerHeadSha !== null && composerHeadSha !== pr.headSha
  const composerDetached = commentAt !== null && (composerPath !== file?.path || !file?.lines.some(line => commentAt.side === 'old' ? line.oldLine === commentAt.line : line.newLine === commentAt.line))

  const previousPath = useRef(activePath)
  useEffect(() => {
    if (previousPath.current === activePath) return
    previousPath.current = activePath
    setCommentAt(null)
    setCommentBody('')
    setComposerHeadSha(null)
    setComposerPath(null)
  }, [activePath])

  const scrollKey = JSON.stringify([file?.path, diffMode])
  useLayoutEffect(() => {
    const saved = scrollPositions.current.get(scrollKey)
    diffScrollRef.current?.scrollTo({ top: saved?.top ?? 0, left: saved?.left ?? 0 })
  }, [scrollKey])

  useEffect(() => {
    if (lastRequestedLocation.current === requestedLocation) return
    lastRequestedLocation.current = requestedLocation
    requestedScrollPending.current = Boolean(requestedLocation)
    if (requestedLocation && pr.files.some(item => item.path === requestedLocation.path)) {
      setActivePath(requestedLocation.path)
      setQuery('')
      if (requestedLocation.line !== undefined) setIgnoreWhitespace(false)
    }
  }, [requestedLocation, pr.number])

  useEffect(() => {
    if (!requestedScrollPending.current || !requestedLocation || activePath !== requestedLocation.path) return
    const viewport = diffScrollRef.current
    const thread = requestedLocation.threadId
      ? document.getElementById(`review-thread-${requestedLocation.threadId}`)
      : viewport?.querySelector(`[data-code-line="${requestedLocation.line}"][data-code-side="${requestedLocation.side}"]`) ?? viewport?.querySelector(`[data-review-${requestedLocation.side}-line="${requestedLocation.line}"]`)
    if (!viewport || !thread || !viewport.contains(thread)) return
    const viewportRect = viewport.getBoundingClientRect()
    const threadRect = thread.getBoundingClientRect()
    viewport.scrollTo({ top: Math.max(0, viewport.scrollTop + threadRect.top - viewportRect.top - Math.max(12, (viewport.clientHeight - threadRect.height) / 2)) })
    requestedScrollPending.current = false
  }, [activePath, requestedLocation, pr.number, diffMode, file])

  const selectAdjacent = (offset: number) => {
    const index = navigableFiles.findIndex(item => item.path === file?.path)
    const next = navigableFiles[index + offset]
    if (next) setActivePath(next.path)
  }

  useEffect(() => {
    const handleKey = (event: KeyboardEvent) => {
      const element = event.target as HTMLElement
      if (element.closest('input, textarea, select, [contenteditable="true"]') || event.metaKey || event.ctrlKey || event.altKey) return
      if (event.key === '[' || event.key === ']') {
        event.preventDefault()
        selectAdjacent(event.key === '[' ? -1 : 1)
      }
    }
    window.addEventListener('keydown', handleKey)
    return () => window.removeEventListener('keydown', handleKey)
  }, [navigableFiles, file?.path])

  const setViewed = async (path: string, viewed: boolean): Promise<boolean> => {
    if (busy || saving) return false
    setSaving(true)
    setMutationError('')
    try {
      await onUpdate({ ...pr, viewedFiles: viewed ? [...new Set([...pr.viewedFiles, path])] : pr.viewedFiles.filter(item => item !== path) })
      return true
    } catch (issue) {
      setMutationError(issue instanceof Error ? issue.message : 'The viewed state could not be saved.')
      return false
    } finally { setSaving(false) }
  }

  const beginComment = (location: LineLocation) => {
    if (!file || busy || saving || !editable) return
    setCommentAt(location)
    setCommentBody('')
    setComposerHeadSha(pr.headSha)
    setComposerPath(file.path)
  }

  const addComment = async () => {
    if (!composerPath || !commentAt || !commentBody.trim() || !editable || busy || saving) return
    setSaving(true)
    setMutationError('')
    try {
      await onUpdate({ ...pr, pendingComments: [...pr.pendingComments, { id: uniqueId('pending'), path: composerPath, ...commentAt, body: commentBody.trim(), headSha: composerHeadSha ?? pr.headSha, outdated: composerOutdated || composerDetached }] })
      setCommentAt(null)
      setCommentBody('')
      onNotify(composerOutdated || composerDetached ? 'Draft kept from earlier changes. Choose a current line before submitting.' : 'Comment added to your pending review')
    } catch (issue) { setMutationError(issue instanceof Error ? issue.message : 'The comment could not be saved. Your text has been kept.') }
    finally { setSaving(false) }
  }

  const renderComposer = () => <form className={`review-thread review-inline-composer${composerDetached ? ' review-detached-composer' : ''}`} onSubmit={event => { event.preventDefault(); addComment() }}>
    <div className="review-composer-header"><PersonAvatar author={currentUser} /><strong>New review comment</strong><span className="muted">{composerDetached ? `${composerPath}:` : 'Line '}{commentAt?.line}</span></div>
    {(composerOutdated || composerDetached) && <div className="review-composer-revision-note" role="status"><Icon name="alert-circle" size={14} /><span>Draft started on an earlier commit. Keep it to reposition on current code.</span>{!composerDetached && <button type="button" className="btn-ghost review-text-button" disabled={busy || saving} onClick={() => setComposerHeadSha(pr.headSha)}>Reposition on this line</button>}</div>}
    <label className="sr-only" htmlFor="new-review-comment">Review comment</label>
    <textarea autoFocus id="new-review-comment" className="input review-textarea" rows={3} placeholder="Ask a question or suggest an improvement…" value={commentBody} onChange={event => setCommentBody(event.target.value)} disabled={saving} />
    <div className="review-composer-actions"><span className="muted">Only you can see this until you submit.</span><button type="button" className="btn btn-ghost" disabled={saving} onClick={() => setCommentAt(null)}>Cancel</button><button className="btn btn-primary" disabled={!commentBody.trim() || busy || saving}>{saving ? 'Saving…' : composerOutdated || composerDetached ? 'Keep draft' : 'Add to review'}</button></div>
  </form>

  const renderDiscussion = (locations: LineLocation[]) => {
    if (!file) return null
    const atLocation = (item: { line: number; side: 'old' | 'new' }) => locations.some(location => location.line === item.line && location.side === item.side)
    const threads = pr.threads.filter(thread => !thread.outdated && thread.path === file.path && atLocation(thread))
    const pending = pr.pendingComments.filter(comment => !comment.outdated && (!comment.headSha || comment.headSha === pr.headSha) && comment.path === file.path && atLocation(comment))
    const hasComposer = !composerDetached && composerPath === file.path && commentAt !== null && atLocation(commentAt)
    if (!threads.length && !pending.length && !hasComposer) return null
    return <div className="review-discussion-row">
      {threads.map(thread => <ReviewThread key={thread.id} thread={thread} pr={pr} onUpdate={onUpdate} onNotify={onNotify} busy={busy || saving} />)}
      {pending.map(comment => <PendingThread key={comment.id} comment={comment} pr={pr} onUpdate={onUpdate} onNotify={onNotify} busy={busy || saving} />)}
      {hasComposer && renderComposer()}
    </div>
  }

  const renderCell = (line: DiffLine | undefined, side: 'old' | 'new') => {
    const number = side === 'old' ? line?.oldLine : line?.newLine
    const targeted = requestedLocation?.path === file?.path && requestedLocation?.line === number && requestedLocation?.side === side
    return <div className={`review-diff-cell ${line ? `diff-${line.kind}` : 'diff-empty'}${targeted ? ' is-review-target' : ''}`}>
      <span className="review-line-number" aria-hidden="true">{number ?? ''}</span>
      <span className="review-line-marker" aria-hidden="true">{line?.kind === 'add' ? '+' : line?.kind === 'remove' ? '−' : ''}</span>
      <code className="review-code-text" data-code-path={number !== undefined ? side === 'old' ? file?.previousPath || file?.path : file?.path : undefined} data-code-line={number} data-code-side={number !== undefined ? side : undefined}>{line ? tokenLine(syntax?.[side].get(syntaxLineKey(line, side)), line.text) : ''}</code>
      {line && number !== undefined && editable && <button className="review-add-comment" disabled={busy || saving} aria-label={`Comment on ${side === 'old' ? 'original' : 'changed'} line ${number} in ${file?.path}`} title={`Comment on line ${number}`} onClick={() => beginComment({ line: number, side })}><Icon name="plus" size={12} /></button>}
    </div>
  }

  const activeIndex = navigableFiles.findIndex(item => item.path === file?.path)
  const activeThreads = file ? pr.threads.filter(thread => !thread.outdated && thread.path === file.path && !thread.resolved && file.lines.some(line => thread.side === 'old' ? line.oldLine === thread.line : line.newLine === thread.line)).length : 0
  const reviewedCount = pr.files.filter(item => pr.viewedFiles.includes(item.path)).length

  return <section className={`review-workspace ${focusMode ? 'review-workspace-expanded' : ''} ${resizingFiles ? 'is-resizing-files' : ''}`} style={{ ...codeReviewStyleVariables(codePreferences), '--review-file-pane-width': `${filePaneWidth}px`, '--review-code-foreground': syntax?.foreground, '--review-code-background': syntax?.background } as CSSProperties} aria-label="Pull request changes">
    <div className="review-toolbar">
      <div className="review-toolbar-title"><Icon name="files" size={15} /><strong>Files changed</strong><span className="review-count">{pr.files.length}</span></div>
      <div className="review-toolbar-controls">
        <div className="review-segmented" aria-label="Diff layout"><button className={diffMode === 'split' ? 'is-selected' : ''} aria-pressed={diffMode === 'split'} onClick={() => setDiffMode('split')}><Icon name="columns" size={13} />Split</button><button className={diffMode === 'unified' ? 'is-selected' : ''} aria-pressed={diffMode === 'unified'} onClick={() => setDiffMode('unified')}><Icon name="align-left" size={13} />Unified</button></div>
        <label className="review-whitespace review-wrap-control"><input type="checkbox" checked={wrapLines} onChange={event => setWrapLines(event.target.checked)} /><span>Wrap lines</span></label>
        <label className="review-whitespace"><input type="checkbox" checked={ignoreWhitespace} onChange={event => setIgnoreWhitespace(event.target.checked)} /><span>Ignore whitespace</span></label>
        <button className="btn btn-ghost review-expand" aria-label={focusMode ? 'Exit expanded review' : 'Expand review to fill the window'} aria-pressed={focusMode} onClick={onToggleFocus}><Icon name={focusMode ? 'minimize' : 'maximize'} size={14} /><span>{focusMode ? 'Exit focus' : 'Expand review'}</span></button>
      </div>
    </div>
    {mutationError && <p className="review-mutation-error" role="alert">{mutationError}</p>}
    {composerDetached && renderComposer()}
    <div className="review-panes">
      <aside className="review-file-pane" aria-label="Changed files">
        <div className="review-file-search"><Icon name="search" size={14} /><input ref={searchRef} aria-label="Filter changed files" placeholder="Filter files…" value={query} onChange={event => setQuery(event.target.value)} />{query && <button className="icon-button" aria-label="Clear file filter" onClick={() => { setQuery(''); searchRef.current?.focus() }}><Icon name="x" size={12} /></button>}</div>
        <div className="review-file-list">
          {groups.map(([folder, files]) => <div className="review-file-group" key={folder}>
            <div className="review-folder-label" title={folder}><Icon name="chevron-down" size={11} /><Icon name="folder" size={13} /><span>{folder}</span></div>
            {files.map(item => <div className={`review-file-row ${file?.path === item.path ? 'is-active' : ''}`} key={item.path}>
              <button className="review-file-select" title={item.path} aria-current={file?.path === item.path ? 'true' : undefined} onClick={() => setActivePath(item.path)}><FileChangeIcon file={item}/><span className="review-file-name">{item.path.slice(item.path.lastIndexOf('/') + 1)}</span></button>
              <input className="review-file-viewed" type="checkbox" checked={pr.viewedFiles.includes(item.path)} disabled={busy || saving} onChange={event => setViewed(item.path, event.target.checked)} aria-label={`Mark ${item.path} as ${pr.viewedFiles.includes(item.path) ? 'not viewed' : 'viewed'}`} title={pr.viewedFiles.includes(item.path) ? 'Viewed' : 'Mark as viewed'} />
            </div>)}
          </div>)}
          {!visibleFiles.length && <div className="review-files-empty"><Icon name="search" size={20} /><strong>No matching files</strong><span>Try another filename or path.</span><button className="btn btn-ghost" onClick={() => setQuery('')}>Clear filter</button></div>}
        </div>
        <div className="review-file-pane-footer"><span>{reviewedCount} of {pr.files.length} viewed</span><span className="review-file-shortcuts"><kbd>[</kbd><kbd>]</kbd> next file</span></div>
        <div className="review-file-resizer" role="separator" aria-label="Resize changed files navigator" aria-orientation="vertical" aria-valuemin={160} aria-valuemax={360} aria-valuenow={filePaneWidth} aria-valuetext={`${filePaneWidth} pixels`} tabIndex={0} title="Drag to resize file navigator" onPointerDown={event => {
          resizeOrigin.current = { x: event.clientX, width: filePaneWidth }
          event.currentTarget.setPointerCapture(event.pointerId)
          setResizingFiles(true)
          event.preventDefault()
        }} onPointerMove={event => {
          if (resizingFiles) setFilePaneWidth(Math.max(160, Math.min(360, resizeOrigin.current.width + event.clientX - resizeOrigin.current.x)))
        }} onPointerUp={event => { event.currentTarget.releasePointerCapture(event.pointerId); setResizingFiles(false) }} onPointerCancel={() => setResizingFiles(false)} onKeyDown={event => {
          if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
          event.preventDefault()
          setFilePaneWidth(width => event.key === 'Home' ? 160 : event.key === 'End' ? 360 : Math.max(160, Math.min(360, width + (event.key === 'ArrowRight' ? 10 : -10))))
        }}><Icon name="more" size={12} /></div>
      </aside>
      <div className="review-diff-pane">
        {file ? <>
          <div className="review-file-header">
            <div className="review-file-heading"><Icon name="file-code" size={15} /><strong title={file.path}>{file.path}</strong>{file.previousPath && <span className="muted review-previous-path" title={file.previousPath}>from {file.previousPath}</span>}<span className="review-change-stats"><span className="text-green">+{file.additions}</span><span className="text-red">−{file.deletions}</span></span></div>
            <div className="review-file-header-actions">{activeThreads > 0 && <button className="review-open-threads" title="Jump to first unresolved conversation" aria-label={`${activeThreads} unresolved ${activeThreads === 1 ? 'conversation' : 'conversations'}. Jump to the first conversation.`} onClick={() => {
              const first = pr.threads.find(thread => !thread.outdated && thread.path === file.path && !thread.resolved && file.lines.some(line => thread.side === 'old' ? line.oldLine === thread.line : line.newLine === thread.line))
              if (first) document.getElementById(`review-thread-${first.id}`)?.scrollIntoView({ block: 'center' })
            }}><Icon name="message-circle" size={13} />{activeThreads}</button>}<label className="review-viewed-control"><input type="checkbox" checked={pr.viewedFiles.includes(file.path)} disabled={busy || saving} onChange={event => setViewed(file.path, event.target.checked)} />Viewed</label><div className="review-file-navigation"><button className="icon-button" disabled={activeIndex <= 0} onClick={() => selectAdjacent(-1)} aria-label="Previous file" title="Previous file ([)"><Icon name="chevron-up" size={14} /></button><button className="icon-button" disabled={activeIndex < 0 || activeIndex >= navigableFiles.length - 1} onClick={() => selectAdjacent(1)} aria-label="Next file" title="Next file (])"><Icon name="chevron-down" size={14} /></button></div></div>
          </div>
          {file.truncated && file.lines.length > 0 && <div className="review-diff-notice"><Icon name="alert-circle" size={14} />This diff is truncated. Only the available patch lines are shown.</div>}
          <div className="review-diff-scroll" ref={diffScrollRef} onScroll={event => scrollPositions.current.set(scrollKey, {top:event.currentTarget.scrollTop,left:event.currentTarget.scrollLeft})} tabIndex={0} aria-label={`Code changes in ${file.path}`}>
            {file.lines.length ? <div className={`review-diff-content ${diffMode === 'split' ? 'is-split' : 'is-unified'} ${wrapLines ? 'wrap-lines' : ''}`}>
              {diffMode === 'split' ? <>
                <div className="review-diff-column-labels"><span><Icon name="git-branch" size={12} />{pr.base}<span className="muted">Original</span></span><span><Icon name="git-branch" size={12} />{pr.head}<span className="muted">Changed</span></span></div>
                <div className="review-hunk-label"><span>{hunkHeaders[0] ?? `@@ −${file.lines.find(line => line.oldLine)?.oldLine ?? 0},${file.lines.filter(line => line.oldLine !== undefined).length} +${file.lines.find(line => line.newLine)?.newLine ?? 0},${file.lines.filter(line => line.newLine !== undefined).length} @@`}</span><span>{ignoreWhitespace ? 'Whitespace changes ignored' : 'Click + beside a line to comment'}</span></div>
                {splitRows.map((row, index) => {
                  const hunk = row.old?.hunk ?? row.next?.hunk
                  const previous = splitRows[index - 1]
                  const previousHunk = previous?.old?.hunk ?? previous?.next?.hunk
                  return <div className="review-diff-row-block" key={row.key}>{index > 0 && hunk !== undefined && hunk !== previousHunk && <div className="review-hunk-label"><span>{hunkHeaders[hunk]}</span><span>Next changed section</span></div>}<div className="review-split-row">{renderCell(row.old, 'old')}{renderCell(row.next, 'new')}</div>{renderDiscussion([...(row.old?.oldLine !== undefined ? [{ line: row.old.oldLine, side: 'old' as const }] : []), ...(row.next?.newLine !== undefined ? [{ line: row.next.newLine, side: 'new' as const }] : [])])}</div>
                })}
              </> : <>
                <div className="review-diff-column-labels review-unified-column-label"><span><Icon name="git-branch" size={12} />{pr.base}<Icon name="arrow-right" size={12} />{pr.head}<span className="muted">Unified diff</span></span></div>
                <div className="review-hunk-label"><span>{hunkHeaders[0] ?? `@@ −${file.lines.find(line => line.oldLine)?.oldLine ?? 0} +${file.lines.find(line => line.newLine)?.newLine ?? 0} @@`}</span><span>{ignoreWhitespace ? 'Whitespace changes ignored' : 'Click + beside a line to comment'}</span></div>
                {unifiedLines.map((line, index) => {
                  const side = line.kind === 'remove' ? 'old' : 'new'
                  const number = side === 'old' ? line.oldLine : line.newLine
                  return <div className="review-diff-row-block" key={`${line.kind}-${index}`}>{index > 0 && line.hunk !== undefined && line.hunk !== unifiedLines[index - 1].hunk && <div className="review-hunk-label"><span>{hunkHeaders[line.hunk]}</span><span>Next changed section</span></div>}<div className={`review-unified-row diff-${line.kind}${requestedLocation?.path === file.path && (requestedLocation.side === "old" ? line.oldLine : line.newLine) === requestedLocation.line && requestedLocation.line !== undefined ? " is-review-target" : ""}`} data-review-old-line={line.oldLine} data-review-new-line={line.newLine}><span className="review-line-number" aria-hidden="true">{line.oldLine ?? ''}</span><span className="review-line-number" aria-hidden="true">{line.newLine ?? ''}</span><span className="review-line-marker" aria-hidden="true">{line.kind === 'add' ? '+' : line.kind === 'remove' ? '−' : ''}</span><code className="review-code-text" data-code-path={number !== undefined ? side === 'old' ? file.previousPath || file.path : file.path : undefined} data-code-line={number} data-code-side={number !== undefined ? side : undefined}>{tokenLine(syntax?.[side].get(syntaxLineKey(line, side)), line.text)}</code>{number !== undefined && editable && <button className="review-add-comment" disabled={busy || saving} aria-label={`Comment on line ${number} in ${file.path}`} title={`Comment on line ${number}`} onClick={() => beginComment({ line: number, side })}><Icon name="plus" size={12} /></button>}</div>{renderDiscussion([...(line.oldLine !== undefined ? [{ line: line.oldLine, side: 'old' as const }] : []), ...(line.newLine !== undefined ? [{ line: line.newLine, side: 'new' as const }] : [])])}</div>
                })}
              </>}
              <div className="review-diff-end"><Icon name="check" size={13} />End of file<button className="btn-ghost review-text-button" disabled={busy || saving} onClick={async () => { const wasViewed = pr.viewedFiles.includes(file.path); const saved = await setViewed(file.path, !wasViewed); if (saved && !wasViewed) selectAdjacent(1) }}>{pr.viewedFiles.includes(file.path) ? 'Mark as not viewed' : activeIndex < navigableFiles.length - 1 ? 'Mark viewed and next file' : 'Mark as viewed'}<Icon name="arrow-right" size={12} /></button></div>
            </div> : <div className="review-code-empty"><Icon name="file" size={28} /><strong>{file.binary ? 'Binary file' : file.truncated ? 'Diff too large to display' : file.patchUnavailable ? 'Text diff unavailable' : 'No text changes to display'}</strong><p>{file.binary ? 'Binary contents cannot be compared as text.' : file.truncated ? 'This file exceeds the available diff limit.' : file.patchUnavailable ? 'The provider did not supply a text patch for this file.' : 'This file has no reviewable text diff.'}</p><button className="btn" disabled={busy || saving} onClick={() => setViewed(file.path, !pr.viewedFiles.includes(file.path))}>{pr.viewedFiles.includes(file.path) ? 'Mark as not viewed' : 'Mark as viewed'}</button></div>}
          </div>
        </> : <div className="review-code-empty"><Icon name={pr.provider?.detailLoaded === false ? 'clock' : 'files'} size={28} /><strong>{pr.provider?.detailLoaded === false ? 'Loading changed files' : 'No changed files'}</strong><p>{pr.provider?.detailLoaded === false ? 'Retrieving the pull request diff.' : 'This pull request has no file changes.'}</p></div>}
      </div>
    </div>
  </section>
}

export function ReviewFooter({ pr, onUpdate, onNotify, busy = false }: ReviewProps) {
  const [expanded, setExpanded] = useReviewSessionState('footer.expanded', false)
  const [decision, setDecision] = useReviewSessionState<ReviewDecision>('footer.decision', 'commented')
  const [body, setBody] = useReviewSessionState('footer.body', '')
  const [error, setError] = useReviewSessionState('footer.error', '')
  const [submitting, setSubmitting] = useReviewSessionState('footer.submitting', false)
  const [deletingId, setDeletingId] = useState<string | null>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const editable = ['open', 'draft'].includes(pr.status) && pr.provider?.detailLoaded !== false && allowsReview(pr,'comments')
  const isDraft = pr.status === 'draft'
  const hasNewCommits = pr.provider?.hasNewCommits === true
  const selfAuthored = pr.author === currentUser
  const review = latestReview(pr, currentUser)
  const viewed = pr.files.filter(file => pr.viewedFiles.includes(file.path)).length
  const unviewed = pr.files.length - viewed
  const pending = pr.pendingComments.length
  const outdatedComments = pr.pendingComments.filter(comment => comment.outdated || (comment.headSha && comment.headSha !== pr.headSha) || !pr.files.some(file => file.path === comment.path && file.lines.some(line => comment.side === 'old' ? line.oldLine === comment.line : line.newLine === comment.line)))
  const locked = busy || submitting || deletingId !== null
  const permittedDecision = allowsReview(pr,decision === 'approved' ? 'approve' : decision === 'changes_requested' ? 'requestChanges' : 'comments')
  const valid = permittedDecision && editable && !locked && !hasNewCommits && !outdatedComments.length && !(selfAuthored && decision === 'approved') && !(isDraft && decision !== 'commented') && (decision !== 'changes_requested' || body.trim().length > 0) && (decision !== 'commented' || body.trim().length > 0 || pending > 0)

  const wasExpanded = useRef(expanded)
  useEffect(() => {
    if (expanded && !wasExpanded.current) textareaRef.current?.focus()
    wasExpanded.current = expanded
  }, [expanded])

  useEffect(() => {
    if (!permittedDecision || (isDraft && decision !== 'commented') || (selfAuthored && decision === 'approved')) setDecision('commented')
  }, [isDraft, selfAuthored, decision, permittedDecision])

  const removeOutdated = async (comment: PendingComment) => {
    if (locked) return
    setDeletingId(comment.id)
    setError('')
    try {
      await onUpdate({ ...pr, pendingComments: pr.pendingComments.filter(item => item.id !== comment.id) })
      onNotify('Outdated draft comment removed')
    } catch (issue) { setError(issue instanceof Error ? issue.message : 'The draft comment could not be removed.') }
    finally { setDeletingId(null) }
  }

  const sendReview = async () => {
    if (!valid) return
    setSubmitting(true)
    setError('')
    try {
      const next = submitReview(pr, decision, body.trim())
      await onUpdate(next)
      setExpanded(false)
      setBody('')
      setError('')
      onNotify(`${decisionLabels[decision]}${pending ? ` · ${pending} ${pending === 1 ? 'comment' : 'comments'} posted` : ''}`)
    } catch (issue) {
      setError(issue instanceof Error ? issue.message : 'The review could not be submitted. Please try again.')
    } finally { setSubmitting(false) }
  }

  return <div className="review-footer-wrap">
    {outdatedComments.length > 0 && <section className="review-outdated-drafts" aria-label="Outdated draft comments">
      <div className="review-outdated-heading"><Icon name="alert-circle" size={14} /><strong>{outdatedComments.length} draft {outdatedComments.length === 1 ? 'comment needs' : 'comments need'} a new line anchor</strong><span>Copy your feedback to a current line, then remove the old draft.</span></div>
      <div className="review-outdated-list">{outdatedComments.map(comment => <div className="review-outdated-comment" key={comment.id}><div><code>{comment.path}:{comment.line}</code><span className="muted">{comment.headSha ? `Commit ${comment.headSha.slice(0, 7)}` : 'Anchor no longer available'}</span><Markdown>{comment.body}</Markdown></div><button className="icon-button" disabled={locked} aria-label={`Delete outdated draft comment on line ${comment.line} in ${comment.path}`} title="Delete outdated draft comment" onClick={() => removeOutdated(comment)}><Icon name="trash" size={14} /></button></div>)}</div>
    </section>}
    {expanded && <form className="review-submit-form" onSubmit={event => { event.preventDefault(); sendReview() }}>
      <div className="review-submit-heading"><strong>Finish your review</strong><span className="muted">{pending ? `${pending} pending ${pending === 1 ? 'comment will' : 'comments will'} be posted.` : 'Add a summary for the author.'}</span><button type="button" className="icon-button" disabled={submitting} aria-label="Close review summary" onClick={() => setExpanded(false)}><Icon name="x" size={15} /></button></div>
      {hasNewCommits && <p className="review-submit-notice"><Icon name="alert-circle" size={14} />You are reviewing commit {pr.headSha.slice(0, 7)}. Refresh to the latest commits before submitting.</p>}
      {isDraft && <p className="review-submit-notice">Draft pull requests accept comments. Approval and change requests become available when the author marks this ready for review.</p>}
      <label className="review-summary-label" htmlFor="review-summary">{decision === 'changes_requested' ? 'What needs to change?' : 'Review summary'}<span className="muted">{decision === 'changes_requested' ? 'Required' : 'Optional'}</span></label>
      <textarea id="review-summary" ref={textareaRef} className="input review-textarea" rows={3} placeholder={decision === 'approved' ? 'Looks good. Add any final notes…' : decision === 'changes_requested' ? 'Describe what needs to be addressed before merging…' : 'Share your overall feedback…'} value={body} onChange={event => { setBody(event.target.value); setError('') }} disabled={submitting} aria-invalid={Boolean(error)} aria-describedby={error ? 'review-submit-error' : undefined} />
      {error && <p id="review-submit-error" className="review-submit-error" role="alert">{error}</p>}
      <div className="review-submit-form-actions"><span className="muted">{unviewed ? `${unviewed} ${unviewed === 1 ? 'file has' : 'files have'} not been marked viewed.` : 'All changed files viewed.'}</span><button type="button" className="btn btn-ghost" disabled={submitting} onClick={() => setExpanded(false)}>Cancel</button><button className="btn btn-primary" disabled={!valid}><Icon name={decision === 'approved' ? 'check' : decision === 'changes_requested' ? 'git-pull-request' : 'message-circle'} size={14} />{submitting ? 'Submitting…' : 'Submit review'}</button></div>
    </form>}
    {error && !expanded && <p className="review-mutation-error" role="alert">{error}</p>}
    <footer className="review-footer">
      <div className="review-progress">{pr.provider?.detailLoaded === false ? <span>Loading review…</span> : <><span className="review-progress-track" role="progressbar" aria-label="Files viewed" aria-valuemin={0} aria-valuemax={pr.files.length || 1} aria-valuenow={viewed}><span style={{ width: `${pr.files.length ? (viewed / pr.files.length) * 100 : 0}%` }} /></span><span><strong>{viewed}</strong> / {pr.files.length} files viewed</span></>}</div>
      <div className="review-footer-status">{pending > 0 ? <span className="review-pending-status"><Icon name="message-circle" size={13} />{pending} pending {pending === 1 ? 'comment' : 'comments'}</span> : review ? <span className={review.decision === 'approved' ? 'text-green' : 'muted'}><Icon name={review.decision === 'approved' ? 'check-circle' : 'message-circle'} size={13} />{decisionLabels[review.decision]}{review.headSha !== pr.headSha ? ' on earlier changes' : ''}</span> : <span className="muted"><Icon name="shield-check" size={13} />Review in progress</span>}</div>
      <div className="review-footer-actions"><label className="sr-only" htmlFor="review-decision">Review decision</label><select id="review-decision" className="review-decision-select" value={decision} disabled={!editable || locked} title={selfAuthored ? 'You cannot approve your own pull request' : 'Choose a review decision'} onChange={event => { setDecision(event.target.value as ReviewDecision); setError('') }}><option value="commented">Comment</option><option value="approved" disabled={selfAuthored || isDraft || !allowsReview(pr,'approve')}>Approve</option><option value="changes_requested" disabled={isDraft || !allowsReview(pr,'requestChanges')}>Request changes</option></select><button className="btn btn-primary" disabled={!editable || locked} onClick={() => setExpanded(!expanded)} aria-expanded={expanded} title={!editable ? pr.provider?.detailLoaded === false ? 'Wait for the pull request details to load' : `Reviews are unavailable for ${pr.status} pull requests` : undefined}><Icon name="check-circle" size={14} />{expanded ? 'Hide summary' : 'Finish review'}{pending > 0 && <span className="review-button-count">{pending}</span>}</button></div>
    </footer>
  </div>
}
