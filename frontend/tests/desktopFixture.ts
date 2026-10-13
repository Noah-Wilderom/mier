import type { Page } from '@playwright/test'
import type { PullRequestDetail, PullRequestSummary, RepositoryDiff, RepositorySnapshot, WorkspaceUpdate } from '../src/services'

export const root = '/fixture/review-workspace'
export const baseSHA = 'a'.repeat(40)
export const headSHA = 'b'.repeat(40)
export const changedSHA = 'c'.repeat(40)
const user = { login: 'reviewer', name: 'Reviewer', avatarUrl: '', url: 'https://github.com/reviewer' }

export function repository(path = root): RepositorySnapshot {
  return {
    root: path, name: path.split('/').pop()!, currentBranch: 'feature', detached: false, hasHead: true, headSha: headSHA, upstream: 'upstream/feature', ahead: 1, behind: 0,
    remotes: ['origin', 'upstream'].map(name => ({ name, fetchUrl: `https://github.com/${name === 'origin' ? 'fork' : 'team'}/app.git`, pushUrl: `https://github.com/${name === 'origin' ? 'fork' : 'team'}/app.git`, host: 'github.com', owner: name === 'origin' ? 'fork' : 'team', repository: 'app', provider: 'github' })),
    branches: [
      { name: 'main', current: false, remote: false, upstream: 'origin/main', ahead: 0, behind: 0, commitSha: baseSHA },
      { name: 'feature', current: true, remote: false, upstream: 'upstream/feature', ahead: 1, behind: 0, commitSha: headSHA },
      { name: 'origin/main', current: false, remote: true, upstream: '', ahead: 0, behind: 0, commitSha: baseSHA },
      { name: 'origin/feature', current: false, remote: true, upstream: '', ahead: 0, behind: 0, commitSha: headSHA },
    ],
    history: [
      { sha: headSHA, parents: [baseSHA], subject: 'feat: reviewed change', body: '', authorName: 'Reviewer', authorEmail: 'reviewer@example.test', createdAt: '2026-10-12T12:00:00Z', refs: ['feature'] },
      { sha: baseSHA, parents: [], subject: 'Initial commit', body: '', authorName: 'Reviewer', authorEmail: 'reviewer@example.test', createdAt: '2026-10-11T12:00:00Z', refs: ['main'] },
    ],
    changes: [{ path: 'src/feature.ts', status: 'modified', indexStatus: '.', worktreeStatus: 'M', staged: false, unstaged: true, conflict: false, untracked: false }],
  }
}

export function diff(path = 'src/feature.ts', text = 'const value = 2'): RepositoryDiff {
  return { path, status: 'modified', additions: 1, deletions: 1, rawPatch: `diff --git a/${path} b/${path}\n--- a/${path}\n+++ b/${path}\n@@ -1 +1 @@\n-const value = 1\n+${text}\n`, lines: [{ kind: 'remove', oldLine: 1, text: 'const value = 1' }, { kind: 'add', newLine: 1, text }], binary: false, truncated: false }
}

export function summary(sha = headSHA, number = 12): PullRequestSummary {
  return { number, nodeId: `PR_${number}`, title: `Review ${number}`, body: 'A **careful** review with <script>literal markup</script>.', url: `https://github.com/fork/app/pull/${number}`, status: 'open', author: { ...user, login: 'author' }, createdAt: '2026-10-12T12:00:00Z', updatedAt: new Date().toISOString(), head: { ref: 'feature', sha, label: 'fork:feature', repository: 'fork/app' }, base: { ref: 'main', sha: baseSHA, label: 'fork:main', repository: 'fork/app' }, reviewers: [user], assignees: [], labels: ['enhancement'], draft: false, merged: false, mergeable: true, mergeableState: 'clean', additions: 1, deletions: 1, changedFiles: 1, commitsCount: 1, commentsCount: 0, reviewCommentsCount: 0, threadsCount: 0, countsKnown: true, warnings: [] }
}

export function detail(sha = headSHA): PullRequestDetail {
  const patch = diff('src/feature.ts', sha === changedSHA ? 'const value = 3' : 'const value = 2')
  return { pullRequest: summary(sha), files: [{ path: patch.path, previousPath: '', status: patch.status, additions: 1, deletions: 1, patch: patch.rawPatch }], commits: [{ sha, subject: 'feat: reviewed change', body: '', author: user, authorName: 'Reviewer', createdAt: '2026-10-12T12:00:00Z', url: `https://github.com/fork/app/commit/${sha}` }], comments: [], reviews: [], checks: [], statuses: [], threads: [], mergeStateStatus: 'CLEAN', reviewDecision: 'REVIEW_REQUIRED', viewerCanUpdate: true, allowedMergeMethods: ['merge', 'squash'], warnings: [], requiredApprovals: 1, requiredStatusCheckContexts: [] }
}

