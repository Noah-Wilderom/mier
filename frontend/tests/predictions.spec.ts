import {test as base,expect} from '@playwright/test'
import {desktopFixture,openFixture,headSHA} from './desktopFixture'
import type {PredictionRequest,PredictionResult,PredictionStatus} from '../src/predictionTypes'
const test=base.extend<{desktop:Awaited<ReturnType<typeof desktopFixture>>}>({desktop:async({page},use)=>{const f=await desktopFixture(page);await use(f);expect(f.unexpected).toEqual([]);expect(f.pageErrors).toEqual([])}})
function assessment(request:PredictionRequest):PredictionResult{
 return {id:'assessment-a',kind:request.kind,model:'jev-1.13.0',questionVersion:'mier-jev-1',headSha:request.headSha,baseSha:request.baseSha,createdAt:new Date().toISOString(),cached:false,durationMs:240,usage:{input_tokens:2400,output_tokens:0},warnings:['Scores prioritize attention; they do not establish correctness.'],rows:request.kind==='tickets'?[{id:'criterion_0',label:'Preserve the original requester',ticketProvider:'jira',ticketKey:'ENG-42',answer:{type:'choice',choice:'verify',confidence:.4,probabilities:{verify:.7,appears_addressed:.3}},options:{verify:'Insufficient evidence or testing to judge coverage',appears_addressed:'Changes appear to address this requirement'}}]:[{id:'file_0_attention',label:'Review attention',path:'src/feature.ts',answer:{type:'score',score:.5,confidence:.7,probabilities:{'0':.5,'1':.5,'2':0}}},{id:'file_1_attention',label:'Review attention',path:'src/security.ts',answer:{type:'score',score:1.9,confidence:.8,probabilities:{'0':0,'1':.1,'2':.9}}},{id:'file_1_specialty',label:'Suggested specialist',path:'src/security.ts',answer:{type:'choice',choice:'authentication',confidence:.8,probabilities:{authentication:.9,general:.1}},options:{authentication:'Authentication and authorization',general:'General review'}}]}
}
async function openInsights(desktop:Awaited<ReturnType<typeof desktopFixture>>){await openFixture(desktop);await desktop.page.locator('.pr-list-item').first().click();await desktop.page.getByRole('button',{name:'Review priorities and ticket coverage'}).click();return desktop.page.getByRole('region',{name:'Review priorities and ticket coverage'})}
function enable(desktop:Awaited<ReturnType<typeof desktopFixture>>){desktop.handlers.set('main.PredictionService.GetStatus',()=>({configured:true,enabled:true,model:'jev-1.13.0',error:''}));desktop.handlers.set('main.PredictionService.Evaluate',args=>assessment(args[0]));desktop.handlers.set('main.PredictionService.Feedback',()=>null)}
test('Jev requires explicit connection and repository consent before sharing source',async({desktop,page})=>{
 let status:PredictionStatus={configured:false,enabled:false,model:'jev-1.13.0',error:''}
 desktop.handlers.set('main.PredictionService.GetStatus',()=>status)
 desktop.handlers.set('main.PredictionService.Connect',args=>{expect(args).toEqual(['fixture-only-key']);status={...status,configured:true};return[{name:status.model,description:'Fixture',release_date:'2026-09-15'}]})
 desktop.handlers.set('main.PredictionService.SaveSettings',args=>{expect(args).toEqual([{host:'github.com',owner:'fork',repo:'app'},status.model,true]);status={...status,enabled:true};return null})
 desktop.handlers.set('main.PredictionService.Evaluate',args=>assessment(args[0]))
 const insights=await openInsights(desktop);await expect(insights.getByRole('button',{name:'Start assessment',exact:true})).toBeDisabled()
 await insights.getByLabel('TypeSafe API key').fill('fixture-only-key');await insights.getByRole('button',{name:'Connect TypeSafe',exact:true}).click()
 await expect(insights.getByLabel('TypeSafe API key')).toHaveValue('');await expect(insights.getByRole('button',{name:'Start assessment',exact:true})).toBeDisabled()
 await insights.getByLabel(/Allow Jev assessments for/).check();await insights.getByRole('button',{name:'Save Jev settings',exact:true}).click();await insights.getByRole('button',{name:'Start assessment',exact:true}).click()
 await expect(insights).toContainText('2,400'.replace(',',''));expect(desktop.calls.filter(c=>c.name.endsWith('.Evaluate'))).toHaveLength(1)
 const stored=await page.evaluate(()=>JSON.stringify({...localStorage}));expect(stored).not.toContain('fixture-only-key')
})
test('review priorities keep every file and offer opt-in ordering with provider usage and feedback',async({desktop,page})=>{
 enable(desktop);desktop.currentDetail.files!.push({path:'src/security.ts',previousPath:'',status:'modified',additions:1,deletions:1,patch:'@@ -1 +1 @@\n-old\n+new\n'})
 const insights=await openInsights(desktop);expect(desktop.calls.filter(c=>c.name.endsWith('.Evaluate'))).toHaveLength(0)
 await insights.getByRole('button',{name:'Start assessment',exact:true}).click();await expect(insights).toContainText('Authentication and authorization');await expect(insights).toContainText('2400 input / 0 output tokens');await expect(insights).toContainText('Charge not reported')
 const files=page.getByRole('complementary',{name:'Changed files'});await expect(files.locator('.review-file-name')).toHaveText(['feature.ts','security.ts']);await files.getByLabel('Suggested review order').check();await expect(files.locator('.review-file-name')).toHaveText(['security.ts','feature.ts'])
 await insights.getByRole('button',{name:'Useful',exact:true}).click();await expect(insights).toContainText('Saved for this app session')
 await insights.getByRole('button',{name:'View code',exact:true}).last().click();await expect(page.locator('.review-file-heading')).toContainText('src/security.ts')
 expect(desktop.calls.filter(c=>/SubmitReview|Stage|Commit|RequestReviewers/.test(c.name))).toHaveLength(0)
})
test('ticket acceptance coverage is selected explicitly and does not update tickets',async({desktop})=>{
 enable(desktop);const insights=await openInsights(desktop);await insights.getByLabel('PR Jev assessment').selectOption('tickets');await insights.getByRole('button',{name:'Start assessment',exact:true}).click();await expect(insights).toContainText('jira · ENG-42 · Preserve the original requester');await expect(insights).toContainText('Insufficient evidence or testing')
 expect(desktop.calls.find(c=>c.name.endsWith('.Evaluate'))?.args[0]).toMatchObject({kind:'tickets',headSha:headSHA});expect(desktop.calls.filter(c=>/SelectJiraTicket|SelectLinearTicket|Update.*Issue/.test(c.name))).toHaveLength(0)
})
test('switching assessments discards late responses and model failures preserve code review',async({desktop,page})=>{
 enable(desktop);let release!:()=>void
 desktop.handlers.set('main.PredictionService.Evaluate',async args=>{if(args[0].kind==='review'){await new Promise<void>(resolve=>{release=resolve});return assessment(args[0])}throw new Error('TypeSafe rate limit reached. Wait before retrying')})
 const insights=await openInsights(desktop);await insights.getByRole('button',{name:'Start assessment',exact:true}).click();await expect(insights.getByRole('button',{name:'Cancel assessment'})).toBeVisible()
 await insights.getByLabel('PR Jev assessment').selectOption('tickets');await insights.getByRole('button',{name:'Start assessment',exact:true}).click();await expect(insights.getByRole('alert')).toContainText('rate limit');release()
 await expect(insights).not.toContainText('Authentication and authorization');await expect(page.locator('[data-code-side="new"][data-code-line="1"]')).toContainText('const value = 2');await expect(page.getByLabel('Suggested review order')).toHaveCount(0)
})

