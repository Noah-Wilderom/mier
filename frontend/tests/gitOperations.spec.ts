import { test as base, expect } from '@playwright/test'
import { desktopFixture, openFixture, root, headSHA, baseSHA, changedSHA, diff } from './desktopFixture'
import type { ConflictFile, GitOperation } from '../src/gitOperations'

const test=base.extend<{desktop:Awaited<ReturnType<typeof desktopFixture>>}>({desktop:async({page},use)=>{const fixture=await desktopFixture(page);await use(fixture);expect(fixture.unexpected).toEqual([]);expect(fixture.pageErrors).toEqual([])}})
const op:GitOperation={kind:'cherry-pick',token:'operation-1',sourceSha:changedSHA,currentLabel:'feature',sourceLabel:'main · fix: incoming',conflicts:1,message:'fix: incoming',canSkip:true}
function conflict(path='src/feature.ts'):ConflictFile {
  const version=(content:string)=>({exists:true,sha:headSHA,mode:'100644',content,binary:false})
  return {path,token:'conflict-1',base:version('const value = 1\n'),current:version('const value = 2\n'),incoming:version('const value = 3\n'),working:'before\n<<<<<<< HEAD\nconst value = 2\n=======\nconst value = 3\n>>>>>>> main\nafter\n',editable:true,reason:'',currentLabel:'feature',incomingLabel:'main · fix: incoming'}
}
function setupConflict(desktop:Awaited<ReturnType<typeof desktopFixture>>) {
  const repo=desktop.repositories.get(root)!
  repo.operation={...op};repo.changes=[{path:'src/feature.ts',status:'conflicted',indexStatus:'U',worktreeStatus:'U',staged:true,unstaged:true,conflict:true,untracked:false}]
  desktop.handlers.set('main.RepositoryService.Conflict',args=>conflict(args[1]))
  return repo
}

test('a conflict presents both sources, keeps the result editable, and continues only after saving',async({desktop,page})=>{
  const repo=setupConflict(desktop)
  desktop.handlers.set('main.RepositoryService.ResolveConflict',args=>{expect(args[1]).toEqual({path:'src/feature.ts',token:'conflict-1',choice:'result',content:'before\nconst value = 2\nconst value = 3\nafter\n'});repo.changes=repo.changes!.map(file=>({...file,conflict:false,unstaged:false,indexStatus:'M',worktreeStatus:'.'}));repo.operation={...op,conflicts:0}})
  desktop.handlers.set('main.RepositoryService.ControlOperation',args=>{expect(args).toEqual([root,op.token,'continue']);repo.operation=undefined;repo.changes=[];repo.headSha=changedSHA;return{headSha:changedSHA}})
  await openFixture(desktop)
  const banner=page.getByRole('region',{name:'Git operation in progress'})
  const bannerBox=await banner.boundingBox(), workspaceBox=await page.locator('.main-workspace').boundingBox()
  expect(bannerBox!.y+bannerBox!.height).toBeLessThanOrEqual(workspaceBox!.y+1)
  await expect(banner.getByRole('button',{name:'Continue cherry-pick'})).toBeDisabled()
  await banner.getByRole('button',{name:'Resolve conflicts'}).click()
  const editor=page.getByRole('region',{name:'Resolve conflict in src/feature.ts'})
  await expect(editor.getByLabel('Current conflict version')).toContainText('const value = 2')
  await expect(editor.getByLabel('Incoming conflict version')).toContainText('const value = 3')
  await expect(editor.getByRole('button',{name:'Save result and stage'})).toBeDisabled()
  await editor.getByRole('button',{name:'Accept both (current first)',exact:true}).click()
  await expect(editor.getByLabel('Merged result')).toHaveValue('before\nconst value = 2\nconst value = 3\nafter\n')
  await editor.getByRole('button',{name:'Save result and stage'}).click()
  await expect(editor).not.toBeVisible()
  await banner.getByRole('button',{name:'Continue cherry-pick'}).click()
  await expect(banner).not.toBeVisible()
  await expect(page.getByText('Working tree is clean')).toBeVisible()
})

test('stale conflict saves retain manual edits and navigation retains the unsaved result',async({desktop,page})=>{
  setupConflict(desktop)
  desktop.handlers.set('main.RepositoryService.ResolveConflict',()=>{throw new Error('This conflict changed on disk. Reload it before saving')})
  await openFixture(desktop)
  await page.getByRole('button',{name:'Resolve conflicts',exact:true}).click()
  const editor=page.getByRole('region',{name:'Resolve conflict in src/feature.ts'})
  await editor.getByLabel('Merged result').fill('carefully merged\n')
  await editor.getByRole('button',{name:'Save result and stage'}).click()
  await expect(editor.getByRole('alert')).toContainText('changed on disk')
  await expect(editor.getByLabel('Merged result')).toHaveValue('carefully merged\n')
  await page.locator('.sidebar').getByRole('button',{name:/History/}).click()
  await page.locator('.sidebar').getByRole('button',{name:/Changes/}).click()
  await expect(editor.getByLabel('Merged result')).toHaveValue('carefully merged\n')
  await editor.getByRole('button',{name:'Reload file'}).click()
  await expect(editor.getByRole('alert')).toContainText('discard your unsaved result')
  await editor.getByRole('button',{name:'Keep editing'}).click()
  await expect(editor.getByLabel('Merged result')).toHaveValue('carefully merged\n')
})

