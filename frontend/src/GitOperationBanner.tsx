import { useRef, useState } from 'react'
import { operationLabel, type GitMutation, type GitOperation } from './gitOperations'
import './GitOperations.css'

export default function GitOperationBanner({ operation, busy, onOperate, onResolve }: { operation: GitOperation; busy: boolean; onOperate: (mutation: GitMutation)=>Promise<unknown>; onResolve: ()=>void }) {
  const [confirm,setConfirm] = useState<'abort'|'skip'>()
  const [error,setError] = useState('')
  const [pending,setPending] = useState(false)
  const guard = useRef(false)
  const run=async(action:'continue'|'abort'|'skip')=>{
    if(guard.current||busy)return
    guard.current=true;setPending(true);setError('')
    try{await onOperate({kind:action,token:operation.token});setConfirm(undefined)}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}finally{guard.current=false;setPending(false)}
  }
  const locked=busy||pending
  if(operation.kind==='bisect')return <BisectBanner operation={operation} busy={busy} onOperate={onOperate} onResolve={onResolve}/>
  return <section className="git-operation-banner" aria-label="Git operation in progress"><div className="git-operation-summary"><strong>{operationLabel(operation.kind)} in progress</strong><span>{operation.pausedForEdit?'Paused to edit a commit. Amend it in Changes, then continue.':operation.empty?'This commit has no remaining changes. Skip it or abort the operation.':operation.conflicts?`${operation.conflicts} conflicted ${operation.conflicts===1?'file':'files'} remaining`:'Conflicts resolved. Review staged changes before continuing.'}</span><button className="btn" disabled={locked} onClick={onResolve}>{operation.conflicts?'Resolve conflicts':'Review result'}</button><button className="btn btn-primary" disabled={locked||operation.conflicts>0||operation.empty} onClick={()=>void run('continue')}>Continue {operationLabel(operation.kind).toLowerCase()}</button>{operation.canSkip&&<button className="btn btn-ghost" disabled={locked} onClick={()=>setConfirm('skip')}>Skip commit…</button>}<button className="btn btn-ghost" disabled={locked} onClick={()=>setConfirm('abort')}>Abort…</button></div>
    {confirm&&<div className="git-confirm"><span>{confirm==='abort'?`Abort this ${operationLabel(operation.kind).toLowerCase()}? Git will restore the branch to before the operation; conflict resolution edits will be discarded.`:'Skip the current commit? Its changes and conflict resolutions will be discarded.'}</span><button className="btn" disabled={locked} onClick={()=>setConfirm(undefined)}>Keep working</button><button className="btn" disabled={locked} onClick={()=>void run(confirm)}>{confirm==='abort'?'Abort operation':'Skip this commit'}</button></div>}{error&&<div className="git-operation-error" role="alert">{error}</div>}
  </section>
}

export function BisectBanner({operation,busy,onOperate,onResolve}:{operation:GitOperation;busy:boolean;onOperate:(mutation:GitMutation)=>Promise<unknown>;onResolve?:()=>void}){
 const [pending,setPending]=useState(false),[error,setError]=useState(''),[confirm,setConfirm]=useState(false),guard=useRef(false)
 const finished=/# (?:first bad commit:|only skipped commits left to test)/.test(operation.message),locked=busy||pending
 const run=async(action:'good'|'bad'|'skip'|'checkout'|'reset')=>{if(locked||guard.current)return;guard.current=true;setPending(true);setError('');try{await onOperate({kind:'bisect',request:{action,good:'',bad:'',expectedHead:'',expectedBranch:'',token:operation.token}});setConfirm(false)}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}finally{guard.current=false;setPending(false)}}
 return <section className="git-operation-banner" aria-label="Bisect session"><div className="git-operation-summary"><strong>{finished?'Bisection complete':'Bisect in progress'}</strong><span>{finished?'Review the result in the bisect log, then return to your original branch.':`Test commit ${operation.sourceSha.slice(0,12)}, then mark its result.`}</span>{onResolve&&<button className="btn" disabled={locked} onClick={onResolve}>Inspect test commit</button>}{!finished&&<><button className="btn" disabled={locked} onClick={()=>void run('good')}>Mark good</button><button className="btn" disabled={locked} onClick={()=>void run('bad')}>Mark bad</button><button className="btn btn-ghost" disabled={locked} onClick={()=>void run('skip')}>Cannot test · skip</button><button className="btn btn-ghost" disabled={locked} onClick={()=>void run('checkout')}>Check out candidate</button></>}<button className="btn" disabled={locked} onClick={()=>setConfirm(true)}>End bisect…</button></div><details><summary>Bisect log</summary><pre className="git-output">{operation.message}</pre></details>{confirm&&<div className="git-confirm"><span>End this bisect session and return to {operation.currentLabel}? Test edits must be preserved first.</span><button className="btn" disabled={locked} onClick={()=>setConfirm(false)}>Keep testing</button><button className="btn" disabled={locked} onClick={()=>void run('reset')}>Return to original branch</button></div>}{error&&<p role="alert" className="git-operation-error">{error}</p>}</section>
}
