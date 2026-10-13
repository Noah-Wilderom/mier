import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { configureCurrentUser, type ChangedFile, type MergeMethod, type PullRequest } from './data'
import { configureGitHubAccount, mapPullRequest, mapPullRequestSummary, mapRepositoryDiff, mapRepositorySnapshot, RemoteActionAppliedError, syncPullRequest } from './liveData'
import { repositoryName, type RepositoryCard } from './RepositorySelector'
import { readReviewDrafts, restoreReviewDraft, reviewDraftStorageKey, type SavedReviewDraft } from './reviewDrafts'
import { hasNativeBackend, services, subscribeWorkspaceUpdates, type AccountStatus, type RepositoryIdentity, type RepositorySnapshot, type WorkspaceUpdate } from './services'

const emptyHistory: ReturnType<typeof mapRepositorySnapshot>['history'] = []
const emptyChanges: ReturnType<typeof mapRepositorySnapshot>['changes'] = []

function identityFor(snapshot: RepositorySnapshot): RepositoryIdentity | undefined {
  const remote = (snapshot.remotes ?? []).find(remote => remote.name === 'origin' && remote.owner && remote.repository) ?? (snapshot.remotes ?? []).find(remote => remote.owner && remote.repository)
  return remote ? { host: remote.host, owner: remote.owner, repo: remote.repository } : undefined
}
export { preserveReviewRevision } from './reviewRevision'
import { preserveReviewRevision } from './reviewRevision'

