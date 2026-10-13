import { test } from 'node:test'
import assert from 'node:assert/strict'
import { commitDraftKey, pullRequestDraftKey, readCommitDraft, readPullRequestDraft } from './workspaceDrafts.ts'

test('workspace drafts distinguish repositories, providers and demo data', () => {
  assert.notEqual(commitDraftKey('/one'), commitDraftKey('/two'))
  assert.notEqual(commitDraftKey('/one'), commitDraftKey())
  assert.notEqual(pullRequestDraftKey('/one', { host: 'github.com', owner: 'team', repo: 'app' }), pullRequestDraftKey('/one', { host: 'enterprise.test', owner: 'team', repo: 'app' }))
})

test('malformed saved drafts recover and preserve valid text without interpreting markup', () => {
  const defaults = { base: 'main', head: 'feature', title: '', body: '', draft: false }
  for (const invalid of ['{', 'null', '[]', '"text"']) assert.deepEqual(readPullRequestDraft(invalid, defaults), defaults)
  assert.deepEqual(readPullRequestDraft(JSON.stringify({ title: '<script>literal</script>', body: 'draft', draft: true, base: 42 }), defaults), { ...defaults, title: '<script>literal</script>', body: 'draft', draft: true })
  assert.equal(readCommitDraft('42'), '')
  assert.equal(readCommitDraft(JSON.stringify('fix: preserve\n\ncontext')), 'fix: preserve\n\ncontext')
})
