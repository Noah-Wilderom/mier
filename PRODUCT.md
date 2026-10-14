# M.I.E.R.

M.I.E.R. stands for **Mag ik een review**. The project, package, and build slug is `mag-ik-een-review`.

## Register

product

## Users

Developers working with Git repositories and reviewing substantial amounts of code, including changes produced with AI assistance. The interface should make careful, frequent review comfortable while keeping everyday Git operations understandable for mixed development teams.

## Product Purpose

A desktop Git client built with Wails v3, targeting Linux, Windows, and macOS. Combine SourceGit's compact repository workspace with an integrated pull request workflow: discover, inspect, discuss, review, and merge changes. The first live provider is GitHub, including Enterprise Server and Enterprise Cloud hosts.

## Implemented Workspace

The native app opens or clones a real repository, reads its Git history and working tree, and uses its remote to identify the GitHub repository. Local Git work includes staged and unstaged diffs, staging, commits, branch creation/checkout/rename/merged-only deletion, fetch, fast-forward pull, and push. The repository toolbar exposes remote selection and tracking status. Branch comparisons and history inspection show committed file changes without changing the checkout or index. The standalone browser demo uses separate sample data for exploring the interface.

GitHub pull requests have an inbox and Conversation, Changes, Commits, and Checks views. Descriptions and discussions render Markdown, GFM tables and task lists, code fences, and emoji shortcodes; raw HTML is disabled and external links open in the browser. Users can create or update PRs, change draft status, close and reopen PRs, edit reviewer/assignee/label metadata, write comments, submit reviews with inline comments, reply to and resolve discussions, and merge with a permitted strategy. GitHub remains the authority for permissions, checks, protection, and merge eligibility. Writes report failure instead of presenting an unsuccessful action as complete.

Authentication reuses an existing GitHub CLI login or a validated token saved in the OS credential store. Accounts are selected by host, including enterprise hosts. Secrets remain in the Go backend and credential store; application account metadata contains hostnames and usernames, not tokens. HTTPS Git operations can use the same host-specific account, while SSH follows the user's existing configuration.

Local and PR code share grammar highlighting, configurable font size, and four syntax themes per appearance. Installed PHP, TypeScript / JavaScript, and Vue language servers provide symbol hovers and definition navigation in isolated revision snapshots. Definitions open in a read-only viewer, including available dependency sources, without changing the user's checkout. Local analysis distinguishes staged and working tree content.

Five inboxes distinguish all PRs, the user's PRs, requested reviews, assigned PRs, and drafts. Comments show account pictures, Bot badges, and Author badges. Editor launch actions support VSCode, PhpStorm, and Zed with remembered selection and a separate GitHub browser action.

Repository notification preferences cover opened, closed, merged, and review-requested events. In-app toasts and all event types default on; desktop notifications default off. The initial imported snapshot establishes a silent baseline.

Optional Jira Cloud setup accepts locally supplied OAuth app credentials. PR ticket discovery scans branch, title, and description references; repository mappings scope Jira sites/projects, and personal selections add or hide tickets. A separate reader displays descriptions, configured acceptance criteria, and recent comments while the GitHub review remains open. Jira operations are read-only. See [Jira setup and qualification limits](docs/JIRA.md).

## Review Continuity

Imported recent repositories and their pull request inboxes refresh in the background every 15 seconds, including the selected PR while it is watched. Updates bring in discussions, reviews, checks, and local changes while preserving the selected file and review progress. A manual refresh is available. Timeouts, offline operation, permission failures, and rate limits must remain understandable without discarding useful local state.

New commits never silently replace a diff during review. Show an explicit new-commit notice and let the reviewer choose **Refresh changes**. Keep draft comments, mark drafts attached to an older head as outdated, and require them to be repositioned before submission. Review and merge writes verify the expected head revision.

Expanding Changes gives code the full window while retaining the selected file, viewed files, and drafts. Escape returns to the previous workspace; an open dialog or command palette handles Escape first.

Commit messages and PR creation drafts persist per repository across navigation and restart. PR creation previews the local committed comparison and requires the compare branch to be published to the matching GitHub remote. Commit inspection shows the selected SHA's changes, including root and merge commits, and identifies unavailable GitHub patches. Rejected writes retain draft text. Accepted remote actions remain successful when a follow-up read fails; the user can refresh without repeating the write. Requests from an abandoned repository cannot update the active workspace.

## Scope and Validation

GitKraken informs local branch, cherry-pick and merge-conflict workflows. M.I.E.R. supports partial staging, guarded local merges/cherry-picks, editable conflict resolutions and continuing or aborting existing Git operations. GitHub is the current hosting provider; other hosting integrations, starting interactive rebases and stash management remain future work. Inaccessible protection details and missing or limited patches are communicated as such.

The retained suite covers isolated Git operations, provider HTTP requests, frontend logic, and browser workflows through the Wails RPC transport. CI tests and builds Linux, macOS, and Windows, with Linux distribution package inspection. The release workflow builds both architectures for all three platforms and signs the updater archives. Native window behavior, real keychain/provider writes, and installer upgrades still need target-platform release qualification. See [validation](docs/VALIDATION.md) and [release readiness](docs/PRODUCTION.md) for the evidence and remaining external setup.

## Brand Personality

Calm, practical, familiar. SourceGit is the reference for desktop interaction. The user selected the light Review Inbox visual direction: a compact sidebar, a pull request list, and a generous code review workspace. GitKraken is the reference for pull request breadth.

## Anti-references

Avoid marketing-style layouts, large decorative cards, unnecessary motion, and browser-like flows that interrupt work. Keep Git and pull request terminology consistent and explain complex actions where a mixed team needs help.

## Design Principles

- Keep repository context visible throughout a review.
- Use compact, predictable panes and familiar desktop controls.
- Make change status and action consequences clear.
- Keep review conversations, file changes, checks, and decisions in one workspace.
- Give code review the full application window when requested, with a clear way back to the inbox.
- Follow the system light/dark appearance, with an explicit user override.

## Accessibility & Inclusion

Provide keyboard navigation, visible focus, labeled controls, sufficient contrast, and reduced-motion support. Pair state colors with icons and text. Use system typography and selectable review and code content.

AI PR review supports ChatGPT through the local Codex app-server, GitHub Copilot through its official SDK, and explicitly connected external MCP clients. Findings become private native comments. The user controls publication through the existing review flow. These reviewers inspect captured changes and source; general coding chat, autonomous working-tree edits and automatic GitHub submission are outside this implementation. See [AI review](docs/AI.md).
