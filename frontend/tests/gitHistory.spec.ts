import { test as base, expect } from '@playwright/test'
import { desktopFixture, openFixture, root, baseSHA } from './desktopFixture'

const test=base.extend<{desktop:Awaited<ReturnType<typeof desktopFixture>>}>({desktop:async({page},use)=>{const fixture=await desktopFixture(page);await use(fixture);expect(fixture.unexpected).toEqual([]);expect(fixture.pageErrors).toEqual([])}})

test('history search and pagination retain loaded rows and use the selected search field',async({desktop,page})=>{
  const commits=desktop.repositories.get(root)!.history!
  desktop.handlers.set('main.RepositoryService.HistoryPage',args=>{
    const query=args[1]
    if(query.query){expect(query.query).toBe('Reviewer');expect(query.search).toBe('author');return {commits:[commits[0]],nextCursor:''}}
    return query.cursor?{commits:[commits[1]],nextCursor:''}:{commits:[commits[0]],nextCursor:'next-1'}
  })
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button',{name:/History/}).click()
  await page.getByRole('button',{name:'Load older commits'}).click()
  await expect(page.locator('.history-workspace > .history-pagination')).toContainText('2 commits loaded')
  await page.getByLabel('Search Git history').fill('Reviewer')
  await page.getByLabel('Search history by').selectOption('author')
  await page.locator('.history-search').getByRole('button',{name:'Search',exact:true}).click()
  await expect(page.locator('.history-workspace > .history-pagination')).toContainText('1 commits loaded')
})

test('file history opens beside its revision list and resolves renamed file paths for blame',async({desktop,page})=>{
  const commits=desktop.repositories.get(root)!.history!
  desktop.handlers.set('main.RepositoryService.HistoryPage',args=>({commits:args[1].path?commits.map((commit,index)=>({...commit,filePath:index?'src/old-feature.ts':'src/feature.ts'})):commits,nextCursor:''}))
  desktop.handlers.set('main.RepositoryService.RevisionFile',args=>({path:args[2],sha:args[1],content:args[1]===baseSHA?'old content\n':'new content\n',binary:false}))
  desktop.handlers.set('main.RepositoryService.Blame',args=>{expect(args).toEqual([root,baseSHA,'src/old-feature.ts']);return[{sha:baseSHA,line:1,originalLine:1,text:'old content',author:'Reviewer',createdAt:'2026-10-15T10:00:00Z',summary:'Initial commit'}]})
  await openFixture(desktop)
  await page.locator('.sidebar').getByRole('button',{name:/History/}).click()
  await page.locator('.commit-changes').getByRole('button',{name:'feature.ts…'}).click()
  const dialog=page.getByRole('dialog',{name:'File history',exact:true})
  await expect(dialog).toContainText('new content')
  await dialog.getByRole('button',{name:/Initial commit/}).click()
  await expect(dialog).toContainText('old content')
  await dialog.getByLabel('Show blame').check()
  await expect(dialog.locator('.blame-lines')).toContainText('Reviewer')
})
