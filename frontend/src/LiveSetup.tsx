import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Icon } from './Icon'
import type { useLiveWorkspace } from './useLiveWorkspace'
import { services } from './services'
import {Avatar, ActorName} from './Avatar'
import { ComparisonPreview, useBranchComparison } from './BranchComparison'
import { pullRequestDraftKey, readPullRequestDraft, type PullRequestDraft } from './workspaceDrafts'
import { usePersistentDraft } from './usePersistentDraft'
type Live = ReturnType<typeof useLiveWorkspace>
export function RepositoryWelcome({live,onOpen,onDemo}:{live:Live;onOpen:()=>void;onDemo:()=>void}) {
  const [cloning,setCloning]=useState(false),[cloneBusy,setCloneBusy]=useState(false),[url,setURL]=useState(''),[directory,setDirectory]=useState(''),[error,setError]=useState('')
  const clone=async(event:FormEvent)=>{event.preventDefault();if(!url.trim()||!directory.trim())return;setCloneBusy(true);try{setError('');const repository=await services.repository.clone(url.trim(),directory.trim());await live.openRepository(repository.root)}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}finally{setCloneBusy(false)}}
  return <section className="repository-welcome"><div className="welcome-symbol"><Icon name="branch" size={36}/></div><h1>Open your code. Start a review.</h1><p>M.I.E.R. brings your repository, pull requests, and conversations into one workspace.</p><div className="welcome-actions"><button className="btn btn-primary" disabled={live.busy||cloneBusy} onClick={onOpen}><Icon name="folder"/>Open repository</button><button className="btn" disabled={live.busy||cloneBusy} onClick={()=>setCloning(v=>!v)}><Icon name="download"/>Clone repository</button></div>{cloning&&<form className="clone-form" onSubmit={clone}><label>Repository URL<input required value={url} onChange={e=>setURL(e.target.value)} placeholder="https://github.com/owner/repository.git" autoComplete="off" disabled={cloneBusy}/></label><label>Destination directory<input required value={directory} onChange={e=>setDirectory(e.target.value)} placeholder="Absolute path for the new repository" disabled={cloneBusy}/></label><div><button className="btn" type="button" disabled={cloneBusy} onClick={()=>setCloning(false)}>Cancel</button><button className="btn btn-primary" disabled={cloneBusy||!url.trim()||!directory.trim()}>{cloneBusy?'Cloning…':'Clone repository'}</button></div>{error&&<p className="form-error" role="alert">{error}</p>}</form>}{live.recent.length>0&&<div className="recent-repositories"><h2>Recent repositories</h2>{live.recent.map(path=><button key={path} onClick={()=>void live.openRepository(path).catch(()=>{})}><Icon name="folder"/><div><strong>{path.split('/').pop()}</strong><span>{path}</span></div><Icon name="arrow-right"/></button>)}</div>}{live.error&&<p className="form-error" role="alert">{live.error}</p>}<div className="welcome-note"><Icon name="shield"/><span>GitHub tokens stay in your OS keychain.</span></div><button className="btn btn-ghost" onClick={onDemo}>Explore the demo workspace</button></section>
}
export function AccountSettings({live}:{live:Live}) {
  const [host,setHost]=useState(live.identity?.host||live.account?.host||'github.com'),[token,setToken]=useState(''),[showToken,setShowToken]=useState(false),[saving,setSaving]=useState(false),[error,setError]=useState(''),[confirmRemove,setConfirmRemove]=useState<string|null>(null)
  const pending=useRef(false)
  useEffect(()=>{if(live.identity?.host){setHost(live.identity.host);setConfirmRemove(null);setToken('')}},[live.identity?.host])
  const connect=async(event?:FormEvent,withToken=false)=>{event?.preventDefault();if(pending.current)return;pending.current=true;setSaving(true);setError('');try{await live.connect(host.trim(),withToken?token:undefined);setToken('');setShowToken(false)}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}finally{pending.current=false;setSaving(false)}}
  const remove=async()=>{if(!confirmRemove||pending.current)return;const accountHost=confirmRemove;pending.current=true;setSaving(true);setError('');try{await services.credentials.deleteToken(accountHost);setConfirmRemove(null);await live.connect(accountHost)}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}finally{pending.current=false;setSaving(false)}}
  return <div className="account-settings"><h2>GitHub accounts</h2><p>Use your existing GitHub CLI login, or save a token in the OS keychain. GitHub Enterprise uses its own hostname.</p>{live.account?.authenticated&&<div className="connected-account"><Avatar user={live.account.login} size={34}/><div><strong><ActorName user={live.account.login}/></strong><span>{live.account.host} · {live.account.source==='gh'?'GitHub CLI':'OS keychain'}</span></div>{live.account.source==='saved'&&<button className="btn" disabled={saving} onClick={()=>setConfirmRemove(live.account?.host??null)}>Remove saved token</button>}</div>}<label>Host<input value={host} onChange={e=>setHost(e.target.value)} placeholder="github.com or github.your-company.com" disabled={!live.available||saving}/></label><div className="account-connect-actions"><button className="btn btn-primary" disabled={!live.available||saving||!host.trim()} onClick={()=>void connect()}><Icon name="terminal"/>{saving?'Connecting…':'Use GitHub CLI login'}</button><button className="btn" disabled={!live.available||saving} onClick={()=>setShowToken(v=>!v)}>Use a saved token</button></div>{showToken&&<form className="token-form" onSubmit={e=>void connect(e,true)}><label>Access token<input type="password" disabled={saving} value={token} onChange={e=>setToken(e.target.value)} autoComplete="off" spellCheck={false} required placeholder="Token with access to your repositories"/></label><p className="prototype-note">Repository read/write and pull request permissions are required. The token is validated before it is saved.</p><div><button type="button" className="btn" disabled={saving} onClick={()=>{setShowToken(false);setToken('')}}>Cancel</button><button className="btn btn-primary" disabled={saving||!token.trim()}>Validate and save token</button></div></form>}{confirmRemove&&<div className="reset-confirm"><span>Remove the saved token for {confirmRemove}?</span><button className="btn" onClick={()=>setConfirmRemove(null)}>Cancel</button><button className="btn" onClick={()=>void remove()} disabled={saving}>Remove token</button></div>}{error&&<p className="form-error" role="alert">{error}</p>}{!live.available&&<p className="prototype-note">Account connection is available in the M.I.E.R. desktop app.</p>}<div className="live-sync-settings"><h2>Live updates</h2><p>Repositories and GitHub activity refresh every 15 seconds. New commits wait for your confirmation while you review code.</p><button className="btn" disabled={!live.repository||live.busy} onClick={()=>void live.refresh()}><Icon name="refresh"/>Refresh now</button>{live.repository&&<button className="btn" disabled={live.busy} onClick={()=>void live.openRepository().catch(()=>{})}><Icon name="folder"/>Open another repository</button>}</div></div>
}

