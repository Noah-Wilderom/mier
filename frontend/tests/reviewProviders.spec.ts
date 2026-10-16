import {test as base,expect} from '@playwright/test'
import {desktopFixture,openFixture,headSHA,root,summary} from './desktopFixture'
import type {ReviewCapabilities,ReviewConnection,ReviewProviderKind} from '../src/services'

const test=base.extend<{desktop:Awaited<ReturnType<typeof desktopFixture>>}>({desktop:async({page},use)=>{const desktop=await desktopFixture(page);await use(desktop);expect(desktop.unexpected).toEqual([]);expect(desktop.pageErrors).toEqual([])}})
const writeCaps:ReviewCapabilities={comments:true,inlineComments:true,approve:true,requestChanges:true,resolveThreads:true,draft:true,merge:true,reviewers:true,assignees:true,labels:true,retarget:true,actions:false,reopen:true}
async function nativeRepository(desktop:Awaited<ReturnType<typeof desktopFixture>>,kind:Exclude<ReviewProviderKind,'github'>,readOnly=false){
  const host=kind==='azure'?'dev.azure.com':kind==='bitbucket'?'bitbucket.org':'gitlab.company.test',owner=kind==='azure'?'org/Shared Project':kind==='gitlab'?'team/nested':'team'
  const repo=desktop.repositories.get(root)!;repo.currentBranch='main';repo.upstream='origin/main';repo.remotes=[{...repo.remotes![0],host,owner,provider:kind,repository:'app'}]
  const raw={...summary(),provider:kind,url:kind==='gitlab'?`https://${host}/${owner}/app/-/merge_requests/12`:kind==='azure'?`https://${host}/${owner}/_git/app/pullrequest/12`:`https://${host}/${owner}/app/pull-requests/12`,head:{...summary().head,repository:owner+'/app'},base:{...summary().base,repository:owner+'/app'}}
  desktop.prs=[raw];desktop.currentDetail.pullRequest=raw;desktop.currentDetail.capabilities={...writeCaps,...(kind==='gitlab'?{requestChanges:false}:{}),...(kind==='bitbucket'?{merge:false,labels:false,assignees:false,reopen:false}:{})}
  if(readOnly){desktop.currentDetail.capabilities=Object.fromEntries(Object.keys(writeCaps).map(key=>[key,false])) as unknown as ReviewCapabilities;desktop.currentDetail.viewerCanUpdate=false}
  desktop.handlers.set('main.CredentialService.AccountStatus',()=>({host,provider:kind,authenticated:true,login:'reviewer',name:'Reviewer',avatarUrl:'',source:'saved',readOnly}))
  desktop.handlers.set('main.GitHubService.ListPullRequestsPage',()=>({items:desktop.prs,totalCount:-1,openCount:-1,hasNextPage:false,nextCursor:''}))
  desktop.handlers.set('main.WorkspaceSyncService.Refresh',async()=>{await desktop.emit({host,owner,repo:'app',openPullRequestCount:-1});return null})
  await openFixture(desktop,false);await desktop.emit({host,owner,repo:'app',openPullRequestCount:-1})
  return {host,owner}
}

for(const [kind,label] of [['gitlab','GitLab'],['bitbucket','Bitbucket'],['azure','Azure DevOps']] as const){
  test(`${label} reviews preserve identity and guarded checkout`,async({page,desktop})=>{
    const identity=await nativeRepository(desktop,kind)
    desktop.handlers.set('main.RepositoryService.CheckoutReviewedPullRequest',args=>{expect(args).toEqual([root,12,'origin',headSHA]);return null})
    await page.locator('.pr-list-item').filter({hasText:'Review 12'}).click()
    await expect(page.locator('.review-workspace')).toBeVisible()
    await expect(page.getByRole('button',{name:`Open in ${label}`,exact:true})).toBeVisible()
    await expect(page.locator('.repository-pr-count').first()).not.toContainText('-1')
    expect(desktop.calls.filter(call=>call.name==='main.WorkspaceSyncService.StartWatch')).toHaveLength(1)
    const detailCall=desktop.calls.find(call=>call.name==='main.GitHubService.GetPullRequest')!
    expect(detailCall.args.slice(0,3)).toEqual([identity.host,identity.owner,'app'])
    await page.getByRole('button',{name:'Checkout branch',exact:true}).click()
    await expect.poll(()=>desktop.calls.some(call=>call.name==='main.RepositoryService.CheckoutReviewedPullRequest')).toBe(true)
    await page.getByRole('button',{name:/^Finish review/}).click()
    const requestChanges=page.locator('#review-decision option[value="changes_requested"]')
    if(kind==='gitlab')await expect(requestChanges).toBeDisabled();else await expect(requestChanges).toBeEnabled()
  })
}

