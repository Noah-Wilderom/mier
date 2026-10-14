import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { configureCurrentUser, type ChangedFile, type MergeMethod, type PullRequest } from './data'
import { configureGitHubAccount, mapPullRequest, mapPullRequestSummary, mapRepositoryDiff, mapRepositorySnapshot, RemoteActionAppliedError, syncPullRequest } from './liveData'
import { repositoryName, type RepositoryCard } from './RepositorySelector'
import { readReviewDrafts, restoreReviewDraft, reviewDraftStorageKey, type SavedReviewDraft } from './reviewDrafts'
import { hasNativeBackend, services, subscribeWorkspaceUpdates, type AccountStatus, type PullRequestPageQuery, type PullRequestSummary, type RepositorySnapshot, type WorkspaceUpdate } from './services'
import type { GitAction } from './gitWorkspace'
import { preferredRemote, gitHubIdentity as identityFor } from './gitWorkspace'

const emptyHistory: ReturnType<typeof mapRepositorySnapshot>['history'] = []
const emptyChanges: ReturnType<typeof mapRepositorySnapshot>['changes'] = []
const initialPullRequestQuery:PullRequestPageQuery = {state:'open',view:'inbox',query:''}

export { preserveReviewRevision } from './reviewRevision'
import { preserveReviewRevision } from './reviewRevision'

