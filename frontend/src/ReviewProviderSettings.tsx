import {useEffect,useRef,useState,type FormEvent} from 'react'
import {services,type ReviewConnection,type ReviewConnectRequest} from './services'
import type {useLiveWorkspace} from './useLiveWorkspace'
import {openWebURL} from './native'
import './ReviewProviderSettings.css'

type Live=ReturnType<typeof useLiveWorkspace>
type Kind=ReviewConnectRequest['kind']
const providers:[Kind,string,string,string][]=[
  ['gitlab','GitLab','gitlab.com','https://docs.gitlab.com/user/profile/personal_access_tokens/'],
  ['bitbucket','Bitbucket Cloud','bitbucket.org','https://support.atlassian.com/bitbucket-cloud/docs/api-token-permissions/'],
  ['azure','Azure DevOps','dev.azure.com','https://learn.microsoft.com/en-us/azure/devops/organizations/accounts/use-personal-access-tokens-to-authenticate'],
]

function ConnectionForm({kind,name,defaultHost,help,connections,live,onChanged}:{kind:Kind;name:string;defaultHost:string;help:string;connections:ReviewConnection[];live:Live;onChanged:()=>Promise<void>}){
  const [editing,setEditing]=useState(false),[host,setHost]=useState(live.identity?.kind===kind?live.identity.host:defaultHost),[username,setUsername]=useState(''),[organization,setOrganization]=useState(''),[token,setToken]=useState(''),[readOnly,setReadOnly]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState(''),[removing,setRemoving]=useState('')
  const pending=useRef(false),mounted=useRef(true)
  useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[])
  const update=async(operation:()=>Promise<void>)=>{if(pending.current)return;pending.current=true;setBusy(true);setError('');try{await operation()}catch(issue){if(mounted.current)setError(issue instanceof Error?issue.message:String(issue))}finally{pending.current=false;if(mounted.current)setBusy(false)}}
  const save=(event:FormEvent)=>{event.preventDefault();if(!token.trim())return;void update(async()=>{const status=await services.reviewProviders.connect({kind,host:host.trim(),username:username.trim(),organization:organization.trim(),token,readOnly});if(!mounted.current)return;setToken('');setEditing(false);await onChanged();await live.providerChanged(status.host)})}
  const connected=connections.filter(item=>item.kind===kind&&!item.disabled)
  const edit=(connection?:ReviewConnection)=>{setError('');setToken('');setHost(connection?.host??(live.identity?.kind===kind?live.identity.host:defaultHost));setUsername(connection?.username??'');setOrganization(connection?.organization??(kind==='azure'&&live.identity?.kind==='azure'?live.identity.owner.split('/')[0]:''));setReadOnly(connection?.readOnly??false);setEditing(true)}
  return <section className="review-provider-card" aria-label={`${name} account`}>
    <div className="review-provider-heading"><img src={`/providers/${kind}.svg`} alt="" width={28} height={28}/><div><h3>{name}</h3><p>{kind==='gitlab'?'GitLab.com and self-managed GitLab':kind==='bitbucket'?'Atlassian email and a scoped API token':'Organization and a personal access token'}</p></div><button className="btn" disabled={!live.available||busy} onClick={()=>edit()}>Connect {name}</button></div>
    {connected.map(connection=><div className="review-provider-account" key={connection.host}><div><strong>{connection.name||connection.login}</strong><span>{connection.host}{connection.organization?` · ${connection.organization}`:''} · {connection.readOnly?'Read-only':'Read and write'}</span></div><button className="btn btn-ghost" disabled={busy} onClick={()=>edit(connection)}>Edit connection</button><button className="btn btn-ghost" disabled={busy} onClick={()=>setRemoving(connection.host)}>Disconnect</button></div>)}
    {removing&&<div className="reset-confirm"><span>Disconnect {removing}? Your review drafts stay on this device.</span><button className="btn" disabled={busy} onClick={()=>setRemoving('')}>Keep connected</button><button className="btn" disabled={busy} onClick={()=>void update(async()=>{await services.reviewProviders.disconnect(removing);const removed=removing;setRemoving('');await onChanged();await live.providerChanged(removed)})}>Disconnect account</button></div>}
    {editing&&<form className="review-provider-form" onSubmit={save}>
      {kind==='gitlab'&&<label>GitLab host<input required value={host} disabled={busy} onChange={e=>setHost(e.target.value)} autoComplete="off" placeholder="gitlab.com or gitlab.your-company.com"/></label>}
      {kind==='bitbucket'&&<label>Atlassian email<input required type="email" value={username} disabled={busy} onChange={e=>setUsername(e.target.value)} autoComplete="off"/></label>}
      {kind==='azure'&&<label>Azure organization<input required value={organization} disabled={busy} onChange={e=>setOrganization(e.target.value)} autoComplete="off" placeholder="The name after dev.azure.com/"/></label>}
      <label>{kind==='bitbucket'?'API token':'Personal access token'}<input required type="password" value={token} disabled={busy} onChange={e=>setToken(e.target.value)} autoComplete="off" spellCheck={false}/></label>
      <label className="checkbox-label"><input type="checkbox" checked={readOnly} disabled={busy} onChange={e=>setReadOnly(e.target.checked)}/>Read-only connection</label>
      <p className="prototype-note">{kind==='gitlab'?'For reviews and metadata, use api plus read_repository. For read-only browsing, read_api plus read_repository is enough. Git over HTTPS also needs write_repository when pushing.':kind==='bitbucket'?'Use separate read and write scopes for pull requests and repositories, plus read:user:bitbucket to validate your account. Write scopes do not include read access.':"Use Code (Read & write), or Code (Read) for a read-only connection. Select the organization containing your repositories."} Tokens are validated, then saved in your OS keychain.</p>
      <div className="review-provider-buttons"><button type="button" className="btn btn-ghost" onClick={()=>void openWebURL(help).catch(issue=>setError(String(issue)))}>Token setup guide</button><span/><button type="button" className="btn" disabled={busy} onClick={()=>{setEditing(false);setToken('');setError('')}}>Cancel</button><button className="btn btn-primary" disabled={busy||!token.trim()}>{busy?'Connecting…':'Validate and connect'}</button></div>
    </form>}
    {error&&<p className="form-error" role="alert">{error}</p>}
  </section>
}

export default function ReviewProviderSettings({live}:{live:Live}){
  const [connections,setConnections]=useState<ReviewConnection[]>([]),[error,setError]=useState(''),[loading,setLoading]=useState(false)
  const mounted=useRef(true)
  const load=async()=>{setLoading(true);try{const value=await services.reviewProviders.connections();if(mounted.current){setConnections(value??[]);setError('')}}catch(issue){if(mounted.current)setError(issue instanceof Error?issue.message:String(issue))}finally{if(mounted.current)setLoading(false)}}
  useEffect(()=>{mounted.current=true;if(live.available)void load();return()=>{mounted.current=false}},[live.available])
  return <div className="review-provider-settings"><h2>Review providers</h2><p>Use native pull requests and merge requests with the same review workspace.</p>{loading&&<p role="status">Loading provider connections…</p>}{providers.map(([kind,name,host,help])=><ConnectionForm key={kind} kind={kind} name={name} defaultHost={host} help={help} connections={connections} live={live} onChanged={load}/>)}{error&&<p className="form-error" role="alert">{error}<button className="btn btn-ghost" onClick={()=>void load()}>Retry connections</button></p>}</div>
}
