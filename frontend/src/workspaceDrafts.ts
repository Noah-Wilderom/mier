export interface PullRequestDraft {
  base: string
  head: string
  title: string
  body: string
  draft: boolean
}

export function commitDraftKey(root?: string): string {
  return `mier.commit-draft.v1:${root ?? 'demo'}`
}

export function pullRequestDraftKey(root: string, identity?: { host: string; owner: string; repo: string }): string {
  return `mier.pull-request-draft.v1:${JSON.stringify([root, identity?.host, identity?.owner, identity?.repo])}`
}

export function readCommitDraft(raw: string | null): string {
  if (!raw) return ''
  try { const value: unknown = JSON.parse(raw); return typeof value === 'string' ? value.slice(0, 65536) : '' } catch { return '' }
}

export function readPullRequestDraft(raw: string | null, defaults: PullRequestDraft): PullRequestDraft {
  if (!raw) return defaults
  try {
    const value = JSON.parse(raw)
    if (!value || typeof value !== 'object' || Array.isArray(value)) return defaults
    return {
      base: typeof value.base === 'string' ? value.base.slice(0, 1024) : defaults.base,
      head: typeof value.head === 'string' ? value.head.slice(0, 1024) : defaults.head,
      title: typeof value.title === 'string' ? value.title.slice(0, 512) : '',
      body: typeof value.body === 'string' ? value.body.slice(0, 65536) : '',
      draft: typeof value.draft === 'boolean' ? value.draft : false,
    }
  } catch { return defaults }
}
