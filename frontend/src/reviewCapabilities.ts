import type {PullRequest} from './data'
import type {RepositoryIdentity,ReviewCapabilities,ReviewProviderKind} from './services'

export function allowsReview(pr:Pick<PullRequest,'provider'>,capability:keyof ReviewCapabilities):boolean {
  return pr.provider?.capabilities?.[capability]!==false
}
export function supportsActions(identity:RepositoryIdentity|undefined):boolean {
  return !!identity&&(!identity.kind||identity.kind==='github')
}
export function reviewProviderName(kind:ReviewProviderKind|undefined):string {
  return {github:'GitHub',gitlab:'GitLab',bitbucket:'Bitbucket',azure:'Azure DevOps'}[kind??'github']
}

export function repositoryWebURL(identity:RepositoryIdentity):string {
  const owner=identity.owner.split('/').map(encodeURIComponent).join('/')
  return `https://${identity.host}/${owner}/${identity.kind==='azure'?'_git/':''}${encodeURIComponent(identity.repo)}`
}
