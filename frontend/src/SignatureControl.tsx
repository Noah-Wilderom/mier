import { useState } from 'react'
import { services } from './services'
import type { CommitSignature } from './repositoryToolTypes'
import './SignatureControl.css'
const errorText=(issue:unknown)=>issue instanceof Error?issue.message:String(issue)

export function SignatureControl({root,sha}:{root:string;sha:string}){
  const [result,setResult]=useState<CommitSignature>()
  const [error,setError]=useState('')
  const [pending,setPending]=useState(false)
  const verify=async()=>{if(pending)return;setPending(true);setError('');try{setResult(await services.repository.commitSignature(root,sha))}catch(issue){setError(errorText(issue))}finally{setPending(false)}}
  const labels:Record<string,string>={G:'Good signature',B:'Bad signature',U:'Good signature; unknown trust',X:'Expired signature',Y:'Expired signing key',R:'Revoked signing key',E:'Signature could not be verified',N:'Unsigned'}
  return <div className="signature-result"><button className="btn btn-ghost" disabled={pending} onClick={()=>void verify()}>{pending?'Verifying…':result?'Verify again':'Verify signature'}</button>{result&&<span title={result.key}>{labels[result.status]??'Unknown signature status'}{result.signer&&` · ${result.signer}`}</span>}{error&&<span role="alert" className="git-operation-error">{error}</span>}</div>
}