test('Jev results dock beside wide code areas and below narrow ones',async({desktop,page})=>{
 enable(desktop);await page.setViewportSize({width:1600,height:1000});const insights=await openInsights(desktop);await page.getByRole('button',{name:'Expand review to fill the window',exact:true}).click();await insights.getByRole('button',{name:'Start assessment',exact:true}).click();await expect(insights).toContainText('Authentication and authorization');
 let code=await page.locator('.review-diff-pane').boundingBox(),aside=await page.getByRole('complementary',{name:'Jev assessment results'}).boundingBox();expect(code!.x+code!.width).toBeLessThanOrEqual(aside!.x+1);expect(code!.height).toBeGreaterThan(200)
 await page.setViewportSize({width:1024,height:900});code=await page.locator('.review-diff-pane').boundingBox();aside=await page.getByRole('complementary',{name:'Jev assessment results'}).boundingBox();expect(aside!.y).toBeGreaterThanOrEqual(code!.y+code!.height-1);expect(code!.height).toBeGreaterThan(100);expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
})

test('local Jev provider uses separate consent and clearly labels synthetic results without a key',async({desktop,page})=>{
 let status:PredictionStatus={provider:'fake',configured:true,enabled:false,model:'jev-local-test',error:''}
 desktop.handlers.set('main.PredictionService.GetStatus',()=>status)
 desktop.handlers.set('main.PredictionService.SaveSettings',args=>{expect(args).toEqual([{host:'github.com',owner:'fork',repo:'app'},'jev-local-test',true]);status={...status,enabled:true};return null})
 desktop.handlers.set('main.PredictionService.Evaluate',args=>({...assessment(args[0]),provider:'fake',model:status.model,usage:{input_tokens:0,output_tokens:0},warnings:['LOCAL TEST PROVIDER: deterministic synthetic answers.']}))
 const insights=await openInsights(desktop)
 await expect(insights.getByRole('heading',{name:'Local Jev test provider'})).toBeVisible()
 await expect(insights.getByLabel('TypeSafe API key')).toHaveCount(0)
 await expect(insights.getByRole('button',{name:'Start assessment',exact:true})).toBeDisabled()
 await insights.getByLabel(/Allow Jev assessments for/).check()
 await insights.getByRole('button',{name:'Save Jev settings',exact:true}).click()
 await insights.getByRole('button',{name:'Start assessment',exact:true}).click()
 await expect(insights).toContainText('Synthetic test answers · no model inference or charge.')
 await expect(insights).toContainText('0 input / 0 output tokens · Local test · no charge')
 await expect(insights).not.toContainText('Charge not reported')
 await expect(page.locator('[data-code-side="new"][data-code-line="1"]')).toContainText('const value = 2')
 expect(desktop.calls.filter(call=>/PredictionService\.(Connect|Disconnect)/.test(call.name))).toHaveLength(0)
})

