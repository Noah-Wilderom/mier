# Release readiness

M.I.E.R.'s current product scope is a desktop Git and GitHub review workspace, including GitHub Enterprise. [PRODUCT.md](../PRODUCT.md) defines that scope; [VALIDATION.md](VALIDATION.md) records what has actually been exercised.

## Implemented delivery requirements

- Real Git operations with repository locks, timeouts, literal argument vectors, fast-forward pulls, and pushes without force. Branch deletion requires merged work and explicit confirmation.
- Branch/network controls, committed comparisons, per-commit inspection, staged/unstaged review, and GitHub review/lifecycle workflows. Unavailable patches remain explicit.
- Optional Jira Cloud OAuth setup with local app credentials, keychain tokens, PR reference discovery, manual selection, repository mappings, and a read-only ticket reader. [Jira setup](JIRA.md) records Atlassian's distribution requirement and live-account qualification limits.
- Per-repository review, commit, and PR creation drafts; stale-response protection; duplicate-action guards; useful failure/retry states; render recovery; isolated language-server source snapshots.
- Managed PHP/TypeScript/Vue server installation and updates, version checks, cancellation/retry, verified downloads, and preservation of the working installation on failure.
- Retained frontend, Git, provider, sync, and Chromium/WebKit RPC regression tests. CI builds Linux, macOS, and Windows and inspects Linux packages.
- Six architecture/OS release targets, Windows setup EXEs, macOS DMGs, Linux DEB/RPM/Arch/AppImage packages, checksums, and verified Ed25519 updater archives. Package-manager installations use their package manager for updates.

The maintainer has chosen to keep M.I.E.R. unlicensed for now. There is no project `LICENSE`, and Linux package metadata remains `unknown`. Third-party assets and dependencies retain their own licenses.

## Maintainer setup before the first public release

1. Back up the local updater private key in 1Password using [the release instructions](RELEASING.md#signing-setup). The repository's `UPDATER_PRIVATE_KEY` Actions secret is configured, and the key remains outside Git. The workflow rejects a missing key or a key that differs from the committed public key.
2. Publish a SemVer-tagged release from tested code. Confirm all six update archives, every installer, `SHA256SUMS`, and `manifest.json` are attached. The manifest is uploaded last after signature verification.
3. Exercise first installation and an actual previous-version update on supported OS/architecture pairs. Test GitHub login/keychains, repository opening and Git writes, real provider reviews/merges in a disposable repository, editor discovery, notifications, and process cleanup.
4. Publish the generated `mier-bin` recipe to the AUR after licensing/package review to enable `omarchy pkg aur add mier-bin`. Release assets alone do not register an AUR or Omarchy package.

macOS downloads use Developer ID signatures and publish while notarization is pending, with that state identified in the release notes and `notarization.json`. A later resume staples and verifies accepted Mac builds and replaces their downloads. Windows installers remain unsigned. Updater signatures are a separate requirement. Native CI compilation is evidence of a build, and does not substitute for keychain, installation, or upgrade testing.

## Current boundaries

GitHub is the supported review provider. Known GitLab/Bitbucket remotes can still be used for local Git; their APIs are not routed through GitHub. Custom hosts are treated as possible GitHub Enterprise hosts. Conflict editing, rebase/cherry-pick/stash UI, Jira issue writes, native Jira development-panel lookup, and additional provider integrations remain future work. Live Jira OAuth consent, site permissions/field mappings, and target-platform keychain/callback behavior need qualification with a maintainer-supplied app.

Local comparisons cap at 2000 files; GitHub commit files cap at 3000, and missing or limited patches are identified in the view. Language servers can be installed and updated from Settings, with PHP/Node/npm prerequisites installed separately; analysis uses immutable source snapshots and read-only definition grants. Platform OS permission services require a usable desktop session.
