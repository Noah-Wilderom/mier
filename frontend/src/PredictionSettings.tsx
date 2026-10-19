import {useEffect,useRef,useState} from 'react'
import {hasNativeBackend,services} from './services'
import {PREDICTIONS_CHANGED,type PredictionScope,type PredictionStatus,type PredictionModel,type PredictionFeedback} from './predictionTypes'
import './Predictions.css'
export function predictionsChanged(){window.dispatchEvent(new Event(PREDICTIONS_CHANGED))}
export function usePredictionStatus(scope:PredictionScope={},active=true){
 const key=JSON.stringify(scope),[loaded,setLoaded]=useState<{key:string;status:PredictionStatus}>(),[error,setError]=useState(''),[revision,setRevision]=useState(0)
 useEffect(()=>{const changed=()=>setRevision(n=>n+1);window.addEventListener(PREDICTIONS_CHANGED,changed);return()=>window.removeEventListener(PREDICTIONS_CHANGED,changed)},[])
 // Keep this scope's status during a refresh so its settings form, returned
 // catalog and success notice stay mounted. Never expose another scope's status.
 useEffect(()=>{let cancelled=false;setError('');if(active&&hasNativeBackend())void services.predictions.status(scope).then(value=>{if(!cancelled)setLoaded({key,status:value})}).catch(issue=>{if(!cancelled)setError(String(issue))});else setLoaded(undefined);return()=>{cancelled=true}},[key,revision,active])
 return {status:active&&loaded?.key===key?loaded.status:undefined,error}
}
export default function PredictionSettings({scope}:{scope?:PredictionScope}){
 const {status,error:loadError}=usePredictionStatus(scope)
 const [key,setKey]=useState(''),[model,setModel]=useState('jev-1.13.0'),[enabled,setEnabled]=useState(false),[models,setModels]=useState<PredictionModel[]>([])
 const [busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState(''),[feedback,setFeedback]=useState<PredictionFeedback[]>()
 const guard=useRef(false),identity=JSON.stringify(scope)
 const fake=status?.provider==='fake'
 useEffect(()=>{if(status){setModel(status.model);setEnabled(status.enabled)}},[status,identity])
 const act=async(task:()=>Promise<void>)=>{if(guard.current)return;guard.current=true;setBusy(true);setError('');setNotice('');try{await task()}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}finally{guard.current=false;setBusy(false)}}
 const names=[...new Set([model,...models.map(m=>m.name)])]
 return <section className="prediction-settings" aria-label="Jev prediction settings"><header><span className="jev-mark" aria-hidden="true">Jev</span><div><h3>{fake?'Local Jev test provider':'TypeSafe · Jev'}</h3><p>Prioritize reviews, assess findings and ticket coverage, and suggest investigation paths.</p></div></header>
 {!hasNativeBackend()?<p>Open the desktop app to connect TypeSafe.</p>:<>
 {fake?<p className="prediction-test-notice" role="status">Local test mode produces synthetic answers inside M.I.E.R. No TypeSafe key, inference request or charge is involved. Repository and ticket content is still captured through the normal read-only services. Restart without MIER_JEV_FAKE to use TypeSafe; its key and settings are separate.</p>:<>
 <p className="muted">Use your own TypeSafe API key. It is stored in the OS keychain. Assessments send bounded source, linked ticket content or job logs to TypeSafe only when you start one.</p>
 <form onSubmit={e=>{e.preventDefault();void act(async()=>{setModels(await services.predictions.connect(key));setKey('');predictionsChanged();setNotice('TypeSafe connected. An available model is selected. Enable each repository separately.')})}}><label>TypeSafe API key<input type="password" aria-label="TypeSafe API key" autoComplete="off" spellCheck={false} value={key} onChange={e=>setKey(e.target.value)} disabled={busy}/></label><button className="btn" disabled={busy||!key.trim()}>{busy?'Connecting…':status?.configured?'Replace API key':'Connect TypeSafe'}</button></form>
 </>}
 {status?.configured&&<><div className="prediction-model"><label>Model<select aria-label="Jev model" disabled={busy} value={model} onChange={e=>setModel(e.target.value)}>{names.map(name=><option key={name} value={name}>{name}</option>)}</select></label><button className="btn btn-ghost" disabled={busy} onClick={()=>void act(async()=>{setModels(await services.predictions.models());predictionsChanged();setNotice('Models refreshed. An available model is selected.')})}>Refresh models</button></div>
 {scope&&<label className="prediction-consent"><input type="checkbox" checked={enabled} disabled={busy} onChange={e=>setEnabled(e.target.checked)}/><span>Allow Jev assessments for {scope.root?'this local repository':'this hosted repository'}. {fake?'Capture source and relevant linked ticket or log excerpts for local fixture testing only. This does not enable TypeSafe assessments.':'Share source and relevant linked ticket or log excerpts with TypeSafe when I start an assessment.'}</span></label>}
 <div className="prediction-settings-actions"><button className="btn" disabled={busy} onClick={()=>void act(async()=>{await services.predictions.save(scope??{},model,scope?enabled:false);predictionsChanged();setNotice('Prediction settings saved.')})}>Save Jev settings</button>{!fake&&<button className="btn btn-ghost" disabled={busy} onClick={()=>void act(async()=>{await services.predictions.disconnect();predictionsChanged();setNotice('TypeSafe disconnected. Running assessments cancelled.')})}>Disconnect TypeSafe</button>}</div>
 </>}
 {(error||loadError||status?.error)&&<p className="prediction-error" role="alert">{error||loadError||status?.error}</p>}{notice&&<p role="status">{notice}</p>}
 {!fake&&<p className="muted">Suggestions remain advisory. Model certainty is not a measured correctness rate. <a href="https://docs.typesafe.ai/confidence" target="_blank" rel="noreferrer">How confidence works</a> · <a href="https://console.typesafe.ai" target="_blank" rel="noreferrer">Get an API key</a></p>}
 <details><summary>Local evaluation feedback</summary><p className="muted">Useful / Not useful ratings stay in memory for this app session. The export contains model, assessment ID, rating and timing; no source or ticket text. These ratings do not measure missed bugs or calibration.</p><button className="btn" disabled={busy} onClick={()=>void act(async()=>setFeedback(await services.predictions.evaluation()))}>Load evaluation metadata</button>{feedback&&<textarea aria-label="Jev evaluation metadata" readOnly value={JSON.stringify(feedback,null,2)} rows={6}/>}</details>
 </>}
 </section>
}
