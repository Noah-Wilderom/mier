// @ts-nocheck -- Node executes these TypeScript tests directly.
import test from 'node:test'
import assert from 'node:assert/strict'
import { seedPullRequests } from './data.ts'
import { preserveReviewRevision } from './reviewRevision.ts'
import { readReviewDrafts, restoreReviewDraft } from './reviewDrafts.ts'

function reviewing() {
  const pr = seedPullRequests().find(item => item.number === 142)
  pr.provider = {host:'github.com',owner:'team',repo:'app',number:142,detailLoaded:true}
  pr.viewedFiles = [pr.files[0].path]
  pr.pendingComments = [{id:'draft',path:pr.files[0].path,line:1,side:'new',body:'Keep this feedback',headSha:pr.headSha}]
  return pr
}

test('new commits preserve the displayed code and feedback while conversations and checks update', () => {
  const old = reviewing(), incoming = structuredClone(old)
  incoming.headSha = 'new-head'
  incoming.files[0].lines[0].text = 'new code'
  incoming.checks = []
  incoming.threads[0].resolved = true
  incoming.threads.push({...structuredClone(incoming.threads[0]),id:'new-thread',headSha:'new-head',resolved:false})
  const result = preserveReviewRevision(old, incoming)
  assert.equal(result.headSha, old.headSha)
  assert.equal(result.files[0].lines[0].text, old.files[0].lines[0].text)
  assert.deepEqual(result.pendingComments, old.pendingComments)
  assert.deepEqual(result.viewedFiles, old.viewedFiles)
  assert.equal(result.provider.hasNewCommits, true)
  assert.equal(result.checkHeadSha, 'new-head')
  assert.equal(result.threads[0].resolved, true)
  assert.equal(result.threads.at(-1).outdated, true)
  assert.equal(result.threads.at(-1).headSha, 'new-head')
})

test('accepting a revision replaces the diff, resets changed-file progress and retains outdated feedback', () => {
  const old = reviewing(), incoming = structuredClone(old)
  incoming.headSha = 'new-head'
  incoming.files[0].lines[0].text = 'new code'
  const result = preserveReviewRevision(old, incoming, true)
  assert.equal(result.headSha, 'new-head')
  assert.equal(result.files[0].lines[0].text, 'new code')
  assert.deepEqual(result.viewedFiles, [])
  assert.equal(result.pendingComments[0].body, 'Keep this feedback')
  assert.equal(result.pendingComments[0].outdated, true)
  assert.equal(result.provider.hasNewCommits, false)
})

test('unchanged files retain viewed progress when accepting a new revision', () => {
  const old = reviewing(), incoming = structuredClone(old)
  incoming.headSha = 'new-head'
  assert.deepEqual(preserveReviewRevision(old, incoming, true).viewedFiles, old.viewedFiles)
})

test('saved drafts restore after reopening but earlier-commit anchors cannot silently become current', () => {
  const old = reviewing()
  const saved = {headSha:old.headSha,viewedFiles:old.viewedFiles,pendingComments:old.pendingComments}
  const loaded = {...old,pendingComments:[],viewedFiles:[]}
  assert.deepEqual(restoreReviewDraft(loaded,saved).viewedFiles, old.viewedFiles)
  const newer = restoreReviewDraft({...loaded,headSha:'new-head'}, saved)
  assert.deepEqual(newer.viewedFiles, [])
  assert.equal(newer.pendingComments[0].outdated, true)
  assert.equal(newer.pendingComments[0].headSha, old.headSha)
})

test('damaged or incompatible draft storage is ignored without blocking the app', () => {
  assert.equal(readReviewDrafts('broken').size, 0)
  assert.equal(readReviewDrafts('{"version":2,"entries":[]}').size, 0)
  const old = reviewing(), saved = {headSha:old.headSha,viewedFiles:old.viewedFiles,pendingComments:old.pendingComments}
  const key = 'github.com/team/app/142'
  const entries = [[key,saved],['bad',{headSha:'old',viewedFiles:[],pendingComments:[{line:0}]}]]
  assert.equal(readReviewDrafts(JSON.stringify({version:1,entries})).size, 1)
  assert.equal(readReviewDrafts(JSON.stringify({version:1,entries})).get(key).pendingComments[0].body,'Keep this feedback')
})

test('unchanged viewed files survive a restarted review at a new head without reanchoring old comments',async()=>{
 const {fileSignature}=await import('./reviewFileSignature.ts')
 const old=reviewing(),signatures=Object.fromEntries(old.files.map(file=>[file.path,fileSignature(file)]))
 const saved={headSha:old.headSha,viewedFiles:old.viewedFiles,pendingComments:old.pendingComments,fileSignatures:signatures}
 const newer=restoreReviewDraft({...old,headSha:'different-head',viewedFiles:[],pendingComments:[]},saved)
 assert.deepEqual(newer.viewedFiles,old.viewedFiles)
 assert.equal(newer.pendingComments[0].outdated,true)
 const changed={...old.files[0],lines:old.files[0].lines.map((line,i)=>i===0?{...line,text:'different source'}:line)}
 const invalidated=restoreReviewDraft({...old,headSha:'different-head',files:[changed,...old.files.slice(1)]},saved)
 assert.ok(!invalidated.viewedFiles.includes(changed.path))
})
