import {test} from 'node:test'
import assert from 'node:assert/strict'
import {allowsReview,repositoryWebURL,supportsActions} from './reviewCapabilities.ts'
import type {PullRequest} from './data.ts'

test('review capabilities preserve GitHub compatibility and restrict native actions',()=>{
  assert.equal(allowsReview({},'approve'),true)
  const pr={provider:{capabilities:{comments:true,approve:true,requestChanges:false,merge:false}}} as Pick<PullRequest,'provider'>
  assert.equal(allowsReview(pr,'comments'),true)
  assert.equal(allowsReview(pr,'requestChanges'),false)
  assert.equal(allowsReview(pr,'merge'),false)
  assert.equal(supportsActions({host:'github.enterprise.test',owner:'team',repo:'app'}),true)
  assert.equal(supportsActions({host:'gitlab.com',owner:'team',repo:'app',kind:'gitlab'}),false)
})
test('repository web links preserve nested namespaces and Azure project paths',()=>{
  assert.equal(repositoryWebURL({host:'gitlab.com',owner:'team/nested',repo:'app',kind:'gitlab'}),'https://gitlab.com/team/nested/app')
  assert.equal(repositoryWebURL({host:'dev.azure.com',owner:'org/Shared Project',repo:'app',kind:'azure'}),'https://dev.azure.com/org/Shared%20Project/_git/app')
})
