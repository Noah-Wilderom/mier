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
  return <section className="git-operation-banner" aria-label="Git operation in progress"><div className="git-operation-summary"><strong>{operationLabel(operation.kind)} in progress</strong><span>{operation.empty?'This commit has no remaining changes. Skip it or abort the operation.':operation.conflicts?`${operation.conflicts} conflicted ${operation.conflicts===1?'file':'files'} remaining`:'Conflicts resolved. Review staged changes before continuing.'}</span><button className="btn" disabled={locked} onClick={onResolve}>{operation.conflicts?'Resolve conflicts':'Review result'}</button><button className="btn btn-primary" disabled={locked||operation.conflicts>0||operation.empty} onClick={()=>void run('continue')}>Continue {operationLabel(operation.kind).toLowerCase()}</button>{operation.canSkip&&<button className="btn btn-ghost" disabled={locked} onClick={()=>setConfirm('skip')}>Skip commit…</button>}<button className="btn btn-ghost" disabled={locked} onClick={()=>setConfirm('abort')}>Abort…</button></div>
    {confirm&&<div className="git-confirm"><span>{confirm==='abort'?`Abort this ${operationLabel(operation.kind).toLowerCase()}? Git will restore the branch to before the operation; conflict resolution edits will be discarded.`:'Skip the current commit? Its changes and conflict resolutions will be discarded.'}</span><button className="btn" disabled={locked} onClick={()=>setConfirm(undefined)}>Keep working</button><button className="btn" disabled={locked} onClick={()=>void run(confirm)}>{confirm==='abort'?'Abort operation':'Skip this commit'}</button></div>}{error&&<div className="git-operation-error" role="alert">{error}</div>}
  </section>
}
