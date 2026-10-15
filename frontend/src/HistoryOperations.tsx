import { useEffect, useRef, useState } from 'react'
import type { ChangedFile, Commit } from './data'
import type { GitMutation, GitOperationResult } from './gitOperations'
import type { RepositorySnapshot } from './services'
import { services } from './services'
import { mapRepositoryDiff } from './liveData'
import { ComparisonPreview } from './BranchComparison'
import Modal from './Modal'
import './GitOperations.css'

interface Props { repository:RepositorySnapshot;busy:boolean;onOperate:(mutation:GitMutation)=>Promise<unknown>;onConflicts:()=>void }
export function HistoryOperations({commit,...props}:Props&{commit:Commit}){
  const [intent,setIntent]=useState<{kind:'reset'|'revert';commit:Commit}>()
  return <><button className="btn" disabled={props.busy||!!props.repository.operation||props.repository.detached} onClick={()=>setIntent({kind:'revert',commit})}>Revert commit…</button><button className="btn" disabled={props.busy||!!props.repository.operation||props.repository.detached} onClick={()=>setIntent({kind:'reset',commit})}>Reset to commit…</button>{intent&&<ChangeHistoryDialog {...props} kind={intent.kind} commit={intent.commit} onClose={()=>setIntent(undefined)}/>}</>
}
export function ChangeHistoryDialog({repository,busy,onOperate,onConflicts,kind,commit,onClose}:Props&{kind:'reset'|'revert';commit:Pick<Commit,'sha'|'subject'|'parents'>;onClose:()=>void}){
  const [target]=useState({head:repository.headSha??'',branch:repository.currentBranch})
  const [mode,setMode]=useState<'soft'|'mixed'|'hard'>('soft')
  const [mainline,setMainline]=useState(0)
  const [confirmed,setConfirmed]=useState(false)
  const [files,setFiles]=useState<ChangedFile[]>()
  const [error,setError]=useState('')
  const [previewError,setPreviewError]=useState('')
  const [pending,setPending]=useState(false)
  const [reload,setReload]=useState(0)
  const guard=useRef(false)
  const changed=repository.headSha!==target.head||repository.currentBranch!==target.branch
  const locked=busy||pending
  const merge=(commit.parents?.length??0)>1
  useEffect(()=>{
    let canceled=false;setFiles(undefined);setPreviewError('')
    const preview=kind==='reset'?services.repository.compareExact(repository.root,target.head,commit.sha):services.repository.commitDiff(repository.root,commit.sha)
    void preview.then(items=>{if(!canceled)setFiles(items.map(mapRepositoryDiff))}).catch(issue=>{if(!canceled)setPreviewError(issue instanceof Error?issue.message:String(issue))})
    return()=>{canceled=true}
  },[repository.root,target.head,commit.sha,kind,reload])
  const run=async()=>{
    if(locked||guard.current||changed||!files)return
    guard.current=true;setPending(true);setError('')
    const expected={expectedHead:target.head,expectedBranch:target.branch}
    try{const result=await onOperate(kind==='reset'?{kind:'reset',request:{sha:commit.sha,mode,...expected}}:{kind:'revert',request:{sha:commit.sha,mainline,...expected}}) as GitOperationResult|undefined;onClose();if(result?.operation||(kind==='reset'&&mode!=='hard'))onConflicts()}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}finally{guard.current=false;setPending(false)}
  }
  return <Modal title={kind==='reset'?'Reset branch':'Revert commit'} onClose={()=>{if(!locked)onClose()}}><div className="git-commit-dialog"><p>{kind==='reset'?'Move':'Add a commit undoing this change on'} <strong>{target.branch}</strong>{kind==='reset'?' to this commit.':'.'}</p><div className="git-selected-commit"><code>{commit.sha.slice(0,12)}</code><strong>{commit.subject}</strong></div>{kind==='reset'?<label>Reset mode<select value={mode} disabled={locked} onChange={e=>{setMode(e.target.value as typeof mode);setConfirmed(false)}}><option value="soft">Soft — keep changes staged</option><option value="mixed">Mixed — keep changes unstaged</option><option value="hard">Hard — replace tracked files</option></select></label>:merge&&<label>Mainline parent<select value={mainline} disabled={locked} onChange={e=>setMainline(Number(e.target.value))}><option value={0}>Choose a parent</option>{commit.parents?.map((sha,index)=><option key={sha} value={index+1}>Parent {index+1} · {sha.slice(0,12)}</option>)}</select></label>}<p className="muted">A checkpoint keeps the current HEAD for recovery. Commit or stash local changes first.</p>{kind==='revert'&&<p className="muted">The preview shows the selected commit’s changes; reverting applies their inverse.</p>}<ComparisonPreview files={files??null} error={previewError} loading={!files&&!previewError} retry={()=>setReload(n=>n+1)} valid/>{kind==='reset'&&mode==='hard'&&<label className="git-check"><input type="checkbox" checked={confirmed} disabled={locked} onChange={e=>setConfirmed(e.target.checked)}/>Replace tracked files with this commit</label>}{changed&&<p className="git-operation-error" role="alert">The checked-out branch changed. Close this dialog and preview again.</p>}{!!repository.changes?.length&&<p className="git-operation-error">Commit or stash your local changes first.</p>}{error&&<p role="alert" className="git-operation-error">{error}</p>}<div className="git-dialog-actions"><button className="btn" disabled={locked} onClick={onClose}>Cancel</button><button className="btn btn-primary" disabled={locked||changed||!files||!!repository.changes?.length||!!repository.operation||(kind==='reset'&&mode==='hard'&&!confirmed)||(kind==='revert'&&merge&&!mainline)} onClick={()=>void run()}>{pending?'Applying…':kind==='reset'?'Reset branch':'Revert commit'}</button></div></div></Modal>
}

