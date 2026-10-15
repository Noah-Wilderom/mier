import { useEffect, useRef, useState } from 'react'
import type { RepositoryBranch, RepositorySnapshot } from './services'
import { preferredRemote, pullUnavailableReason, type GitAction } from './gitWorkspace'
import { Icon } from './Icon'
import Modal from './Modal'
import BranchComparison from './BranchComparison'
import { MergeBranchDialog } from './GitCommitActions'
import type { GitMutation } from './gitOperations'
import './RepositoryWorkspace.css'
import RepositoryTools from './RepositoryTools'

type RunGit = (action: GitAction, argument?: string | string[], remote?: string) => Promise<void>

export default function RepositoryToolbar({ repository, busy, onGit, onRefresh, onOperate, onConflicts, onOpenRepository }: {
  repository: RepositorySnapshot; busy: boolean; onGit: RunGit; onRefresh: () => Promise<void>; onOperate?: (mutation:GitMutation)=>Promise<unknown>; onConflicts?:()=>void; onOpenRepository?:(path:string)=>void
}) {
  const [remote, setRemote] = useState(() => preferredRemote(repository))
  const [dialog, setDialog] = useState<'branches' | 'compare' | 'tools' | null>(null)
  const [mergeBranch, setMergeBranch] = useState<RepositoryBranch>()
  const [error, setError] = useState('')
  const [pending, setPending] = useState('')
  const guard = useRef(false)
  const mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  useEffect(() => setRemote(previous => preferredRemote(repository, previous)), [repository.remotes, repository.upstream])
  const locked = busy || !!pending
  const pullReason = pullUnavailableReason(repository, remote)
  const run = async (action: GitAction, argument?: string | string[]) => {
    if (guard.current || busy) return
    guard.current = true; setPending(action); setError('')
    try { await onGit(action, argument, remote) }
    catch (issue) { if (mounted.current) setError(issue instanceof Error ? issue.message : String(issue)); throw issue }
    finally { guard.current = false; if (mounted.current) setPending('') }
  }
  return <section className="repository-toolbar" aria-label="Repository Git actions">
    <div className="repository-toolbar-row">
      <button className="btn repository-branch-button" title="Manage branches" disabled={locked} onClick={event => { event.currentTarget.focus(); setDialog('branches') }}><Icon name="branch"/><span>{repository.detached ? `Detached ${repository.headSha?.slice(0, 7) ?? 'HEAD'}` : repository.currentBranch || 'New repository'}</span><Icon name="chevron-down" size={12}/></button>
      <label className="repository-remote"><span className="sr-only">Git remote</span><select aria-label="Git remote" disabled={locked || !repository.remotes?.length} value={remote} onChange={event => setRemote(event.target.value)}>{!remote && <option value="">{repository.remotes?.length ? 'Choose a remote' : 'No remotes'}</option>}{repository.remotes?.map(item => <option key={item.name} value={item.name}>{item.name}</option>)}</select></label>
      <span className="repository-tracking" title={repository.upstream || 'This branch has no upstream'}>{repository.upstream ? <><span aria-label={`${repository.ahead} commits ahead`}>↑ {repository.ahead}</span><span aria-label={`${repository.behind} commits behind`}>↓ {repository.behind}</span></> : 'Not published'}</span>
      <div className="repository-toolbar-spacer"/>
      <button className="btn" disabled={locked || !remote} title={remote ? `Fetch remote references from ${remote}` : 'Choose a remote first'} onClick={() => void run('fetch').catch(() => {})}><Icon name="refresh"/><span>{pending === 'fetch' ? 'Fetching…' : 'Fetch'}</span></button>
      <button className="btn" disabled={locked || !!pullReason} title={pullReason || `Fast-forward pull from ${remote}`} onClick={() => void run('pull').catch(() => {})}><Icon name="download"/><span>{pending === 'pull' ? 'Pulling…' : 'Pull'}</span></button>
      <button className="btn" disabled={locked || !remote || !repository.hasHead || repository.detached} title={!repository.hasHead ? 'Create the first commit before publishing' : repository.detached ? 'Check out a branch before pushing' : `Push ${repository.currentBranch} to ${remote}`} onClick={() => void run('push').catch(() => {})}><Icon name="upload"/><span>{pending === 'push' ? 'Pushing…' : repository.upstream ? 'Push' : 'Publish branch'}</span></button>
      <button className="btn" disabled={locked || !repository.hasHead} onClick={event => { event.currentTarget.focus(); setDialog('compare') }}><Icon name="files"/><span>Compare branches</span></button>
      <button className="icon-button" aria-label="Refresh repository" title="Refresh repository status" disabled={locked} onClick={() => { setError(''); void onRefresh() }}><Icon name="refresh" size={15}/></button>
      {onOperate&&<button className="btn" disabled={locked} onClick={()=>setDialog('tools')}>Repository tools…</button>}
    </div>
    {error && <div className="repository-operation-error" role="alert"><Icon name="alert-circle"/><span>{error}</span><button className="btn btn-ghost" disabled={locked} onClick={() => { setError(''); void onRefresh() }}>Refresh status</button></div>}
    {dialog === 'branches' && <Modal title="Manage branches" onClose={() => setDialog(null)}><BranchManager repository={repository} busy={locked} onGit={run} onMerge={onOperate?branch=>{setDialog(null);setMergeBranch(branch)}:undefined}/></Modal>}
    {dialog === 'compare' && <Modal title="Compare branches" onClose={() => setDialog(null)}><BranchComparison repository={repository}/></Modal>}
    {dialog === 'tools' && onOperate && <RepositoryTools repository={repository} busy={locked} onOperate={onOperate} onClose={()=>setDialog(null)} onPaused={onConflicts} onOpenRepository={onOpenRepository}/>}
    {mergeBranch&&onOperate&&<MergeBranchDialog branch={mergeBranch} repository={repository} busy={locked} onClose={()=>setMergeBranch(undefined)} onOperate={onOperate} onConflicts={()=>onConflicts?.()}/>}
  </section>
}

