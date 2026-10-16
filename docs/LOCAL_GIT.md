# Local Git workflows

## Build a commit

Open **Changes** to filter files, inspect their staged or working-tree diff, and stage files individually or together. Modified text files also offer **Stage section**, **Unstage section**, and checkboxes to stage or unstage selected changed lines within a section. Partial staging changes the index and preserves the working file. If the diff changes in another editor, reload it before applying a section.

Additions, deletions, renames, binary files, and conflicts are staged as whole files. A file can appear in both lists when only some of its changes are staged. The commit message is retained per repository when you navigate away.

The optional conventional commit type prefixes your message without generating it. **Amend last commit** preloads the previous message when the draft is empty and asks before replacing the displayed HEAD. A message-only amend does not require staged files. A checkpoint preserves the old commit. Amending a published commit still requires coordination with collaborators.

## Discard, ignore, and recover files

**Discard file…** previews unstaged changes before restoring the index version or removing an untracked file. Modified text sections and selected lines can also be discarded. The backend compares file bytes, type, permissions and index with the preview; a stale preview cannot overwrite newer edits. Staged content is preserved.

Each discard first saves the original file in the worktree's Git directory under `mier/discard`. **Recovery…** restores these copies only if the file and index still match the state after discarding. These local copies are private, are not pushed, and persist until restored or manually removed. Files larger than 64 MiB, submodules, renames and conflicts require an external client for discarding.

**Ignore file** adds an anchored, escaped rule for an untracked file to the root `.gitignore`. It does not broaden wildcard characters in the filename or silently stop tracking an existing file.

## Stashes

**Stashes…** saves all tracked changes, keeps staged changes in place, or saves staged changes only. Untracked files can be included except in staged-only mode. Inspect a saved stash before applying, popping, or dropping it. Applying keeps the entry; popping removes it only after successful application. Conflicting applications retain the stash and expose the resulting files in Changes. Restoring the index is optional.

## Search history and inspect a file

**History** filters local or remote branches and searches commit messages, authors, or a full/abbreviated commit ID. **Load older commits** pages beyond the initial history. A changed ref invalidates an older pagination cursor and asks you to refresh.

In the selected commit's changes, choose a filename under **File history and blame** to inspect its revisions. History follows renames and reads the filename recorded at each commit. **Show blame** includes the originating commit, author and summary for each line. These views do not check out files or change the index.

## Cherry-pick a local commit

Open **History**, choose a local branch in **History from**, select a commit and click **Cherry-pick commit…**. Review the destination branch and confirm. Merge commits require an explicit mainline parent. The original author is retained, and the source SHA is recorded with Git's `-x` attribution.

Commit or stash existing changes first. M.I.E.R. checks the destination branch and HEAD again before applying the selected immutable commit; it never checks out a different branch implicitly.

Ctrl/Command-click or Shift-click history rows to select multiple commits. **Cherry-pick selected…** previews the replay order, with controls to reorder or remove commits. Merge commits must be picked individually to choose their mainline parent. A conflict pauses the Git sequencer and preserves the remaining queue.

## Reset, revert, and recover commits

**Revert commit…** creates a new commit undoing the selected commit. **Reset to commit…** previews the exact tree difference and offers soft (keep changes staged), mixed (keep changes unstaged), or hard (replace files) modes. Reset requires a clean worktree, checks the destination again, and refuses hard resets that would replace ignored files. Hard reset also requires an explicit file-replacement acknowledgement.

**Repository tools → Activity & recovery** lists HEAD reflog entries and M.I.E.R. checkpoints. Recovering creates a new branch at the selected immutable commit without changing working files. **Restore branch…** instead opens the guarded reset preview for that checkpoint or reflog entry. Checkpoints preserve HEAD before amend, cherry-pick, merge, rebase, reset, and revert; restoring through reset creates another checkpoint for returning to the newer state. They are local Git refs, excluded from normal history and normal branch pushes. This is scoped recovery, not a claim that every Git or filesystem action can be undone.

## Tags, remotes, and signing

**Repository tools** creates lightweight or annotated tags, renames or deletes local tags, and pushes one selected tag to an explicit remote. Renaming preserves the annotated tag object. Existing tags cannot be overwritten, and stale tag edits are rejected. Deleting a local tag keeps remote tags.