export function CherryPickSelection({commits,onClear,...props}:Props&{commits:Commit[];onClear:()=>void}){
  const [selected,setSelected]=useState<Commit[]>()
  return <><button className="btn" disabled={!commits.length||props.busy||!!props.repository.operation||props.repository.detached} onClick={()=>setSelected([...commits].reverse())}>Cherry-pick selected ({commits.length})…</button>{!!commits.length&&<button className="btn btn-ghost" onClick={onClear}>Clear selection</button>}{selected&&<CherryPickSelectionDialog {...props} initial={selected} onClose={()=>setSelected(undefined)} onDone={onClear}/>}</>
}
function CherryPickSelectionDialog({repository,busy,onOperate,onConflicts,initial,onClose,onDone}:Props&{initial:Commit[];onClose:()=>void;onDone:()=>void}){
  const [commits,setCommits]=useState(initial)
  const [target]=useState({head:repository.headSha??'',branch:repository.currentBranch})
  const [error,setError]=useState(''),[pending,setPending]=useState(false)
  const guard=useRef(false)
  const locked=busy||pending,changed=repository.headSha!==target.head||repository.currentBranch!==target.branch
  const run=async()=>{if(guard.current||locked||changed||!commits.length)return;guard.current=true;setPending(true);setError('');try{const result=await onOperate({kind:'cherry-pick-many',request:{shas:commits.map(commit=>commit.sha),expectedHead:target.head,expectedBranch:target.branch}}) as GitOperationResult|undefined;onDone();onClose();if(result?.operation)onConflicts()}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}finally{guard.current=false;setPending(false)}}
  const move=(index:number,offset:number)=>setCommits(items=>{const result=[...items];[result[index],result[index+offset]]=[result[index+offset],result[index]];return result})
  const hasMerge=commits.some(commit=>(commit.parents?.length??0)>1)
  return <Modal title="Cherry-pick selected commits" onClose={()=>{if(!locked)onClose()}}><div className="git-commit-dialog"><p>Apply these commits to <strong>{target.branch}</strong> in this order. A checkpoint keeps the starting HEAD.</p><ol className="cherry-pick-order">{commits.map((commit,index)=><li key={commit.sha}><div><strong>{commit.subject}</strong><code>{commit.sha.slice(0,12)}</code></div><button className="btn btn-ghost" disabled={locked||index===0} aria-label={`Move ${commit.subject} earlier`} onClick={()=>move(index,-1)}>↑</button><button className="btn btn-ghost" disabled={locked||index===commits.length-1} aria-label={`Move ${commit.subject} later`} onClick={()=>move(index,1)}>↓</button><button className="btn btn-ghost" disabled={locked} aria-label={`Remove ${commit.subject} from selection`} onClick={()=>setCommits(items=>items.filter(item=>item.sha!==commit.sha))}>Remove</button></li>)}</ol>{hasMerge&&<p className="git-operation-error">Cherry-pick merge commits individually to choose a mainline parent.</p>}{changed&&<p className="git-operation-error">The destination branch changed. Close this dialog and preview again.</p>}{!!repository.changes?.length&&<p className="git-operation-error">Commit or stash local changes first.</p>}{error&&<p role="alert" className="git-operation-error">{error}</p>}<div className="git-dialog-actions"><button className="btn" disabled={locked} onClick={onClose}>Cancel</button><button className="btn btn-primary" disabled={locked||changed||hasMerge||!commits.length||!!repository.changes?.length||!!repository.operation} onClick={()=>void run()}>{pending?'Applying…':`Cherry-pick ${commits.length} commits`}</button></div></div></Modal>
}
