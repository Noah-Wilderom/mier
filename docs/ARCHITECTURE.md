# Implementation architecture

This describes M.I.E.R. as implemented on 2026-10-14. Product scope and intent are in [PRODUCT.md](../PRODUCT.md); run/build instructions are in [README.md](../README.md). The initial reference image and current visual decisions are in [DESIGN.md](../DESIGN.md).

## Boundaries and runtime

The Go application owns filesystem access, Git, credentials, provider requests, background polling, native editor launches, notifications, and language-server processes. React renders structured data and invokes typed service adapters; browser demo actions use separate sample state. Running Vite alone does not access repositories or GitHub.

`main.go` registers services and the `workspace:updated`, `notifications:pr`, and `lsp:changed` Wails events, embeds `frontend/dist`, and creates the native window and menu. The project pins Go Wails and CLI `v3.0.0-alpha2.117` and npm runtime `3.0.0-beta.29`. The active application service namespace is `main.*`. Do not derive binding names from the module slug or upgrade one runtime independently without checking transport and generated bindings.

`frontend/src/services.ts` is the typed RPC boundary, including cancellation for LSP calls. `native.ts` owns native commands, browser opening, full screen, and the runtime handshake. Native availability requires an actual Wails OS environment. Importing the runtime in a normal browser does not create a live backend.

## Module map

| Area | Go ownership | Frontend ownership |
| --- | --- | --- |
| Repository selection and recent paths | `workspaceservice.go` | `LiveSetup.tsx`, `RepositorySelector.tsx` |
| Git state and operations | `repositoryservice.go`, `internal/gitclient/` | `useLiveWorkspace.ts`, `LocalChangesView.tsx`, `HistoryGraph.tsx` |
| GitHub and enterprise accounts | `credentialservice.go`, `githubservice.go`, `internal/githubclient/` | `services.ts`, `liveData.ts`, `LiveSetup.tsx` |
| Background synchronization | `workspace_sync.go` | `useLiveWorkspace.ts`, `Loading.tsx` |
| PR review and drafts | Provider mutations and revision checks | `App.tsx`, `ReviewWorkspace.tsx`, `reviewRevision.ts`, `reviewDrafts.ts` |
| Markdown and identities | GitHub user/comment DTOs | `Markdown.tsx`, `Avatar.tsx` |
| Code highlighting and preferences | No server dependency | `syntaxHighlighter.ts`, `codeReviewPreferences.ts`, `CodeReviewSettings.tsx` |
| Language intelligence | `lspservice.go`, `internal/lspclient/`, Git snapshot APIs | `CodeIntelligence.tsx`, `LanguageServerSettings.tsx` |
| Notifications | `prnotificationservice.go`, `desktopnotificationservice.go` | `NotificationSettings.tsx`, `NotificationToasts.tsx` |
| Editor launching | `editorservice.go` | `EditorActions.tsx` |
| Planned Jira integration | `integrationservice.go` rejects unavailable installation | `JiraIntegration.tsx` displays Coming soon |

## Git and provider execution

Git runs through direct argument vectors with repository locks, contexts, bounded output, and timeouts. Normal operations do not build shell command strings. Pull is fast-forward only; push does not force. PR checkout handles existing branches and rejects unsafe dirty/divergent states. Snapshot HEAD is the actual checkout's `headSha`; history includes other branches and must not be used to guess HEAD.

The credential service resolves an app-saved OS keychain token before an existing `gh` login for the selected hostname. Account metadata contains no tokens. HTTPS Git receives ephemeral host-scoped configuration; SSH uses the existing user configuration. Credentials stay in Go, and errors/remotes are redacted. Redirects and Git tracing do not carry authentication to another host.

GitHub uses GraphQL cursor pages for inbox summaries and their counts, and REST for files, lifecycle, checks, and mutations. Each inbox request returns only its requested page, continuation cursor, and total counts. Unfiltered repository history uses the repository connection; personal views and text searches use scoped GitHub search. Search results follow GitHub's 1,000-result limit and expose that limit so the user can narrow the query. Pagination, ETags, Link headers, rate-limit cooldowns, and cancellation are handled by the provider client. The current review-comment query uses `fullDatabaseId`; side belongs to the thread. See [Git/GitHub/Wails research](RESEARCH_GIT_GITHUB_WAILS.md) for the merged-PR schema regression and observed API behavior.

Mutations reconcile canonical provider state after success. If a remote action succeeds but reloading fails, `RemoteActionAppliedError` distinguishes that outcome from a rejected action so the UI does not blindly repeat a write. Reviews and merges carry an expected head SHA. Permissions and merge protection are enforced by GitHub; missing protection information is presented as unknown.

## Refresh and review continuity

Native startup reads recent repositories locally and starts a watch for each valid imported repository. Each repository worker refreshes every 15 seconds. Initial provider refreshes fetch only the 25 most recently updated PRs. Later refreshes page back only until they reach the previous successful refresh checkpoint, providing complete change batches for notifications without rereading unchanged history. Failed or incomplete batches never advance the checkpoint. Local data becomes visible before provider requests complete. Workers do not overlap for the same repository, queue manual refreshes, and cancel abandoned work. Provider list/detail phases have independent bounded contexts; local Git keeps its own timeout. A later local snapshot prevents slow HTTP results from restoring pre-mutation Git state.

`useLiveWorkspace.ts` loads inbox pages independently of background events. Filter, search, and repository changes reset pagination and invalidate obsolete responses. **Load more** appends unique rows while retaining loaded details and review drafts; failed requests retain rows and their retry cursor. Background events update matching loaded PRs without replacing the inbox or discarding later pages. Repository cards use the provider's global open count, rather than counting a partial page. Initial count requests show skeletons; errors end the pending state without inventing a count. Assignee/reviewer metadata updates even when the selected PR already has a loaded detail view.

