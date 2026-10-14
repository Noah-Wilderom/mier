# Local Git workflows

## Build a commit

Open **Changes** to filter files, inspect their staged or working-tree diff, and stage files individually or together. Modified text files also offer **Stage section** and **Unstage section**. Partial staging changes the index and preserves the working file. If the diff changes in another editor, reload it before applying a section.

Additions, deletions, renames, binary files, and conflicts are staged as whole files. A file can appear in both lists when only some of its changes are staged. The commit message is retained per repository when you navigate away.

## Cherry-pick a local commit

Open **History**, choose a local branch in **History from**, select a commit and click **Cherry-pick commit…**. Review the destination branch and confirm. Merge commits require an explicit mainline parent. The original author is retained, and the source SHA is recorded with Git's `-x` attribution.

Commit or stash existing changes first. M.I.E.R. checks the destination branch and HEAD again before applying the selected immutable commit; it never checks out a different branch implicitly.

## Merge a branch

Open the branch selector, then **Merge…** beside a local branch. Preview its changes relative to your current branch and confirm the destination. Git fast-forwards when possible and otherwise creates a merge commit. Source and destination revisions are checked again before merging.

## Resolve conflicts

An operation bar appears for merges, cherry-picks, rebases, reverts and patch application, including operations started in another client. Choose **Resolve conflicts** to open Changes.

The conflict editor shows the current and incoming versions above an editable result. Accept either side per section, accept both in current-first order, inspect the common ancestor, or edit the result yourself. **Undo choice** reverses a section choice. **Whole file** also handles binary versions and delete/modify conflicts. Saving checks for remaining markers, writes the result and stages the file.

Unsaved resolutions remain in memory when switching files or views. They do not survive quitting the app. If another editor changes the file or index, saving is rejected and your edited result stays available to copy. Reloading a dirty result asks before discarding it.

When all conflicts are resolved, review the staged diff and choose **Continue** in the operation bar. **Abort…** restores the pre-operation Git state and discards resolution edits; **Skip commit…** is available for sequenced commits and also requires confirmation. These actions do not push to a remote.

During rebase, Git's current index version is the rebased result, and the incoming version is the commit being replayed. The editor labels these explicitly. Symbolic-link conflicts require choosing a complete version; submodule conflicts require an external Git editor. Text editing is bounded to 2 MiB per file, and files over 64 MiB require an external editor.
