import {test as base,expect} from '@playwright/test'
import {desktopFixture,openFixture,headSHA,baseSHA} from './desktopFixture'
import type {ReviewFileContent} from '../src/diffContext'
const test=base.extend<{desktop:Awaited<ReturnType<typeof desktopFixture>>}>({desktop:async({page},use)=>{const f=await desktopFixture(page);await use(f);expect(f.unexpected).toEqual([]);expect(f.pageErrors).toEqual([])}})
function contextFixture(desktop:Awaited<ReturnType<typeof desktopFixture>>){
 const old=Array.from({length:70},(_,i)=>`source line ${i+1}`),next=[...old];next[25]='changed source line 26'
 desktop.currentDetail.files=[{path:'src/feature.ts',previousPath:'',status:'modified',additions:1,deletions:1,patch:'@@ -25,3 +25,3 @@\n source line 25\n-source line 26\n+changed source line 26\n source line 27\n'}]
 const source:ReviewFileContent={path:'src/feature.ts',previousPath:'',headSha:headSHA,baseSha:baseSHA,mergeBaseSha:baseSHA,old:old.join('\n')+'\n',new:next.join('\n')+'\n',oldExists:true,newExists:true}
 desktop.handlers.set('main.GitHubService.GetReviewFile',()=>source);return source
}
test('PR context expands above and below in both layouts, keeping comments on reviewed lines',async({desktop,page})=>{
 contextFixture(desktop);await openFixture(desktop);await page.locator('.pr-list-item').first().click()
 const workspace=page.getByRole('region',{name:'Pull request changes'})
 await expect(workspace.locator('[data-code-side="new"][data-code-line="5"]')).toHaveCount(0)
 await workspace.getByRole('button',{name:'Load more lines above section 1'}).click()
 await expect(workspace.locator('[data-code-side="new"][data-code-line="5"]')).toContainText('source line 5')
 await expect(workspace.getByRole('button',{name:'Comment on changed line 5 in src/feature.ts',exact:true})).toHaveCount(0)
 await workspace.getByRole('button',{name:'Load more lines below section 1'}).click()
 await expect(workspace.locator('[data-code-side="new"][data-code-line="47"]')).toContainText('source line 47')
 await workspace.getByRole('button',{name:'Unified',exact:true}).click()
 await expect(workspace.locator('[data-code-side="new"][data-code-line="5"]')).toContainText('source line 5')
 await expect(workspace.locator('[data-code-side="new"][data-code-line="26"]')).toContainText('changed source line 26')
 await expect(workspace.getByRole('button',{name:'Comment on changed line 26 in src/feature.ts',exact:true})).toBeVisible()
 expect(desktop.calls.filter(c=>c.name.endsWith('.GetReviewFile'))).toHaveLength(1)
 expect(desktop.calls.find(c=>c.name.endsWith('.GetReviewFile'))?.args[0]).toEqual({host:'github.com',owner:'fork',repository:'app',pullRequest:12,headSha:headSHA,baseSha:baseSHA,path:'src/feature.ts'})
})
test('the separate full-file viewer is read only and switches exact source versions',async({desktop,page})=>{
 contextFixture(desktop);await openFixture(desktop);await page.locator('.pr-list-item').first().click();await page.getByRole('button',{name:'View entire file',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'Entire file · src/feature.ts'})
 await expect(dialog.getByLabel('Entire changed file')).toContainText('source line 1')
 await expect(dialog.locator('.cm-content')).toHaveAttribute('contenteditable','false')
 await dialog.getByLabel('Entire file version').selectOption('old')
 await expect(dialog.getByLabel('Entire original file')).toContainText('source line 1')
 await expect(dialog.getByLabel('Entire original file')).not.toContainText('changed source line 26')
 await page.keyboard.press('Escape');await expect(dialog).not.toBeVisible()
 await expect(page.getByRole('button',{name:'View entire file',exact:true})).toBeFocused()
})
test('source errors leave the reviewed patch intact and can be retried',async({desktop,page})=>{
 const source=contextFixture(desktop);desktop.handlers.set('main.GitHubService.GetReviewFile',()=>{throw new Error('The pull request base changed. Refresh and retry')})
 await openFixture(desktop);await page.locator('.pr-list-item').first().click();await page.getByRole('button',{name:'Load more lines above section 1'}).click()
 await expect(page.getByRole('alert').filter({hasText:'base changed'})).toBeVisible();await expect(page.locator('[data-code-side="new"][data-code-line="26"]')).toContainText('changed source line 26')
 desktop.handlers.set('main.GitHubService.GetReviewFile',()=>source);await page.getByRole('button',{name:'Load more lines above section 1'}).click();await expect(page.locator('[data-code-side="new"][data-code-line="5"]')).toBeVisible()
})
test('the expanded review header reserves macOS window controls at narrow and wide sizes',async({desktop,page})=>{
 await openFixture(desktop);await page.locator('.pr-list-item').first().click();await page.evaluate(()=>{(window as any)._wails.environment.OS='darwin';document.documentElement.dataset.platform='darwin';document.documentElement.style.setProperty('--native-titlebar-inset','80px')})
 await page.getByRole('button',{name:'Expand review to fill the window',exact:true}).click()
 for(const width of [1440,1024]){await page.setViewportSize({width,height:900});const header=await page.locator('.app-topbar').boundingBox(),back=await page.getByRole('button',{name:'Back to inbox',exact:true}).boundingBox();expect(header?.height).toBeGreaterThanOrEqual(50);expect(back!.x-header!.x).toBeGreaterThanOrEqual(80)}
 await page.keyboard.press('Escape');await expect(page.locator('.app-shell')).not.toHaveClass(/review-focused/)
})
