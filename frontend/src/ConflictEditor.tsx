import { lazy, Suspense, useEffect, useRef, useState, useSyncExternalStore, type UIEvent, type KeyboardEvent } from 'react'
import { services } from './services'
import { conflictChunks, containsConflictMarkers, resolveChunk, type ConflictFile, type GitMutation, type ResolveConflictRequest } from './gitOperations'
import { Icon } from './Icon'
import './GitOperations.css'
import { DEFAULT_CODE_REVIEW_PREFERENCES, effectiveCodeTheme, getCodeAppearance, getCodeReviewPreferences, subscribeCodeAppearance, subscribeCodeReviewPreferences } from './codeReviewPreferences'
import { highlightCodeLines, syntaxTokenStyle, type HighlightedCode } from './syntaxHighlighter'

// Keep unsaved resolutions when moving between files or application views.
// Source code stays in memory; it is not copied into browser persistence.
const drafts = new Map<string,{file:ConflictFile;result:string;choice:ResolveConflictRequest['choice']}>()
const AIActionPanel=lazy(()=>import('./AIActionPanel'))
const CodeEditor=lazy(()=>import('./CodeEditor'))

export default function ConflictEditor({ root, path, headSha, operationToken, busy, onOperate }: { root: string; path: string; headSha?: string; operationToken?: string; busy: boolean; onOperate: (mutation: GitMutation) => Promise<unknown> }) {
  const [aiOpen,setAIOpen]=useState(false)
  const [file, setFile] = useState<ConflictFile>()
  const [result, setResult] = useState('')
  const [choice, setChoice] = useState<ResolveConflictRequest['choice']>('result')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState(false)
  const [reload, setReload] = useState(0)
  const [index, setIndex] = useState(0)
  const [showBase, setShowBase] = useState(false)
  const [undo, setUndo] = useState<string[]>([])
  const [reloadConfirm, setReloadConfirm] = useState(false)
  const guard = useRef(false)
  const sourcePanes=useRef<HTMLDivElement>(null)
  const synchronizedScroll=useRef(new WeakMap<HTMLPreElement,{top:number;left:number}>())
  const appearance=useSyncExternalStore(subscribeCodeAppearance,getCodeAppearance,()=> 'light' as const)
  const draftKey=JSON.stringify([root,path])
  useEffect(() => {
    let canceled = false
    setLoading(true); setError('')
    void services.repository.conflict(root, path).then(next => {
      if(canceled)return
      const saved=drafts.get(draftKey)
      setFile(saved?.file??next);setResult(saved?.result??next.working);setChoice(saved?.choice??(next.editable?'result':'current'));setIndex(0);setUndo([])
      if(saved&&saved.file.token!==next.token)setError('This conflict changed on disk. Your unsaved result was retained. Copy it if needed, then reload the file before saving.')
    }).catch(issue => {if(!canceled)setError(issue instanceof Error?issue.message:String(issue))}).finally(()=>{if(!canceled)setLoading(false)})
    return()=>{canceled=true}
  },[root,path,operationToken,reload])
  const chunks = conflictChunks(result)
  const active = chunks[Math.min(index, Math.max(0, chunks.length-1))]
  const locked = busy || loading || pending
  const dirty = !!file && (result !== file.working || choice !== (file.editable?'result':'current'))
  useEffect(()=>{
    if(!file)return
    if(dirty){drafts.set(draftKey,{file,result,choice});while(drafts.size>20)drafts.delete(drafts.keys().next().value!)}else drafts.delete(draftKey)
  },[draftKey,file,result,choice,dirty])
  useEffect(()=>{
    const warn=(event:BeforeUnloadEvent)=>{if(dirty){event.preventDefault();event.returnValue=''}}
    window.addEventListener('beforeunload',warn);return()=>window.removeEventListener('beforeunload',warn)
  },[dirty])
  const reloadFile=()=>{drafts.delete(draftKey);setReloadConfirm(false);setReload(n=>n+1)}
  const replace = (value:string) => {setUndo(items=>[...items.slice(-19),result]);setResult(value);setChoice('result')}
  const choose=(choice:'current'|'incoming'|'both'|'incoming-first')=>{if(active&&!locked)replace(resolveChunk(result,active,choice))}
  const syncSources=(event:UIEvent<HTMLPreElement>)=>{
    const source=event.currentTarget,expected=synchronizedScroll.current.get(source)
    synchronizedScroll.current.delete(source)
    if(expected&&source.scrollTop===expected.top&&source.scrollLeft===expected.left)return
    sourcePanes.current?.querySelectorAll('pre').forEach(pane=>{
      if(pane===source)return
      pane.scrollTop=source.scrollTop;pane.scrollLeft=source.scrollLeft
      synchronizedScroll.current.set(pane,{top:pane.scrollTop,left:pane.scrollLeft})
    })
  }
  const shortcuts=(event:KeyboardEvent)=>{
    if(locked||!event.altKey||event.ctrlKey||event.metaKey)return
    if(event.key==='ArrowUp'){event.preventDefault();event.stopPropagation();setIndex(n=>Math.max(0,n-1))}
    else if(event.key==='ArrowDown'){event.preventDefault();event.stopPropagation();setIndex(n=>Math.min(Math.max(0,chunks.length-1),n+1))}
    else if(['1','2','3','4'].includes(event.key)){event.preventDefault();event.stopPropagation();choose(({'1':'current','2':'incoming','3':'both','4':'incoming-first'} as const)[event.key as '1'|'2'|'3'|'4'])}
  }
  const save = async () => {
    if(locked||guard.current||!file)return
    guard.current=true;setPending(true);setError('')
    try{await onOperate({kind:'resolve',request:{path,token:file.token,choice,content:choice==='result'?result:''}});drafts.delete(draftKey)}
    catch(issue){setError(issue instanceof Error?issue.message:String(issue))}
    finally{guard.current=false;setPending(false)}
  }
  const current=active?active.current:file?.current.content||''
  const incoming=active?active.incoming:file?.incoming.content||''
  const rows=Math.max(current.split('\n').length,incoming.split('\n').length)
  const align=(source:string)=>source+'\n'.repeat(Math.max(0,rows-source.split('\n').length))
  return <section className="conflict-workspace" aria-label={`Resolve conflict in ${path}`} aria-busy={loading} onKeyDown={shortcuts}><div className="conflict-editor">
    <header className="conflict-heading"><Icon name="alert-circle" size={16}/><strong>{path}</strong><span>{chunks.length ? `${chunks.length} unresolved ${chunks.length===1?'section':'sections'}`:'Review result'}</span><button className="btn btn-ghost" disabled={locked} onClick={()=>{if(dirty)setReloadConfirm(true);else reloadFile()}}>Reload file</button></header>
    {reloadConfirm&&<div className="git-confirm" role="alert"><span>Reload this file and discard your unsaved result?</span><button className="btn" onClick={()=>setReloadConfirm(false)}>Keep editing</button><button className="btn" onClick={reloadFile}>Discard result and reload</button></div>}
    {error&&<div className="git-operation-error" role="alert">{error}{!file&&<button className="btn" disabled={loading} onClick={()=>setReload(n=>n+1)}>Retry conflict</button>}</div>}
    {loading?<p className="conflict-loading" role="status">Loading conflict versions…</p>:file&&<>
      {file.reason&&<p className="conflict-notice">{file.reason}</p>}
      {file.editable?<>
        <nav className="conflict-navigation" aria-label="Conflict sections"><strong>{active?`Section ${Math.min(index,chunks.length-1)+1} of ${chunks.length}`:'All sections resolved'}</strong><span className="conflict-shortcuts" title="Alt+Up/Down: previous/next; Alt+1/2: accept side; Alt+3/4: accept both in each order">Alt+↑/↓ · Alt+1–4</span><button className="icon-button" aria-label="Previous conflict" disabled={locked||!active||index===0} onClick={()=>setIndex(n=>Math.max(0,n-1))}><Icon name="chevron-up"/></button><button className="icon-button" aria-label="Next conflict" disabled={locked||!active||index>=chunks.length-1} onClick={()=>setIndex(n=>n+1)}><Icon name="chevron-down"/></button><label className="checkbox-label"><input type="checkbox" checked={showBase} onChange={e=>setShowBase(e.target.checked)}/>Show base</label></nav>
        <div className="conflict-sources" ref={sourcePanes}><section><header><div><strong>Current</strong><span>{file.currentLabel}</span></div><button className="btn" disabled={locked||!active} onClick={()=>replace(resolveChunk(result,active,'current'))}>Accept current</button></header><ConflictCode path={path} label="Current conflict version" content={align(current||(file.current.exists?'Empty file':'File deleted'))} onScroll={syncSources}/></section><section><header><div><strong>Incoming</strong><span>{file.incomingLabel}</span></div><button className="btn" disabled={locked||!active} onClick={()=>replace(resolveChunk(result,active,'incoming'))}>Accept incoming</button></header><ConflictCode path={path} label="Incoming conflict version" content={align(incoming||(file.incoming.exists?'Empty file':'File deleted'))} onScroll={syncSources}/></section></div>
        {showBase&&<details className="conflict-base" open><summary>Common ancestor</summary><pre><code>{active?.base??file.base.content??'No common ancestor'}</code></pre></details>}
        <div className="conflict-result-heading"><strong>Result</strong><button type="button" className="btn btn-ghost" disabled={locked} onClick={()=>setAIOpen(value=>!value)}>{aiOpen?'Hide AI':'Propose with AI'}</button><span>{dirty?'Unsaved changes':'Edit the result or accept a version above'}</span><button className="btn btn-ghost" disabled={locked||!active} onClick={()=>replace(resolveChunk(result,active,'both'))}>Accept both (current first)</button><button className="btn btn-ghost" disabled={locked||!active} onClick={()=>choose('incoming-first')}>Accept both (incoming first)</button><button className="btn btn-ghost" disabled={locked||!undo.length} onClick={()=>{setResult(undo[undo.length-1]);setUndo(items=>items.slice(0,-1));setChoice('result')}}>Undo choice</button></div>
        <div className="conflict-result-area"><div className="conflict-result-editor"><Suspense fallback={<p role="status" className="conflict-loading">Loading result editor…</p>}><CodeEditor path={path} label="Merged result" value={result} disabled={locked} dark={appearance==='dark'} revealLine={active?result.slice(0,active.start).split('\n').length:undefined} revealKey={JSON.stringify([path,index,chunks.length])} onChange={value=>{setResult(file.working.includes('\r\n')?value.replace(/\r?\n/g,'\r\n'):value);setChoice('result')}}/></Suspense></div></div>
      </>:<div className="conflict-whole-choices"><p>Choose the version to keep, then save the resolution.</p></div>}
      <footer className="conflict-footer"><div className="conflict-whole-choices"><label>Whole file<select aria-label="Whole file resolution" value={choice} disabled={locked} onChange={e=>setChoice(e.target.value as ResolveConflictRequest['choice'])}>{file.editable&&<option value="result">Edited result</option>}<option value="current">Keep current{!file.current.exists?' (delete file)':''}</option><option value="incoming">Keep incoming{!file.incoming.exists?' (delete file)':''}</option><option value="delete">Delete file</option></select></label></div><span>{choice==='result'&&containsConflictMarkers(result)?'Resolve all markers to save.':'Saving also stages this file.'}</span><button className="btn btn-primary" disabled={locked||(choice==='result'&&containsConflictMarkers(result))} onClick={()=>void save()}>{pending?'Saving…':choice==='delete'||(choice==='current'&&!file.current.exists)||(choice==='incoming'&&!file.incoming.exists)?'Accept deletion and stage':'Save result and stage'}</button></footer>
    </>}
  </div>{aiOpen&&file?.editable&&<aside className="conflict-ai-sidebar" aria-label="AI conflict assistant"><header><strong>AI proposal</strong><button type="button" className="icon-button" aria-label="Close AI conflict assistant" onClick={()=>setAIOpen(false)}><Icon name="close" size={14}/></button></header><Suspense fallback={<p role="status">Loading AI proposal…</p>}><AIActionPanel scope={{root,kind:'conflict',expectedHead:headSha??'',path,currentResult:result}} disabled={locked} applyLabel="Use proposal in result" onApply={proposal=>{if(proposal.content!==undefined)replace(proposal.content)}}/></Suspense></aside>}
  </section>
}

function ConflictCode({path,label,content,onScroll}:{path:string;label:string;content:string;onScroll?:(event:UIEvent<HTMLPreElement>)=>void}) {
  const preferences=useSyncExternalStore(subscribeCodeReviewPreferences,getCodeReviewPreferences,()=>DEFAULT_CODE_REVIEW_PREFERENCES)
  const appearance=useSyncExternalStore(subscribeCodeAppearance,getCodeAppearance,()=> 'light' as const)
  const theme=effectiveCodeTheme(appearance,preferences)
  const key=JSON.stringify([path,content,theme])
  const [syntax,setSyntax]=useState<{key:string;code:HighlightedCode}>()
  useEffect(()=>{let canceled=false;void highlightCodeLines(path,content.split('\n'),theme).then(code=>{if(!canceled)setSyntax({key,code})});return()=>{canceled=true}},[key])
  const code=syntax?.key===key?syntax.code:undefined
  return <pre tabIndex={0} aria-label={label} onScroll={onScroll}><code>{code?code.lines.map((line,i)=><span className="conflict-code-line" key={i}>{line.map((token,j)=><span key={j} style={syntaxTokenStyle(token)}>{token.content}</span>)}{i<code.lines.length-1?'\n':''}</span>):content}</code></pre>
}
