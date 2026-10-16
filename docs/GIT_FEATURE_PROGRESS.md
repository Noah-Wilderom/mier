# Git feature implementation

This tracks the gaps in the October 2026 GitKraken / SourceGit comparison.
An unchecked item is planned, not a supported capability. AI additions come last.

- [x] Selected-line staging and unstaging
- [x] Discard with preview, stale-file protection, and recovery
- [x] Ignore controls
- [x] Stash save, preview, apply, pop, and drop
- [x] Amend and conventional commit helpers
- [x] Searchable, paginated history and reference filtering
- [x] File history, revision contents, and blame
- [x] Multiple-commit cherry-pick
- [x] Rebase and interactive history composition
- [x] Reset, revert, recovery checkpoints, and activity
- [x] Tags, remotes, and signing configuration
- [x] Conflict editor alignment, result highlighting, both orderings, and shortcuts
- [x] Merge conflict preflight
- [x] Worktree management
- [x] Submodules and Git LFS
- [x] Patch import and export
- [x] Gitflow and bisect
- [x] Terminal and custom commands
- [x] Stacked pull requests
- [x] GitLab, Bitbucket Cloud, and Azure DevOps Services review providers
- [x] AI commit / PR writing and branch explanations (last)
- [x] Coding-agent worktrees (last)
- [x] AI conflict proposals with manual approval (last)

Each group must have real Git/provider integration, appropriate automated tests,
and usable controls before being marked complete.

Provider-specific API limits and authentication are documented in [Review providers](REVIEW_PROVIDERS.md). The native provider group is covered by HTTP contract tests, real Git source/checkout tests, and Chromium/WebKit review and account flows. [AI assistance](AI.md) documents the separate proposal and coding tools, manual use/save steps, subscription connections, and explicit limits. Live Linux Codex/Copilot checks drafted commits and edited isolated worktrees while preserving the original checkout and index.
