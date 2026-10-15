import { test as base, expect } from '@playwright/test'
import { desktopFixture, openFixture, root, headSHA, baseSHA } from './desktopFixture'

const test=base.extend<{desktop:Awaited<ReturnType<typeof desktopFixture>>}>({desktop:async({page},use)=>{const fixture=await desktopFixture(page);await use(fixture);expect(fixture.unexpected).toEqual([]);expect(fixture.pageErrors).toEqual([])}})

test('tags carry their original object ID when renamed and need confirmation before deletion',async({desktop,page})=>{
  let name='v1'
  desktop.handlers.set('main.RepositoryService.Tags',()=>[{name,object:baseSHA,commit:headSHA,message:'Release',createdAt:'2026-10-15T12:00:00Z',annotated:true}])
  desktop.handlers.set('main.RepositoryService.MutateTag',args=>{expect(args[1].expectedObject).toBe(baseSHA);expect(args[1].action).toBe('rename');expect(args[1].name).toBe('v1');name=args[1].newName})
  await openFixture(desktop)
  await page.getByRole('button',{name:'Repository tools…'}).click()
  const dialog=page.getByRole('dialog',{name:'Repository tools',exact:true})
  await dialog.getByRole('button',{name:'rename tag v1',exact:true}).click()
  await dialog.getByLabel('New tag name').fill('v2')
  await dialog.getByRole('button',{name:'Rename tag',exact:true}).click()
  await expect(dialog.getByText('v2',{exact:true})).toBeVisible()
  await dialog.getByRole('button',{name:'delete tag v2',exact:true}).click()
  expect(desktop.calls.filter(call=>call.name.endsWith('.MutateTag'))).toHaveLength(1)
  await dialog.getByRole('button',{name:'Cancel',exact:true}).click()
})

test('activity recovers into a new branch without resetting the current checkout',async({desktop,page})=>{
  desktop.handlers.set('main.RepositoryService.Tags',()=>[])
  desktop.handlers.set('main.RepositoryService.Checkpoints',()=>[{ref:'refs/mier/checkpoints/one',sha:baseSHA,label:'amend',createdAt:'2026-10-15T12:00:00Z'}])
  desktop.handlers.set('main.RepositoryService.Reflog',()=>[])
  desktop.handlers.set('main.RepositoryService.RecoverBranch',args=>{expect(args).toEqual([root,{sha:baseSHA,name:'recovery/work'}])})
  await openFixture(desktop)
  await page.getByRole('button',{name:'Repository tools…'}).click()
  const dialog=page.getByRole('dialog',{name:'Repository tools',exact:true})
  await dialog.getByRole('button',{name:'Activity & recovery',exact:true}).click()
  await dialog.getByRole('button',{name:'Recover…',exact:true}).click()
  await dialog.getByLabel('New recovery branch').fill('recovery/work')
  await dialog.getByRole('button',{name:'Create recovery branch',exact:true}).click()
  await expect.poll(()=>desktop.calls.filter(call=>call.name.endsWith('.RecoverBranch')).length).toBe(1)
  expect(desktop.calls.filter(call=>call.name.endsWith('.Reset'))).toHaveLength(0)
})

test('hard reset requires a file preview and explicit acknowledgement',async({desktop,page})=>{
  const repo=desktop.repositories.get(root)!;repo.changes=[]
  desktop.handlers.set('main.RepositoryService.Reset',args=>{expect(args).toEqual([root,{sha:headSHA,mode:'hard',expectedHead:headSHA,expectedBranch:'feature'}]);return{headSha:headSHA}})
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button',{name:/History/}).click()
  await page.getByRole('button',{name:'Reset to commit…'}).click()
  const dialog=page.getByRole('dialog',{name:'Reset branch',exact:true})
  await dialog.getByLabel('Reset mode').selectOption('hard')
  await expect(dialog.getByLabel('Diff for src/feature.ts')).toBeVisible()
  await expect(dialog.getByRole('button',{name:'Reset branch',exact:true})).toBeDisabled()
  await dialog.getByLabel('Replace tracked files with this commit').check()
  await dialog.getByRole('button',{name:'Reset branch',exact:true}).click()
  await expect(dialog).not.toBeVisible()
})

test('selected commits can be reordered before a multiple cherry-pick',async({desktop,page})=>{
  desktop.repositories.get(root)!.changes=[]
  desktop.handlers.set('main.RepositoryService.CherryPickMany',args=>{expect(args).toEqual([root,{shas:[headSHA,baseSHA],expectedHead:headSHA,expectedBranch:'feature'}]);return{headSha:headSHA}})
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button',{name:/History/}).click()
  const list=page.getByRole('listbox',{name:'Repository commits'})
  await list.getByRole('option',{name:/feat: reviewed change/}).click({modifiers:['Control']})
  await list.getByRole('option',{name:/Initial commit/}).click({modifiers:['Shift']})
  await page.getByRole('button',{name:'Cherry-pick selected (2)…'}).click()
  const dialog=page.getByRole('dialog',{name:'Cherry-pick selected commits',exact:true})
  await dialog.getByRole('button',{name:'Move feat: reviewed change earlier',exact:true}).click()
  await dialog.getByRole('button',{name:'Cherry-pick 2 commits',exact:true}).click()
  await expect(dialog).not.toBeVisible()
})
