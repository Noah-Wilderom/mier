import { test } from 'node:test'
import assert from 'node:assert/strict'
import { comparisonBase, gitHubIdentity, reviewIdentity, knownCount, preferredRemote, pullUnavailableReason, trackingRemote } from './gitWorkspace.ts'
import type { RepositorySnapshot } from './services.ts'

function repository(overrides: Partial<RepositorySnapshot> = {}): RepositorySnapshot {
  return { root: '/repo', name: 'repo', currentBranch: 'feature', detached: false, hasHead: true, upstream: 'company/upstream/main', ahead: 2, behind: 1, branches: [], history: [], changes: [], remotes: ['origin', 'company', 'company/upstream'].map(name => ({ name, host: '', owner: '', repository: '', provider: '', fetchUrl: '', pushUrl: '' })), ...overrides }
}

test('Git actions select the tracking remote, including names containing slashes', () => {
  const repo = repository()
  assert.equal(trackingRemote(repo), 'company/upstream')
  assert.equal(preferredRemote(repo), 'company/upstream')
  assert.equal(preferredRemote(repo, 'origin'), 'origin')
  assert.equal(preferredRemote(repository({ upstream: '' })), 'origin')
  assert.equal(preferredRemote(repository({ remotes: [] })), '')
})

test('pull explains missing tracking, wrong remotes, detached HEAD, and conflicts', () => {
  assert.equal(pullUnavailableReason(repository(), 'company/upstream'), '')
  assert.match(pullUnavailableReason(repository(), 'origin'), /company\/upstream/)
  assert.match(pullUnavailableReason(repository({ upstream: '' }), 'origin'), /Publish/)
  assert.match(pullUnavailableReason(repository({ detached: true }), 'origin'), /Check out/)
  assert.match(pullUnavailableReason(repository({ changes: [{ path: 'a', originalPath: '', status: 'modified', indexStatus: 'U', worktreeStatus: 'U', conflict: true, staged: true, unstaged: true, untracked: false }] }), 'company/upstream'), /conflicts/)
})

test('comparison defaults to an available base different from the checked-out branch', () => {
  const branches = ['main', 'feature'].map(name => ({ name, remote: false, current: name === 'feature', upstream: '', ahead: 0, behind: 0, commitSha: '' }))
  assert.equal(comparisonBase(repository({ branches })), 'main')
  assert.equal(comparisonBase(repository({ branches, currentBranch: 'main' })), 'feature')
  assert.equal(comparisonBase(repository({ branches: [] })), '')
})

test('GitHub identity skips known other providers and supports enterprise remotes', () => {
  const remote = { name: 'origin', host: 'gitlab.com', owner: 'team', repository: 'app', provider: 'gitlab', fetchUrl: '', pushUrl: '' }
  assert.equal(gitHubIdentity(repository({ remotes: [remote] })), undefined)
  assert.deepEqual(gitHubIdentity(repository({ remotes: [remote, { ...remote, name: 'upstream', host: 'github.com', provider: 'github' }] })), { host: 'github.com', owner: 'team', repo: 'app' })
  assert.deepEqual(gitHubIdentity(repository({ remotes: [{ ...remote, host: 'github.company.test', provider: 'unknown' }] })), { host: 'github.company.test', owner: 'team', repo: 'app' })
})

test('native review identities preserve namespaces and keep unknown counts out of badges',()=>{
  for(const provider of ['gitlab','bitbucket','azure'] as const){
    const remote={name:'origin',host:provider==='azure'?'dev.azure.com':`${provider}.com`,owner:'team/subgroup',repository:'app',provider,fetchUrl:'',pushUrl:''}
    const repo=repository({remotes:[remote]})
    assert.equal(gitHubIdentity(repo),undefined)
    assert.deepEqual(reviewIdentity(repo),{host:remote.host,owner:remote.owner,repo:'app',kind:provider})
  }
  assert.equal(knownCount(-1),null)
  assert.equal(knownCount(undefined),null)
  assert.equal(knownCount(0),0)
})
