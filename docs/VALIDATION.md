# Validation record

Workspace, managed language tools, SSH remote handling, and large PHP dependency capture verified through **2026-10-14**. The earlier documentation handoff is preserved in [the historical record](validation/handoff-2026-10-13.md); its temporary QA paths and Jira simulation are historical, not the current test suite.

## Environment

| Component | Observed version |
| --- | --- |
| Native host | Ubuntu 24.04, Linux amd64, GTK 4 / WebKitGTK 6 |
| Go | 1.27.2; module minimum 1.25.0 |
| Node / npm | 24.21.0 / 11.19.0 |
| Wails CLI and Go module | 3.0.0-alpha2.117 |
| npm Wails runtime | 3.0.0-beta.29 |
| Vite / Shiki | 8.3.4 / 4.5.0 |
| Playwright | 1.64.0; Chromium and WebKit projects |
| PHP / Phpactor | 8.3.6 / 2026.10.07.0 |
| TypeScript / language server | 5.9.3 / 6.0.2 |
| Vue server / TypeScript plugin | 3.3.13 / 3.3.13 |

Actual language-server tools were installed outside the repository in `/tmp/mier-lsp-smoke-tools`. Phpactor indexing uses a bounded warmup in its smoke test. Browser tests run one worker; headless Linux WebKit uses Mesa software rendering because this host's GPU device is not available to the user.

The managed installer also downloaded and verified all three current servers into `/tmp/mier-managed-lsp-installation`. A separate temporary tools directory linked those newly installed packages into the five actual hover/definition tests. Desktop install/update checks used an isolated `XDG_CONFIG_HOME` under `/tmp/mier-native-lsp-validation`.

## Commands and results

| Check | Result |
| --- | --- |
| `npm --prefix frontend test` | 46 passed, zero failed or skipped |
| `npm --prefix frontend run test:ui -- --project=chromium` | 26 passed; includes TypeScript checking of the harness |
| `npm --prefix frontend run test:ui -- --project=webkit` | 26 passed; final browser validation ran each engine separately after the native build finished |
| Modal focus regression after the final focus change | Both browser projects passed |
| `npm --prefix frontend run build` | TypeScript and production frontend build passed |
| `go test -race ./...` | All ordinary package tests passed |
| `go vet ./...` | Passed |
| `MIER_LSP_SMOKE_TOOLS="$(cat /tmp/mier-managed-lsp-protocol-root)" go test -race -timeout 3m -run '^TestReal' -count=1 -v .` | All seven actual PHP/TypeScript/Vue tests passed, including PHP navigation after fetching a missing PR commit and into installed vendor dependencies above the former 128 MiB cap |
| `MIER_LSP_INSTALL_SMOKE=1 MIER_LSP_INSTALL_TOOLS=/tmp/mier-managed-lsp-installation go test -race ./internal/lspclient -run '^TestRealManagedToolInstallation$' -count=1 -timeout 10m -v` | Official Phpactor, TypeScript, and Vue downloads installed, verified, and activated; no repository/global package installation |
| Five real-server tests rerun against the newly managed packages | All five passed with the race detector |
| `node --test scripts/release.test.mjs` | 10 passed |
| `actionlint .github/workflows/test.yml .github/workflows/release.yml` | Passed |
| `wails3 build` | Binding generation and Linux production build passed; 9 bound RPC services, 61 methods, 52 models, 3 events |

Go embeds `frontend/dist`, so frontend builds and Go checks were sequenced with the Wails watcher stopped. The native app was launched from the production binary, rather than Vite's standalone demo.

## Retained regression coverage

`frontend/tests/` now holds the browser workflow suite. The fixture supplies a desktop environment and intercepts the real Wails HTTP RPC protocol, including provider errors and delayed responses. It does not modify a user repository or send a GitHub write. Tests cover:

- Branch creation rejection/retry, draft retention, duplicate suppression, dialog focus and Escape restoration.
- Tracking remote selection, incorrect pull targets, and network failure recovery.
- Comparisons with out-of-order responses, committed previews, and unavailable patches.
- Local diff retry, failed commits, staging, commit draft restoration across navigation and reload, and completed drafts clearing after their composer unmounts.
- PR creation preview, correct publishing remote, and failed creation draft restoration.
- Per-commit history/PR inspection and exact immutable commit requests.
- New PR commits preserving the reviewed code until explicitly accepted.
- Repository switching and isolation of pending requests and saved draft text.
- Account removal using the confirmed account's hostname, invalid appearance recovery, and Jira's unavailable state.
- Managed language-server installs before repository selection, persisted progress after navigation, duplicate suppression, cancellation/retry, failed updates preserving the installed version, pending-update badges, prerequisite blocking, repair, offline checks, custom executable selection, and background activation refreshing code navigation.
- Inbox pagination, overlapping pages, preservation of the selected review revision, full-result filters/counts, empty filtered results, error recovery, duplicate suppression, stale responses after filter/repository changes, and account lookup failures surviving local refreshes.

New backend tests cover dirty/unborn branch creation, protected merged-only deletion, rename/tracking preservation, provider-specific PR checkout, root/merge commit diffs without checkout/index mutation, immutable GitHub commit validation and pagination, and canonical path/dependency metadata handling across OS aliases. Existing Git, provider, sync, notification, editor, highlighting, and review-draft tests remain in the suite.