`reviewRevision.ts` preserves the displayed diff/head/base and draft anchors when new commits arrive. Conversation/check state can update while the UI offers **Refresh changes**. Accepting the revision replaces the diff explicitly, retains old drafts as outdated, and retains viewed progress only for unchanged files. `reviewDrafts.ts` persists review state per provider/repository/PR. An open inline composer also keeps its original head if the user refreshes before saving it.

Local diff reloads retain the visible code during a same-file background request. Changing file or staged/unstaged side shows a skeleton without exposing the previous file's content. Failed background refreshes retain usable code and offer Retry. `workspaceDrafts.ts` and `usePersistentDraft.ts` save commit and PR creation drafts under separate repository keys, report storage failures, and keep rejected writes editable. Repository epochs prevent obsolete responses and queued actions from changing a newer workspace; opening a repository or entering the demo invalidates earlier context.

## Code analysis

Shiki provides grammar highlighting shared by PR diffs, local diffs, definition files, and settings previews. Old/new sequences keep independent grammar state and reset across omitted hunks. Unknown or oversized sources remain exact plain text. Code font and theme preferences are separate from application appearance.

The LSP manager creates private revision snapshots and owns stdio JSON-RPC processes. PR sides use exact Git blobs and merge-base comparisons; staged and unstaged views use immutable content digests of index/working snapshots. Packaging attributes must not alter source content. Workspace package links resolve inside the selected snapshot; installed dependency source is isolated there. Definitions receive opaque grants tied to source revision and frozen content rather than accepting arbitrary frontend file paths.

UTF-16 positions, exact line-text validation, canceled requests, bounded caches, process cleanup, and read-only edit-request handling protect review continuity. Vue has a TypeScript companion process with its plugin and broker; empty Vue script-semantic results fall back to that companion. PHP can initially return partial information while indexing, so the UI preserves the first result and performs one canceled-safe warmup refresh. See [language intelligence](LANGUAGE_INTELLIGENCE.md) and [LSP research](RESEARCH_LSP.md).

Managed language tools live in the user configuration directory's `mag-ik-een-review/language-tools/`. The installer resolves stable versions from official providers, checks PHP/Node/npm prerequisites, verifies the Phpactor digest and npm package versions, and installs into a new directory before atomically changing `installed.json`. Server updates keep the tested TypeScript SDK and matching Vue plugin/companion. Installation uses literal process arguments, isolated npm config/cache, and disabled lifecycle scripts. A failed or cancelled operation retains the active installation. Successful activation closes existing analysis sessions and emits `lsp:changed`; Settings polls progress while an operation runs, and code views refresh availability from the event. Custom configuration takes priority over managed tools.

## Preferences and delivery

OS configuration lives below the platform's user configuration directory in `mag-ik-een-review/`: `accounts.json`, `workspace.json`, `notifications.json`, `editor.json`, and `language-servers.json`. Secrets live separately in the OS credential store. Configuration writers use atomic replacement and private file permissions where supported. LSP session data uses the OS user cache directory.

Frontend storage includes `mag-ik-een-review.appearance`, `mier.code-review.preferences.v1`, `mag-ik-een-review.review-drafts.v1`, `mier.commit-draft.v1:<root>`, and `mier.pull-request-draft.v1:<identity>`. Demo draft keys are separate. Do not merge demo state into live repository state or erase saved drafts when changing appearance or navigation.

Notification preferences key by normalized remote identity, so different local clones share one policy. The first snapshot establishes a silent baseline; missing PRs do not imply closes. Older provider timestamps cannot undo a newer baseline and duplicate events. Event types/toasts default on; desktop delivery defaults off. The native notification backend initializes only when requested and handles permission separately from in-app toast delivery.

The editor split button remembers successful editor launches; selecting a menu item cannot be overwritten by a late preference response. The GitHub browser action is independent. Jira installation is unavailable in both the frontend and backend. `ErrorBoundary.tsx` offers workspace reload recovery for render failures.

## Scope and next seams

`RepositoryToolbar.tsx` provides branch creation, switching, rename, merged-only deletion, remote selection, fetch, pull, push, and a comparison dialog. `BranchComparison.tsx` uses Git's merge-base comparison; the same preview is embedded in `LiveCreatePullRequest`. PR publishing and checkout choose the remote matching the provider identity rather than a different tracking remote. Known GitLab/Bitbucket remotes remain local Git contexts; custom hosts support GitHub Enterprise.

`CommitChanges.tsx` loads history changes from `RepositoryService.CommitDiff`, comparing with the first parent or the empty tree for a root commit. PR commit inspection uses `GitHubService.GetCommitDiff`, validates the full immutable SHA, paginates files inside the commit object, and preserves patch-unavailable states. It does not replace the PR's review diff or draft anchors. Comparison requests ignore responses after their revision/repository selection changes.

Other provider integrations, conflict editing, rebase/cherry-pick/stash UI, comment reactions, PR projects/milestones, framework-specific LSP plugins, completion/rename/diagnostics, OS-signed installers, and notarization remain outside the current implementation. The initial design image depicts some of these concepts; it is not evidence that the corresponding controls are implemented. [Releasing](RELEASING.md) describes the six-target pipeline and signed updater separately from installer signing.

Keep new hosting providers behind the provider boundary, framework server configuration behind the LSP manager, and view-state continuity in the review helpers. Validate each distribution OS's window behavior, credentials, editor discovery, notification permissions, and process cleanup before claiming a tested platform release.