export function useLiveWorkspace(notify: (message:string)=>void) {
  const [available,setAvailable]=useState(hasNativeBackend)
  const [enabled,setEnabled]=useState(hasNativeBackend)
  const enabledRef=useRef(hasNativeBackend())
  const [repository,setRepository]=useState<RepositorySnapshot|null>(null)
  const [prs,setPrs]=useState<PullRequest[]>([])
  const [account,setAccount]=useState<AccountStatus|null>(null)
  const [recent,setRecent]=useState<string[]>([])
  const [repositoryCards,setRepositoryCards]=useState<Record<string,RepositoryCard>>({})
  const [busy,setBusy]=useState(false)
  const [openingRepository,setOpeningRepository]=useState('')
  const [loadingPullRequests,setLoadingPullRequests]=useState(false)
  const [loadingMorePullRequests,setLoadingMorePullRequests]=useState(false)
  const [pullRequestQuery,setPullRequestQueryState]=useState(initialPullRequestQuery)
  const [pullRequestNumbers,setPullRequestNumbers]=useState<number[]>([])
  const [hasMorePullRequests,setHasMorePullRequests]=useState(false)
  const [pullRequestTotal,setPullRequestTotal]=useState<number|null>(null)
  const [openPullRequestCount,setOpenPullRequestCount]=useState<number|null>(null)
  const [pullRequestResultLimit,setPullRequestResultLimit]=useState(0)
  const [pullRequestPageError,setPullRequestPageError]=useState('')
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
  const openIntent=useRef(0)
  const localMutationAt=useRef(0)
  const pageGeneration=useRef(0)
  const pageCursor=useRef('')
  const pageRequest=useRef<Promise<void>|null>(null)
  const pageQueryRef=useRef(initialPullRequestQuery)
  const pageNumbersRef=useRef<number[]>([])
  const pendingSearch=useRef<ReturnType<typeof setTimeout>>()
  const requestedQuery=useRef(initialPullRequestQuery)
  const authenticationError=useRef('')
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
  const mergeSummary=useCallback((raw:PullRequestSummary,identity:NonNullable<ReturnType<typeof identityFor>>)=>{
    const summary=mapPullRequestSummary(raw,identity),previous=prsRef.current.find(pr=>pr.number===raw.number)
    if(!previous?.provider?.detailLoaded)return restoreReviewDraft(summary,drafts.current.get(draftKey(summary)))
    if(previous.headSha!==summary.headSha)return {...previous,title:summary.title,status:summary.status,assignees:summary.assignees,reviewers:summary.reviewers,labels:summary.labels,updatedAt:summary.updatedAt,provider:{...previous.provider,hasNewCommits:true}}
    return {...previous,title:summary.title,status:summary.status,body:summary.body,assignees:summary.assignees,reviewers:summary.reviewers,labels:summary.labels,updatedAt:summary.updatedAt}
  },[])
  const loadPullRequestPage=useCallback((append=false):Promise<void>=>{
    const repo=repoRef.current,identity=repo&&identityFor(repo)
    if(!repo||!identity)return Promise.resolve()
    if(pageRequest.current)return pageRequest.current
    const epoch=activeEpoch.current,generation=pageGeneration.current,cursor=append?pageCursor.current:''
    if(append)setLoadingMorePullRequests(true);else setLoadingPullRequests(true)
    setPullRequestPageError('')
    const request=services.github.listPullRequestsPage(identity.host,identity.owner,identity.repo,{...pageQueryRef.current,cursor,pageSize:25}).then(page=>{
      if(epoch!==activeEpoch.current||generation!==pageGeneration.current)return
      const mapped=(page.items??[]).map(raw=>mergeSummary(raw,identity))
      const numbers=[...new Set([...(append?pageNumbersRef.current:[]),...mapped.map(pr=>pr.number)])]
      const items=append?[...prsRef.current]:[]
      for(const pr of mapped){const index=items.findIndex(item=>item.number===pr.number);if(index>=0)items[index]=pr;else items.push(pr)}
      // Keep opened reviews separate from page membership. A newly created PR
      // may not appear in GitHub's listing immediately after its detail loads.
      for(const retained of prsRef.current.filter(pr=>pr.provider?.detailLoaded||pr.number===selectedRef.current))if(!items.some(pr=>pr.number===retained.number))items.push(retained)
      pageNumbersRef.current=numbers;setPullRequestNumbers(numbers);setItems(items)
      pageCursor.current=page.nextCursor;setHasMorePullRequests(page.hasNextPage)
      setPullRequestTotal(page.totalCount);setOpenPullRequestCount(page.openCount);setPullRequestResultLimit(page.resultLimit??0)
      setRepositoryCards(cards=>({...cards,[repo.root]:{...cards[repo.root],path:repo.root,name:repo.name,openPullRequests:page.openCount,loadingPullRequests:false}}))
      setError(authenticationError.current)
      setUpdatedAt(new Date().toISOString())
    }).catch(issue=>{
      if(epoch===activeEpoch.current&&generation===pageGeneration.current)setPullRequestPageError(issue instanceof Error?issue.message:String(issue))
    }).finally(()=>{
      if(pageRequest.current===request)pageRequest.current=null
      if(epoch===activeEpoch.current&&generation===pageGeneration.current){setLoadingPullRequests(false);setLoadingMorePullRequests(false)}
    })
    pageRequest.current=request
    return request
  },[mergeSummary,setItems])
  const resetPullRequestPage=useCallback(()=>{
    ++pageGeneration.current;pageRequest.current=null;pageCursor.current='';pageNumbersRef.current=[];setPullRequestNumbers([])
    setHasMorePullRequests(false);setPullRequestTotal(null);setPullRequestResultLimit(0);setPullRequestPageError('');setLoadingMorePullRequests(false)
    setItems(prsRef.current.filter(pr=>pr.provider?.detailLoaded||pr.number===selectedRef.current))
  },[setItems])
  const setPullRequestQuery=useCallback((next:PullRequestPageQuery)=>{
    if(JSON.stringify(next)===JSON.stringify(requestedQuery.current))return
    const previous=requestedQuery.current;requestedQuery.current=next
    clearTimeout(pendingSearch.current)
    resetPullRequestPage();setLoadingPullRequests(!!(repoRef.current&&identityFor(repoRef.current)))
    const apply=()=>{pageQueryRef.current=next;setPullRequestQueryState(next)}
    if(previous.state===next.state&&previous.view===next.view)pendingSearch.current=setTimeout(apply,250)
    else apply()
  },[resetPullRequestPage])
  useEffect(()=>{
    if(!enabled||!repository||!identityFor(repository)||!account?.authenticated)return
    if(JSON.stringify(pullRequestQuery)!==JSON.stringify(requestedQuery.current))return
    resetPullRequestPage();pageQueryRef.current=pullRequestQuery
    void loadPullRequestPage()
  },[enabled,repository?.root,account?.host,account?.authenticated,pullRequestQuery,resetPullRequestPage,loadPullRequestPage])
  useEffect(()=>()=>clearTimeout(pendingSearch.current),[])
  const applyUpdate=useCallback((update:WorkspaceUpdate)=>{
    if(update.repository||update.pullRequests||update.error||update.openPullRequestCount!==undefined)setRepositoryCards(cards=>({...cards,[update.path]:{...cards[update.path],path:update.path,name:update.repository?.name??cards[update.path]?.name??repositoryName(update.path),branch:update.repository?.currentBranch??cards[update.path]?.branch,host:update.host||cards[update.path]?.host,owner:update.owner||cards[update.path]?.owner,repo:update.repo||cards[update.path]?.repo,...(update.pullRequests||update.error?{loadingPullRequests:false}:{}),...(update.openPullRequestCount!==undefined?{openPullRequests:update.openPullRequestCount}:{})}}))
    if(!enabledRef.current)return
    if(update.path!==repoRef.current?.root)return
    if(update.repository && Date.parse((update as WorkspaceUpdate & {repositoryUpdatedAt?:string}).repositoryUpdatedAt??update.updatedAt)>localMutationAt.current){repoRef.current=update.repository;setRepository(update.repository)}
    const identity=repoRef.current&&identityFor(repoRef.current)
    const eventIdentity=update.host&&update.owner&&update.repo?{host:update.host,owner:update.owner,repo:update.repo}:undefined
    if(JSON.stringify(identity)!==JSON.stringify(eventIdentity)){
      const epoch=++activeEpoch.current;authenticationError.current='';selectedRef.current=0;resetPullRequestPage();setItems([]);setSelected(0);setOpenPullRequestCount(null);setLoadingPullRequests(!!identity);setAccount(null);detailRequests.current.clear();latestRevisions.current.clear()
      void services.sync.startWatch({path:update.path,host:identity?.host??'',owner:identity?.owner??'',repo:identity?.repo??''}).catch(issue=>{if(epoch===activeEpoch.current){setLoadingPullRequests(false);setError(String(issue))}})
      if(identity)void services.credentials.accountStatus(identity.host).then(status=>{if(epoch!==activeEpoch.current)return;setAccount(status);authenticationError.current=status.authenticated?'':status.error||`Connect your account for ${identity.host} to load pull requests.`;if(status.authenticated)configureGitHubAccount(status);else{setLoadingPullRequests(false);setError(authenticationError.current)}}).catch(issue=>{if(epoch===activeEpoch.current){authenticationError.current=issue instanceof Error?issue.message:String(issue);setLoadingPullRequests(false);setError(authenticationError.current)}})
      return
    }
    if(update.openPullRequestCount!==undefined)setOpenPullRequestCount(update.openPullRequestCount)
    if(update.pullRequests&&identity){const updated=new Map(update.pullRequests.map(raw=>[raw.number,raw]));setItems(prsRef.current.map(previous=>{const raw=updated.get(previous.number);return raw?mergeSummary(raw,identity):previous}))}
    if(update.pullRequest&&identity)applyDetail(mapPullRequest(update.pullRequest,identity))
    setError(update.error||authenticationError.current)
    setUpdatedAt(update.updatedAt)
  },[applyDetail,mergeSummary,resetPullRequestPage,setItems])
  const perform=useCallback(async<T,>(operation:()=>Promise<T>,epoch?:number):Promise<T>=>{
    const run=queue.current.then(async()=>{setBusy(true);try{if(epoch!==undefined&&epoch!==activeEpoch.current)throw new Error('The active repository changed. Run this action from its repository.');return await operation()}catch(issue){if(epoch===undefined||epoch===activeEpoch.current){const message=issue instanceof Error?issue.message:String(issue);setError(message);notify(message)}throw issue}finally{setBusy(false)}})
    queue.current=run.then(()=>undefined,()=>undefined)
    return run
  },[notify])
  const refresh=useCallback(async()=>{
    const repo=repoRef.current,epoch=activeEpoch.current;if(!repo)return
    const reload=account?.authenticated&&JSON.stringify(pageQueryRef.current)===JSON.stringify(requestedQuery.current)
    if(reload)resetPullRequestPage()
    await Promise.all([reload?loadPullRequestPage():Promise.resolve(),services.sync.refresh(repo.root).catch(issue=>{if(epoch===activeEpoch.current)setError(issue instanceof Error?issue.message:String(issue))})])
  },[account?.authenticated,loadPullRequestPage,resetPullRequestPage])
  const loadDetail=useCallback(async(number:number,force=false)=>{
    const repo=repoRef.current,identity=repo&&identityFor(repo);if(!repo||!identity||number<=0)return
    const epoch=activeEpoch.current
    const detail=mapPullRequest(await services.github.getPullRequest(identity.host,identity.owner,identity.repo,number),identity)
    if(epoch===activeEpoch.current)applyDetail(detail,force)
  },[applyDetail])
  const openRepository=useCallback(async(path?:string)=>{const intent=++openIntent.current;return perform(async()=>{
    if(intent!==openIntent.current)return
    const chosen=path??await services.workspace.chooseRepository();if(!chosen)return
    setOpeningRepository(chosen.split(/[\\/]/).filter(Boolean).pop()||chosen)
    try{
      const snapshot=await services.repository.open(chosen)
      if(intent!==openIntent.current)return
      const epoch=++activeEpoch.current
      detailRequests.current.clear();localMutationAt.current=0
      authenticationError.current=''
      repoRef.current=snapshot;setRepository(snapshot);enabledRef.current=true;setEnabled(true);setItems([]);setSelected(0);selectedRef.current=0;latestRevisions.current.clear();setError('');setAccount(null)
      resetPullRequestPage();setOpenPullRequestCount(null)
      const identity=identityFor(snapshot)
      setRepositoryCards(cards=>({...cards,[snapshot.root]:{...cards[snapshot.root],path:snapshot.root,name:snapshot.name,branch:snapshot.currentBranch,host:identity?.host,owner:identity?.owner,repo:identity?.repo}}))
      setLoadingPullRequests(!!identity)
      // Start the background worker immediately; local Git is ready before account/network requests finish.
      await services.sync.startWatch({path:snapshot.root,host:identity?.host??'',owner:identity?.owner??'',repo:identity?.repo??''})
      if(epoch!==activeEpoch.current)return
      void services.workspace.rememberRepository(snapshot.root).then(()=>services.workspace.recentRepositories()).then(setRecent).catch(issue=>setError(String(issue)))
      if(identity)void services.credentials.accountStatus(identity.host).then(status=>{
        if(epoch!==activeEpoch.current)return
        setAccount(status)
        authenticationError.current=status.authenticated?'':status.error||`Connect your account for ${identity.host} to load pull requests.`
        if(status.authenticated)configureGitHubAccount(status)
        else{setLoadingPullRequests(false);setError(authenticationError.current)}
      }).catch(issue=>{if(epoch===activeEpoch.current){authenticationError.current=issue instanceof Error?issue.message:String(issue);setLoadingPullRequests(false);setError(authenticationError.current)}})
      setUpdatedAt(new Date().toISOString());notify(`Opened ${snapshot.name}.`)
    }finally{setOpeningRepository('')}
  })},[notify,perform,resetPullRequestPage,setItems])
  const connect=useCallback(async(host:string,token?:string)=>{const epoch=activeEpoch.current;return perform(async()=>{
    const status=token?await services.credentials.saveToken(host,token):await services.credentials.accountStatus(host)
    if(epoch!==activeEpoch.current)return
    const activeIdentity=repoRef.current&&identityFor(repoRef.current)
    const isActiveAccount=!activeIdentity||activeIdentity.host.toLowerCase()===status.host.toLowerCase()
    if(isActiveAccount){setAccount(status);authenticationError.current=status.authenticated?'':status.error||`No account connected for ${host}. Run gh auth login --hostname ${host} or save a token.`}
    if(!status.authenticated)throw new Error(status.error||`No account connected for ${host}. Run gh auth login --hostname ${host} or save a token.`)
    if(isActiveAccount)configureGitHubAccount(status);setError('');if(repoRef.current&&isActiveAccount){setEnabled(true);if(account?.authenticated)await refresh();else await services.sync.refresh(repoRef.current.root)}notify(`Connected as ${status.login} on ${status.host}.`)
  },epoch)},[account?.authenticated,perform,refresh,notify])
  const selectPullRequest=useCallback((number:number)=>{
    selectedRef.current=number;setSelected(number)
    const repo=repoRef.current;if(!repo)return
    void services.sync.watchPullRequest(repo.root,number).catch(()=>{})
    if(!prsRef.current.find(pr=>pr.number===number)?.provider?.detailLoaded&&!detailRequests.current.has(number)){
      const epoch=activeEpoch.current
      const request=loadDetail(number).catch(issue=>{if(epoch===activeEpoch.current)setError(issue instanceof Error?issue.message:String(issue))}).finally(()=>{if(detailRequests.current.get(number)===request)detailRequests.current.delete(number)})
      detailRequests.current.set(number,request)
    }
  },[loadDetail])
  const updatePullRequest=useCallback(async(previous:PullRequest,next:PullRequest)=>{const epoch=activeEpoch.current;return perform(async()=>{
    let canonical:PullRequest
    try{canonical=await syncPullRequest(previous,next)}catch(issue){if(issue instanceof RemoteActionAppliedError&&epoch===activeEpoch.current){await loadDetail(previous.number).catch(()=>{});void refresh()}throw issue}
    if(epoch!==activeEpoch.current)return
    applyDetail(canonical);
    setItems(prsRef.current.map(p=>p.number===next.number?{...p,pendingComments:next.pendingComments,viewedFiles:next.viewedFiles}:p));
    if(canonical!==next)notify("Pull request updated.");
    const saved=prsRef.current.find(pr=>pr.number===next.number)
    if(saved)saveDraft(saved)
    setError('')
  },epoch)},[applyDetail,perform,setItems,notify,saveDraft,loadDetail,refresh])
  const acceptNewCommits=useCallback(async()=>{const epoch=activeEpoch.current,number=selectedRef.current;return perform(async()=>{await loadDetail(number,true);if(epoch===activeEpoch.current)notify('Review updated to the latest commits. Draft comments from earlier commits are retained for you to reposition.')},epoch)},[loadDetail,notify,perform])
  const merge=useCallback(async(pr:PullRequest,method:MergeMethod)=>{const epoch=activeEpoch.current;return perform(async()=>{
    const p=pr.provider;if(!p)throw new Error('This pull request is not connected to GitHub.')
    await services.github.mergePullRequest(p.host,p.owner,p.repo,p.number,method,pr.headSha)
    if(epoch!==activeEpoch.current)return
    setItems(prsRef.current.map(item=>item.number===pr.number?{...item,status:'merged'}:item))
    try{await loadDetail(pr.number,true)}catch{if(epoch===activeEpoch.current)setError(`Pull request #${pr.number} was merged, but its details could not be refreshed. Refresh to load them.`)}
    if(epoch!==activeEpoch.current)return
    await refresh();notify(`Pull request #${pr.number} merged.`)
  },epoch)},[loadDetail,notify,perform,refresh,setItems])
  const create=useCallback(async(title:string,body:string,base:string,head:string,draft:boolean)=>{const epoch=activeEpoch.current;const repo=repoRef.current,identity=repo&&identityFor(repo);return perform(async()=>{
    if(!identity)throw new Error('Open a repository with a GitHub remote first.')
    const result=await services.github.createPullRequest(identity.host,identity.owner,identity.repo,{title,body,base,head,draft})
    if(epoch!==activeEpoch.current)throw new Error(`Pull request #${result.number} was created in ${identity.owner}/${identity.repo}. Reopen that repository to view it.`)
    selectedRef.current=result.number;setSelected(result.number)
    setItems([mapPullRequestSummary(result,identity),...prsRef.current.filter(item=>item.number!==result.number)])
    try{await loadDetail(result.number,true)}catch{if(epoch===activeEpoch.current)setError(`Pull request #${result.number} was created, but its details could not be loaded. Refresh to load it; do not create it again.`)}
    if(epoch!==activeEpoch.current)throw new Error(`Pull request #${result.number} was created in ${identity.owner}/${identity.repo}. Reopen that repository to view it.`)
    await refresh();notify(`Created pull request #${result.number}.`);return result.number
  },epoch)},[loadDetail,notify,perform,refresh,setItems])
  const git=useCallback(async(action:GitAction,argument?:string|string[],remote?:string)=>{
    const repo=repoRef.current,epoch=activeEpoch.current
    return perform(async()=>{
      if(!repo)throw new Error('Open a Git repository first.')
      const target=remote??preferredRemote(repo)
      switch(action){
        case 'fetch':await services.repository.fetch(repo.root,target);break
        case 'pull':await services.repository.pull(repo.root,target);break
        case 'push':await services.repository.push(repo.root,target);break
        case 'checkout':await services.repository.checkout(repo.root,String(argument));break
        case 'checkoutPullRequest':{
          const identity=identityFor(repo),source=repo.remotes?.find(item=>item.host===identity?.host&&item.owner===identity?.owner&&item.repository===identity?.repo)?.name
          if(!source)throw new Error('This repository needs a matching GitHub remote to check out a pull request.')
          await services.repository.checkoutPullRequest(repo.root,Number(argument),source);break
        }
        case 'createBranch':await services.repository.createBranch(repo.root,String(argument));break
        case 'createAndCheckoutBranch':await services.repository.createAndCheckoutBranch(repo.root,String(argument));break
        case 'deleteBranch':await services.repository.deleteBranch(repo.root,String(argument));break
        case 'renameBranch':{const [branch,name]=argument as string[];await services.repository.renameBranch(repo.root,branch,name);break}
        case 'stage':await services.repository.stage(repo.root,argument as string[]);break
        case 'unstage':await services.repository.unstage(repo.root,argument as string[]);break
        case 'commit':await services.repository.commit(repo.root,String(argument));break
      }
      if(epoch!==activeEpoch.current)return
      // Mark the mutation before reloading: an older background poll must not
      // restore the pre-action branch/index while this request is in flight.
      localMutationAt.current=Date.now()
      let snapshot:RepositorySnapshot
      try{snapshot=await services.repository.snapshot(repo.root)}catch(issue){throw new Error(`The Git action completed, but the repository could not be refreshed. Refresh before repeating the action. ${issue instanceof Error?issue.message:String(issue)}`)}
      if(epoch!==activeEpoch.current)return
      repoRef.current=snapshot;setRepository(snapshot);setRepositoryCards(cards=>({...cards,[snapshot.root]:{...cards[snapshot.root],path:snapshot.root,name:snapshot.name,branch:snapshot.currentBranch}}));setError('')
      const messages:Partial<Record<GitAction,string>>={commit:'Commit created.',createBranch:'Branch created.',createAndCheckoutBranch:'New branch created and checked out.',deleteBranch:'Local branch deleted.',renameBranch:'Local branch renamed.',checkout:'Branch checked out.',checkoutPullRequest:'Pull request branch checked out.'}
      notify(messages[action]??(action==='push'?`Pushed to ${target}.`:action==='fetch'?`Fetched from ${target}.`:action==='pull'?`Pulled from ${target}.`:`${action[0].toUpperCase()+action.slice(1)} completed.`))
    },epoch)
  },[notify,perform])
  const diff=useCallback(async(path:string,staged:boolean):Promise<ChangedFile>=>{const repo=repoRef.current;if(!repo)throw new Error('Open a repository first.');return mapRepositoryDiff(await services.repository.diff(repo.root,path,staged))},[])
  const commitDiff=useCallback(async(sha:string)=>{const repo=repoRef.current;if(!repo)throw new Error('Open a repository first.');return {files:(await services.repository.commitDiff(repo.root,sha)).map(mapRepositoryDiff)}},[])
  const useDemo=useCallback(()=>{++activeEpoch.current;++openIntent.current;authenticationError.current='';enabledRef.current=false;repoRef.current=null;selectedRef.current=0;resetPullRequestPage();setRepository(null);setEnabled(false);setItems([]);setAccount(null);configureCurrentUser('noah');setError('')},[resetPullRequestPage,setItems])
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
  const inboxPullRequests=useMemo(()=>{const byNumber=new Map(prs.map(pr=>[pr.number,pr]));return pullRequestNumbers.flatMap(number=>{const pr=byNumber.get(number);return pr?[pr]:[]})},[prs,pullRequestNumbers])
  const loadMorePullRequests=useCallback(()=>loadPullRequestPage(true),[loadPullRequestPage])
  const retryPullRequests=useCallback(()=>loadPullRequestPage(pageNumbersRef.current.length>0),[loadPullRequestPage])
  return {available,enabled,repository,identity:repository?identityFor(repository):undefined,prs,inboxPullRequests,account,recent,repositoryCards,busy,openingRepository,loadingPullRequests,loadingMorePullRequests,hasMorePullRequests,pullRequestTotal,openPullRequestCount,pullRequestResultLimit,pullRequestPageError,setPullRequestQuery,loadMorePullRequests,retryPullRequests,error,updatedAt,selected,history:mapped?.history??emptyHistory,changes:mapped?.changes??emptyChanges,branch:mapped?.branch??'',openRepository,connect,selectPullRequest,updatePullRequest,acceptNewCommits,merge,create,git,diff,commitDiff,refresh,useDemo}
}
