# M.I.E.R.

**Mag ik een review**. Project and build slug: `mag-ik-een-review`.

A desktop Git and pull request workspace built with Wails v3, Go, React, and TypeScript. The native app opens real Git repositories and connects to GitHub, GitHub Enterprise Server, and GitHub Enterprise Cloud hosts. Git and provider actions operate on the selected repository and account.

The interface follows the approved Review Inbox design: compact navigation, a pull request inbox, and a code-first review pane. The Changes workspace expands to the full window while retaining the selected file, viewed progress, and pending comments.

Documentation: [product scope](../PRODUCT.md), [design and initial reference](../DESIGN.md), [architecture](ARCHITECTURE.md), [language intelligence](LANGUAGE_INTELLIGENCE.md), [research index](RESEARCH.md), [validation](VALIDATION.md), and [brand asset sources](../ASSET_ATTRIBUTIONS.md).

## Run the native app

Install Go 1.25 or newer, Git, Node.js with npm, and the platform dependencies described in the [Wails installation guide](https://v3.wails.io/getting-started/installation/). Install the CLI version used by this project:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117
wails3 doctor
```

From the project root:

```sh
wails3 dev
```

Choose **Open repository** or **Clone repository** on the welcome screen. Recent repositories are available there too. A GitHub remote supplies the account hostname and repository identity; local Git work remains available without a connected GitHub account.

## Connect an account

The app can use an existing [GitHub CLI login](https://cli.github.com/manual/gh_auth_login). For GitHub.com:

```sh
gh auth login --hostname github.com
```

For GitHub Enterprise Server, use your company's hostname:

```sh
gh auth login --hostname github.your-company.com
```

In **Settings → GitHub accounts**, enter the matching host and choose **Use GitHub CLI login**. Enterprise Cloud data-residency hosts such as `your-company.ghe.com` are also supported. Authentication is checked against the selected host before it is used.

If account connection fails, run `gh auth status --hostname github.com` (or your Enterprise hostname) to check the CLI login. A token lookup timeout is reported separately from a missing login. Inbox pages have a 30-second request timeout and selected PR details have a separate 90-second timeout; background refreshes never overlap for the same local repository.

Alternatively, choose **Use a saved token**, enter an access token, and select **Validate and save token**. Validated tokens are stored per host in the operating system credential store: macOS Keychain, Windows Credential Manager, or a Linux Secret Service keyring. An app-saved token takes precedence over the CLI login for that host. Removing it does not log out GitHub CLI; a valid CLI login can become active again. Tokens are not returned through frontend service responses or saved in the application's account metadata. If the OS keyring is unavailable, use an existing CLI login or unlock the keyring; the app does not fall back to plaintext token storage.

Repository access and token permissions determine which operations GitHub permits. Read access supports browsing; submitting reviews, editing PR metadata, and merging require the corresponding write access. Organization SSO and repository protection continue to apply. The same account can authenticate HTTPS Git clone/fetch/pull/push for that host. SSH remotes continue to use your existing SSH configuration.

## Work with a repository

- Inspect commit history, local and remote branches, remotes, tracking status, and working tree changes.
- View staged and unstaged diffs, stage or unstage selected files, and commit staged changes.
- Use the repository toolbar to create, switch, rename, or delete merged local branches, choose a remote, fetch, pull, and push. Pull uses fast-forward-only behavior; push sets upstream tracking and does not force-push. Delete confirmation never force-deletes unmerged work.
- Compare committed branch changes in a read-only dialog and inspect individual history or PR commits without checking them out. Root commits compare against an empty tree; merge commits compare against the first parent.
- Browse, search, and filter real open, draft, closed, and merged pull requests. The inbox requests 25 matching PRs at a time; choose **Load more** to fetch the next page. Filters and searches run on GitHub, and counts describe the full matching result rather than only the loaded rows. Inspect Conversation, Changes, Commits, and Checks.
- Keep multiple PRs open in closable tabs above the review. Clicking an already opened PR selects its existing tab. Tabs stay available across inbox pages and filters, including expanded review. Use Left/Right, Home/End, and Delete while a tab is focused, or close with the × button or middle-click. Closing an active tab selects its neighbor; closing the final tab leaves the review pane empty.
- Each PR remembers its section, selected file, diff options, scroll position, unfinished inline/conversation replies, review summary, metadata edits and AI pane/focus. Private native snapshots restore tabs and unfinished text when reopening a repository or restarting the app. Closing a tab retains its draft for reopening. Saved pending comments keep their existing revision checks and persisted draft store. See [workspace restoration and incremental reviews](WORKSPACE.md).
- Use separate inboxes for all PRs, your PRs, requested reviews, PRs assigned to you, and drafts. Comment authors show profile pictures, an Author badge when appropriate, and a Bot badge for bot accounts.
- Review split or unified diffs, filter files, mark files viewed, and expand code review to the full window. Selected files, viewed progress, and pending comments remain in place when expanding or returning to the inbox.
- Read Markdown descriptions and comments with headings, lists, code fences, tables, task lists, and emoji shortcodes. External web links open in the browser; raw HTML is disabled.
- Write conversation comments and inline review drafts, reply to discussions, resolve or reopen threads, and submit an approval, request for changes, or comment review.
- Create draft or open PRs with a committed comparison preview, edit titles and descriptions, change draft status, close or reopen PRs, and edit reviewers, assignees, and labels. Publish the compare branch before creating a PR. Commit messages and PR creation drafts are saved per repository across navigation and restart.
- Merge with a strategy allowed by the repository. GitHub enforces checks, review requirements, permissions, and protection. Reviews and merges verify the expected head commit so an unseen revision cannot be submitted accidentally.
- Open the current repository in VSCode, PhpStorm, or Zed using the editor split button. VSCode is the default; successful launches remember the last editor. A separate GitHub button opens the selected PR or repository in your browser.

See the PR tabs in [dark appearance](../art/mier-pr-tabs-dark.png) and [expanded light appearance](../art/mier-pr-tabs-light.png). These screenshots use sample RPC data.

## Code highlighting and language intelligence

Local changes and PR Changes share grammar-based syntax highlighting. **Settings → Code review** offers a 12–20px code font and four preview cards for each appearance: GitHub Light, Light+, One Light, and Catppuccin Latte; GitHub Dark, Dark+, One Dark Pro, and Nord. Light and dark preferences are independent and follow the active app appearance.

The desktop app includes a [catalog of 57 language servers](LANGUAGE_SERVERS.md), including Phpactor, TypeScript / JavaScript, Vue, Dockerfiles, YAML and GitHub Actions. Hover over a symbol for its signature. Use **Ctrl/Cmd-click**, **F12**, or **gD** to open a definition in a separate read-only code viewer. References, document outlines and repository symbol search use the same revision-bound reader where the server supports them. Returning to the review preserves the diff, selected file, and draft comments.

PR analysis uses an isolated snapshot of the displayed commit (and the merge base for the old side). Local changes use separate snapshots of the Git index and working tree, including uncommitted files. M.I.E.R. rejects language-server edit requests and keeps the code viewer read-only. Analysis is recreated when local source content changes; responses from abandoned views are canceled or ignored.

**Settings → Code review → Language intelligence** provides logos, search, pagination, installed/latest versions, pending-update badges, progress, cancellation, and retries. Forty catalog entries have managed install/update recipes; the other entries provide upstream setup instructions and executable configuration. Tools install in M.I.E.R.'s own configuration directory and preserve repository dependencies and custom server settings. Runtime prerequisites are shown when missing. Server discovery checks custom settings, managed installations, project dependencies, and PATH. Explicit executable, argument and supported SDK/plugin configuration remains available. See [language intelligence](LANGUAGE_INTELLIGENCE.md) for requirements, cache validation and startup timing. Completion, rename, diagnostic squiggles and code editing remain outside the current implementation.

## Notifications and apps

**Settings → Notifications** configures each GitHub repository independently. Pull request opened, closed, merged, and review requested events default on. In-app toasts default on; desktop notifications default off and request OS permission when enabled. The first recent page establishes a silent baseline. Subsequent background refreshes read recently updated PRs back to the previous successful refresh, without scanning unchanged history. An old PR appearing in that window establishes its baseline; it does not trigger an opened or review-requested notification based solely on its existing state. Missing rows never imply a close.

**Settings → Apps → Configure Jira** accepts your own Jira Cloud OAuth app credentials, guides callback/permission registration, and starts browser consent. Related tickets are discovered from PR branch/title/description references, with optional repository site/project mappings, manual search, and personal hide/restore. A reader displays descriptions, configured acceptance criteria, and recent comments while keeping the review open. Credentials and refresh tokens stay in the OS keychain. See [Jira setup](JIRA.md) for registration, storage, recovery, and live-account qualification limits.

**Settings → Apps → Configure Linear** accepts your own Linear OAuth client ID and guides callback registration. Authorization uses PKCE and read access, with rotating tokens in the OS keychain and no client secret. The shared ticket row switches between Jira and Linear without remounting the editor or its draft. Linear supports PR reference discovery, team filters, search, personal hide/restore, Markdown descriptions, and recent comments. See [Linear setup](LINEAR.md).

## Live updates during a review

The Go backend refreshes imported recent repositories and their PR inboxes every **15 seconds**, including the selected PR while it is watched. Conversation replies, review state, checks, and local Git status update without changing your selected review file. **Refresh now** in Settings requests an immediate update. Polls do not overlap; request timeouts and GitHub rate limits can delay a successful refresh, with failures shown in the workspace.

When a PR receives a new head commit, the app shows **New commits are available** and retains the diff you are reading and your draft comments. Choose **Refresh changes** to load the new revision. Drafts tied to an older commit remain available and are marked outdated for repositioning; unchanged files can retain their viewed state. Refreshing activity does not silently publish a review or replace the code under your cursor.

## Browser demo

The standalone frontend includes an interactive sample workspace for trying the interface:

```sh
cd frontend
npm ci
npm run dev
```

Open `http://127.0.0.1:9245`. Running only Vite uses sample data and local browser storage; it does not connect to Git or send provider actions. The native welcome screen also offers **Explore the demo workspace**. Demo data remains separate from live repository data.

The native app supplies platform window decorations, application menus, folder selection, and full screen. It remembers the normal window size and maximised state across restarts. Both interfaces support System, Light, and Dark appearance.

Keyboard shortcuts:

| Action | Shortcut |
| --- | --- |
| Command palette | Cmd/Ctrl K |
| History / Local changes / Inbox | Cmd/Ctrl 1 / 2 / 3 |
| Create pull request | Cmd/Ctrl Shift N |
| Expand code review | F |
| Return from expanded review | Escape |
| Previous / next review file | [ / ] |
| Move through the PR inbox | J / K |
| Filter the inbox | / |
| Toggle window full screen | Shift F while reviewing; native View menu also available |
| Go to definition | Ctrl/Cmd-click, F12, or gD in supported code |

## Verify and build

```sh
cd frontend
npm test
npx playwright install --with-deps chromium webkit
npm run test:ui
npm run build
cd ..
go test ./...
wails3 build
```

The native build is written to `bin/` (`bin/mag-ik-een-review` on Linux). Run the built app with `wails3 task run`. Build frontend assets before Go tests because the executable embeds `frontend/dist`; stop the Wails watcher during verification. CI builds all three desktop OSes, while target-platform keychain, notification, installer upgrade, and signing qualification remain separate. See [validation](VALIDATION.md) and the [Wails cross-platform guide](https://v3.wails.io/guides/build/cross-platform/).

`npm run test:ui` type-checks and runs the retained Chromium/WebKit suite in `frontend/tests/`. Playwright starts a separate Vite server on port 9255 and fixtures intercept Wails' HTTP RPC calls; no provider or user repository is modified. Tests retain traces/screenshots on failure. Linux WebKit uses Mesa software rendering and the suite runs one browser worker to remain usable on small desktop/CI hosts. [Playwright web-server configuration](https://playwright.dev/docs/test-webserver) and [API fixtures](https://playwright.dev/docs/mock) explain that harness.

For an isolated engine run, use `npm run test:ui -- --project=chromium` or `npm run test:ui -- --project=webkit`. On memory-limited hosts, finish native builds and standalone type checks before starting browser tests.

`npm test` also checks source and generated module names for case-insensitive import conflicts, including `.ts`/`.tsx` stems. Give components and state/helper modules distinct names: casing alone does not distinguish extensionless imports reliably on macOS and Windows. This check runs in the existing CI test matrix and catches these conflicts on Linux too.

Automated provider tests use fake HTTP servers and credential stores. They cover pagination, rate limits, permissions, review threads, mutation payloads, and stale-head protection without changing a remote user repository. Sync tests cover the 15-second default, cancellation, queued refreshes, and preservation of local data after provider failures.

Optional real language-server smoke tests use tooling outside the project. Set `MIER_LSP_SMOKE_TOOLS` to a directory containing `node_modules/typescript`, `typescript-language-server`, `@vue/language-server`, and `@vue/typescript-plugin`; add `phpactor.phar` there for PHP. Run `go test -run '^TestReal' -count=1 -v .`. These tests use temporary Git repositories and leave the user's checkout unchanged.

## Current scope

GitHub is the implemented pull request provider. GitLab, Bitbucket, and Azure DevOps integrations, conflict-resolution editing, rebasing, cherry-picking, and stash management are outside the current implementation. SourceGit and GitKraken guide the product direction; exhaustive feature parity is not claimed. Provider errors and inaccessible protection metadata are shown rather than replaced with invented checks or rules. GitHub API patch limits can restrict the code available for very large diffs.

## Implementation

- `frontend/src/App.tsx`: inbox, PR lifecycle, conversation, checks, local Git workspace, and appearance.
- `frontend/src/ReviewWorkspace.tsx`: code diffs, inline discussions, file navigation, and review submission.
- `frontend/src/Markdown.tsx`: safe Markdown, GFM, and emoji rendering shared by provider descriptions and discussions.
- `frontend/src/services.ts`, `liveData.ts`, and `useLiveWorkspace.ts`: typed backend calls, provider mapping, and revision-preserving updates.
- `repositoryservice.go` and `internal/gitclient/`: Git CLI operations and structured repository data.
- `githubservice.go` and `internal/githubclient/`: paginated GitHub REST/GraphQL reads and mutations.
- `credentialservice.go`: host-specific CLI authentication, OS credential storage, and nonsecret account metadata.
- `workspace_sync.go`: cancellation-aware background polling and workspace events.
- `prnotificationservice.go` and `desktopnotificationservice.go`: repository preferences, event transitions, and native notification delivery.
- `lspservice.go`, `internal/lspclient/`, and `frontend/src/CodeIntelligence.tsx`: isolated analysis, language-server transport, symbol hovers, and definition navigation.
- `editorservice.go` and `frontend/src/EditorActions.tsx`: editor discovery, launching, and remembered selection.
- `frontend/src/data.ts`: separate demo fixtures and review state helpers.
- `frontend/src/native.ts` and `main.go`: Wails menus, native command bridge, and full screen.
- `PRODUCT.md` and `DESIGN.md`: the confirmed product and visual direction.

## AI reviewer development

[AI review setup](AI.md) describes ChatGPT/Codex, Copilot and external MCP connections. Deterministic CI tests exercise Codex initialization, immutable read-only review configuration, rejection of command/file approvals, cancellation, a real local MCP transport and Copilot's SDK tool forwarding. No provider account is needed for the automated suite. The Chromium/WebKit cases cover manual finding acceptance/editing/batch selection, dismissal/restoration, human draft coexistence, duplicate starts, cancellation, external connection restoration, old revisions, activity/usage presentation and failed device login. Live subscription smoke reviews are a separate manual check using disposable source, without publishing GitHub comments.

Development actions share these runtimes through a separate typed MCP surface. Real Git tests cover immutable staged reads, stale proposals, conflict versions, scoped file writes, worktree identity/path/hash guards, cancellation, persistence, follow-ups after manual commits and clean-only removal. Browser tests exercise explicit commit/PR draft use, read-only explanations, conflict proposal/Undo/separate save, and agent worktree controls. CI does not send code to a model or require subscription credentials.
