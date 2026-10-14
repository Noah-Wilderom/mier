import { useEffect, useRef, useState } from 'react'
import type { Commit } from './data'
import { services, type RepositoryBranch, type RepositorySnapshot } from './services'
import type { GitMutation } from './gitOperations'
import Modal from './Modal'
import { ComparisonPreview, useBranchComparison } from './BranchComparison'
import './GitOperations.css'

export function CherryPickControl({ commit, repository, busy, onOperate, onConflicts }: { commit:Commit; repository:RepositorySnapshot; busy:boolean; onOperate:(mutation:GitMutation)=>Promise<unknown>; onConflicts:()=>void }) {
  const [open,setOpen] = useState(false)
  return <><button className="btn" disabled={busy||!!repository.operation||repository.detached} onClick={()=>setOpen(true)}>Cherry-pick commit…</button>{open&&<CherryPickDialog commit={commit} repository={repository} busy={busy} onClose={()=>setOpen(false)} onOperate={onOperate} onConflicts={onConflicts}/>}</>
}
function CherryPickDialog({ commit, repository, busy, onClose, onOperate, onConflicts }: { commit:Commit; repository:RepositorySnapshot; busy:boolean; onClose:()=>void; onOperate:(mutation:GitMutation)=>Promise<unknown>; onConflicts:()=>void }) {
  const [target] = useState({head:repository.headSha??'',branch:repository.currentBranch})
  const [mainline,setMainline] = useState(0)
  const [pending,setPending] = useState(false)
  const [error,setError] = useState('')
  const guard = useRef(false)
  const merge = (commit.parents?.length??0)>1
  const changed = target.head!==repository.headSha||target.branch!==repository.currentBranch
  const locked = busy||pending
  const run=async()=>{
    if(locked||guard.current)return
    guard.current=true;setPending(true);setError('')
    try{const result=await onOperate({kind:'cherry-pick',request:{sha:commit.sha,expectedHead:target.head,expectedBranch:target.branch,mainline}}) as {operation?:unknown}|undefined;onClose();if(result?.operation)onConflicts()}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}finally{guard.current=false;setPending(false)}
  }
  return <Modal title="Cherry-pick commit" onClose={()=>{if(!locked)onClose()}}><div className="git-commit-dialog"><p>Apply this commit to <strong>{target.branch}</strong> as a new commit.</p><div className="git-selected-commit"><code>{commit.sha.slice(0,12)}</code><strong>{commit.subject}</strong></div>{merge&&<label>Mainline parent<select aria-label="Mainline parent" value={mainline} disabled={locked} onChange={e=>setMainline(Number(e.target.value))}><option value={0}>Choose a parent</option>{commit.parents?.map((parent,index)=><option key={parent} value={index+1}>Parent {index+1} · {parent.slice(0,12)}</option>)}</select><span className="muted">Changes are taken relative to this parent.</span></label>}{!!repository.changes?.length&&<p className="git-operation-error">Commit or stash your local changes first.</p>}{changed&&<p className="git-operation-error">Your checked-out branch changed. Close this dialog and preview again.</p>}{error&&<p className="git-operation-error" role="alert">{error}</p>}<p className="muted">If there are conflicts, the operation will pause in Local changes. You can resolve them or abort there.</p><div className="git-dialog-actions"><button className="btn" disabled={locked} onClick={onClose}>Cancel</button><button className="btn btn-primary" disabled={locked||changed||!!repository.changes?.length||!!repository.operation||(merge&&!mainline)} onClick={()=>void run()}>{pending?'Applying…':`Cherry-pick onto ${target.branch}`}</button></div></div></Modal>
}

export function MergeBranchDialog({ branch, repository, busy, onClose, onOperate, onConflicts }: { branch:RepositoryBranch; repository:RepositorySnapshot; busy:boolean; onClose:()=>void; onOperate:(mutation:GitMutation)=>Promise<unknown>; onConflicts:()=>void }) {
  const [target] = useState({head:repository.headSha??'',branch:repository.currentBranch,source:branch.commitSha})
  const comparison = useBranchComparison(repository,target.branch,branch.name)
  const [error,setError] = useState(''),[pending,setPending] = useState(false)
  const guard = useRef(false)
  const changed=target.head!==repository.headSha||target.branch!==repository.currentBranch||target.source!==repository.branches?.find(item=>!item.remote&&item.name===branch.name)?.commitSha
  const locked=busy||pending
  const run=async()=>{
    if(guard.current||locked)return
    guard.current=true;setPending(true);setError('')
    try{const result=await onOperate({kind:'merge',request:{branch:branch.name,expectedSource:target.source,expectedHead:target.head,expectedBranch:target.branch}}) as {operation?:unknown}|undefined;onClose();if(result?.operation)onConflicts()}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}finally{guard.current=false;setPending(false)}
  }
  return <Modal title="Merge local branch" onClose={()=>{if(!locked)onClose()}}><div className="git-commit-dialog"><p>Merge <strong>{branch.name}</strong> into <strong>{target.branch}</strong>.</p><p className="muted">A fast-forward is used when possible. Otherwise Git creates a merge commit and preserves both branches’ history.</p><ComparisonPreview {...comparison} valid={true}/>{!!repository.changes?.length&&<p className="git-operation-error">Commit or stash local changes before merging.</p>}{changed&&<p className="git-operation-error">A branch changed. Close this dialog and preview again.</p>}{error&&<p className="git-operation-error" role="alert">{error}</p>}<div className="git-dialog-actions"><button className="btn" disabled={locked} onClick={onClose}>Cancel</button><button className="btn btn-primary" disabled={locked||changed||!!repository.changes?.length||!!repository.operation||repository.detached||!comparison.files||!!comparison.error} onClick={()=>void run()}>{pending?'Merging…':`Merge into ${target.branch}`}</button></div></div></Modal>
}

export function useHistoryBranch(repository?:RepositorySnapshot) {
  const [branch,setBranch] = useState('')
  const [result,setResult] = useState<{key:string;commits:NonNullable<RepositorySnapshot['history']>}>()
  const [failure,setFailure] = useState<{key:string;message:string}>()
  const [retry,setRetry] = useState(0)
  const tip = repository?.branches?.find(item=>!item.remote&&item.name===branch)?.commitSha
  const key = JSON.stringify([repository?.root,branch,tip])
  useEffect(()=>{
    if(!repository||!branch||!tip)return
    let canceled=false
    setFailure(undefined)
    void services.repository.branchCommits(repository.root,branch,tip).then(commits=>{if(!canceled)setResult({key,commits})}).catch(issue=>{if(!canceled)setFailure({key,message:issue instanceof Error?issue.message:String(issue)})})
    return()=>{canceled=true}
  },[key,retry])
  return {branch,setBranch,commits:branch?(result?.key===key?result.commits:[]):undefined,loading:!!branch&&result?.key!==key&&failure?.key!==key,error:failure?.key===key?failure.message:'',retry:()=>setRetry(n=>n+1)}
}
