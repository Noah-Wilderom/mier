import { test as base, expect } from '@playwright/test'
import { desktopFixture, openFixture, root, headSHA } from './desktopFixture'

const test=base.extend<{desktop:Awaited<ReturnType<typeof desktopFixture>>}>({desktop:async({page},use)=>{const fixture=await desktopFixture(page);await use(fixture);expect(fixture.unexpected).toEqual([]);expect(fixture.pageErrors).toEqual([])}})
const linked='/fixture/linked-worktree'
test('worktrees create a separate directory and removal confirms its exact path',async({desktop,page})=>{
  let created=false,removed=false
  desktop.handlers.set('main.RepositoryService.Tags',()=>[])
  desktop.handlers.set('main.RepositoryService.Worktrees',()=>[{path:root,sha:headSHA,branch:'feature',token:'main-1',primary:true,current:true,locked:false,lockReason:'',prunable:false,changes:1,error:''},...(created&&!removed?[{path:linked,sha:headSHA,branch:'feature/isolated',token:'linked-1',primary:false,current:false,locked:false,lockReason:'',prunable:false,changes:0,error:''}]:[])])
  desktop.handlers.set('main.RepositoryService.MutateWorktree',args=>{const request=args[1];expect(args[0]).toBe(root);expect(request.path).toBe(linked);if(request.action==='add'){expect(request.mode).toBe('new');expect(request.branch).toBe('feature/isolated');expect(request.startSha).toBe(headSHA);expect(request.expectedHead).toBe(headSHA);created=true}else{expect(request.action).toBe('remove');expect(request.token).toBe('linked-1');removed=true}})
  await openFixture(desktop)
  await page.getByRole('button',{name:'Repository tools…'}).click()
  const dialog=page.getByRole('dialog',{name:'Repository tools'})
  await dialog.getByRole('button',{name:'Worktrees',exact:true}).click()
  await dialog.getByLabel('Worktree directory').fill(linked)
  await dialog.getByLabel('New worktree branch').fill('feature/isolated')
  await dialog.getByRole('button',{name:'Create worktree',exact:true}).click()
  await dialog.getByRole('button',{name:`Remove worktree ${linked}`,exact:true}).click()
  await expect(dialog.locator('.repository-tool-confirm')).toContainText(linked)
  expect(desktop.calls.filter(call=>call.name.endsWith('.MutateWorktree'))).toHaveLength(1)
  await dialog.getByRole('button',{name:'Remove worktree',exact:true}).click()
  await expect(dialog.getByRole('button',{name:`Remove worktree ${linked}`,exact:true})).not.toBeVisible()
})
