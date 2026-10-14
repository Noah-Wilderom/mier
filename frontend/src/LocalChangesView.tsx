import { useEffect, useMemo, useRef, useState, useSyncExternalStore, type CSSProperties } from 'react'
import { Icon } from './Icon'
import CodeIntelligence from './CodeIntelligence'
import type { ChangedFile } from './data'
import { codeReviewStyleVariables, DEFAULT_CODE_REVIEW_PREFERENCES, effectiveCodeTheme, getCodeAppearance, getCodeReviewPreferences, subscribeCodeAppearance, subscribeCodeReviewPreferences } from './codeReviewPreferences'
import { highlightDiff, syntaxLineKey, syntaxTokenStyle, type HighlightedDiff } from './syntaxHighlighter'
import './LocalChangesView.css'
import { commitDraftKey, readCommitDraft } from './workspaceDrafts'
import { usePersistentDraft } from './usePersistentDraft'
import FileChangeIcon from './FileChangeIcon'
import { fileChangeStatus } from './fileChangeStatus'

export type LocalFile = ChangedFile & { staged: boolean; unstaged?: boolean; conflict?: boolean; untracked?: boolean; indexStatus?: string; worktreeStatus?: string }
export interface LocalChangesViewProps {
  changes: LocalFile[]
  branch: string
  onChange: (changes: LocalFile[]) => void
  onCommit: (message: string) => void | Promise<void>
  onStage?: (paths: string[], stage: boolean) => Promise<void>
  loadDiff?: (path: string, staged: boolean) => Promise<ChangedFile>
  busy?: boolean
  root?: string
  headSha?: string
  host?: string
  owner?: string
  repo?: string
  onOpenSettings?: () => void
}

