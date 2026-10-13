# Validation record

Production-readiness work verified on **2026-10-13**. The earlier documentation handoff is preserved in [the historical record](validation/handoff-2026-10-13.md); its temporary QA paths and Jira simulation are historical, not the current test suite.

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

## Commands and results

| Check | Result |
| --- | --- |
| `npm --prefix frontend test` | 46 passed, zero failed or skipped |
| `npm --prefix frontend run test:ui` | 24 passed across Chromium and WebKit; includes TypeScript checking of the harness |
| Modal focus regression after the final focus change | Both browser projects passed |
| `npm --prefix frontend run build` | TypeScript and production frontend build passed |
| `go test -race ./...` | All ordinary package tests passed |
| `go vet ./...` | Passed |
| `MIER_LSP_SMOKE_TOOLS=/tmp/mier-lsp-smoke-tools go test -race -timeout 3m -run '^TestReal' -count=1 -v .` | All five actual PHP/TypeScript/Vue tests passed |
| `node --test scripts/release.test.mjs` | 10 passed |
| `actionlint .github/workflows/test.yml .github/workflows/release.yml` | Passed |
| `wails3 build` | Binding generation and Linux production build passed; 9 bound RPC services, 57 methods, 48 models, 2 events |

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

New backend tests cover dirty/unborn branch creation, protected merged-only deletion, rename/tracking preservation, provider-specific PR checkout, root/merge commit diffs without checkout/index mutation, immutable GitHub commit validation and pagination, and canonical path/dependency metadata handling across OS aliases. Existing Git, provider, sync, notification, editor, highlighting, and review-draft tests remain in the suite.

Actual language-server checks exercised PHP cross-file signatures/definitions, staged/working signatures, TypeScript commit and SDK definitions, Vue/TypeScript cooperation, and monorepo workspace links bound to the selected revision.

## Native and CI evidence

The Linux production binary opened through the native folder dialog in a disposable Git repository. Its comparison dialog displayed the committed branch change while the working file remained dirty. Creating and selecting a new branch from the UI preserved that dirty work. Native staging and commit actions then produced a clean working tree; history displayed the committed change. [The native comparison screenshot](validation/native-comparison.png) records the actual GTK/WebKit window.

[Cross-platform CI run 38482044287](https://github.com/Noah-Wilderom/mier/actions/runs/38482044287) passed on Linux, macOS, and Windows after canonical source scopes and Composer metadata rebasing were fixed. Those runners also built the native application; Linux built and inspected DEB/RPM/Arch/AppImage packages. That run predates the remaining workspace feature commit; its subsequent workflow includes the retained browser job as well.

## Limits

Provider writes, OS keychain operations, external editor launching, OS notification delivery, macOS/Windows native interaction, six-target installation, and a real previous-version updater migration still require release qualification. HTTP fixtures and native compilation do not establish those outcomes. Installers remain unsigned initially, as requested.

The updater private key is outside Git; configuring the Actions secret, selecting a project license, publishing an AUR package, and qualifying actual platform installs are tracked in [release readiness](PRODUCTION.md). No live GitHub release was published during verification.

Vite's large-chunk and npm's unsupported `minimum-release-age` configuration warnings remain nonfatal. This record is evidence for the current implementation, not a claim of exhaustive Git-provider parity.