test('read-only provider reviews keep source browsing and disable writes',async({page,desktop})=>{
  await nativeRepository(desktop,'gitlab',true)
  await page.locator('.pr-list-item').filter({hasText:'Review 12'}).click()
  await expect(page.locator('.review-code-text').first()).toBeVisible()
  await expect(page.getByRole('button',{name:/^Finish review/})).toBeDisabled()
  await page.getByRole('tab',{name:/^Conversation /}).click()
  await expect(page.getByRole('textbox',{name:'Add a conversation comment'})).toHaveCount(0)
  await expect(page.getByRole('button',{name:'Merge pull request',exact:true})).toHaveCount(0)
  expect(desktop.calls.filter(call=>/SubmitReview|CreateReviewComment|MergePullRequest/.test(call.name))).toEqual([])
})

test('provider settings validate tokens without browser persistence and disconnect explicitly',async({page,desktop})=>{
  let connections:ReviewConnection[]=[]
  desktop.handlers.set('main.ReviewProviderService.Connections',()=>connections)
  desktop.handlers.set('main.ReviewProviderService.Connect',args=>{expect(args[0]).toMatchObject({kind:'gitlab',host:'gitlab.company.test',readOnly:true,token:'synthetic-account-token'});connections=[{kind:'gitlab',host:'gitlab.company.test',login:'reviewer',name:'Reviewer',username:'',organization:'',readOnly:true,avatarUrl:'',disabled:false}];return {host:'gitlab.company.test',authenticated:true,login:'reviewer',source:'saved',provider:'gitlab',readOnly:true}})
  desktop.handlers.set('main.ReviewProviderService.Disconnect',args=>{expect(args).toEqual(['gitlab.company.test']);connections=[];return null})
  await openFixture(desktop)
  await page.getByRole('button',{name:'Settings',exact:true}).click()
  const card=page.getByRole('region',{name:'GitLab account',exact:true})
  await card.getByRole('button',{name:'Connect GitLab',exact:true}).click()
  await card.getByRole('textbox',{name:'GitLab host',exact:true}).fill('gitlab.company.test')
  await card.getByLabel('Personal access token',{exact:true}).fill('synthetic-account-token')
  await card.getByRole('checkbox',{name:'Read-only connection'}).check()
  await card.getByRole('button',{name:'Validate and connect',exact:true}).click()
  await expect(card.locator('.review-provider-account')).toContainText('Read-only')
  expect(await page.evaluate(()=>JSON.stringify({...localStorage}))).not.toContain('synthetic-account-token')
  await card.getByRole('button',{name:'Disconnect',exact:true}).click()
  expect(desktop.calls.some(call=>call.name==='main.ReviewProviderService.Disconnect')).toBe(false)
  await card.getByRole('button',{name:'Disconnect account',exact:true}).click()
  await expect(card.locator('.review-provider-account')).toHaveCount(0)
})

test('GitLab inline review carries stable draft IDs and the displayed head',async({page,desktop})=>{
  await nativeRepository(desktop,'gitlab')
  desktop.handlers.set('main.GitHubService.SubmitReview',args=>{const input=args[4];expect(input.commitId).toBe(headSHA);expect(input.event).toBe('COMMENT');expect(input.comments).toHaveLength(1);expect(input.comments[0]).toMatchObject({body:'Check this value',path:'src/feature.ts',line:1,side:'RIGHT'});expect(input.comments[0].clientId).toBeTruthy();return {id:99,user:{login:'reviewer'},body:'Summary',state:'COMMENTED',commitId:headSHA,submittedAt:new Date().toISOString()}})
  await page.locator('.pr-list-item').filter({hasText:'Review 12'}).click()
  await page.getByRole('button',{name:'Unified',exact:true}).click()
  await page.locator('.review-unified-row.diff-add').getByRole('button',{name:'Comment on changed line 1 in src/feature.ts',exact:true}).click()
  await page.locator('#new-review-comment').fill('Check this value')
  await page.locator('.review-inline-composer').getByRole('button',{name:'Add to review',exact:true}).click()
  await page.getByRole('button',{name:/^Finish review/}).click()
  await page.locator('#review-summary').fill('Summary')
  await page.getByRole('button',{name:'Submit review',exact:true}).click()
  await expect.poll(()=>desktop.calls.some(call=>call.name==='main.GitHubService.SubmitReview')).toBe(true)
})
