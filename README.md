# M.I.E.R.

**Mag ik een review**. Project and build slug: `mag-ik-een-review`.

A desktop Git and pull request workspace built with Wails v3, Go, React, and TypeScript. The native app opens real Git repositories and connects to GitHub, GitHub Enterprise Server, and GitHub Enterprise Cloud hosts. Git and provider actions operate on the selected repository and account.

The interface follows the approved Review Inbox design: compact navigation, a pull request inbox, and a code-first review pane. The Changes workspace expands to the full window while retaining the selected file, viewed progress, and pending comments.

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

Alternatively, choose **Use a saved token**, enter an access token, and select **Validate and save token**. Validated tokens are stored per host in the operating system credential store: macOS Keychain, Windows Credential Manager, or a Linux Secret Service keyring. An app-saved token takes precedence over the CLI login for that host. Removing it does not log out GitHub CLI; a valid CLI login can become active again. Tokens are not returned through frontend service responses or saved in the application's account metadata. If the OS keyring is unavailable, use an existing CLI login or unlock the keyring; the app does not fall back to plaintext token storage.

Repository access and token permissions determine which operations GitHub permits. Read access supports browsing; submitting reviews, editing PR metadata, and merging require the corresponding write access. Organization SSO and repository protection continue to apply. The same account can authenticate HTTPS Git clone/fetch/pull/push for that host. SSH remotes continue to use your existing SSH configuration.

## Work with a repository

- Inspect commit history, local and remote branches, remotes, tracking status, and working tree changes.
- View staged and unstaged diffs, stage or unstage selected files, and commit staged changes.
- Create and check out branches; fetch, pull, and push through the Git backend. Pull uses fast-forward-only behavior; push sets upstream tracking and does not force-push.
- Browse, search, and filter real open, draft, closed, and merged pull requests. Inspect Conversation, Changes, Commits, and Checks.
- Review split or unified diffs, filter files, mark files viewed, and expand code review to the full window. Selected files, viewed progress, and pending comments remain in place when expanding or returning to the inbox.
- Read Markdown descriptions and comments with headings, lists, code fences, tables, task lists, and emoji shortcodes. External web links open in the browser; raw HTML is disabled.
- Write conversation comments and inline review drafts, reply to discussions, resolve or reopen threads, and submit an approval, request for changes, or comment review.
- Create draft or open PRs, edit titles and descriptions, change draft status, close or reopen PRs, and edit reviewers, assignees, and labels. Push the compare branch before creating a PR.
- Merge with a strategy allowed by the repository. GitHub enforces checks, review requirements, permissions, and protection. Reviews and merges verify the expected head commit so an unseen revision cannot be submitted accidentally.

## Live updates during a review

The Go backend refreshes the active repository, PR inbox, and selected PR every **15 seconds**. Conversation replies, review state, checks, and local Git status update without changing your selected review file. **Refresh now** in Settings requests an immediate update. Polls do not overlap; request timeouts and GitHub rate limits can delay a successful refresh, with failures shown in the workspace.

When a PR receives a new head commit, the app shows **New commits are available** and retains the diff you are reading and your draft comments. Choose **Refresh changes** to load the new revision. Drafts tied to an older commit remain available and are marked outdated for repositioning; unchanged files can retain their viewed state. Refreshing activity does not silently publish a review or replace the code under your cursor.

## Browser demo

The standalone frontend includes an interactive sample workspace for trying the interface:

```sh
cd frontend
npm ci
npm run dev
```

Open `http://127.0.0.1:9245`. Running only Vite uses sample data and local browser storage; it does not connect to Git or send provider actions. The native welcome screen also offers **Explore the demo workspace**. Demo data remains separate from live repository data.

The native app supplies platform window decorations, application menus, folder selection, and full screen. Both interfaces support System, Light, and Dark appearance.

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

## Verify and build

```sh
cd frontend
npm test
npm run build
cd ..
go test ./...
wails3 build
```

The native build is written to `bin/` (`bin/mag-ik-een-review` on Linux). Run the built app with `wails3 task run`. The Wails tasks include Linux, Windows, and macOS targets; see the [Wails cross-platform build guide](https://v3.wails.io/guides/build/cross-platform/) for required toolchains. Linux builds and isolated Git/provider tests have been exercised here. Local Git Go cross-compilation does not establish that Windows or macOS desktop binaries, keychains, packaging, or signing have been tested.

Automated provider tests use fake HTTP servers and credential stores. They cover pagination, rate limits, permissions, review threads, mutation payloads, and stale-head protection without changing a remote user repository. Sync tests cover the 15-second default, cancellation, queued refreshes, and preservation of local data after provider failures.

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
- `frontend/src/data.ts`: separate demo fixtures and review state helpers.
- `frontend/src/native.ts` and `main.go`: Wails menus, native command bridge, and full screen.
- `PRODUCT.md` and `DESIGN.md`: the confirmed product and visual direction.
