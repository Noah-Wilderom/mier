import { useEffect, useRef, useState } from 'react'
import { services, type RepositoryCommit, type RepositorySnapshot } from './services'
import type { HistoryQuery } from './gitHistory'

export function useGitHistory(repository?:RepositorySnapshot, path='') {
  const [branch,setBranch]=useState('')
  const [query,setQuery]=useState('')
  const [search,setSearch]=useState<HistoryQuery['search']>('message')
  const [submitted,setSubmitted]=useState({query:'',search:'message' as HistoryQuery['search']})
  const [result,setResult]=useState<{key:string;commits:RepositoryCommit[];cursor:string}>()
  const [failure,setFailure]=useState<{key:string;message:string}>()
  const [loading,setLoading]=useState(false)
  const [reload,setReload]=useState(0)
  const pending=useRef(false)
  const generation=useRef(0)
  const revision=branch?(repository?.branches?.find(item=>item.name===branch)?.commitSha??''):''
  const key=JSON.stringify([repository?.root,repository?.headSha,repository?.branches?.map(item=>[item.name,item.commitSha]),revision,submitted,path,reload])
  const request:HistoryQuery={revision,query:submitted.query,search:submitted.search,path,cursor:'',limit:100}
  useEffect(()=>{
    const epoch=++generation.current
    if(!repository) return
    setLoading(true);setFailure(undefined);pending.current=false
    void services.repository.historyPage(repository.root,request).then(page=>{if(epoch===generation.current)setResult({key,commits:page.commits??[],cursor:page.nextCursor})})
      .catch(issue=>{if(epoch===generation.current)setFailure({key,message:issue instanceof Error?issue.message:String(issue)})})
      .finally(()=>{if(epoch===generation.current)setLoading(false)})
    return()=>{++generation.current}
  },[key])
  const more=async()=>{
    if(!repository||result?.key!==key||!result.cursor||pending.current)return
    const epoch=generation.current
    pending.current=true;setLoading(true);setFailure(undefined)
    try{const page=await services.repository.historyPage(repository.root,{...request,cursor:result.cursor});if(epoch===generation.current)setResult(previous=>previous?.key===key?{key,commits:[...previous.commits,...(page.commits??[]).filter(commit=>!previous.commits.some(item=>item.sha===commit.sha))],cursor:page.nextCursor}:previous)}
    catch(issue){if(epoch===generation.current)setFailure({key,message:issue instanceof Error?issue.message:String(issue)})}
    finally{if(epoch===generation.current){pending.current=false;setLoading(false)}}
  }
  return {key,branch,setBranch,query,setQuery,search,setSearch,submit:()=>setSubmitted({query:query.trim(),search}),commits:result?.key===key?result.commits:[],cursor:result?.key===key?result.cursor:'',loading,error:failure?.key===key?failure.message:'',refresh:()=>setReload(n=>n+1),more}
}