test('abort requires a reviewable confirmation and duplicate clicks cannot issue two commands',async({desktop,page})=>{
  const repo=setupConflict(desktop)
  let release!:()=>void
  desktop.handlers.set('main.RepositoryService.ControlOperation',async args=>{expect(args[2]).toBe('abort');await new Promise<void>(resolve=>{release=resolve});repo.operation=undefined;repo.changes=[];return{headSha:headSHA}})
  await openFixture(desktop)
  const banner=page.getByRole('region',{name:'Git operation in progress'})
  await banner.getByRole('button',{name:'Abort…',exact:true}).click()
  expect(desktop.calls.filter(call=>call.name.endsWith('.ControlOperation'))).toHaveLength(0)
  await banner.getByRole('button',{name:'Abort operation',exact:true}).evaluate(button=>{(button as HTMLButtonElement).click();(button as HTMLButtonElement).click()})
  await expect.poll(()=>desktop.calls.filter(call=>call.name.endsWith('.ControlOperation')).length).toBe(1)
  release();await expect(banner).not.toBeVisible()
})

test('branch history filters commits and cherry-pick carries immutable destination guards',async({desktop,page})=>{
  const repo=desktop.repositories.get(root)!;repo.changes=[]
  desktop.handlers.set('main.RepositoryService.BranchCommits',args=>{expect(args).toEqual([root,'main',baseSHA]);return[repo.history![1]]})
  let release!:()=>void
  desktop.handlers.set('main.RepositoryService.CherryPick',async args=>{expect(args[1]).toEqual({sha:baseSHA,expectedHead:headSHA,expectedBranch:'feature',mainline:0});await new Promise<void>(resolve=>{release=resolve});repo.headSha=changedSHA;return{headSha:changedSHA}})
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button',{name:/History/}).click()
  await page.getByLabel('History branch').selectOption('main')
  await expect(page.locator('.commit-inspector h2')).toHaveText('Initial commit')
  await page.getByRole('button',{name:'Cherry-pick commit…'}).click()
  const dialog=page.getByRole('dialog',{name:'Cherry-pick commit'})
  await expect(dialog).toContainText('Initial commit')
  await dialog.getByRole('button',{name:'Cherry-pick onto feature'}).evaluate(button=>{(button as HTMLButtonElement).click();(button as HTMLButtonElement).click()})
  await expect.poll(()=>desktop.calls.filter(call=>call.name.endsWith('.CherryPick')).length).toBe(1)
  release();await expect(dialog).not.toBeVisible()
})

test('a merge preview applies the selected local branch and opens conflict resolution when paused',async({desktop,page})=>{
  const repo=desktop.repositories.get(root)!;repo.changes=[]
  desktop.handlers.set('main.RepositoryService.MergeBranch',args=>{expect(args[1]).toEqual({branch:'main',expectedSource:baseSHA,expectedHead:headSHA,expectedBranch:'feature'});setupConflict(desktop);repo.operation={...op,kind:'merge',canSkip:false};return{headSha:headSHA,operation:repo.operation}})
  desktop.handlers.set('main.RepositoryService.Conflict',()=>conflict())
  await openFixture(desktop)
  await page.getByTitle('Manage branches').click()
  await page.getByRole('button',{name:'Merge main into feature',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'Merge local branch'})
  await expect(dialog.getByLabel('Diff for src/feature.ts')).toBeVisible()
  await dialog.getByRole('button',{name:'Merge into feature',exact:true}).click()
  await expect(page.getByRole('region',{name:'Resolve conflict in src/feature.ts'})).toBeVisible()
  await expect(page.getByRole('button',{name:'Continue merge'})).toBeDisabled()
})

test('partial staging uses the displayed patch and keeps commit drafts on failure',async({desktop,page})=>{
  desktop.handlers.set('main.RepositoryService.StageHunk',args=>{expect(args[1]).toEqual({path:'src/feature.ts',staged:false,hunk:0,expectedPatch:diff().rawPatch});throw new Error('This diff changed. Refresh it before staging a section')})
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button',{name:/Changes/}).click()
  await page.getByLabel('Commit message').fill('fix: keep my draft')
  await page.getByRole('button',{name:'Stage section',exact:true}).click()
  await expect(page.locator('.local-action-error')).toContainText('This diff changed')
  await expect(page.getByLabel('Commit message')).toHaveValue('fix: keep my draft')
  await page.getByLabel('Filter changed files').fill('missing')
  await expect(page.locator('.staging-file')).toHaveCount(0)
})
