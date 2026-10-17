import type { PullRequest } from './data'
import {fileSignature} from './reviewFileSignature.ts'

export interface SavedReviewDraft {
  fileSignatures?:Record<string,string>
  headSha: string
  viewedFiles: string[]
  pendingComments: PullRequest['pendingComments']
}
export const reviewDraftStorageKey = 'mag-ik-een-review.review-drafts.v1'

export function readReviewDrafts(value: string | null): Map<string, SavedReviewDraft> {
  const result = new Map<string, SavedReviewDraft>()
  try {
    const saved = JSON.parse(value ?? 'null')
    if (saved?.version !== 1 || !Array.isArray(saved.entries)) return result
    for (const entry of saved.entries) {
      if (!Array.isArray(entry) || typeof entry[0] !== 'string') continue
      const draft = entry[1]
      if (typeof draft?.headSha !== 'string' || !Array.isArray(draft.viewedFiles) || !draft.viewedFiles.every((path: unknown) => typeof path === 'string') || !Array.isArray(draft.pendingComments)) continue
      if (!draft.pendingComments.every((comment: PullRequest['pendingComments'][number]) => typeof comment?.id === 'string' && typeof comment.path === 'string' && typeof comment.body === 'string' && Number.isSafeInteger(comment.line) && comment.line > 0 && ['old', 'new'].includes(comment.side))) continue
      result.set(entry[0], draft)
    }
  } catch { /* A damaged store never blocks opening the workspace. */ }
  return result
}

export function restoreReviewDraft(pr: PullRequest, saved?: SavedReviewDraft): PullRequest {
  if (!saved) return pr
  return {...pr, viewedFiles: saved.viewedFiles.filter(path=>pr.files.some(file=>file.path===path&&(saved.headSha===pr.headSha||saved.fileSignatures?.[path]===fileSignature(file)))), pendingComments: saved.pendingComments.map(comment => ({...comment, headSha: comment.headSha ?? saved.headSha, outdated: comment.outdated || (comment.headSha ?? saved.headSha) !== pr.headSha}))}
}
