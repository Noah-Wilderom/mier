import type { PullRequest } from './data'

export function preserveReviewRevision(previous: PullRequest | undefined, next: PullRequest, force = false): PullRequest {
  if (!previous) return next
  const changed = previous.headSha !== next.headSha
  if (changed && previous.provider?.detailLoaded && !force) {
    return {...next, files:previous.files, commits:previous.commits, headSha:previous.headSha, baseSha:previous.baseSha, viewedFiles:previous.viewedFiles, pendingComments:previous.pendingComments,
      threads:[...previous.threads.map(thread => {const incoming=next.threads.find(t=>t.id===thread.id);return incoming ? {...thread,resolved:incoming.resolved,comments:incoming.comments,canResolve:incoming.canResolve,canUnresolve:incoming.canUnresolve} : thread}),...next.threads.filter(thread=>!previous.threads.some(old=>old.id===thread.id)).map(thread=>({...thread,outdated:true,headSha:next.headSha}))],
      provider:{...next.provider!,hasNewCommits:true}, checkHeadSha:next.headSha} as PullRequest
  }
  return {...next,
    viewedFiles:previous.viewedFiles.filter(path => {const oldFile=previous.files.find(f=>f.path===path),newFile=next.files.find(f=>f.path===path);return !!newFile && (!changed || JSON.stringify(oldFile?.lines)===JSON.stringify(newFile.lines))}),
    pendingComments:previous.pendingComments.map(comment => ({...comment, headSha:comment.headSha??previous.headSha, outdated:comment.outdated || (comment.headSha??previous.headSha)!==next.headSha})),
    provider:{...next.provider!,hasNewCommits:false}}
}