function BranchManager({ repository, busy, onGit, onMerge }: { repository: RepositorySnapshot; busy: boolean; onGit: RunGit; onMerge?:(branch:RepositoryBranch)=>void }) {
  const [query, setQuery] = useState('')
  const [name, setName] = useState('')
  const [checkout, setCheckout] = useState(true)
  const [edit, setEdit] = useState<{ action: 'deleteBranch' | 'renameBranch'; branch: RepositoryBranch } | null>(null)
  const [renamed, setRenamed] = useState('')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const guard = useRef(false)
  const locked = busy || pending
  const run = async (action: GitAction, argument: string | string[]) => {
    if (guard.current || busy) return
    guard.current = true; setPending(true); setError('')
    try { await onGit(action, argument); setEdit(null); if (action === 'createBranch' || action === 'createAndCheckoutBranch') setName('') }
    catch (issue) { setError(issue instanceof Error ? issue.message : String(issue)) }
    finally { guard.current = false; setPending(false) }
  }
  const branches = (repository.branches ?? []).filter(branch => branch.name.toLowerCase().includes(query.toLowerCase()))
  return <div className="branch-manager">
    <form className="branch-create" onSubmit={event => { event.preventDefault(); if (name.trim()) void run(checkout ? 'createAndCheckoutBranch' : 'createBranch', name.trim()) }}>
      <label>New branch name<input autoFocus required value={name} onChange={event => setName(event.target.value)} disabled={locked} placeholder="feature/review-workflow" autoComplete="off" spellCheck={false}/></label>
      <label className="checkbox-label"><input type="checkbox" checked={checkout} onChange={event => setCheckout(event.target.checked)} disabled={locked || !repository.hasHead}/>Switch to the new branch</label>
      <button className="btn btn-primary" disabled={locked || !name.trim() || (!repository.hasHead && !checkout)}><Icon name="plus"/>{pending ? 'Working…' : 'Create branch'}</button>
    </form>
    {error && <p className="form-error" role="alert">{error}</p>}
    {edit && <form className="branch-edit" onSubmit={event => { event.preventDefault(); void run(edit.action, edit.action === 'renameBranch' ? [edit.branch.name, renamed.trim()] : edit.branch.name) }}>
      {edit.action === 'renameBranch' ? <label>Rename {edit.branch.name}<input autoFocus required value={renamed} onChange={event => setRenamed(event.target.value)} disabled={locked}/></label> : <p>Delete the local branch <strong>{edit.branch.name}</strong>? The branch must be merged into your current checkout. The remote branch is kept.</p>}
      <div className="branch-edit-actions"><button type="button" className="btn" disabled={locked} onClick={() => setEdit(null)}>Cancel</button><button className="btn" disabled={locked || (edit.action === 'renameBranch' && (!renamed.trim() || renamed.trim() === edit.branch.name))}>{edit.action === 'renameBranch' ? 'Rename branch' : 'Delete local branch'}</button></div>
    </form>}
    <label className="branch-search"><span className="sr-only">Filter branches</span><input aria-label="Filter branches" value={query} onChange={event => setQuery(event.target.value)} placeholder="Filter local and remote branches…"/></label>
    <div className="branch-list">{branches.map(branch => <div className="branch-row" key={`${branch.remote}/${branch.name}`}>
      <Icon name="branch"/><div className="branch-description"><strong title={branch.name}>{branch.name}</strong><span>{branch.current ? 'Current branch' : branch.remote ? 'Remote branch' : branch.upstream || 'Local branch'}{branch.upstream && ` · ↑ ${branch.ahead} ↓ ${branch.behind}`}</span></div>
      <button className="btn" disabled={locked || branch.current || !!repository.operation} aria-label={`Switch to ${branch.name}`} onClick={() => void run('checkout', branch.name)}>{branch.current ? 'Current' : 'Switch'}</button>
      {onMerge&&!branch.remote&&!branch.current&&<button className="btn" disabled={locked||!!repository.operation||repository.detached} aria-label={`Merge ${branch.name} into ${repository.currentBranch}`} onClick={()=>onMerge(branch)}>Merge…</button>}
      {!branch.remote && <><button className="icon-button" disabled={locked} aria-label={`Rename ${branch.name}`} onClick={() => { setEdit({ action: 'renameBranch', branch }); setRenamed(branch.name) }}><Icon name="edit" size={14}/></button><button className="icon-button" disabled={locked || branch.current} aria-label={`Delete ${branch.name}`} onClick={() => setEdit({ action: 'deleteBranch', branch })}><Icon name="trash" size={14}/></button></>}
    </div>)}{!branches.length && <p className="branch-empty">{repository.hasHead ? 'No branches match this filter.' : 'Create your first commit to record this branch.'}</p>}</div>
  </div>
}
