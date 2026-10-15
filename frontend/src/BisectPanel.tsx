import { useState } from 'react'
import type { AdvancedPanelProps } from './AdvancedGitPanel'
import { BisectBanner } from './GitOperationBanner'

export default function BisectPanel({repository,busy,run}:AdvancedPanelProps){
 const [good,setGood]=useState(repository.branches?.find(branch=>!branch.current&&!branch.remote)?.commitSha??''),[bad,setBad]=useState(repository.headSha??'')
 return <><h3>Find a regression with bisect</h3><p className="muted">Choose a known working commit and a failing descendant. Git checks out a commit between them. Test it, then mark it good, bad, or untestable until the first failing commit is found. Preserve your test edits before moving to the next commit.</p>{repository.operation?.kind==='bisect'?<BisectBanner operation={repository.operation} busy={busy} onOperate={async mutation=>{if(!await run(mutation))throw new Error('Bisect could not advance. Check the message below.')}}/>:<form className="repository-tool-form" onSubmit={e=>{e.preventDefault();void run({kind:'bisect',request:{action:'start',good,bad,expectedHead:repository.headSha??'',expectedBranch:repository.currentBranch,token:''}})}}><label>Known good commit<input required value={good} onChange={e=>setGood(e.target.value)} disabled={busy} spellCheck={false}/></label><label>Known bad commit<input required value={bad} onChange={e=>setBad(e.target.value)} disabled={busy} spellCheck={false}/></label><button className="btn btn-primary" disabled={busy||!!repository.operation||!good||!bad||good===bad}>Start bisect</button></form>}</>
}