The same dialog adds, renames or removes remotes and edits their fetch/push URLs. Editing checks the previously displayed URLs before changing configuration. Removing a remote also removes its local tracking refs; it does not delete a server repository.

**Commit signing** configures repository-local OpenPGP, SSH, or X.509 signing using Git and an existing key. M.I.E.R. stores the Git key identifier/path, not private key contents. **Verify signature** in a commit's inspector reports Git's verification result; verification depends on the locally installed signer and trust configuration.

## Merge a branch

Open the branch selector, then **Merge…** beside a local branch. Preview its changes relative to your current branch and confirm the destination. Git fast-forwards when possible and otherwise creates a merge commit. Source and destination revisions are checked again before merging.

The preview also runs Git's merge calculation to predict conflicted paths or a fast-forward. It may write temporary Git objects, but does not change the index, working files, or start a merge. This requires Git 2.38 or newer. The actual merge refuses to overwrite ignored files.

## Rebase and compose history

**Repository tools → Rebase** previews the current branch's commits after a selected base, replayed onto a destination commit. Leaving the base empty uses the common ancestor. Every previewed commit appears exactly once in the plan. Reorder rows and choose pick, reword, edit, squash, fixup, or drop; reworded messages are editable. The first retained commit cannot squash/fixup. Linear ranges up to 100 commits are supported; merge topology requires an external client.

The branch and immutable plan token are checked again before applying. A checkpoint preserves the old HEAD. Rebase uses Git's real sequencer, including normal conflict resolution, skip and abort. Approved messages are stored privately in the Git directory and continue to work after a conflict pause or app restart. Neither messages nor caller-provided shell commands are interpolated into a rebase todo.

An **edit** step pauses after its commit. In Changes, use **Amend last commit**, then Continue in the operation bar. A message-only amendment is supported while paused at an edit step. Unrelated Git operations cannot use this exception.

## Worktrees

**Repository tools → Worktrees** lists the primary and linked worktrees, branches, lock reasons, and changed-file counts. Create a separate directory using a new branch, an existing branch, or detached HEAD, then **Open** it as a repository. The starting commit is explicit and current working files are preserved.

Worktrees can be locked and unlocked. Removal previews its directory and keeps the branch and commits. It refuses the current/primary worktree, stale entries, locked or dirty worktrees, and unregistered directories. Git may also refuse submodule-containing worktrees. Removal deletes ignored build files with the directory. Worktree creation preserves existing directories and refuses nested/overlapping worktrees, including paths reached through directory symlinks.

## Patches, submodules, and Git LFS

**Repository tools → Advanced** exports unstaged/staged binary-capable patches or a non-merge commit as an author-preserving mailbox. Save through the native file dialog. Import a file or paste its contents, choose working files, files plus index, or mailbox commits, and preview before applying. Changes to the patch, index, branch, or affected files invalidate approval. Mailbox conflicts use Git's actual apply-mailbox sequencer with continue, skip, and abort. Imports are limited to 8 MiB, 2,000 paths, and bounded binary expansion.

**Submodules** lists recorded and checked-out commits. Add a submodule in a new directory, initialize it, update to its recorded commit, edit/sync its URL, or open its repository. Updates preserve dirty work, protect ignored-file collisions, and force ordinary checkout rather than repository-configured update commands. Deinitialization requires confirmation and clean files. It removes the checkout, including ignored files, but retains Git's stored repository. Nested submodules are managed by opening their parent module.

**Git LFS** uses the installed Git LFS CLI. Enable repository filters, track patterns or exact filenames, mark patterns lockable, and stop tracking with confirmation. Stage .gitattributes with the affected files. Existing history is kept. Fetch/download objects, check out cached objects, and explicitly load remote locks. File unlocks verify ownership and never force another person's lock. Older CLI versions fall back to textual listings when JSON catalogs are unavailable.

## Gitflow and bisect

**Repository tools → Advanced → Gitflow** configures conventional production/development branches and feature/release/hotfix prefixes in repository-local Git configuration. A missing development branch is created from production. New features/releases start at development; hotfixes start at production. Starting a branch requires clean files.

Finishing first saves a reviewable plan. Features merge into development. Releases/hotfixes merge into production, create an annotated tag, then merge production into development. Each step is explicit and checks source/destination revisions. Conflicts use the normal merge editor; after continuing or aborting that merge, reopen Gitflow to resume. Progress survives app restarts. Source branches stay by default; optional deletion is a final guarded step. Canceling a plan keeps completed merges/tags and recovery checkpoints. No step pushes or rewrites remote history.

