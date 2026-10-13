import type { RepositoryIdentity, RepositorySnapshot } from './services'

export type GitAction = 'fetch' | 'pull' | 'push' | 'checkout' | 'createBranch' | 'createAndCheckoutBranch' | 'deleteBranch' | 'renameBranch' | 'checkoutPullRequest' | 'stage' | 'unstage' | 'commit'

export function gitHubIdentity(repository: RepositorySnapshot): RepositoryIdentity | undefined {
  // Custom hosts remain eligible for GitHub Enterprise. Known other providers
  // are local Git remotes and must not be queried using GitHub's API.
  const remotes = (repository.remotes ?? []).filter(remote => remote.host && remote.owner && remote.repository && !['gitlab', 'bitbucket', 'local'].includes(remote.provider))
  const remote = remotes.find(remote => remote.name === 'origin') ?? remotes[0]
  return remote ? { host: remote.host, owner: remote.owner, repo: remote.repository } : undefined
}

export function trackingRemote(repository: RepositorySnapshot): string {
  return [...(repository.remotes ?? [])].sort((a, b) => b.name.length - a.name.length)
    .find(remote => repository.upstream.startsWith(remote.name + '/'))?.name ?? ''
}

export function preferredRemote(repository: RepositorySnapshot, previous = ''): string {
  const names = (repository.remotes ?? []).map(remote => remote.name)
  return names.includes(previous) ? previous : trackingRemote(repository) || (names.includes('origin') ? 'origin' : names.length === 1 ? names[0] : '')
}

export function pullUnavailableReason(repository: RepositorySnapshot, remote: string): string {
  if (!remote) return 'Choose a remote first.'
  if (!repository.hasHead || repository.detached) return 'Check out a branch with an upstream before pulling.'
  if (!repository.upstream) return 'Publish this branch or configure its upstream before pulling.'
  if (trackingRemote(repository) !== remote) return `This branch tracks ${trackingRemote(repository)}; choose that remote to pull.`
  if (repository.changes?.some(file => file.conflict)) return 'Resolve the merge conflicts before pulling.'
  return ''
}

export function comparisonBase(repository: RepositorySnapshot): string {
  const names = (repository.branches ?? []).map(branch => branch.name)
  return ['main', 'master', 'origin/main', 'origin/master', 'HEAD'].find(name => name !== repository.currentBranch && names.includes(name)) ?? names.find(name => name !== repository.currentBranch) ?? ''
}
