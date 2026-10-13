# Git, GitHub, and Wails research

This document consolidates the research and implementation experiments behind M.I.E.R. (Mag ik een review). Research was checked on **13 October 2026**. It records what influenced the design, what the APIs actually provide, and which conclusions were verified locally. It does not claim exhaustive SourceGit or GitKraken parity, or native runtime validation on every operating system.

Evidence labels used throughout:

- **Primary source**: official documentation, first-party source code, or a live first-party API schema.
- **Local inspection / experiment**: installed dependency source, the application implementation, temporary Git repositories, fake HTTP/SDK/JSON-RPC servers, browser harnesses, or explicitly read-only public API requests.
- **Design decision / inference**: the team's interpretation or implementation choice; it is not an upstream product guarantee.

See [Frontend research](RESEARCH_FRONTEND.md) for rendering, native interaction, external editors, and visual QA, and [Language intelligence](LANGUAGE_INTELLIGENCE.md) for the complete LSP contract and setup. The initial visual reference is preserved at [Initial repository workspace](design/initial-repository-workspace.png).

## Versions and the scope of verification

| Item | Observed version or scope | Evidence |
| --- | --- | --- |
| Wails Go SDK and CLI target | `v3.0.0-alpha2.117` | Project `go.mod`; Go module origin resolves to Wails commit `3440ae554b8ae3c0fffb7f7a778ba778b6bbf1e5`, tag dated 8 July 2026 |
| Frontend runtime | `@wailsio/runtime 3.0.0-beta.29` | Project `frontend/package.json` and installed runtime source |
| Project Go requirement | `go 1.25.0` | Project `go.mod` |
| Local execution tools | Go `1.26.1 linux/amd64`, Git `2.55.0`, Node `26.1.0`, GitHub CLI `2.100.0` | Local version commands on 13 October 2026 |
| GitHub REST version requested | `2022-11-28` | `internal/githubclient/client.go`; this is an explicit compatibility choice, not the newest date shown in current examples |
| GitHub GraphQL | Live GitHub.com schema on 13 October 2026 | Read-only introspection and a public merged-PR query |
| Real language servers | TypeScript language server `6.0.2`, Vue language server `3.3.13`, and official Phpactor PHAR under PHP 8.5 in isolated smoke fixtures | Opt-in real-server tests; no automatic installation into a user repository |

**Local verification:** Git integration tests use temporary repositories. Provider tests use fake HTTP servers and credential stores. Notification tests use an injected fake SDK, without sending system notifications. Browser QA uses mocked Wails calls/events and does not establish native editor launch or operating-system notification delivery. The final `go test -race ./...`, all five opt-in actual-language-server race tests, and the final `wails3 build` passed. That build generated nine RPC services, 52 methods, 47 models, and two events, producing a stripped Linux AMD64 ELF of approximately 16.5 MB. Windows/macOS Go compilation or source inspection does not establish desktop packaging, signing, keychain, permission-dialog, or GUI behavior on those systems. Exact commands and final evidence are recorded in [Validation](VALIDATION.md).

## SourceGit and GitKraken as product references

**Primary source:** SourceGit's own repository describes a Windows/macOS/Linux Git GUI with light/dark/custom themes, a visual commit graph, SSH access, repository/worktree management, history and diff views, and many Git operations. Its published feature list includes creating PRs on several hosts. That alone does not establish a complete provider review workflow. [SourceGit repository and README](https://github.com/sourcegit-scm/sourcegit).

**Primary source:** GitKraken's September 2026 PR documentation covers creation, templates, filters, metadata, drafts, comments, checkout, CI links, suggestions, and merge strategies. It lists GitHub, GitLab, Bitbucket, and Azure DevOps, while explicitly separating GitHub-specific PR views, suggestions, and GitHub.com stacks. Some review functionality opens GitKraken.dev in a browser. Provider presence therefore cannot be treated as identical feature coverage across every provider or deployment. [GitKraken pull request documentation](https://help.gitkraken.com/gitkraken-desktop/pull-requests/).