const belongs = (file: LocalFile, staged: boolean) => staged ? file.staged : (file.unstaged ?? !file.staged)
export default function LocalChangesView({ changes, branch, onChange, onCommit, onStage, loadDiff, busy = false, root, headSha, host, owner, repo, onOpenSettings }: LocalChangesViewProps) {
  const [selected, setSelected] = useState(changes[0]?.path)
  const [selectedStage, setSelectedStage] = useState(false)
  const [message, setMessage, draftError] = usePersistentDraft(commitDraftKey(root), readCommitDraft)
  const [loaded, setLoaded] = useState<{ key: string; file: ChangedFile } | null>(null)
  const [diffFailure, setDiffFailure] = useState<{ key: string; message: string } | null>(null)
  const [actionError, setActionError] = useState('')
  const [loading, setLoading] = useState(false)
  const [mutating, setMutating] = useState(false)
  const [reload, setReload] = useState(0)
  const mutationPending = useRef(false)
  const preferences = useSyncExternalStore(subscribeCodeReviewPreferences, getCodeReviewPreferences, () => DEFAULT_CODE_REVIEW_PREFERENCES)
  const appearance = useSyncExternalStore(subscribeCodeAppearance, getCodeAppearance, () => 'light' as const)
  const theme = effectiveCodeTheme(appearance, preferences)
  const [highlighted, setHighlighted] = useState<{ source: string; theme: string; code: HighlightedDiff } | null>(null)
  const status = changes.find(file => file.path === selected) ?? changes[0]
  const currentStage = status && !belongs(status, selectedStage) && belongs(status, !selectedStage) ? !selectedStage : selectedStage
  const requestKey = JSON.stringify([root, status?.path, currentStage, headSha])
  const file = loadDiff ? loaded?.key === requestKey ? loaded.file : undefined : status
  const diffError = diffFailure?.key === requestKey ? diffFailure.message : ''
  const waiting = !!loadDiff && !!status && !file && !diffError
  const locked = busy || mutating
  const staged = changes.filter(file => file.staged)
  const source = useMemo(() => file ? JSON.stringify([file.path, file.previousPath, file.lines]) : '', [file])
  const syntax = highlighted?.source === source && highlighted.theme === theme ? highlighted.code : undefined

  useEffect(() => {
    if (!loadDiff || !status) { setLoaded(null); setLoading(false); setDiffFailure(null); return }
    let canceled = false
    setLoading(true)
    setLoaded(previous => previous?.key === requestKey ? previous : null)
    setDiffFailure(null)
    void loadDiff(status.path, currentStage).then(diff => {
      if (!canceled) setLoaded({ key: requestKey, file: diff })
    }).catch(issue => {
      if (!canceled) setDiffFailure({ key: requestKey, message: issue instanceof Error ? issue.message : String(issue) })
    }).finally(() => { if (!canceled) setLoading(false) })
    return () => { canceled = true }
  }, [loadDiff, requestKey, changes, reload])

  useEffect(() => {
    let canceled = false
    if (file) void highlightDiff(file, theme).then(code => { if (!canceled) setHighlighted({ source, theme, code }) })
    return () => { canceled = true }
  }, [source, theme])

  const move = async (paths: string[], stage: boolean) => {
    if (busy || mutationPending.current || !paths.length) return
    mutationPending.current = true
    setMutating(true)
    setActionError('')
    try {
      if (onStage) await onStage(paths, stage)
      else onChange(changes.map(file => paths.includes(file.path) ? { ...file, staged: stage } : file))
    } catch (issue) { setActionError(issue instanceof Error ? issue.message : String(issue)) }
    finally { mutationPending.current = false; setMutating(false) }
  }

  const commit = async () => {
    if (!staged.length || !message.trim() || busy || mutationPending.current) return
    mutationPending.current = true
    setMutating(true)
    setActionError('')
    try { await onCommit(message); setMessage('') }
    catch (issue) { setActionError(issue instanceof Error ? issue.message : String(issue)) }
    finally { mutationPending.current = false; setMutating(false) }
  }

  const diff = <div className="local-diff-body">
    {status ? <>
      <div className="local-diff-heading"><FileChangeIcon file={status} layer={currentStage ? 'index' : 'working'}/><code>{status.path}</code><span className="muted">{currentStage ? 'Staged' : 'Working tree'}</span>{loading && file && <span className="local-diff-refresh" role="status">Refreshing…</span>}{file && !waiting && <><span className="text-green">+{file.additions}</span><span className="text-red">−{file.deletions}</span></>}</div>
      {status.conflict && <div className="live-error"><Icon name="alert-circle" /><span>This file has merge conflicts. Resolve its conflict markers in your editor, then stage it.</span></div>}
      {diffError && <div className="local-diff-error" role="alert"><Icon name="alert-circle" /><div><strong>Could not load this diff</strong><span>{diffError}</span></div><button className="btn" disabled={locked} onClick={() => setReload(value => value + 1)}>Retry</button></div>}
      {waiting ? <div className="local-diff-loading" role="status"><span>Loading diff…</span><div aria-hidden="true">{Array.from({ length: 9 }, (_, index) => <span key={index} style={{ width: [78, 60, 88, 47, 69][index % 5] + '%' }} />)}</div></div>
        : file?.binary ? <div className="empty-state"><Icon name="file" size={28} /><h2>Binary file changed</h2><span>There is no text diff for this file.</span></div>
        : file ? <div className="local-code-scroll"><div className="local-code">{file.lines.map((line, index) => {
          const side = line.kind === 'remove' ? 'old' : 'new'
          const number = side === 'old' ? line.oldLine : line.newLine
          const tokens = syntax?.[side].get(syntaxLineKey(line, side))
          return <div className={'local-code-line kind-' + line.kind} key={index}><span className="local-code-number" aria-hidden="true">{number ?? ''}</span><span className="local-code-marker" aria-hidden="true">{line.kind === 'add' ? '+' : line.kind === 'remove' ? '−' : ' '}</span><code className="review-code-text local-code-text" data-code-path={number !== undefined ? side === 'old' ? file.previousPath || file.path : file.path : undefined} data-code-line={number} data-code-side={number !== undefined ? side : undefined}>{tokens ? tokens.map((token, part) => <span key={part} style={syntaxTokenStyle(token)} data-syntax-type={token.type === 1 ? 'comment' : token.type === 2 ? 'string' : undefined}>{token.content}</span>) : line.text}</code></div>
        })}{file.truncated && <div className="live-error">This diff is too large to display in full.</div>}{!file.lines.length && <div className="empty-state"><Icon name="file" size={28} /><h2>No text changes</h2><span>There is no text diff in this selection.</span></div>}</div></div> : null}
    </> : <div className="empty-state"><Icon name="check-circle" size={32} /><h2>Working tree is clean</h2><span>No local changes to commit.</span></div>}
  </div>

  return <section className="local-workspace" style={{ ...codeReviewStyleVariables(preferences), '--review-code-foreground': syntax?.foreground, '--review-code-background': syntax?.background } as CSSProperties}>
    <div className="workspace-toolbar"><h1>Local changes</h1><code><Icon name="branch" size={14} />{branch}</code><span className="muted">{changes.length} changed files</span></div>
    {(actionError || draftError) && <div className="live-error local-action-error" role="alert"><Icon name="alert-circle" /><span>{actionError || draftError}</span></div>}
    <div className="local-columns"><div className="staging-pane">
      {([false, true] as const).map(stage => <section className="staging-group" aria-label={stage ? 'Staged changes' : 'Unstaged changes'} key={String(stage)}>
        <div className="staging-heading"><strong>{stage ? 'Staged changes' : 'Unstaged changes'}</strong><span className="count">{changes.filter(file => belongs(file, stage)).length}</span><button className="btn btn-ghost" disabled={locked || !changes.some(file => belongs(file, stage))} onClick={() => void move(changes.filter(file => belongs(file, stage)).map(file => file.path), !stage)}>{stage ? 'Unstage all' : 'Stage all'}</button></div>
        <div className="staging-files">{changes.filter(file => belongs(file, stage)).map(file => <div className={'staging-file' + (status?.path === file.path && currentStage === stage ? ' selected' : '')} key={file.path}>
          <button title={file.previousPath ? `${file.previousPath} → ${file.path}` : file.path} aria-label={`Open ${file.path}, ${fileChangeStatus(file, stage ? 'index' : 'working').label}, ${stage ? 'staged' : 'unstaged'}`} onClick={() => { setSelected(file.path); setSelectedStage(stage) }}><FileChangeIcon file={file} layer={stage ? 'index' : 'working'}/><span className="staging-file-text"><span className="staging-file-name">{file.path.split('/').pop()}</span>{file.path.includes('/') && <span className="staging-file-directory">{file.path.slice(0, file.path.lastIndexOf('/'))}</span>}</span></button>
          <button className="icon-button" disabled={locked} aria-label={(stage ? 'Unstage ' : 'Stage ') + file.path} title={stage ? 'Unstage file' : 'Stage file'} onClick={() => void move([file.path], !stage)}><Icon name={stage ? 'minus' : 'plus'} size={15}/></button>
        </div>)}{!changes.some(file => belongs(file, stage)) && <p className="staging-empty">{stage ? 'Stage files to include them in your commit.' : 'No unstaged changes.'}</p>}</div>
      </section>)}
      <form className="commit-form" onSubmit={event => { event.preventDefault(); void commit() }}><label>Commit message<textarea rows={4} placeholder="What changed?" value={message} onChange={event => setMessage(event.target.value)} disabled={locked} /></label><button className="btn btn-primary" disabled={locked || !staged.length || !message.trim()}><Icon name="git-commit" />{locked ? 'Working…' : 'Commit ' + (staged.length ? staged.length + ' staged files' : 'staged changes')}</button>{!onStage && <span className="prototype-note">Commits stay in the sample workspace.</span>}</form>
    </div><div className="local-diff">{loadDiff && root ? <CodeIntelligence root={root} host={host} owner={owner} repo={repo} headSha={headSha || ''} headRevision={currentStage ? 'index' : 'working'} baseRevision={currentStage ? headSha || '' : 'index'} revisionKey={JSON.stringify([requestKey, file?.rawPatch || source])} onOpenSettings={onOpenSettings}>{diff}</CodeIntelligence> : diff}</div></div>
  </section>
}