type Handler = (args: any[]) => unknown | Promise<unknown>
export async function desktopFixture(page: Page) {
  const fixture = {
    page, selected: root, repositories: new Map([[root, repository()]]), prs: [summary()], currentDetail: detail(),
    calls: [] as { name: string; args: any[] }[], handlers: new Map<string, Handler>(), unexpected: [] as string[], pageErrors: [] as string[], recent: [] as string[],
    async emit(update: Partial<WorkspaceUpdate> = {}) {
      const repo = fixture.repositories.get(fixture.selected)!
      await page.evaluate(update => (window as any)._wails.dispatchWailsEvent({ name: 'workspace:updated', data: update }), { path: repo.root, repository: repo, host: 'github.com', owner: 'fork', repo: 'app', pullRequests: fixture.prs, updatedAt: new Date().toISOString(), ...update })
    },
  }
  page.on('pageerror', error => fixture.pageErrors.push(error.message))
  await page.addInitScript(() => { (window as any)._wails = { environment: { OS: 'linux' }, invoke: () => {} } })
  await page.route('https://avatars.githubusercontent.com/**', route => route.abort())
  await page.route('**/wails/custom.js', route => route.fulfill({ status: 404, body: '' }))
  await page.route('**/wails/runtime', async route => {
    const request = route.request().postDataJSON()
    if (request.object !== 0) { await route.fulfill({ json: null }); return }
    const name = request.args.methodName as string, args = request.args.args as any[]
    fixture.calls.push({ name, args })
    try {
      let result: unknown
      const handler = fixture.handlers.get(name)
      if (handler) result = await handler(args)
      else switch (name) {
        case 'main.WorkspaceService.ChooseRepository': result = fixture.selected; break
        case 'main.WorkspaceService.RecentRepositories': result = fixture.recent; break
        case 'main.WorkspaceService.RememberRepository': fixture.recent = [...new Set([...fixture.recent, args[0]])]; result = null; break
        case 'main.RepositoryService.Open': case 'main.RepositoryService.Snapshot': result = fixture.repositories.get(args[0]); break
        case 'main.WorkspaceSyncService.StartWatch': case 'main.WorkspaceSyncService.WatchPullRequest': case 'main.WorkspaceSyncService.StopWatch': result = null; break
        case 'main.WorkspaceSyncService.Refresh': await fixture.emit(); result = null; break
        case 'main.CredentialService.AccountStatus': result = { host: args[0], login: 'reviewer', authenticated: true, source: 'gh', name: 'Reviewer', avatarUrl: '', url: 'https://github.com/reviewer', error: '' }; break
        case 'main.EditorService.PreferredEditor': result = 'vscode'; break
        case 'main.PRNotificationService.GetPreferences': result = { toastsEnabled: true, desktopEnabled: false, opened: true, closed: true, merged: true, reviewRequested: true }; break
        case 'main.LSPService.Servers': result = []; break
        case 'main.GitHubService.GetPullRequest': result = fixture.currentDetail; break
        case 'main.RepositoryService.Diff': result = diff(args[1]); break
        case 'main.RepositoryService.Compare': case 'main.RepositoryService.CommitDiff': result = [diff()]; break
        case 'main.GitHubService.GetCommitDiff': result = { sha: args[3], files: [{ path: 'src/feature.ts', status: 'modified', additions: 1, deletions: 1, patch: diff().rawPatch }], warnings: [] }; break
        case 'main.RepositoryService.CreateAndCheckoutBranch': case 'main.RepositoryService.CreateBranch': {
          const repo = fixture.repositories.get(args[0])!, checkout = name.endsWith('CreateAndCheckoutBranch')
          if (repo.branches?.some(branch => branch.name === args[1])) throw new Error('Branch already exists')
          repo.branches = [...(repo.branches ?? []).map(branch => checkout ? { ...branch, current: false } : branch), { name: args[1], current: checkout, remote: false, upstream: '', ahead: 0, behind: 0, commitSha: repo.headSha! }]
          if (checkout) { repo.currentBranch = args[1]; repo.upstream = ''; repo.ahead = 0; repo.behind = 0 }
          result = null; break
        }
        case 'main.RepositoryService.Stage': case 'main.RepositoryService.Unstage': {
          const staged = name.endsWith('.Stage'), repo = fixture.repositories.get(args[0])!
          repo.changes = repo.changes?.map(file => args[1].includes(file.path) ? { ...file, staged, unstaged: !staged, indexStatus: staged ? 'M' : '.', worktreeStatus: staged ? '.' : 'M' } : file) ?? []
          result = null; break
        }
        case 'main.RepositoryService.Fetch': case 'main.RepositoryService.Pull': case 'main.RepositoryService.Push': result = null; break
        default: fixture.unexpected.push(name); throw new Error(`Unexpected fixture RPC: ${name}`)
      }
      await route.fulfill({ json: result ?? null })
    } catch (error) {
      await route.fulfill({ status: 500, json: { kind: 'RuntimeError', message: error instanceof Error ? error.message : String(error) } })
    }
  })
  return fixture
}

export async function openFixture(fixture: Awaited<ReturnType<typeof desktopFixture>>, withPullRequests = true) {
  await fixture.page.goto('/')
  await fixture.page.locator('.repository-welcome').getByRole('button', { name: 'Open repository', exact: true }).click()
  await fixture.page.getByRole('region', { name: 'Repository Git actions' }).waitFor()
  await fixture.page.waitForFunction(() => typeof (window as any)._wails?.dispatchWailsEvent === 'function')
  if (withPullRequests) await fixture.emit()
}