| Research question | Outcome for M.I.E.R. |
| --- | --- |
| What does “SourceGit look and feel” mean here? | **Design decision:** compact desktop density, clear repository/branch context, restrained surfaces, resizable panes, graph/history, keyboard navigation, and system light/dark. This is the accepted product direction, not a measured usability claim about SourceGit. |
| What does “GitKraken full PR feature” mean? | **Design decision:** a meaningful end-to-end provider workflow: inbox, conversation, changes, commits, checks, review comments, review decisions, metadata, draft/ready transitions, close/reopen, checkout, create, and merge. Each action needs a real provider operation and visible failure states. |
| Should review be the primary navigation? | **User-selected direction:** the review inbox is central for developers who review frequently; Changes has a focused full-screen layout. Git history and local changes remain available. |
| Can a compact desktop client claim feature parity? | **Inference:** parity requires a provider-by-provider capability matrix and further implementation. GitLab, Bitbucket, Azure DevOps, advanced local Git workflows, provider-specific stacks, and every GitKraken suggestion feature are not established by this implementation. |

The prototype originally used explicit fixtures, including PR #142, “Add keyboard navigation to file tree,” Elena Chen's `feature/keyboard-navigation` branch into `main`, four commits, six files, reviewers Noah and Marcus, and a focus-preservation thread. Those were interaction/design fixtures, not remote repository activity. Production data now comes from local Git and provider services; demos must remain clearly identified.

## Wails v3: architecture and compatibility