export function useLiveWorkspace(notify: (message:string)=>void) {
  const [available,setAvailable]=useState(hasNativeBackend)
  const [enabled,setEnabled]=useState(hasNativeBackend)
  const [repository,setRepository]=useState<RepositorySnapshot|null>(null)
  const [prs,setPrs]=useState<PullRequest[]>([])
  const [account,setAccount]=useState<AccountStatus|null>(null)
  const [recent,setRecent]=useState<string[]>([])
  const [repositoryCards,setRepositoryCards]=useState<Record<string,RepositoryCard>>({})
  const [busy,setBusy]=useState(false)
  const [openingRepository,setOpeningRepository]=useState('')
  const [loadingPullRequests,setLoadingPullRequests]=useState(false)
  const [initialDrafts]=useState(()=>{try{return readReviewDrafts(localStorage.getItem(reviewDraftStorageKey))}catch{return new Map<string,SavedReviewDraft>()}})
  const [error,setError]=useState('')
  const [updatedAt,setUpdatedAt]=useState('')
  const [selected,setSelected]=useState(0)
  const repoRef=useRef<RepositorySnapshot|null>(null)
  const prsRef=useRef<PullRequest[]>([])
  const selectedRef=useRef(0)
  const latestRevisions=useRef(new Map<number,PullRequest>())
  const drafts=useRef(initialDrafts)
  const detailRequests=useRef(new Map<number,Promise<void>>())
  const queue=useRef(Promise.resolve())
  const activeEpoch=useRef(0)
  const localMutationAt=useRef(0)
  const setItems=useCallback((items:PullRequest[])=>{prsRef.current=items;setPrs(items)},[])
  const saveDraft=useCallback((pr:PullRequest)=>{
    const key=`${pr.provider?.host}/${pr.provider?.owner}/${pr.provider?.repo}/${pr.number}`
    drafts.current.delete(key);drafts.current.set(key,{headSha:pr.headSha,viewedFiles:pr.viewedFiles,pendingComments:pr.pendingComments})
    try{localStorage.setItem(reviewDraftStorageKey,JSON.stringify({version:1,entries:[...drafts.current.entries()].slice(-200)}))}catch{setError('Your review draft could not be saved on this device. Keep this window open until you submit it.')}
  },[])
  const draftKey=(pr:PullRequest)=>`${pr.provider?.host}/${pr.provider?.owner}/${pr.provider?.repo}/${pr.number}`
  const applyDetail=useCallback((next:PullRequest,force=false)=>{
    const previous=prsRef.current.find(pr=>pr.number===next.number)
    if(previous?.headSha!==next.headSha&&previous?.provider?.detailLoaded&&!force) latestRevisions.current.set(next.number,next)
    if(force) latestRevisions.current.delete(next.number)
    const saved=drafts.current.get(draftKey(next))
    const merged=previous?.provider?.detailLoaded?preserveReviewRevision(previous,next,force):restoreReviewDraft(next,saved)
    saveDraft(merged)
    setItems(prsRef.current.some(pr=>pr.number===next.number)?prsRef.current.map(pr=>pr.number===next.number?merged:pr):[merged,...prsRef.current])
  },[setItems,saveDraft])
  const applyUpdate=useCallback((update:WorkspaceUpdate)=>{
    if(update.repository||update.pullRequests||update.error)setRepositoryCards(cards=>({...cards,[update.path]:{...cards[update.path],path:update.path,name:update.repository?.name??cards[update.path]?.name??repositoryName(update.path),branch:update.repository?.currentBranch??cards[update.path]?.branch,host:update.host||cards[update.path]?.host,owner:update.owner||cards[update.path]?.owner,repo:update.repo||cards[update.path]?.repo,...(update.pullRequests||update.error?{loadingPullRequests:false}:{}),...(update.pullRequests?{openPullRequests:update.pullRequests.filter(pr=>['open','draft'].includes(pr.status)).length}:{})}}))
    if(update.path!==repoRef.current?.root)return
    if(update.repository && Date.parse((update as WorkspaceUpdate & {repositoryUpdatedAt?:string}).repositoryUpdatedAt??update.updatedAt)>localMutationAt.current){repoRef.current=update.repository;setRepository(update.repository)}
    const identity=repoRef.current&&identityFor(repoRef.current)
    const eventIdentity=update.host&&update.owner&&update.repo?{host:update.host,owner:update.owner,repo:update.repo}:undefined
    if(JSON.stringify(identity)!==JSON.stringify(eventIdentity)){
      ++activeEpoch.current;setItems([]);setSelected(0);selectedRef.current=0;setLoadingPullRequests(!!identity)
      void services.sync.startWatch({path:update.path,host:identity?.host??'',owner:identity?.owner??'',repo:identity?.repo??''}).catch(issue=>{setLoadingPullRequests(false);setError(String(issue))})
      return
    }
    if(update.pullRequests||update.error)setLoadingPullRequests(false)
    if(update.pullRequests&&identity){setItems(update.pullRequests.map(raw=>{
      const summary=mapPullRequestSummary(raw,identity),previous=prsRef.current.find(pr=>pr.number===raw.number)
      if(!previous?.provider?.detailLoaded)return summary
      if(previous.headSha!==summary.headSha)return {...previous,title:summary.title,status:summary.status,assignees:summary.assignees,reviewers:summary.reviewers,labels:summary.labels,updatedAt:summary.updatedAt,provider:{...previous.provider,hasNewCommits:true}}
      return {...previous,title:summary.title,status:summary.status,body:summary.body,assignees:summary.assignees,reviewers:summary.reviewers,labels:summary.labels,updatedAt:summary.updatedAt}
    }))}
    if(update.pullRequest&&identity)applyDetail(mapPullRequest(update.pullRequest,identity))
    setError(update.error??'');setUpdatedAt(update.updatedAt)
  },[applyDetail,setItems])
  const perform=useCallback(async<T,>(operation:()=>Promise<T>):Promise<T>=>{
    const run=queue.current.then(async()=>{setBusy(true);try{return await operation()}catch(issue){const message=issue instanceof Error?issue.message:String(issue);setError(message);notify(message);throw issue}finally{setBusy(false)}})
    queue.current=run.then(()=>undefined,()=>undefined)
    return run
  },[notify])
  const refresh=useCallback(async()=>{
    const repo=repoRef.current;if(!repo)return
    try{await services.sync.refresh(repo.root)}catch(issue){setError(issue instanceof Error?issue.message:String(issue))}
  },[])
  const loadDetail=useCallback(async(number:number,force=false)=>{
    const repo=repoRef.current,identity=repo&&identityFor(repo);if(!repo||!identity||number<=0)return
    const epoch=activeEpoch.current
    const detail=mapPullRequest(await services.github.getPullRequest(identity.host,identity.owner,identity.repo,number),identity)
    if(epoch===activeEpoch.current)applyDetail(detail,force)
  },[applyDetail])
  const openRepository=useCallback(async(path?:string)=>perform(async()=>{
    const chosen=path??await services.workspace.chooseRepository();if(!chosen)return
    setOpeningRepository(chosen.split(/[\\/]/).filter(Boolean).pop()||chosen)
    try{
      const snapshot=await services.repository.open(chosen)
      const epoch=++activeEpoch.current
      detailRequests.current.clear();localMutationAt.current=0
      repoRef.current=snapshot;setRepository(snapshot);setEnabled(true);setItems([]);setSelected(0);selectedRef.current=0;latestRevisions.current.clear();setError('');setAccount(null)
      const identity=identityFor(snapshot)
      setRepositoryCards(cards=>({...cards,[snapshot.root]:{...cards[snapshot.root],path:snapshot.root,name:snapshot.name,branch:snapshot.currentBranch,host:identity?.host,owner:identity?.owner,repo:identity?.repo}}))
      setLoadingPullRequests(!!identity)
      // Start the background worker immediately; local Git is ready before account/network requests finish.
      await services.sync.startWatch({path:snapshot.root,host:identity?.host??'',owner:identity?.owner??'',repo:identity?.repo??''})
      void services.workspace.rememberRepository(snapshot.root).then(()=>services.workspace.recentRepositories()).then(setRecent).catch(issue=>setError(String(issue)))
      if(identity)void services.credentials.accountStatus(identity.host).then(status=>{
        if(epoch!==activeEpoch.current)return
        setAccount(status)
        if(status.authenticated)configureGitHubAccount(status)
        else{setLoadingPullRequests(false);setError(status.error||`Connect your account for ${identity.host} to load pull requests.`)}
      }).catch(issue=>{if(epoch===activeEpoch.current){setLoadingPullRequests(false);setError(issue instanceof Error?issue.message:String(issue))}})
      setUpdatedAt(new Date().toISOString());notify(`Opened ${snapshot.name}.`)
    }finally{setOpeningRepository('')}
  }),[notify,perform,setItems])
  const connect=useCallback(async(host:string,token?:string)=>perform(async()=>{
    const status=token?await services.credentials.saveToken(host,token):await services.credentials.accountStatus(host)
    const activeIdentity=repoRef.current&&identityFor(repoRef.current)
    const isActiveAccount=!activeIdentity||activeIdentity.host.toLowerCase()===status.host.toLowerCase()
    if(isActiveAccount)setAccount(status)
    if(!status.authenticated)throw new Error(status.error||`No account connected for ${host}. Run gh auth login --hostname ${host} or save a token.`)
    if(isActiveAccount)configureGitHubAccount(status);setError('');if(repoRef.current&&isActiveAccount)setEnabled(true);await refresh();notify(`Connected as ${status.login} on ${status.host}.`)
  }),[perform,refresh,notify])
  const selectPullRequest=useCallback((number:number)=>{
    selectedRef.current=number;setSelected(number)
    const repo=repoRef.current;if(!repo)return
    void services.sync.watchPullRequest(repo.root,number).catch(()=>{})
    if(!prsRef.current.find(pr=>pr.number===number)?.provider?.detailLoaded&&!detailRequests.current.has(number)){
      const request=loadDetail(number).catch(issue=>setError(issue instanceof Error?issue.message:String(issue))).finally(()=>detailRequests.current.delete(number))
      detailRequests.current.set(number,request)
    }
  },[loadDetail])
  const updatePullRequest=useCallback(async(previous:PullRequest,next:PullRequest)=>perform(async()=>{
    let canonical:PullRequest
    try{canonical=await syncPullRequest(previous,next)}catch(issue){if(issue instanceof RemoteActionAppliedError){await loadDetail(previous.number).catch(()=>{});void refresh()}throw issue}
    applyDetail(canonical);
    setItems(prsRef.current.map(p=>p.number===next.number?{...p,pendingComments:next.pendingComments,viewedFiles:next.viewedFiles}:p));
    if(canonical!==next)notify("Pull request updated.");
    const saved=prsRef.current.find(pr=>pr.number===next.number)
    if(saved)saveDraft(saved)
    setError('')
  }),[applyDetail,perform,setItems,notify,saveDraft,loadDetail,refresh])
  const acceptNewCommits=useCallback(async()=>perform(async()=>{await loadDetail(selectedRef.current,true);notify('Review updated to the latest commits. Draft comments from earlier commits are retained for you to reposition.')}),[loadDetail,notify,perform])
  const merge=useCallback(async(pr:PullRequest,method:MergeMethod)=>perform(async()=>{
    const p=pr.provider;if(!p)throw new Error('This pull request is not connected to GitHub.')
    await services.github.mergePullRequest(p.host,p.owner,p.repo,p.number,method,pr.headSha)
    await loadDetail(pr.number,true);await refresh();notify(`Pull request #${pr.number} merged.`)
  }),[loadDetail,notify,perform,refresh])
  const create=useCallback(async(title:string,body:string,base:string,head:string,draft:boolean)=>perform(async()=>{
    const repo=repoRef.current,identity=repo&&identityFor(repo);if(!identity)throw new Error('Open a repository with a GitHub remote first.')
    const result=await services.github.createPullRequest(identity.host,identity.owner,identity.repo,{title,body,base,head,draft})
    await refresh();await loadDetail(result.number,true);notify(`Created pull request #${result.number}.`);return result.number
  }),[loadDetail,notify,perform,refresh])
  const git=useCallback(async(action:'fetch'|'pull'|'push'|'checkout'|'createBranch'|'checkoutPullRequest'|'stage'|'unstage'|'commit',argument?:string|string[])=>perform(async()=>{
    const repo=repoRef.current;if(!repo)throw new Error('Open a Git repository first.')
    switch(action){case 'fetch':await services.repository.fetch(repo.root,'origin');break;case 'pull':await services.repository.pull(repo.root,'origin');break;case 'push':await services.repository.push(repo.root,'origin');break;case 'checkout':await services.repository.checkout(repo.root,String(argument));break;case 'checkoutPullRequest':await services.repository.checkoutPullRequest(repo.root,Number(argument));break;case 'createBranch':await services.repository.createBranch(repo.root,String(argument));break;case 'stage':await services.repository.stage(repo.root,argument as string[]);break;case 'unstage':await services.repository.unstage(repo.root,argument as string[]);break;case 'commit':await services.repository.commit(repo.root,String(argument));break}
    const snapshot=await services.repository.snapshot(repo.root);localMutationAt.current=Date.now();repoRef.current=snapshot;setRepository(snapshot);setRepositoryCards(cards=>({...cards,[snapshot.root]:{...cards[snapshot.root],path:snapshot.root,name:snapshot.name,branch:snapshot.currentBranch}}));setError('');notify(action==='commit'?'Commit created.':`${action[0].toUpperCase()+action.slice(1)} completed.`)
  }),[notify,perform])
  const diff=useCallback(async(path:string,staged:boolean):Promise<ChangedFile>=>{const repo=repoRef.current;if(!repo)throw new Error('Open a repository first.');return mapRepositoryDiff(await services.repository.diff(repo.root,path,staged))},[])
  const useDemo=useCallback(()=>{setEnabled(false);configureCurrentUser('noah');setError('')},[])
  useEffect(()=>{
    let initialized=false
    let canceled=false
    const initialize=()=>{
      if(initialized||!hasNativeBackend())return
      initialized=true;setAvailable(true);setEnabled(true)
      void services.workspace.recentRepositories().then(paths=>{
        if(canceled)return
        setRecent(paths)
        for(const path of paths)void services.repository.snapshot(path).then(snapshot=>{
          if(canceled)return
          const identity=identityFor(snapshot)
          setRepositoryCards(cards=>({...cards,[snapshot.root]:{...cards[snapshot.root],path:snapshot.root,name:snapshot.name,branch:snapshot.currentBranch,host:identity?.host,owner:identity?.owner,repo:identity?.repo,loadingPullRequests:!!identity}}))
          return services.sync.startWatch({path:snapshot.root,host:identity?.host??'',owner:identity?.owner??'',repo:identity?.repo??''}).catch(()=>{
            if(!canceled)setRepositoryCards(cards=>({...cards,[snapshot.root]:{...cards[snapshot.root],loadingPullRequests:false}}))
          })
        }).catch(()=>{})
      }).catch(issue=>{if(!canceled)setError(String(issue))})
    }
    initialize();window.addEventListener('wails:runtime-config-ready',initialize)
    return()=>{canceled=true;window.removeEventListener('wails:runtime-config-ready',initialize)}
  },[])
  useEffect(()=>subscribeWorkspaceUpdates(applyUpdate,error=>setError(error.message)),[applyUpdate])
  useEffect(()=>{const online=()=>void refresh();window.addEventListener('online',online);return()=>window.removeEventListener('online',online)},[refresh])
  const mapped=useMemo(()=>repository?mapRepositorySnapshot(repository):null,[repository])
  return {available,enabled,repository,identity:repository?identityFor(repository):undefined,prs,account,recent,repositoryCards,busy,openingRepository,loadingPullRequests,error,updatedAt,selected,history:mapped?.history??emptyHistory,changes:mapped?.changes??emptyChanges,branch:mapped?.branch??'',openRepository,connect,selectPullRequest,updatePullRequest,acceptNewCommits,merge,create,git,diff,refresh,useDemo}
}