test('connecting and refreshing use an available catalog model instead of the old default',async({desktop})=>{
 let status:PredictionStatus={provider:'typesafe',configured:false,enabled:false,model:'jev-1.13.0',error:''}
 desktop.handlers.set('main.PredictionService.GetStatus',()=>status)
 desktop.handlers.set('main.PredictionService.Connect',()=>{status={...status,configured:true,model:'jev-latest'};return[{name:'jev-latest',description:'Available stable model',release_date:'2026-10-19'},{name:'jev-alternative',description:'Another available model',release_date:'2026-10-19'}]})
 desktop.handlers.set('main.PredictionService.Models',()=>{status={...status,model:'jev-next'};return[{name:'jev-next',description:'Updated available model',release_date:'2026-10-19'}]})
 desktop.handlers.set('main.PredictionService.SaveSettings',args=>{expect(args[1]).toBe('jev-next');return null})
 const insights=await openInsights(desktop)
 await insights.getByLabel('TypeSafe API key').fill('fixture-only-key')
 await insights.getByRole('button',{name:'Connect TypeSafe',exact:true}).click()
 await expect(insights.getByLabel('Jev model')).toHaveValue('jev-latest')
 await expect(insights.getByLabel('Jev model').locator('option')).toHaveText(['jev-latest','jev-alternative'])
 await insights.getByRole('button',{name:'Refresh models',exact:true}).click()
 await expect(insights.getByLabel('Jev model')).toHaveValue('jev-next')
 await insights.getByRole('button',{name:'Save Jev settings',exact:true}).click()
 await expect(insights).toContainText('Prediction settings saved.')
})
