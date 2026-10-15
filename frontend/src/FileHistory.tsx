import { useEffect, useState } from 'react'
import Modal from './Modal'
import { services } from './services'
import type { BlameLine, HistoryPage, RevisionFile } from './gitHistory'
import './FileHistory.css'

export default function FileHistory({ root,path,revision,onClose }: { root:string;path:string;revision:string;onClose:()=>void }) {
  const [history,setHistory]=useState<HistoryPage>()
  const [selected,setSelected]=useState(revision)
  const [document,setDocument]=useState<RevisionFile>()
  const [blame,setBlame]=useState<{sha:string;lines:BlameLine[]}>()
  const [showBlame,setShowBlame]=useState(false)
  const [error,setError]=useState('')
  const [loading,setLoading]=useState(false)
  const [morePending,setMorePending]=useState(false)
  const [retry,setRetry]=useState(0)
  const request={revision,query:'',search:'message' as const,path,cursor:'',limit:100}
  const revisionPath=history?.commits.find(commit=>commit.sha===selected)?.filePath||path
  useEffect(()=>{
    let canceled=false
    void services.repository.historyPage(root,request).then(page=>{if(!canceled)setHistory(page)}).catch(issue=>{if(!canceled)setError(String(issue))})
    return()=>{canceled=true}
  },[root,path,revision,retry])
  useEffect(()=>{
    let canceled=false
    setDocument(undefined);setLoading(true);setError('')
    void services.repository.revisionFile(root,selected,revisionPath).then(file=>{if(!canceled)setDocument(file)}).catch(issue=>{if(!canceled)setError(issue instanceof Error?issue.message:String(issue))}).finally(()=>{if(!canceled)setLoading(false)})
    return()=>{canceled=true}
  },[root,revisionPath,selected,retry])
  useEffect(()=>{
    if(!showBlame)return
    let canceled=false
    void services.repository.blame(root,selected,revisionPath).then(lines=>{if(!canceled)setBlame({sha:selected,lines:lines??[]})}).catch(issue=>{if(!canceled)setError(String(issue))})
    return()=>{canceled=true}
  },[root,revisionPath,selected,showBlame,retry])
  const more=async()=>{
    if(!history?.nextCursor||morePending)return
    setMorePending(true)
    try{const page=await services.repository.historyPage(root,{...request,cursor:history.nextCursor});setHistory(previous=>({commits:[...(previous?.commits??[]),...(page.commits??[])],nextCursor:page.nextCursor}))}catch(issue){setError(String(issue))}finally{setMorePending(false)}
  }
  return <Modal title="File history" onClose={onClose}><div className="file-history-dialog"><div className="file-history-heading"><code>{path}</code><label><input type="checkbox" checked={showBlame} onChange={e=>setShowBlame(e.target.checked)} disabled={!!document?.binary}/>Show blame</label><button className="btn" onClick={()=>setRetry(n=>n+1)}>Refresh</button></div><div className="file-history-columns"><nav aria-label="File revisions">{!history?<p role="status">Loading history…</p>:history.commits.map(commit=><button key={commit.sha} className={selected===commit.sha?'selected':''} onClick={()=>setSelected(commit.sha)}><strong>{commit.subject}</strong><span>{commit.authorName} · {commit.sha.slice(0,8)}</span></button>)}{history?.nextCursor&&<button disabled={morePending} onClick={()=>void more()}>{morePending?'Loading…':'Load older revisions'}</button>}</nav><div className="file-revision-content">{loading?<p role="status">Loading revision…</p>:document?.binary?<p>Binary file. No text preview is available.</p>:showBlame?blame?.sha===selected?<div className="blame-lines">{blame.lines.map(line=><div key={line.line}><span title={`${line.summary}\n${line.author}\n${line.createdAt}`}><code>{line.sha.slice(0,8)}</code> {line.author}</span><span>{line.line}</span><code>{line.text||' '}</code></div>)}</div>:<p role="status">Loading blame…</p>:<pre className="file-revision-text">{document?.content}</pre>}</div></div>{error&&<p className="git-operation-error" role="alert">{error}</p>}</div></Modal>
}
