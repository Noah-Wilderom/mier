# Review workspace

## Resume work

Opening the app restores its last repository and that repository/account's PR tabs. Up to 50 PRs can remain open. Each PR keeps its selected section/file, split or unified layout, filters, scroll positions, unfinished inline comment, conversation replies, and review summary. Closing a tab keeps its unfinished draft available when reopened. Accepted pending comments retain their existing revision checks.

Conflict result edits are saved locally. If the conflict token changed while the app was closed, the result is retained with a warning; reload the conflict before applying it. An old result is never silently staged against a different conflict.

UI state lives in private atomic files under the OS configuration directory's `mag-ik-een-review/sessions`. Records have a 4 MiB limit; the store retains up to 256 records within 64 MiB, pruning the oldest records when needed. Credentials use their existing credential stores. Execution flags, MCP bearers, command approvals, and running processes are not restored. Saving/restoration errors are visible in the app.

## Review only new work

In Changes, choose **Since my last review** to compare the exact previously reviewed head with the current head, including divergent histories after a force push. A provider review by your current account can supply the initial baseline. **Save review checkpoint** records a local baseline without posting a review; successful reviews also save one.

The incremental comparison is read-only. Return to **All PR changes** to comment using the provider's canonical line anchors. Pending comments at an older head remain outdated until repositioned. Unchanged file signatures retain viewed progress. **Resolved review feedback** identifies provider conversations resolved since the saved checkpoint; it does not infer that an unresolved finding was fixed just because its line changed.

## Navigate code

The language-intelligence toolbar offers **References**, **Outline**, and **Search symbols**, alongside hover and definition navigation. References use the selected symbol; outlines/search use the current file's language server and captured revision. Results open in the same read-only revision-bound source reader. Availability depends on the selected server's protocol support; unsupported methods produce an explicit error. Navigation results are bounded to 128 entries.

Opening a file prepares analysis in advance. The toolbar shows the actual startup phase and elapsed time, with cache reuse and completed timing available in its tooltip. Immutable source captures can be reused across restarts after dependency-content and captured-file validation. Four persistent snapshots are retained; active snapshots have process locks. Cold captures and language-server indexing can still take time on large projects. **Restart code analysis** stops idle sessions, invalidates old navigation grants, and checks dependencies again. Local/index captures remain temporary. See [language intelligence](LANGUAGE_INTELLIGENCE.md).

Large PR diffs initially mount nearby row chunks. Further chunks mount as they approach the viewport; exposed comment anchors remain mounted. Browser content visibility skips offscreen layout, and constant-time membership checks avoid repeatedly scanning the complete patch for every comment button.

## All repositories inbox

Use **All repositories** in the sidebar for PRs from opened/recent repositories with connected review providers. **Attention** offers review requests, your blocked PRs, failed checks, unread conversations, and all open PRs. Failed/blocked/unread filters load the provider detail they require; requests are bounded and each repository has its own error/retry and pagination. Counts describe loaded results rather than claiming a global provider total.

**Saved filters and project groups** stores reusable attention/search/group choices and named sets of repositories. Opening a result switches its repository and opens its PR tab. Unread conversation state compares actual provider comment timestamps with when the current account last opened that PR.

## Forecast conflicts

Expand **Conflict forecast** below the Git toolbar, choose a local or remote-tracking target branch, and enable **Check in background**. Committed branch/ref changes trigger a new check. Fetch separately to update remote-tracking refs.

The forecast lists files changed on both branches and files expected to conflict in a merge. **Preview rebase** simulates each non-merge commit in order and stops at the first conflict, up to 50 commits. A final merge can be clean even when an intermediate rebase step conflicts. Merge commits are skipped, matching rebase without preserving merges, with an explicit notice.

Previews write disposable Git objects; they never change the working tree, index, branch refs, or start a Git operation. Branch tips are validated before and after the forecast. Actual Git operations retain their normal separate controls and stale-state guards.

## CI and AI

[CI annotations, artifacts and run comparisons](ACTIONS.md) connect failures to exact source revisions. [AI conversations and approved worktree checks](AI.md) retain prior findings and real verification evidence without automatically executing a model's proposed command or publishing a comment.