export function LiveCreatePullRequest({live,onCancel,onCreated}:{live:Live;onCancel:()=>void;onCreated:(number:number)=>void}) {
  const repository=live.repository,identity=live.identity
  const branches=(repository?.branches??[]).filter(branch=>!branch.remote).map(branch=>branch.name)
  const defaultBase=branches.includes('main')?'main':branches.includes('master')?'master':branches[0]??''
  const defaults:PullRequestDraft={base:defaultBase,head:live.branch!==defaultBase?live.branch:branches.find(branch=>branch!==defaultBase)??'',title:'',body:'',draft:false}
  const [saved,setSaved,draftError]=usePersistentDraft(pullRequestDraftKey(repository?.root??'',identity),raw=>readPullRequestDraft(raw,defaults))
  const {base,head,title,body,draft}=saved
  const [error,setError]=useState(''),[pending,setPending]=useState<'create'|'push'|null>(null)
  const guard=useRef(false),mounted=useRef(true)
  useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[])
  const patch=(values:Partial<PullRequestDraft>)=>setSaved(previous=>({...previous,...values}))
  const remote=repository?.remotes?.find(remote=>remote.host.toLowerCase()===identity?.host.toLowerCase()&&remote.owner.toLowerCase()===identity?.owner.toLowerCase()&&remote.repository.toLowerCase()===identity?.repo.toLowerCase())?.name
  const headSHA=repository?.branches?.find(branch=>!branch.remote&&branch.name===head)?.commitSha
  const published=!!remote&&!!headSHA&&repository?.branches?.some(branch=>branch.remote&&branch.name===`${remote}/${head}`&&branch.commitSha===headSHA)
  const authenticated=!!live.account?.authenticated&&live.account.host.toLowerCase()===identity?.host.toLowerCase()
  const valid=branches.includes(base)&&branches.includes(head)&&base!==head
  const comparison=useBranchComparison(repository,valid?base:'',valid?head:'')
  const locked=live.busy||!!pending
  const allowed=valid&&!!title.trim()&&!!identity&&authenticated&&published&&!comparison.loading&&!comparison.error&&!!comparison.files?.length
  const create=async(event:FormEvent)=>{
    event.preventDefault();if(!allowed||locked||guard.current)return
    guard.current=true;setPending('create');setError('')
    try{const number=await live.create(title.trim(),body,base,head,draft);setSaved(defaults);if(mounted.current)onCreated(number)}
    catch(issue){if(mounted.current)setError(issue instanceof Error?issue.message:String(issue))}
    finally{guard.current=false;if(mounted.current)setPending(null)}
  }
  const push=async()=>{
    if(!remote||head!==live.branch||locked||guard.current)return
    guard.current=true;setPending('push');setError('')
    try{await live.git('push',undefined,remote)}catch(issue){if(mounted.current)setError(issue instanceof Error?issue.message:String(issue))}
    finally{guard.current=false;if(mounted.current)setPending(null)}
  }
  return <section className="create-pr-pane">
    <div className="create-pr-heading"><span className="eyebrow">{identity?`${identity.owner}/${identity.repo}`:'Local repository'}</span><h1>Create a pull request</h1><p>Give reviewers the context they need to understand your changes. Your draft is saved on this device.</p></div>
    <form onSubmit={create}>
      <div className="create-branches"><label>Base branch<select value={base} onChange={event=>patch({base:event.target.value})} disabled={locked}><option value="">Choose a base</option>{base&&!branches.includes(base)&&<option value={base} disabled>{base} (no longer available)</option>}{branches.map(branch=><option key={branch}>{branch}</option>)}</select></label><Icon name="arrow-left"/><label>Compare branch<select value={head} onChange={event=>patch({head:event.target.value})} disabled={locked}><option value="">Choose a branch</option>{head&&!branches.includes(head)&&<option value={head} disabled>{head} (no longer available)</option>}{branches.map(branch=><option key={branch}>{branch}</option>)}</select></label></div>
      <label>Title<input autoFocus required maxLength={512} value={title} onChange={event=>patch({title:event.target.value})} placeholder="Describe what changed" disabled={locked}/></label>
      <label>Description<textarea rows={7} maxLength={65536} value={body} onChange={event=>patch({body:event.target.value})} placeholder="What changed, why, and what should reviewers look for?" disabled={locked}/></label>
      <label className="checkbox-label"><input type="checkbox" checked={draft} disabled={locked} onChange={event=>patch({draft:event.target.checked})}/>Create as draft</label>
      {head&&head===base&&<p className="form-error">Choose different base and compare branches.</p>}
      {(error||draftError)&&<p className="form-error" role="alert">{error||draftError}</p>}
      {!identity?<p className="form-error">This repository needs a GitHub remote to create pull requests.</p>:!authenticated&&<p className="form-error">Connect your account for {identity.host} in Settings to create a pull request.</p>}
      <div className="branch-preview"><Icon name="branch"/><div><strong>{published?`${head} is published on ${remote}`:`Publish ${head||'the compare branch'} before creating a pull request`}</strong><span>{head===live.branch?'Local unstaged changes are excluded.':`Switch to ${head||'the compare branch'} to publish it.`}</span></div>{head===live.branch&&<button type="button" className="btn" disabled={locked||!remote||!repository?.hasHead||repository.detached} onClick={()=>void push()}><Icon name="upload"/>{pending==='push'?'Pushing…':'Push branch'}</button>}</div>
      <p className="comparison-note">Preview of committed changes using your local branches. Fetch first to include recent remote changes; GitHub checks the published branches when you create the pull request.</p>
      <ComparisonPreview {...comparison} valid={valid}/>
      <div className="create-form-actions"><button type="button" className="btn" disabled={locked} onClick={onCancel}>Cancel</button><button className="btn btn-primary" disabled={locked||!allowed}><Icon name="pull-request"/>{pending==='create'?'Creating…':draft?'Create draft':'Create pull request'}</button></div>
    </form>
  </section>
}
