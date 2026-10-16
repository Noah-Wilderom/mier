import {lazy,Suspense,useState,useSyncExternalStore} from 'react'
import Modal from './Modal'
import {getCodeAppearance,subscribeCodeAppearance} from './codeReviewPreferences'
import type {ReviewFileContent} from './diffContext'
const CodeEditor=lazy(()=>import('./CodeEditor'))
export default function ReviewFullFile({source,onClose}:{source:ReviewFileContent;onClose:()=>void}){
 const [side,setSide]=useState<'old'|'new'>(source.newExists?'new':'old')
 const appearance=useSyncExternalStore(subscribeCodeAppearance,getCodeAppearance,()=> 'light' as const)
 return <Modal title={`Entire file · ${source.path}`} onClose={onClose}><div className="review-full-file"><div className="review-full-file-tools"><label>Version <select aria-label="Entire file version" value={side} onChange={e=>setSide(e.target.value as 'old'|'new')}><option value="old" disabled={!source.oldExists}>Original · {source.mergeBaseSha.slice(0,8)}</option><option value="new" disabled={!source.newExists}>Changed · {source.headSha.slice(0,8)}</option></select></label><span className="muted">Read only · exact reviewed revision</span></div><Suspense fallback={<p role="status">Loading source viewer…</p>}><CodeEditor value={source[side]} path={side==='old'?source.previousPath||source.path:source.path} label={`Entire ${side==='old'?'original':'changed'} file`} disabled dark={appearance==='dark'} onChange={()=>{}}/></Suspense></div></Modal>
}
