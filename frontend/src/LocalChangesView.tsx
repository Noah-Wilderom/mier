import { lazy, Suspense, Fragment, useEffect, useMemo, useRef, useState, useSyncExternalStore, type CSSProperties } from 'react'
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
import ConflictEditor from './ConflictEditor'
import type { GitMutation, GitOperation } from './gitOperations'
import LocalGitTools, { DiscardDialog } from './LocalGitTools'
import Modal from './Modal'

const AIActionDialog = lazy(async () => ({default:(await import('./AIActionPanel')).AIActionDialog}))

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
  headMessage?: string
  host?: string
  owner?: string
  repo?: string
  onOpenSettings?: () => void
  operation?: GitOperation
  onOperate?: (mutation:GitMutation)=>Promise<unknown>
}

const belongs = (file: LocalFile, staged: boolean) => staged ? file.staged && !file.conflict : file.conflict || (file.unstaged ?? !file.staged)
export default function LocalChangesView({ changes, branch, onChange, onCommit, onStage, loadDiff, busy = false, root, headSha, headMessage, host, owner, repo, onOpenSettings, operation, onOperate }: LocalChangesViewProps) {
  const [aiDraft, setAIDraft] = useState<{head:string}>()
  const [selected, setSelected] = useState(changes[0]?.path)
  const [selectedStage, setSelectedStage] = useState(false)
  const [filter,setFilter] = useState('')
  const [message, setMessage, draftError] = usePersistentDraft(commitDraftKey(root), readCommitDraft)
  const [loaded, setLoaded] = useState<{ key: string; file: ChangedFile } | null>(null)
  const [diffFailure, setDiffFailure] = useState<{ key: string; message: string } | null>(null)
  const [actionError, setActionError] = useState('')
  const [loading, setLoading] = useState(false)
  const [mutating, setMutating] = useState(false)
  const [reload, setReload] = useState(0)
  const [selection, setSelection] = useState<{ hunk:number; rows:number[] }>({ hunk:-1, rows:[] })
  const [discard, setDiscard] = useState<{ path:string; hunk?:number; lines?:number[]; expectedPatch?:string }>()
  const [amend, setAmend] = useState(false)
  const [confirmAmend, setConfirmAmend] = useState<{ head:string; message:string; operationToken?:string }>()
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
  const staged = changes.filter(file => file.staged && !file.conflict)
  const stageable = (stage:boolean) => changes.filter(file=>belongs(file,stage)&&!file.conflict)
  const hunkEligible = !!onOperate && !!file?.rawPatch && !file.binary && !file.truncated && !status?.conflict && !file.previousPath && (currentStage?status?.indexStatus:status?.worktreeStatus)==='M'
  const source = useMemo(() => file ? JSON.stringify([file.path, file.previousPath, file.lines]) : '', [file])
  const syntax = highlighted?.source === source && highlighted.theme === theme ? highlighted.code : undefined
  useEffect(() => { setSelection({hunk:-1,rows:[]}) }, [requestKey, file?.rawPatch])
  const hunkStarts = useMemo(() => {
    const starts = new Map<number,number>()
    file?.lines.forEach((line,index) => { if(line.hunk!==undefined&&!starts.has(line.hunk)) starts.set(line.hunk,index) })
    return starts
  }, [file])

  useEffect(() => {
    if (!loadDiff || !status || (status.conflict && onOperate)) { setLoaded(null); setLoading(false); setDiffFailure(null); return }
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
  useEffect(()=>{if(operation?.conflicts){const first=changes.find(file=>file.conflict);if(first){setSelected(first.path);setSelectedStage(false)}}},[operation?.token])

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
    if ((!staged.length&&!amend) || !message.trim() || busy || mutationPending.current || (operation&&!(amend&&operation.pausedForEdit)) || changes.some(file=>file.conflict)) return
    if (amend && onOperate && headSha) { setConfirmAmend({head:headSha,message,operationToken:operation?.pausedForEdit?operation.token:undefined}); return }
    mutationPending.current = true
    setMutating(true)
    setActionError('')
    try { await onCommit(message); setMessage('') }
    catch (issue) { setActionError(issue instanceof Error ? issue.message : String(issue)) }
    finally { mutationPending.current = false; setMutating(false) }
  }

  const stageHunk = async(hunk:number, lines?:number[]) => {
    if(!onOperate||!file||locked||mutationPending.current)return
    mutationPending.current=true;setMutating(true);setActionError('')
    try{await onOperate({kind:'hunk',request:{path:file.path,staged:currentStage,hunk,expectedPatch:file.rawPatch??'',lines}});setReload(n=>n+1)}catch(issue){setActionError(issue instanceof Error?issue.message:String(issue))}finally{mutationPending.current=false;setMutating(false)}
  }

  const amendCommit = async () => {
    if (!confirmAmend || !onOperate || locked || mutationPending.current) return
    mutationPending.current=true;setMutating(true);setActionError('')
    try { await onOperate({kind:'amend',request:{message:confirmAmend.message,expectedHead:confirmAmend.head,operationToken:confirmAmend.operationToken}});setMessage('');setAmend(false);setConfirmAmend(undefined) }
    catch(issue){setActionError(issue instanceof Error?issue.message:String(issue));setConfirmAmend(undefined)}
    finally {mutationPending.current=false;setMutating(false)}
  }

  const ignore = async (path:string) => {
    if (!onOperate || locked || mutationPending.current) return
    mutationPending.current=true;setMutating(true);setActionError('')
    try {await onOperate({kind:'ignore',path})}catch(issue){setActionError(issue instanceof Error?issue.message:String(issue))}finally{mutationPending.current=false;setMutating(false)}
  }

  const diff = <div className="local-diff-body">
    {status ? <>
      <div className="local-diff-heading"><FileChangeIcon file={status} layer={currentStage ? 'index' : 'working'}/><code>{status.path}</code><span className="muted">{currentStage ? 'Staged' : 'Working tree'}</span>{root&&onOperate&&!currentStage&&!status.conflict&&<><button className="btn btn-ghost" disabled={locked||!!operation} onClick={()=>setDiscard({path:status.path})}>Discard file…</button>{status.untracked&&<button className="btn btn-ghost" disabled={locked||!!operation} onClick={()=>void ignore(status.path)}>Ignore file</button>}</>}{loading && file && <span className="local-diff-refresh" role="status">Refreshing…</span>}{file && !waiting && <><span className="text-green">+{file.additions}</span><span className="text-red">−{file.deletions}</span></>}</div>
      {status.conflict && <div className="live-error"><Icon name="alert-circle" /><span>This file has merge conflicts. Resolve its conflict markers in your editor, then stage it.</span></div>}
      {diffError && <div className="local-diff-error" role="alert"><Icon name="alert-circle" /><div><strong>Could not load this diff</strong><span>{diffError}</span></div><button className="btn" disabled={locked} onClick={() => setReload(value => value + 1)}>Retry</button></div>}
      {waiting ? <div className="local-diff-loading" role="status"><span>Loading diff…</span><div aria-hidden="true">{Array.from({ length: 9 }, (_, index) => <span key={index} style={{ width: [78, 60, 88, 47, 69][index % 5] + '%' }} />)}</div></div>
        : file?.binary ? <div className="empty-state"><Icon name="file" size={28} /><h2>Binary file changed</h2><span>There is no text diff for this file.</span></div>
        : file ? <div className="local-code-scroll"><div className="local-code">{file.lines.map((line, index) => {
          const side = line.kind === 'remove' ? 'old' : 'new'
          const number = side === 'old' ? line.oldLine : line.newLine
          const tokens = syntax?.[side].get(syntaxLineKey(line, side))
          return <Fragment key={index}>{hunkEligible&&line.hunk!==undefined&&(index===0||file.lines[index-1].hunk!==line.hunk)&&<div className="local-hunk-heading"><span>Section {line.hunk+1}</span>{selection.hunk===line.hunk&&selection.rows.length>0&&<><button className="btn btn-ghost" disabled={locked||loading} onClick={()=>void stageHunk(line.hunk!,selection.rows)}>{currentStage?'Unstage':'Stage'} {selection.rows.length} selected lines</button>{!currentStage&&root&&<button className="btn btn-ghost" disabled={locked||loading||!!operation} onClick={()=>setDiscard({path:file.path,hunk:line.hunk!,lines:selection.rows,expectedPatch:file.rawPatch})}>Discard selected…</button>}</>}<button className="btn btn-ghost" disabled={locked||loading} onClick={()=>void stageHunk(line.hunk!)}>{currentStage?'Unstage section':'Stage section'}</button>{!currentStage&&root&&<button className="btn btn-ghost" disabled={locked||loading||!!operation} onClick={()=>setDiscard({path:file.path,hunk:line.hunk!,expectedPatch:file.rawPatch})}>Discard section…</button>}</div>}<div className={'local-code-line kind-' + line.kind}>{hunkEligible&&<span className="local-line-select">{line.kind!=='context'&&line.hunk!==undefined&&<input type="checkbox" aria-label={`Select ${line.kind==='remove'?'removed':'added'} line ${number}`} disabled={locked||loading} checked={selection.hunk===line.hunk&&selection.rows.includes(index-(hunkStarts.get(line.hunk)??0))} onChange={event=>{const row=index-(hunkStarts.get(line.hunk!)??0);setSelection(previous=>({hunk:line.hunk!,rows:event.target.checked?[...(previous.hunk===line.hunk?previous.rows:[]),row]:previous.rows.filter(item=>item!==row)}))}}/>}</span>}<span className="local-code-number" aria-hidden="true">{number ?? ''}</span><span className="local-code-marker" aria-hidden="true">{line.kind === 'add' ? '+' : line.kind === 'remove' ? '−' : ' '}</span><code className="review-code-text local-code-text" data-code-path={number !== undefined ? side === 'old' ? file.previousPath || file.path : file.path : undefined} data-code-line={number} data-code-side={number !== undefined ? side : undefined}>{tokens ? tokens.map((token, part) => <span key={part} style={syntaxTokenStyle(token)} data-syntax-type={token.type === 1 ? 'comment' : token.type === 2 ? 'string' : undefined}>{token.content}</span>) : line.text}</code></div></Fragment>
        })}{file.truncated && <div className="live-error">This diff is too large to display in full.</div>}{!file.lines.length && <div className="empty-state"><Icon name="file" size={28} /><h2>No text changes</h2><span>There is no text diff in this selection.</span></div>}</div></div> : null}
    </> : <div className="empty-state"><Icon name="check-circle" size={32} /><h2>Working tree is clean</h2><span>No local changes to commit.</span></div>}
  </div>

  return <section className="local-workspace" style={{ ...codeReviewStyleVariables(preferences), '--review-code-foreground': syntax?.foreground, '--review-code-background': syntax?.background } as CSSProperties}>
    <div className="workspace-toolbar"><h1>Local changes</h1><code><Icon name="branch" size={14} />{branch}</code><span className="muted">{changes.length} changed files</span>{root&&onOperate&&<LocalGitTools root={root} headSha={headSha??''} busy={locked} disabled={!!operation||changes.some(item=>item.conflict)} onOperate={onOperate}/>}</div>
    {(actionError || draftError) && <div className="live-error local-action-error" role="alert"><Icon name="alert-circle" /><span>{actionError || draftError}</span></div>}
    <div className="local-columns"><div className="staging-pane">
      <div className="local-file-filter"><input aria-label="Filter changed files" placeholder="Filter files…" type="search" value={filter} onChange={e=>setFilter(e.target.value)}/></div>
      {([false, true] as const).map(stage => <section className="staging-group" aria-label={stage ? 'Staged changes' : 'Unstaged changes'} key={String(stage)}>
        <div className="staging-heading"><strong>{stage ? 'Staged changes' : 'Unstaged changes'}</strong><span className="count">{changes.filter(file => belongs(file, stage)).length}</span><button className="btn btn-ghost" disabled={locked || !stageable(stage).length} onClick={() => void move(stageable(stage).map(file => file.path), !stage)}>{stage ? 'Unstage all' : 'Stage all'}</button></div>
        <div className="staging-files">{changes.filter(file => belongs(file, stage)&&file.path.toLowerCase().includes(filter.toLowerCase())).map(file => <div className={'staging-file' + (status?.path === file.path && currentStage === stage ? ' selected' : '')} key={file.path}>
          <button title={file.previousPath ? `${file.previousPath} → ${file.path}` : file.path} aria-label={`Open ${file.path}, ${fileChangeStatus(file, stage ? 'index' : 'working').label}, ${stage ? 'staged' : 'unstaged'}`} onClick={() => { setSelected(file.path); setSelectedStage(stage) }}><FileChangeIcon file={file} layer={stage ? 'index' : 'working'}/><span className="staging-file-text"><span className="staging-file-name">{file.path.split('/').pop()}</span>{file.path.includes('/') && <span className="staging-file-directory">{file.path.slice(0, file.path.lastIndexOf('/'))}</span>}</span></button>
          <button className="icon-button" disabled={locked||!!file.conflict} aria-label={(stage ? 'Unstage ' : 'Stage ') + file.path} title={file.conflict?'Resolve the conflict first':stage ? 'Unstage file' : 'Stage file'} onClick={() => void move([file.path], !stage)}><Icon name={stage ? 'minus' : 'plus'} size={15}/></button>
        </div>)}{!changes.some(file => belongs(file, stage)) && <p className="staging-empty">{stage ? 'Stage files to include them in your commit.' : 'No unstaged changes.'}</p>}</div>
      </section>)}
      {discard&&root&&onOperate&&<DiscardDialog root={root} {...discard} busy={locked} onOperate={onOperate} onClose={()=>{setDiscard(undefined);setReload(n=>n+1)}}/>}
      {confirmAmend&&<Modal title="Amend last commit" onClose={()=>{if(!locked)setConfirmAmend(undefined)}}><div className="local-git-dialog"><p>Replace commit <code>{confirmAmend.head.slice(0,12)}</code> with the staged changes and this message?</p><p className="muted">Amending changes the commit ID. Coordinate with others before rewriting a published commit.</p><pre className="git-patch-preview">{confirmAmend.message}</pre><div className="git-tool-actions"><button className="btn" disabled={locked} onClick={()=>setConfirmAmend(undefined)}>Cancel</button><button className="btn btn-primary" disabled={locked||headSha!==confirmAmend.head} onClick={()=>void amendCommit()}>Amend commit</button></div></div></Modal>}
      {aiDraft&&root&&<Suspense fallback={<p role="status">Loading AI drafting…</p>}><AIActionDialog scope={{root,kind:'commit',expectedHead:aiDraft.head}} onClose={()=>setAIDraft(undefined)} disabled={locked} onOpenSettings={onOpenSettings} applyLabel="Use commit draft" onApply={proposal=>{setMessage(proposal.text??'');setAIDraft(undefined)}}/></Suspense>}
      <form className="commit-form" onSubmit={event => { event.preventDefault(); void commit() }}><div className="commit-type"><span>Commit type</span><select aria-label="Conventional commit type" value="" disabled={locked} onChange={e=>{if(e.target.value)setMessage(e.target.value+': '+message.replace(/^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([^)]*\))?!?:\s*/,''))}}><option value="">Optional…</option>{['feat','fix','docs','refactor','perf','test','build','ci','chore','revert'].map(type=><option key={type} value={type}>{type}</option>)}</select></div>{root&&onStage&&<button type="button" className="btn" disabled={locked||!staged.length||!!operation||changes.some(file=>file.conflict)} onClick={()=>setAIDraft({head:headSha??''})}>Draft with AI</button>}<label>Commit message<textarea aria-label="Commit message" rows={4} placeholder="What changed?" value={message} onChange={event => setMessage(event.target.value)} disabled={locked} /></label>{onOperate&&headSha&&<label className="commit-options"><input type="checkbox" checked={amend} disabled={locked||(!!operation&&!operation.pausedForEdit)} onChange={e=>{setAmend(e.target.checked);if(e.target.checked&&!message.trim()&&headMessage)setMessage(headMessage)}}/>Amend last commit</label>}<button className="btn btn-primary" disabled={locked || (!staged.length&&!amend) || !message.trim()||(!!operation&&!(amend&&operation.pausedForEdit))||changes.some(file=>file.conflict)}><Icon name="git-commit" />{locked ? 'Working…' : operation&&!(amend&&operation.pausedForEdit)?'Use Continue above':amend?'Amend last commit…':'Commit ' + (staged.length ? staged.length + ' staged files' : 'staged changes')}</button>{!onStage && <span className="prototype-note">Commits stay in the sample workspace.</span>}</form>
    </div><div className="local-diff">{status?.conflict&&root&&onOperate?<ConflictEditor key={root+status.path} root={root} path={status.path} headSha={headSha} operationToken={operation?.token} busy={locked} onOperate={onOperate}/>:loadDiff && root ? <CodeIntelligence root={root} host={host} owner={owner} repo={repo} headSha={headSha || ''} headRevision={currentStage ? 'index' : 'working'} baseRevision={currentStage ? headSha || '' : 'index'} revisionKey={JSON.stringify([requestKey, file?.rawPatch || source])} onOpenSettings={onOpenSettings}>{diff}</CodeIntelligence> : diff}</div></div>
  </section>
}
