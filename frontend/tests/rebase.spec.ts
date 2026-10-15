import { test as base, expect } from '@playwright/test'
import { desktopFixture, openFixture, root, headSHA, baseSHA } from './desktopFixture'

const test=base.extend<{desktop:Awaited<ReturnType<typeof desktopFixture>>}>({desktop:async({page},use)=>{const fixture=await desktopFixture(page);await use(fixture);expect(fixture.unexpected).toEqual([]);expect(fixture.pageErrors).toEqual([])}})
test('rebase previews a fixed range and sends only the explicitly edited plan',async({desktop,page})=>{
  const repo=desktop.repositories.get(root)!;repo.changes=[]
  const commits=[...repo.history!].reverse()
  desktop.handlers.set('main.RepositoryService.Tags',()=>[])
  desktop.handlers.set('main.RepositoryService.PreviewRebase',args=>{expect(args[1]).toEqual({onto:baseSHA,upstream:'',expectedHead:headSHA,expectedBranch:'feature'});return{token:'rebase-preview-1',onto:baseSHA,upstream:baseSHA,head:headSHA,branch:'feature',commits}})
  desktop.handlers.set('main.RepositoryService.Rebase',args=>{expect(args).toEqual([root,{preview:{onto:baseSHA,upstream:'',expectedHead:headSHA,expectedBranch:'feature'},token:'rebase-preview-1',steps:[{sha:baseSHA,action:'reword',message:'feat: clear first message'},{sha:headSHA,action:'fixup',message:'feat: reviewed change'}]}]);return{headSha:'d'.repeat(40)}})
  await openFixture(desktop)
  await page.getByRole('button',{name:'Repository tools…'}).click()
  const dialog=page.getByRole('dialog',{name:'Repository tools'})
  await dialog.getByRole('button',{name:'Rebase',exact:true}).click()
  await dialog.getByRole('button',{name:'Preview rebase',exact:true}).click()
  await dialog.getByLabel('Action for Initial commit').selectOption('reword')
  await dialog.getByLabel('Message for Initial commit').fill('feat: clear first message')
  await dialog.getByLabel('Action for feat: reviewed change').selectOption('fixup')
  await dialog.getByRole('button',{name:'Rebase feature',exact:true}).click()
  await expect(dialog).toContainText('Rebase completed.')
})

test('a paused rebase edit allows message-only amend with the operation token',async({desktop,page})=>{
  const repo=desktop.repositories.get(root)!;repo.detached=true;repo.currentBranch='HEAD';repo.changes=[]
  repo.operation={kind:'rebase',token:'edit-1',sourceSha:headSHA,currentLabel:'Rebased result',sourceLabel:'Replayed commit',conflicts:0,message:'',canSkip:true,pausedForEdit:true}
  desktop.handlers.set('main.RepositoryService.Amend',args=>{expect(args).toEqual([root,{message:'fix: paused amendment',expectedHead:headSHA,operationToken:'edit-1'}]);return 'd'.repeat(40)})
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button',{name:/Changes/}).click()
  await page.getByLabel('Amend last commit',{exact:true}).check()
  await page.getByLabel('Commit message',{exact:true}).fill('fix: paused amendment')
  await page.getByRole('button',{name:'Amend last commit…',exact:true}).click()
  await page.getByRole('dialog',{name:'Amend last commit'}).getByRole('button',{name:'Amend commit',exact:true}).click()
  await expect(page.getByRole('dialog',{name:'Amend last commit'})).not.toBeVisible()
})