**Bisect** searches between full good/bad commit IDs, where good must be an ancestor of bad. Git records the bisection and M.I.E.R. explicitly checks out candidates without overwriting ignored files. Test each commit, then mark good, bad, or skip. The operation bar displays the log and final result. Ending requires confirmation and clean files, then returns to the original branch. Sessions begun in another client are also detected; marking a result adopts the same guarded checkout behavior.

## Terminal and custom commands

**Repository tools → Advanced → Terminal & custom commands** opens an installed terminal in the current repository. Terminal paths and working directories are passed as literal arguments.

Custom commands are stored privately on this device, outside the repository. Save an executable, a JSON array of arguments, and a timeout (1–900 seconds); saving never runs it. **Run** executes the selected definition against the displayed checkout, with live stdout/stderr and **Cancel command**. App Git mutations are serialized while it runs, and repository status refreshes when it finishes or fails. Each output stream displays up to 512 KiB without stopping the process when the display limit is reached. Commands inherit the user's environment and permissions; shell syntax requires explicitly choosing a shell executable. They are never loaded or run automatically from repository configuration.

**Repository tools → Coding agents** manages separate worktrees for Codex/Copilot file-editing tasks. Open a result to review its local diff; use the terminal/custom-command controls there to run checks yourself, then stage and commit normally. Commit/PR drafting, branch explanations, and conflict proposals are described in [AI assistance](AI.md). Using a proposal never commits, publishes, or stages it automatically.

## Stacked branches and pull requests

**Repository tools → Advanced → Branch stacks** creates a child at the current branch's exact tip and records its parent/fork point in local Git configuration. Existing branches can be linked with a reviewed fork point; cycles are rejected. Each branch is restacked separately, parent first. Preview lists only commits after the saved fork point. Restack uses the real rebase sequencer, checkpoints, conflict editor, and abort/continue. Successful completion records the new fork point, including after a conflict pause; abort keeps the original one. An empty child can move to its updated parent. Merge-topology ranges use an external client, as in the normal rebase tool.

Publishing is separate. **Preview publishing** reads the configured push destination's current branch. A rewrite requires explicit acknowledgement. **Publish with lease** pins both the local commit and remote commit (or absence of a remote branch); changed previews are rejected and Git's exact lease protects the final push. This never performs an unconditional force push. Original remote commits already present locally are checkpointed before replacement.

In a GitHub PR, expand **PR stack** to navigate related PRs as separate closable tabs. Relationships match base/head branches and repository identities, including recently closed/merged parents; matching branch names in unrelated forks are excluded. Ambiguous parents/cycles are shown as warnings. Discovery loads up to the 1,000 most recently updated PRs and reports truncation. To create a stacked PR, select its parent's published head as the base in the normal creation form. Changing a PR's base requires a preview and checks the reviewed head/base before the request; draft review comments must first be submitted or removed.

## Resolve conflicts

An operation bar appears for merges, cherry-picks, rebases, reverts and patch application, including operations started in another client. Choose **Resolve conflicts** to open Changes.

The conflict editor shows aligned current/incoming sections with synchronized scrolling above a CodeMirror result editor with line numbers, syntax highlighting, and editing history. Accept either side per section, accept both in either order, inspect the common ancestor, or edit the result yourself. **Undo choice** reverses a section choice. Alt+Up/Down navigates sections; Alt+1/2 accepts a side and Alt+3/4 accepts both in each order. **Whole file** also handles binary versions and delete/modify conflicts. Saving checks for remaining markers, writes the result and stages the file.

Unsaved resolutions remain in memory when switching files or views. They do not survive quitting the app. If another editor changes the file or index, saving is rejected and your edited result stays available to copy. Reloading a dirty result asks before discarding it.

When all conflicts are resolved, review the staged diff and choose **Continue** in the operation bar. **Abort…** restores the pre-operation Git state and discards resolution edits; **Skip commit…** is available for sequenced commits and also requires confirmation. These actions do not push to a remote.

During rebase, Git's current index version is the rebased result, and the incoming version is the commit being replayed. The editor labels these explicitly. Symbolic-link conflicts require choosing a complete version; submodule conflicts require an external Git editor. Text editing is bounded to 2 MiB per file, and files over 64 MiB require an external editor.
