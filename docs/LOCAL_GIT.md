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

**Repository tools → Activity & recovery** lists HEAD reflog entries and M.I.E.R. checkpoints. Recovering creates a new branch at the selected immutable commit without changing working files. Checkpoints preserve HEAD before amend, cherry-pick, merge, reset, and revert. They are local Git refs, excluded from normal history and normal branch pushes. This is scoped recovery, not a claim that every Git or filesystem action can be undone.

## Tags, remotes, and signing

**Repository tools** creates lightweight or annotated tags, renames or deletes local tags, and pushes one selected tag to an explicit remote. Renaming preserves the annotated tag object. Existing tags cannot be overwritten, and stale tag edits are rejected. Deleting a local tag keeps remote tags.

The same dialog adds, renames or removes remotes and edits their fetch/push URLs. Editing checks the previously displayed URLs before changing configuration. Removing a remote also removes its local tracking refs; it does not delete a server repository.

**Commit signing** configures repository-local OpenPGP, SSH, or X.509 signing using Git and an existing key. M.I.E.R. stores the Git key identifier/path, not private key contents. **Verify signature** in a commit's inspector reports Git's verification result; verification depends on the locally installed signer and trust configuration.

## Merge a branch

Open the branch selector, then **Merge…** beside a local branch. Preview its changes relative to your current branch and confirm the destination. Git fast-forwards when possible and otherwise creates a merge commit. Source and destination revisions are checked again before merging.

## Resolve conflicts

An operation bar appears for merges, cherry-picks, rebases, reverts and patch application, including operations started in another client. Choose **Resolve conflicts** to open Changes.

The conflict editor shows the current and incoming versions above an editable result. Accept either side per section, accept both in current-first order, inspect the common ancestor, or edit the result yourself. **Undo choice** reverses a section choice. **Whole file** also handles binary versions and delete/modify conflicts. Saving checks for remaining markers, writes the result and stages the file.

Unsaved resolutions remain in memory when switching files or views. They do not survive quitting the app. If another editor changes the file or index, saving is rejected and your edited result stays available to copy. Reloading a dirty result asks before discarding it.

When all conflicts are resolved, review the staged diff and choose **Continue** in the operation bar. **Abort…** restores the pre-operation Git state and discards resolution edits; **Skip commit…** is available for sequenced commits and also requires confirmation. These actions do not push to a remote.

During rebase, Git's current index version is the rebased result, and the incoming version is the commit being replayed. The editor labels these explicitly. Symbolic-link conflicts require choosing a complete version; submodule conflicts require an external Git editor. Text editing is bounded to 2 MiB per file, and files over 64 MiB require an external editor.