SSH regressions reproduce the former **Invalid Git remote URL** failure before Git was invoked. The shared authentication runner now leaves SCP-style addresses and local paths to Git. Tests cover SSH with/without a username, host aliases, explicit ports, bracketed IPv6 hosts, percent characters in local paths, and Windows path spelling; non-HTTPS transports never request an application token. An isolated Git URL rewrite fixture exercises actual clone/fetch/pull/push and missing-PR source capture, while checking that analysis preserves dirty files, refs, and `FETCH_HEAD`. External helper transports remain rejected; IPv6 colons no longer trigger that rejection.

Managed-tool backend regressions cover persisted installations, correct Vue companion/plugin versions, dependency updates, SHA-256 rejection of a corrupted PHAR, failed updates retaining the previous server, cancellation, concurrent-install rejection, custom configuration preservation, cached updates during provider failure, invalid paths/versions/redirects, missing prerequisites, shutdown, repository isolation, and successful-activation notifications.

Actual language-server checks exercised PHP cross-file signatures/definitions, staged/working signatures, TypeScript commit and SDK definitions, Vue/TypeScript cooperation, and monorepo workspace links bound to the selected revision.

Large dependency regressions first reproduced **Capture vendor source: Installed dependency source exceeds the 128 MiB analysis limit** with 130 MiB of PHP source. PHP now captures its vendor source/configuration independently of JS/Vue's node modules, with a 1 GiB dependency budget per captured revision/family. Tests cover switching and reusing both families for one commit, freezing large dependency copies, retaining/rebasing Composer metadata above the ordinary 4 MiB file limit, budget rejection before copying, and cancellation removing a partial file. The real Phpactor regression uses a Composer-compatible autoloader, resolves an installed method's signature/definition with 130 MiB of vendor PHP source, and reads the frozen grant after the original dependency changes, while preserving dirty tracked code and Git state.

## Native and CI evidence

The Linux production binary opened through the native folder dialog in a disposable Git repository. Its comparison dialog displayed the committed branch change while the working file remained dirty. Creating and selecting a new branch from the UI preserved that dirty work. Native staging and commit actions then produced a clean working tree; history displayed the committed change. [The native comparison screenshot](validation/native-comparison.png) records the actual GTK/WebKit window.

The managed-tool desktop check used a real installed TypeScript language server 6.0.1 in isolated configuration. Settings discovered 6.0.2 and displayed [the pending update](validation/native-lsp-update-pending.png). Clicking **Update** installed and activated 6.0.2, retained the 6.0.1 directory, and displayed [Up to date](validation/native-lsp-update-ready.png). Clicking **Install** for Phpactor and Vue installed their current official versions; Vue included its SDK, matching plugin, and TypeScript companion. These checks used the native GTK/WebKit window through T3's browser and noVNC, without opening or modifying a user repository.

The SSH fix was also checked in the production desktop binary through T3/noVNC, with a disposable PHP repository and isolated application configuration. Clicking **Fetch** contacted `git@github.com:Noah-Wilderom/mier.git` through the existing SSH setup and created `origin/master` at `250f169`. No application GitHub account was connected in that profile. Native [PHP hover](validation/native-ssh-php-hover.png) displayed the actual method signature, and [Go to definition](validation/native-ssh-php-definition.png) opened `src/Greeter.php` in the read-only reader. The working change remained intact. The separate real-server regression validates the missing PR commit path end to end; no remote repository was written during these checks.

The large-vendor fix was checked in another isolated native profile with **130 MiB of PHP vendor source**. Hover completed, and Go to definition opened [the installed method](validation/native-large-vendor-definition.png) with a Dependency file label. The captured cache contained all 67 vendor files (130 MiB), with no node modules copied, and the original tracked working change remained intact. This is a size-limit and dependency-navigation fixture, rather than a benchmark of a full Symfony/Laravel project.

The final feature commit `de9bf12` passed [CI run 38487224237](https://github.com/Noah-Wilderom/mier/actions/runs/38487224237) on **2026-10-14**: the retained browser regression job and native Linux, macOS, and Windows tests/builds all succeeded. Linux built and inspected DEB/RPM/Arch/AppImage packages. Canonical source scope and Composer metadata fixes had also passed [the preceding cross-platform run](https://github.com/Noah-Wilderom/mier/actions/runs/38482044287).

## Limits

Provider writes, OS keychain operations, external editor launching, OS notification delivery, macOS/Windows native interaction, six-target installation, and a real previous-version updater migration still require release qualification. HTTP fixtures and native compilation do not establish those outcomes. Installers remain unsigned initially, as requested.

Managed LSP installation was exercised on Linux amd64. macOS/Windows native installation interaction is not established by these checks. Managed Phpactor needs PHP's POSIX extension and is unavailable with native Windows PHP; TypeScript/Vue have Windows runtime/CLI paths.

The updater private key is outside Git; configuring the Actions secret, publishing an AUR package, and qualifying actual platform installs are tracked in [release readiness](PRODUCTION.md). M.I.E.R. remains unlicensed by maintainer choice. No live GitHub release was published during verification.

Vite's large-chunk and npm's unsupported `minimum-release-age` configuration warnings remain nonfatal. This record is evidence for the current implementation, not a claim of exhaustive Git-provider parity.