**Primary source:** Wails exposes Go services, frontend method calls, windows, menus, events, and native dialogs through an application runtime. The current overview describes asynchronous frontend calls and platform-specific menu/dialog behavior. This supports a Go backend with a React interface and native integration points; it does not make HTML controls operating-system widgets. [Wails overview](https://v3.wails.io/reference/overview/).

**Compatibility finding:** the public website is moving independently of this application's pinned alpha SDK. It currently describes beta APIs; source at the pinned commit is authoritative for compilation and binding behavior here. Do not copy newer examples blindly or upgrade only one side of the bridge. [Pinned Wails application bindings](https://github.com/wailsapp/wails/blob/3440ae554b8ae3c0fffb7f7a778ba778b6bbf1e5/v3/pkg/application/bindings.go).

**Local source inspection:** Wails derives a bound method's package component from `reflect.Type.PkgPath()`. Services declared in Go's `main` package therefore use names such as `main.RepositoryService.Snapshot`, `main.GitHubService.GetPullRequest`, and `main.LSPService.Hover`; changing the `go.mod` module name does not change them to `mag-ik-een-review.*`. The frontend uses dynamic `Call.ByName` calls. The installed runtime's cancellable promise exposes `cancel()`, which the LSP adapter connects to `AbortSignal`; cancellation propagates to the Go request context. See `frontend/src/services.ts` and the pinned binding source above. This behavior was inspected and tested against the installed pair, not inferred from Wails v2 examples.

**Design decision:** the browser-only development surface checks the injected `window._wails.environment.OS` before importing/calling native runtime functionality. Without a native bridge, production services produce an actionable desktop-only message. Browser fixtures deliberately inject a mock bridge. This keeps preview behavior understandable and prevents a missing runtime from being mistaken for a successful real Git operation.

**Primary source:** Wails requires platform-specific build/runtime tools. Current installation instructions identify Xcode command-line tools on macOS, WebView2 on Windows, and a C toolchain plus GTK4/WebKitGTK 6 on Linux. The pinned Linux source also contains a `gtk3` variant for GTK3/WebKit2GTK 4.1. Cross-platform targets still require appropriate toolchains; packaging and signing are separate concerns. [Wails installation](https://v3.wails.io/getting-started/installation/), [cross-platform builds](https://v3.wails.io/guides/build/cross-platform/), [pinned default Linux CGO configuration](https://github.com/wailsapp/wails/blob/3440ae554b8ae3c0fffb7f7a778ba778b6bbf1e5/v3/pkg/application/linux_cgo.go), [pinned GTK3 configuration](https://github.com/wailsapp/wails/blob/3440ae554b8ae3c0fffb7f7a778ba778b6bbf1e5/v3/pkg/application/linux_cgo_gtk3.go).

**Implementation choice:** display name `M.I.E.R.`, binary/package slug `mag-ik-een-review`, and bundle identifier `app.magikeenreview.desktop` are kept separate. Dotted display names are not used as executable paths or iOS product-directory names. Branding updates covered desktop metadata and generated mobile configuration, without claiming an iOS/Android product was tested.

## Native notifications: what the pinned SDK really does

**Primary source:** the SDK supports notification IDs, title/body, grouping, categories, and response callbacks, with platform differences. macOS requires a properly bundled/signed application; Windows uses toast notifications; Linux uses `org.freedesktop.Notifications`. Scheduled delivery and some advanced features differ by platform. [Pinned notification API and platform notes](https://github.com/wailsapp/wails/blob/3440ae554b8ae3c0fffb7f7a778ba778b6bbf1e5/v3/pkg/services/notifications/notifications.go).

**Local source inspection:** `notifications.New()` allocates its singleton; actual platform startup occurs through `ServiceStartup`. Linux startup connects to the session D-Bus, which can fail in an environment without a session bus. Its authorization methods are stubs returning success, so they do not prove a desktop notification daemon will display a notification. Windows authorization methods are also stubs; startup registers its activation machinery. [Pinned Linux implementation](https://github.com/wailsapp/wails/blob/3440ae554b8ae3c0fffb7f7a778ba778b6bbf1e5/v3/pkg/services/notifications/notifications_linux.go), [pinned Windows implementation](https://github.com/wailsapp/wails/blob/3440ae554b8ae3c0fffb7f7a778ba778b6bbf1e5/v3/pkg/services/notifications/notifications_windows.go).

**Local source inspection:** macOS startup validates notification availability/bundle identity. Its authorization call has its own 180-second timeout and accepts no caller context. Cancelling the app request cannot instantly dismiss that native SDK operation. [Pinned macOS implementation](https://github.com/wailsapp/wails/blob/3440ae554b8ae3c0fffb7f7a778ba778b6bbf1e5/v3/pkg/services/notifications/notifications_darwin.go).

**Implementation decision and fake-SDK verification:** `DesktopNotificationService` captures lifecycle context without starting the SDK. Desktop delivery defaults off; startup/permission checking happens after opt-in. SDK work is serialized, denial/startup errors reach the UI, caller shutdown returns promptly, and cleanup waits for any in-flight permission operation rather than racing it. A false desktop flag sends nothing and starts nothing. Linux bodies escape provider text because notification bodies may interpret markup. Tests cover disabled startup, denied/error paths, partial-startup cleanup/retry, cancellation, and shutdown without generating real system notifications.

**Product decision:** repository preferences select opened, closed, merged, and review-requested transitions. The first imported snapshot establishes a baseline instead of notifying about historical PRs. Poll-derived transitions are not a complete provider event history and can miss intermediate changes. Actor names are attached only when the actual actor is known; the PR author is not assumed to be the person who closed, merged, or requested review.

## Local Git: parsing, operations, and safety

**Primary source:** porcelain v2 status records expose index/worktree status, branch object/upstream metadata, renames, and conflicts. `-z` preserves filename boundaries without relying on whitespace or line splitting. Diff output can compare worktree/index, index/HEAD, or merge-base/head. [Git status manual](https://git-scm.com/docs/git-status), [Git diff manual](https://git-scm.com/docs/git-diff).

**Implementation and temporary-repository verification:** status uses `--porcelain=v2 -z --branch --untracked-files=all`. DTOs preserve staged and unstaged flags separately, rename source paths, conflict states, detached/unborn HEAD, and exact `headSha`. History retains full object IDs, parents, and refs, capped at 200 commits. The graph draws only known parent connections and marks unknown history boundaries. Tests cover partial staging, rename/unstage, whitespace/newline filenames, conflicts, unborn repositories, detached HEAD, and a different branch whose newer history must not replace the checked-out HEAD.

**Implementation decision:** Git is discovered as an executable and invoked with direct argument vectors, never a shell command. File arguments are confined to the selected repository; refs/remotes reject option-like input and branch names are checked with Git. Repository operations serialize per canonical root and honor context/timeouts. Default operation limits are 15 seconds for local commands, two minutes for remote commands, five minutes for clone, with bounded captured output. Git terminal credential prompts are disabled. These bounds are application choices, not Git defaults. [Git branch-name validation](https://git-scm.com/docs/git-check-ref-format).

**Primary source:** ordinary `git switch` preserves changes when safe and rejects a switch that would overwrite them. `git pull --ff-only` refuses a non-fast-forward integration. [Git switch manual](https://git-scm.com/docs/git-switch), [Git pull manual](https://git-scm.com/docs/git-pull).

**Implementation and local tests:** checkout uses safe switching without force; remote-tracking checkout explicitly creates a tracking branch only when its local name is available. Pull is fast-forward-only; push does not force. PR checkout fetches `refs/pull/N/head` from `origin`, then uses `pr-N`; an existing branch at a different object is preserved with an error. Compatible uncommitted work may survive a switch; conflicting work is rejected by Git. PR checkout is an explicit user action, unlike read-only code analysis. [GitHub's local PR checkout guidance](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/reviewing-changes-in-pull-requests/checking-out-pull-requests-locally).

**Limit:** serialization coordinates this application, not every other process editing a repository. Git locking handles its own writes; snapshots still need validation when external tools change index/worktree contents concurrently. Git hooks and repository Git configuration retain their normal Git behavior when a user intentionally commits or performs a Git operation; the client is not a sandbox for arbitrary repository tooling.

## Git credentials and remote URLs

**Primary source:** Git supports process configuration through `GIT_CONFIG_COUNT` with indexed keys/values, HTTP extra headers scoped by URL, and redirect policy. [Git configuration manual](https://git-scm.com/docs/git-config).

**Implementation and synthetic-HTTP verification:** HTTPS Git authentication can use a host-matched saved account token through an ephemeral `Authorization` extra header. The token is passed through process environment configuration, not written to `.git/config` or command arguments. The authenticated invocation disables HTTP redirects to avoid sending credentials to a redirected origin. Existing SSH setup remains Git's responsibility. Without an application token, normal Git credential-helper behavior can still work. Credential-bearing HTTPS clone URLs have userinfo removed before entering clone arguments/config; the private raw input is used only to derive authentication. Displayed remote URLs and command errors redact URL userinfo, query values, and known token/header secrets.

**Verification limits:** tests use synthetic credentials and local HTTP servers to inspect header scoping, redirect rejection, and clone sanitization. This does not prove every third-party Git credential helper, SSH agent, proxy, custom certificate authority, or OS secret-store setup works. No real user token is printed in research results.

## Exact source snapshots: why `git archive` was rejected

**Primary source:** `git archive` is an export operation. Git attributes `export-ignore` and `export-subst` can omit tracked files or substitute contents. That is appropriate for release packaging, but not for exact reviewed source. [Git archive manual](https://git-scm.com/docs/git-archive), [Git attributes](https://git-scm.com/docs/gitattributes).

**Local experiment:** a committed `hidden.ts export-ignore` rule removed a reviewed file; a committed `substitute.ts export-subst` rule replaced literal `$Format:%h$` with a commit abbreviation. `git show HEAD:substitute.ts` retained the original bytes. The original archive-based LSP snapshot was therefore incorrect and was replaced.

**Implementation:** immutable snapshots list tree entries with `ls-tree --full-tree -r -z` and read recorded object IDs through bounded `cat-file --batch-check` / `--batch`. Paths do not enter the line-based cat-file request protocol, so newline filenames cannot change its framing. Export attributes do not alter the content. Regular source blobs are supported; tracked symlinks and gitlinks are not materialized as arbitrary external content. Bounds are 20,000 source files, 64 MiB total eligible source, and four MiB per document. [Git ls-tree](https://git-scm.com/docs/git-ls-tree), [Git cat-file](https://git-scm.com/docs/git-cat-file).

**Index experiment and fix:** reading stage-zero contents later through `:0:path` rereads the live index and can mix objects after an external `git add/reset`. The replacement retains immutable stage-zero blob IDs from one `ls-files --stage -z` listing. Conflicted indexes are explicitly unsupported for a coherent index analysis snapshot. [Git ls-files](https://git-scm.com/docs/git-ls-files).

**Working-tree experiment and fix:** overlaying only status-reported changes misses present files marked assume-unchanged or skip-worktree. Working capture instead reads all recorded tracked files and untracked source paths; missing sparse files can fall back to their recorded index blobs. It checks file metadata and index/name listings for detected concurrent changes. This detects common races; it is not an operating-system atomic filesystem snapshot. Deterministic captured contents produce an immutable SHA256 revision digest.

**Primary source and inference:** three-dot comparison uses the merge base. A PR's current base-tip SHA can differ from the source bytes represented on the old side of its patch. Code analysis therefore resolves the merge base when `comparisonHead` is supplied; it must not analyze the latest worktree as a substitute. [Git diff three-dot behavior](https://git-scm.com/docs/git-diff), [Git merge-base](https://git-scm.com/docs/git-merge-base).

**Read-only fetch implementation:** missing selected PR objects are obtained only through a configured remote matching host/owner/repository identity. The fetch uses `--no-tags --no-write-fetch-head --recurse-submodules=no --refmap=` and a PR head ref or required immutable object ID. It adds objects without checkout, index changes, local branch changes, remote-tracking ref changes, or changing `FETCH_HEAD`. An unavailable object remains an error rather than falling back to another revision. [Git fetch options](https://git-scm.com/docs/git-fetch).

**Real-server experiment:** a committed workspace package returned `string`, while its dirty local source returned `number`; an original `node_modules` workspace symlink caused TypeScript hover to report the dirty version. Installed dependency source/metadata is now captured into private revision directories, workspace package links rebase to corresponding selected source, and Composer generated base paths are rebased. The real regression now returns the committed signature and a project definition with `external=false`. Full contracts, bounds, and remaining dependency caveats are in [Language intelligence](LANGUAGE_INTELLIGENCE.md).

## GitHub REST/GraphQL responsibilities

**Primary source:** PR REST endpoints provide list/detail/files/commits/create/update/merge operations; review endpoints group review comments with a decision and optional body; review-comment endpoints provide inline comments and replies. General PR conversation comments and some metadata use issue endpoints because a PR also has issue functionality. GraphQL supplies thread identity/resolution, viewer capabilities, draft transitions, and selected protection/review metadata. [PR endpoints](https://docs.github.com/en/rest/pulls/pulls), [reviews](https://docs.github.com/en/rest/pulls/reviews), [review comments](https://docs.github.com/en/rest/pulls/comments), [issue comments](https://docs.github.com/en/rest/issues/comments), [GraphQL reference](https://docs.github.com/en/graphql/reference).

**Implementation:** the provider combines these APIs instead of pretending one endpoint returns the full review model. Thread pages and comment pages inside each thread are paginated independently. REST list pagination follows `Link` metadata. Unknown/unavailable totals are represented as unknown, not fabricated zero counts. GitHub's changed-file endpoint can return at most 3,000 files, and omitted patch data cannot be reconstructed into complete code by a frontend fixture. The UI surfaces incomplete-file/patch warnings. [REST pagination](https://docs.github.com/en/rest/using-the-rest-api/using-pagination-in-the-rest-api), [changed-file endpoint](https://docs.github.com/en/rest/pulls/pulls#list-pull-requests-files).

### Current schema compatibility finding

**Live primary API experiment, 13 October 2026:** read-only introspection validated all 37 selected nested fields used by the provider queries across nine relevant types. In the live GitHub.com schema, `PullRequestReviewComment` has `fullDatabaseId`, `path`, `line`, and `originalLine`; it does **not** have `diffSide` or the previously used `databaseId`. `diffSide` belongs to `PullRequestReviewThread`. Replies use their own `fullDatabaseId` field. The provider inherits each comment's side from its thread, including later comment pages. The schema was queried rather than assuming older examples still applied. [GitHub GraphQL reference](https://docs.github.com/en/graphql/reference), [review-comment reference address](https://docs.github.com/en/graphql/reference/objects#pullrequestreviewcomment), [review-thread reference address](https://docs.github.com/en/graphql/reference/objects#pullrequestreviewthread), [BigInt scalar reference](https://docs.github.com/en/graphql/reference/scalars#bigint).

**Local parsing verification:** BigInt values are decoded directly from quoted decimal or JSON-number representations into checked Go integer values; they do not pass through `float64`. Tests cover identifiers beyond JavaScript's exact-integer range, inherited old-side coordinates on merged/outdated threads, nested pagination, malformed/overflow values, and schema errors. Node IDs remain separate from database IDs. A GraphQL undefined-field error is reported as application/schema incompatibility, not misdiagnosed as missing token permissions.

**Read-only public smoke:** the exact updated thread query succeeded for the merged [wailsapp/wails PR #6254](https://github.com/wailsapp/wails/pull/6254), returning zero threads. The operation did not create comments/reviews, change labels, merge, or write to a repository. Zero returned threads validates query compatibility on that PR; populated old-side threads and large identifiers are covered by the controlled regression fixtures instead.

**Documentation caveat:** the old GraphQL `objects` URLs now redirect to a categorized reference index in the browser. Some categorized reference pages could not be fetched by the research tool. The exact live introspection is the evidence for the schema finding above; a redirect or unavailable webpage is not treated as documentation proof of nonexistent fields.

### Hosts, authentication, permissions, and policy

| Hosting mode | REST base | GraphQL endpoint | Verification |
| --- | --- | --- | --- |
| GitHub.com | `https://api.github.com` | `https://api.github.com/graphql` | Live schema/public read-only request plus fake-HTTP coverage |
| GitHub Enterprise Server | `https://HOST/api/v3` | `https://HOST/api/graphql` | Primary documentation and endpoint/host-validation tests; no claim of live private GHES validation |
| Enterprise Cloud with data residency | `https://api.SUBDOMAIN.ghe.com` | `https://api.SUBDOMAIN.ghe.com/graphql` | Primary documentation and endpoint normalization tests; no live enterprise tenancy exercised |

**Primary source:** Enterprise Server uses its own API host; data-residency Enterprise Cloud uses the enterprise's dedicated `api.*.ghe.com` host. This is a distinct deployment from Enterprise Server. [Enterprise Server REST usage](https://docs.github.com/en/enterprise-server@3.19/rest/using-the-rest-api/getting-started-with-the-rest-api), [data-residency API access](https://docs.github.com/en/enterprise-cloud%40latest/admin/data-residency/about-github-enterprise-cloud-with-data-residency), [GraphQL endpoint variants](https://docs.github.com/en/enterprise-cloud%40latest/graphql/guides/managing-enterprise-accounts).

**Implementation:** host normalization accepts HTTPS host/API-root input, rejects credential-bearing hosts/repository paths/query/fragment, and normalizes GitHub.com and `api.*.ghe.com` aliases. Saved OS-keyring credentials take precedence over an existing host-specific GitHub CLI login. Only account identity/status crosses the frontend boundary; tokens remain backend-side. CLI token lookup uses an argument vector and suppresses sensitive stderr. These are implementation choices, not a promise that every machine has a usable keyring. [GitHub CLI token retrieval](https://cli.github.com/manual/gh_auth_token), [GitHub CLI login](https://cli.github.com/manual/gh_auth_login), [go-keyring source](https://github.com/zalando/go-keyring).

**Primary source:** endpoint permissions differ. PR reading/reviews, issue metadata, checks, and merging do not share one universal permission. Organization SSO, access level, repository settings, and protection remain relevant even with an authenticated token. The REST merge operation supports an expected head SHA and repository-supported merge methods. [PR permissions and merge parameters](https://docs.github.com/en/rest/pulls/pulls), [review permissions](https://docs.github.com/en/rest/pulls/reviews), [checks](https://docs.github.com/en/rest/checks/runs), [issue metadata](https://docs.github.com/en/rest/issues/issues).

**Implementation and fake-provider verification:** merge includes the displayed expected SHA; review submission rechecks its displayed head before creating a review. A changed head causes refresh/error instead of submitting feedback against unseen code. A provider-side check can still race another remote push, so GitHub remains the final authority. Draft state, viewer permissions, allowed merge methods, checks, approvals, and unresolved threads inform the UI; they do not bypass branch policy.

**Protection caveat:** classic `branchProtectionRule` fields can supply required approvals and check contexts, but they do not model every ruleset or inaccessible rule. Missing metadata stays unknown and produces a warning. A locally computed blocker list is not permission to merge; GitHub may reject the operation under additional rules. [Protected branches](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches), [rulesets](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets).

### Conditional reads, rate limits, and sync

**Primary source:** GitHub recommends webhooks where possible and efficient authenticated conditional requests when polling is necessary. `If-None-Match`/ETag can yield `304`; rate-limit responses expose retry/reset information. [GitHub REST best practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api).

**Implementation and fake-HTTP verification:** cached REST responses retain body and pagination headers. A `304` response reuses cached `Link` headers even if GitHub omits them on the new response, preventing page two and later pages from disappearing. Successful mutations invalidate response caches. `Retry-After` and exhausted primary-rate-limit reset times suppress additional calls until their cooldown. GraphQL requests do not gain REST GET/ETag semantics just because the same client manages them.

**Product decision:** the desktop workspace refreshes local changes and provider activity on a 15-second default schedule and after explicit mutations. It queues refresh work, cancels abandoned requests, keeps local data when a provider read fails, and preserves pending review content when the remote head changes until the user accepts the new revision. A polling desktop app is not a webhook server; detection is delayed by the interval/network/server limits and can miss intermediate events. Redirects are disabled on authenticated provider HTTP calls in this implementation, so a moved repository redirect may require refreshing its identity rather than following an arbitrary authenticated redirect.

## Research questions and retained outcomes

| Question investigated | Primary starting point / experiment | Retained outcome |
| --- | --- | --- |
| Does supported provider mean full review parity? | GitKraken PR page and SourceGit README | Provider coverage and feature coverage must be documented separately. |
| Can the user-selected native-feeling UI use Wails3? | Wails overview, pinned bindings, actual installed runtime | Go services + React + native menus/dialogs are viable; pin and verify the bridge pair. |
| What is the actual service namespace? | Pinned `bindings.go` reflection | `main.Service.Method`, independent of the module slug. |
| Do desktop notifications safely initialize at app startup? | Pinned OS implementations; fake SDK lifecycle tests | Keep optional notification startup lazy; surface unavailable D-Bus/signing and denial errors. |
| How are unusual Git paths and stage states retained? | Porcelain v2 / NUL status; temp repos | Parse record boundaries, separate index/worktree flags, handle renames/unborn/conflicts. |
| Can PR checkout replace existing local work? | `git switch`; local PR-ref fixture | No force; preserve a differently positioned existing `pr-N` branch. |
| Is Git export an exact review snapshot? | Export attributes fixture | No; exact blobs replace `git archive`. |
| Is status enough to capture working source? | assume-unchanged/skip-worktree fixtures | No; inspect recorded tracked files plus untracked paths. |
| Is re-reading `:0:path` a coherent index snapshot? | External index-change review | No; retain stage-zero object IDs from one listing. |
| Can installed workspace aliases analyze dirty HEAD? | Real TS dirty-package signature regression | Yes without rebasing; private dependency capture now fixes the proven leak. |
| Are GitHub comment fields stable across examples? | Live schema introspection | Current comment uses `fullDatabaseId`; diff side belongs to the thread. |
| Did the updated query work on a real merged PR? | Read-only wailsapp/wails #6254 query | Yes; zero threads, no provider writes. |
| Does `304` retain complete list pagination? | Fake server omitting `Link` on 304 | Only if the cached headers are retained; regression covers it. |
| Does one Enterprise URL cover all deployments? | GHES and data-residency Cloud docs/tests | No; distinct REST/GraphQL bases are required. |
| Does a Linux build establish Windows/macOS runtime behavior? | Toolchain and SDK source inspection | No; platform packaging/permissions/keychain/editor delivery remain explicit verification work. |

The source and experiment distinctions above are intentional. Documentation can establish a supported API, temporary tests can establish a specific implementation behavior, and a browser fixture can establish an interaction. None of those alone establishes end-to-end behavior on an untested operating system or private enterprise installation.
