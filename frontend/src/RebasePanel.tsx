import { useRef, useState } from 'react'
import { services, type RepositorySnapshot } from './services'
import type { GitMutation, GitOperationResult } from './gitOperations'
import type { RebasePreview, RebasePreviewRequest, RebaseStep } from './repositoryToolTypes'

export default function RebasePanel({repository,locked,onOperate,onPaused}:{repository:RepositorySnapshot;locked:boolean;onOperate:(mutation:GitMutation)=>Promise<unknown>;onPaused?:()=>void}){
  const [onto,setOnto]=useState(repository.branches?.find(branch=>!branch.current)?.commitSha??repository.headSha??'')
  const [upstream,setUpstream]=useState('')
  const [preview,setPreview]=useState<{value:RebasePreview;request:RebasePreviewRequest}>()
  const [steps,setSteps]=useState<RebaseStep[]>([])
  const [error,setError]=useState('')
  const [pending,setPending]=useState(false)
  const [complete,setComplete]=useState(false)
  const guard=useRef(false)
  const busy=locked||pending
  const changed=!!preview&&(preview.value.head!==repository.headSha||preview.value.branch!==repository.currentBranch||preview.request.onto!==onto||preview.request.upstream!==upstream)
  const load=async()=>{
    if(busy||guard.current)return
    guard.current=true;setPending(true);setError('');setComplete(false)
    const request={onto,upstream,expectedHead:repository.headSha??'',expectedBranch:repository.currentBranch}
    try{const value=await services.repository.previewRebase(repository.root,request);setPreview({value,request});setSteps((value.commits??[]).map(commit=>({sha:commit.sha,action:'pick',message:[commit.subject,commit.body].filter(Boolean).join('\n\n')})))}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}finally{guard.current=false;setPending(false)}
  }
  const run=async()=>{
    if(!preview||busy||guard.current||changed)return
    guard.current=true;setPending(true);setError('')
    try{const result=await onOperate({kind:'rebase',request:{preview:preview.request,token:preview.value.token,steps}}) as GitOperationResult|undefined;setPreview(undefined);setSteps([]);if(result?.operation){setComplete(false);onPaused?.()}else setComplete(true)}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}finally{guard.current=false;setPending(false)}
  }
  const move=(index:number,offset:number)=>setSteps(previous=>{const items=[...previous];[items[index],items[index+offset]]=[items[index+offset],items[index]];return items})
  const change=(index:number,value:Partial<RebaseStep>)=>setSteps(previous=>previous.map((step,i)=>i===index?{...step,...value}:step))
  let kept=0;let invalid=false
  for(const step of steps){if(step.action!=='drop'){if(kept===0&&(step.action==='squash'||step.action==='fixup'))invalid=true;kept++}if(step.action==='reword'&&!step.message.trim())invalid=true}
  return <div className="rebase-panel"><h3>Rebase & compose history</h3><p className="muted">Replay commits from the current branch onto another commit. Reordering, rewording, squash, fixup, drop, and edit are available for linear ranges up to 100 commits. A checkpoint preserves the original HEAD.</p><form className="repository-tool-form" onSubmit={e=>{e.preventDefault();void load()}}><label>Destination commit<input aria-label="Rebase destination commit" required value={onto} disabled={busy} onChange={e=>setOnto(e.target.value)} list="rebase-branch-tips" spellCheck={false}/><datalist id="rebase-branch-tips">{repository.branches?.map(branch=><option key={branch.name} value={branch.commitSha}>{branch.name}</option>)}</datalist></label><label>Base of commits to replay (optional)<input value={upstream} disabled={busy} onChange={e=>setUpstream(e.target.value)} placeholder="Automatic common ancestor" spellCheck={false}/></label><button className="btn" disabled={busy||repository.detached||!repository.hasHead||!!repository.operation}>Preview rebase</button></form>
    {preview&&!changed&&<><p>Replay {steps.length} commits after <code>{preview.value.upstream.slice(0,12)}</code> onto <code>{preview.value.onto.slice(0,12)}</code>, on <strong>{preview.value.branch}</strong>.</p>{!steps.length?<p className="muted">No commits to replay. Choose a different base or destination.</p>:<ol className="rebase-plan">{steps.map((step,index)=>{const commit=preview.value.commits.find(commit=>commit.sha===step.sha)!;return <li key={step.sha} className={step.action==='drop'?'rebase-dropped':''}><div className="rebase-step-heading"><select aria-label={`Action for ${commit.subject}`} value={step.action} disabled={busy} onChange={e=>change(index,{action:e.target.value as RebaseStep['action']})}>{['pick','reword','edit','squash','fixup','drop'].map(action=><option key={action}>{action}</option>)}</select><div><strong>{commit.subject}</strong><code>{step.sha.slice(0,12)}</code></div><button className="btn btn-ghost" aria-label={`Move ${commit.subject} earlier`} disabled={busy||index===0} onClick={()=>move(index,-1)}>↑</button><button className="btn btn-ghost" aria-label={`Move ${commit.subject} later`} disabled={busy||index===steps.length-1} onClick={()=>move(index,1)}>↓</button></div>{step.action==='reword'&&<label>New commit message<textarea aria-label={`Message for ${commit.subject}`} disabled={busy} value={step.message} onChange={e=>change(index,{message:e.target.value})}/></label>}{step.action==='edit'&&<p className="muted">Pause after this commit. Use Amend last commit in Changes, then Continue.</p>}</li>})}</ol>}<p className="muted">Published commits will receive new IDs. Coordinate with collaborators before rewriting shared history.</p>{invalid&&<p className="git-operation-error">The first kept commit must be pick, reword, or edit. Reworded commits need a message.</p>}<button className="btn btn-primary" disabled={busy||invalid||kept===0||!!repository.changes?.length||!!repository.operation} onClick={()=>void run()}>Rebase {preview.value.branch}</button></>}
    {changed&&<p className="git-operation-error" role="alert">The branch or destination changed. Preview the rebase again.</p>}{!!repository.changes?.length&&<p className="git-operation-error">Commit or stash local changes before applying the plan.</p>}{complete&&<p role="status">Rebase completed. The original HEAD is available in Activity & recovery.</p>}{error&&<p className="git-operation-error" role="alert">{error}</p>}
  </div>
}
