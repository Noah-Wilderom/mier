# Language intelligence

M.I.E.R. provides real language-server hover and read-only definition navigation for PHP, TypeScript, JavaScript, and Vue. It analyzes the source revision shown in the diff, rather than silently using the current checkout. This document covers setup, behavior, implementation boundaries, and validation. General application setup belongs in the [README](../README.md); the source research and experiments are recorded in [RESEARCH_LSP.md](RESEARCH_LSP.md).

## Current capabilities

| Area | Implemented behavior |
| --- | --- |
| PHP | Phpactor hover and definitions for `.php` and `.phtml` files. |
| TypeScript / JavaScript | `typescript-language-server` with an installed TypeScript SDK; includes TSX, JSX, `.mts`, `.cts`, `.mjs`, and `.cjs`. |
| Vue | `@vue/language-server` plus a TypeScript peer with `@vue/typescript-plugin`. Vue results are preserved; empty or unsupported semantic hover/definition requests can use the TypeScript peer. |
| Pull requests | New-side source at the displayed head commit; old-side source at the comparison merge base. |
| Local changes | Working-tree and index source layers, including an unborn repository's new files. |
| Navigation | Ctrl/Cmd-click, F12, the `g` then `d` shortcut (case insensitive), and the visible Go to definition action. Multiple targets get a chooser. |
| Definition reader | Full source, line numbers, current code font/theme, recursive navigation, a Dependency file label where appropriate, and Back to review. The review remains mounted so file selection, scroll, and drafts survive. |
| Hover | Actual server Markdown or literal plaintext, highlighted signatures using the selected code theme/font, a source location, and a definition action. PHP gets bounded delayed refreshes while the same symbol remains active to accommodate initial indexing. |
| Preparation | Opening a supported review file starts source capture and language-server initialization before the first hover. Preparation failures can be retried. |

The feature runs in the native desktop app through Wails. The standalone browser prototype does not provide fake language-server responses. Syntax highlighting is independent of language-server installation; see [frontend research](RESEARCH_FRONTEND.md).

Completion, diagnostics, rename, references, code actions, inlay hints, source editing, and framework-specific IDE integrations are not exposed. A server's broader advertised capabilities do not imply that M.I.E.R. implements those features.

## Install and configure servers

Open **Settings → Code review → Language intelligence** in the desktop app. Each server has an **Install** button, installed/latest version labels, and an **Update available** state with an **Update** button. Servers can be installed before opening a repository. **Check for updates** refreshes both prerequisites and upstream versions; entering Settings uses update metadata cached for up to six hours.

Installation progress survives navigation because the backend owns the operation. **Cancel**, **Retry install**, **Retry update**, and **Reinstall** cover cancellation, failed downloads, and missing installation files. Updates install into a new directory and verify it before switching the active manifest. A failed update leaves the previous installation active. Successful activation restarts analysis sessions and broadcasts `lsp:changed`, so code navigation refreshes even when Settings is closed.

| Managed server | Prerequisites | Installed components |
| --- | --- | --- |
| Phpactor | PHP 8.2+ with mbstring, tokenizer, and posix | Latest stable official PHAR, checked against GitHub's published SHA-256 digest and file size, then launched for a version check. |
| TypeScript / JavaScript | Node.js 22.22.2+ and npm | Latest stable `typescript-language-server` and the tested TypeScript 5.9.3 SDK. |
| Vue | Node.js 22.22.2+ and npm | Latest stable Vue server, exactly matching `@vue/typescript-plugin`, its own latest TypeScript language-server companion, and the tested SDK. |

The app detects runtimes from PATH, common Homebrew locations, and installed mise versions, and records the actual runtime executable rather than a shell shim. Node's npm CLI is run directly, including on Windows. Missing prerequisites disable managed installation and show what to install; M.I.E.R. does not install system PHP/Node packages. Native Windows PHP does not provide the POSIX extension required by current Phpactor releases. See [PHP's POSIX documentation](https://www.php.net/manual/en/book.posix.php).

Tools live under the OS configuration directory's `mag-ik-een-review/language-tools`, independently of repositories and global npm packages. npm installs use exact server versions, engine checks, their own config/cache, and disabled lifecycle scripts. npm verifies package integrity; Phpactor downloads require the official HTTPS URL and published digest. The managed TypeScript SDK is pinned to the version exercised by M.I.E.R.'s real-server tests; server/plugin updates do not silently migrate SDK versions. Prior successful installation directories are retained, and interrupted staging directories are removed.

Update checks preserve the last known version and pending-update badge when the network is unavailable, while showing a check error. Update installation still verifies the latest provider metadata before downloading. Vue also reports a dependency update when its TypeScript companion changes without a new Vue server version. There are no automatic package installations or updates.

Explicit executable/argument/SDK/plugin configuration continues to take priority. Installing a managed copy preserves custom settings; **Use installed version** explicitly switches back to M.I.E.R.'s installation. Each language shows its detected source and any setup error. Available means that the executable and required files were found; startup or analysis can still fail and reports its actual error.

The verified combination is Phpactor **2026.10.07.0**, PHP **8.3.6**, TypeScript **5.9.3**, `typescript-language-server` **6.0.2**, and Vue server/plugin **3.3.13**. The TypeScript server's [6.0.2 package manifest](https://github.com/typescript-language-server/typescript-language-server/blob/v6.0.2/package.json) defines its Node requirement. Manual installations remain supported: [Phpactor installation](https://phpactor.readthedocs.io/en/master/usage/standalone.html), [TypeScript server](https://github.com/typescript-language-server/typescript-language-server), and [Vue's versioned instructions](https://github.com/vuejs/language-tools/blob/v3.3.13/packages/language-server/README.md).

### Automatic detection

Detection checks an explicitly configured executable first, then M.I.E.R.'s managed installation, project installations, and the process `PATH`. A managed Vue installation uses its own bundled TypeScript companion, independently of the separately configured TypeScript entry.

| Language | Project installation | PATH executable | Automatic arguments |
| --- | --- | --- | --- |
| PHP | `vendor/bin/phpactor`, run by `php` | `phpactor` | `language-server` |
| TypeScript / JavaScript | `node_modules/typescript-language-server/lib/cli.mjs`, run by `node` | `typescript-language-server` | `--stdio` |
| Vue | `node_modules/@vue/language-server/bin/vue-language-server.js`, run by `node` | `vue-language-server` | `--stdio`; the SDK argument is added at startup |

The repository root is searched first. Nested directories containing `package.json` or `composer.json` are also considered, with a bounded walk of 10,000 entries and limited depth; `.git`, `node_modules`, `vendor`, and `dist` subtrees are excluded from that discovery walk. Unusual or large monorepos may need explicit paths.

The TypeScript SDK directory must contain `tsserver.js`, typically `node_modules/typescript/lib`. Vue's plugin directory must contain its `package.json`, typically `node_modules/@vue/typescript-plugin`. Detection searches project directories and several ancestors of the resolved executable or absolute script arguments. Vue also needs the TypeScript language-server entry to be usable. At startup, the peer must advertise `typescript.tsserverRequest`; older peers lacking that command are rejected.

### Manual configuration

Arguments are **one literal argument per line**. They are passed directly to the executable as a string array, without a shell. Do not enter a whole command line into the executable field, quote paths as shell expressions, or expect variable expansion. An explicitly configured executable gets exactly the supplied arguments; an empty argument field does not inherit the automatic defaults.

For tools installed under `/absolute/path/to/mier-lsp-tools`, these entries match the real-server tests:

| Language | Executable | Argument lines | Additional directories |
| --- | --- | --- | --- |
| PHP | `php` | `/absolute/path/to/mier-lsp-tools/phpactor.phar`<br>`language-server` | None |
| TypeScript / JavaScript | `node` | `/absolute/path/to/mier-lsp-tools/node_modules/typescript-language-server/lib/cli.mjs`<br>`--stdio` | SDK: `/absolute/path/to/mier-lsp-tools/node_modules/typescript/lib` |
| Vue | `node` | `/absolute/path/to/mier-lsp-tools/node_modules/@vue/language-server/bin/vue-language-server.js`<br>`--stdio` | Same SDK; plugin: `/absolute/path/to/mier-lsp-tools/node_modules/@vue/typescript-plugin` |

For manually installed tools, configure the TypeScript entry as well as Vue. The Vue session starts a peer with the plugin loaded and brokers Vue's custom TypeScript requests. Managed Vue installs already include that peer. The Vue plugin field applies to the Vue entry; the peer configuration is assembled by the backend.

SDK and plugin directories must be absolute. PHP does not accept those fields. On Windows, `.cmd` and `.bat` wrappers are rejected; use `node.exe` or `php.exe` with the absolute server script/PHAR as an argument. Windows/macOS behavior has code paths, but the real-tool validation reported here ran on Linux.

**Use automatic detection**, followed by Save, clears the explicit executable, arguments, and dependency paths. Saving closes existing server sessions and causes availability to be checked again. Failed saves retain the entered configuration in the UI.

Configuration is stored atomically at the OS configuration directory's `mag-ik-een-review/language-servers.json`. It is global per language, not per repository. Source workspaces live under the OS cache directory's `mag-ik-een-review/lsp/session-*/revision-*` and are removed on normal eviction/shutdown. A crash can leave cache files behind.

## Which source is analyzed

| Displayed code | Request revision | Captured source |
| --- | --- | --- |
| PR new side | Complete displayed `headSha` | Exact Git tree for that commit. |
| PR old side | Complete `baseSha`, with `comparisonHead=headSha` | The actual merge base used for a three-dot comparison. The returned revision may differ from the base branch tip. |
| Staged local new side | `index` | Recorded stage-0 index blobs. |
| Staged local old side | Complete HEAD SHA | Committed source. No old HEAD exists in an unborn repository. |
| Unstaged local new side | `working` | Actual tracked working files plus nonignored untracked files. |
| Unstaged local old side | `index` | The staged source layer. |
| Recursive definition | Granted document URI and its returned revision | Frozen granted source, including dependency definitions. |

Immutable snapshots read tree entries and blob object IDs. Packaging attributes such as `export-ignore` and `export-subst` cannot omit or rewrite analysis source. Capture does not checkout a branch or change the working tree/index. If a PR commit is missing locally, the Git backend can fetch it only through a configured remote matching the PR's host/repository identity, without changing tracking refs or `FETCH_HEAD`. See [Git/provider research](RESEARCH_GIT_GITHUB_WAILS.md) for that remote boundary.

PR hover and definitions can therefore need Git authentication before the language server starts. SCP-style SSH remotes (`git@github.com:owner/repository.git`), explicit `ssh://` URLs, and local repository paths are passed to Git with its existing SSH configuration and URL rewrites. They do not request an application HTTPS token. Bracketed IPv6 SSH hosts are supported too. Git documents these transport spellings in [Git URLs](https://git-scm.com/docs/git-fetch#_git_urls). A fetch failure is reported by code navigation; reinstalling a language server cannot repair a Git authentication or network failure.

An index snapshot records one `ls-files --stage` listing and reads those immutable blob IDs; it does not reread mutable `:0:path` values for each file. An unresolved index is rejected. A working snapshot reads actual tracked files even when Git flags would hide their edits, includes nonignored untracked files, omits deletions/symlinks, and uses index content for missing sparse files. Capture checks metadata and the final index/name listing; an observed concurrent change produces an error rather than a known mixed snapshot.

Local input tokens `working` and `index` resolve to a **64-character content digest**. That digest identifies captured bytes, not a Git commit. Each mutable request captures again; identical bytes reuse the cache, changed bytes produce another revision. Mutable requests never use `comparisonHead`.

The frontend includes the displayed line text with a symbol request. The backend checks the line, UTF-16 character boundary, and matching source text before asking a server. This rejects a position from stale displayed code. PR refreshes retain the reviewed revision until the user accepts the new code; local view context includes the source diff fingerprint. An unchanged background poll therefore does not close an open definition reader.

## Dependencies and definition boundaries

Installed dependencies are copied into the private revision workspace for the requested language family: PHP captures `vendor`; TypeScript, JavaScript, and Vue capture `node_modules`. PHP capture includes PHP source and package/configuration metadata, while skipping frontend scripts, source maps, stylesheets, and binary assets. Selected tracked files take precedence. Workspace package links into the repository are rebased to the selected snapshot, and generated Composer autoload metadata has absolute project base paths rebased. Links outside the repository and the known installed dependency root are omitted.

Each captured workspace allows up to **1 GiB of dependency source**, shared across its monorepo projects. Ordinary files stream through a reusable 64 KiB buffer, so the cache size does not become one large memory allocation. Source files remain bounded at 4 MiB; Composer metadata has a separate 32 MiB file limit to retain large generated autoload maps. PHP and JS/Vue captures for the same commit are cached separately, so an unrelated dependency tree does not consume the requested language's budget. A capture failure or cancellation removes the incomplete workspace.

This uses **installed dependencies**, not dependencies reconstructed from the selected commit's lockfile. Dependency content is captured on first analysis for a cached revision. Updating an installation without changing source does not invalidate that cache; restart the app or let the revision be evicted to recapture it. Saving server configuration stops sessions but preserves cached source/dependency workspaces. Binaries/assets are excluded from dependency copying. Yarn PnP, arbitrary external workspace links, and every framework resolver have not been validated.

Known dependency installations and configured TypeScript SDK/plugin directories are permitted read-only definition scopes. Server-returned targets are checked against those scopes and the private source workspace. Valid definitions receive opaque `mier-lsp://definition/...` grants; the frontend cannot use the reader to request arbitrary paths or `file:` URIs. Grants are tied to the canonical repository root and captured revision, and their text is frozen when granted. Recursive requests use the opaque URI and returned revision, never the display path.

Outside-scope targets, traversal, Git metadata, unsupported URI schemes, invalid ranges, and binary text are excluded from definition results; if a returned set has no valid target, the request reports an error. Expired grants also produce errors. A project package rebased into the snapshot is a repository file; an installed dependency is labeled **Dependency file**. M.I.E.R. rejects `workspace/applyEdit` and exposes no source-editing action. Language servers still run as normal installed processes; this read/navigation boundary is not an operating-system sandbox for the server itself.

## Cancellation, warmup, and resource limits

Opening a supported review file prepares its preferred new side (or old side when no new side exists). This captures the displayed revision, starts its server, and opens the document. Only the selected side is prepared, so a second revision does not compete with the source about to be inspected. Concurrent preparation of the same file is shared; cancelling a caller stops its wait without interrupting app-owned preparation. Up to four preparation jobs can be active, each bounded by the request deadline. Initialization retains its separate 20-second limit, and application shutdown cancels preparation.

Hover waits 450 ms after pointer positioning. Moving within the same identifier keeps its pending request. Changing symbols, dismissing the popup, switching context, navigating, or unmounting aborts pending symbol work. Abort signals cancel the actual Wails promise, reaching the Go request context and LSP `$/cancelRequest`; frontend generation guards also discard late results if a server ignores cancellation. The popup keeps its viewport placement when delayed results arrive and renders plaintext literally; Markdown retains the application's safe link and HTML handling.

For PHP, the first authentic result appears as soon as the server returns it. If the same symbol and view remain active, up to four refreshes follow after delays of 500 ms, 1 s, 2 s, and 4 s. They reuse the revision resolved by the first response, avoiding another mutable source capture. The previous result remains visible and is retained if a refresh fails. Dismissal cancels the timer/request so a late response cannot reopen the popup. Other languages do not get this indexing refresh.

Phpactor is initialized with unused on-open/update/save diagnostics and document highlighting disabled. M.I.E.R. exposes hover and definitions, so those additional computations have no corresponding UI. These options follow [Phpactor's performance guidance](https://phpactor.readthedocs.io/en/master/tips/performance.html) and [configuration reference](https://phpactor.readthedocs.io/en/master/reference/configuration.html).

Preparation finishing means the document is open, not that every dependency has been indexed. First-time Git capture, dependency copying, process startup, and server indexing still cost time. Preparing when the file opens moves that work ahead of pointer interaction; it does not promise instant cold analysis of a large project. The retained real-PHP timing test logs preparation, first/full hover, and warmed hover separately, without using a machine-dependent timing threshold.

| Resource | Current limit |
| --- | --- |
| Cached source workspaces | Four across the manager, keyed by revision and dependency family; active requests prevent eviction. An all-busy cache returns an explicit error. |
| Selected source | 20,000 files and 64 MiB per revision; individual analyzed text files are at most 4 MiB. |
| Copied dependencies | 1 GiB of source per revision/family; up to 200,000 walked entries per dependency capture; 4 MiB per source file and 32 MiB per Composer metadata file. |
| Definition targets | 32 per response. |
| Frozen definition grants | 64 grants and 32 MiB aggregate text; eviction requires reopening a definition. |
| Hover content | 64 KiB, then truncation. |
| LSP request / initialization | 60 seconds / 20 seconds. |
| Concurrent preparation | Four active jobs; duplicate file/revision requests share a job. |
| Vue broker request | 15 seconds. |
| JSON-RPC frame | 8 MiB; malformed/oversized frames are rejected. |

Sessions are owned by the app, not one hover request. Configuration changes and normal application shutdown close them; failed initialization closes its process. Vue reuse requires both the Vue server and TypeScript peer to remain alive. Process shutdown and RPC writes are bounded, with child cleanup implemented for Unix and Windows.

Only one managed installation runs at a time, with a five-minute deadline. Cancellation or application shutdown terminates its npm/PHP process tree. Package installation never runs against the selected repository's package manifest or lockfile.

## Validation

Run from the repository root:

```sh
go test -race ./internal/gitclient ./internal/lspclient
go test ./...
```

Frontend checks run in `frontend`:

```sh
npm test
npx tsc --noEmit
```

The Go suites cover exact tree capture, merge-base selection, index/working separation, sparse/hidden edits, UTF-16 boundaries, stale lines, definition confinement and frozen grants, Vue peer/broker behavior, RPC cancellation, and shutdown. See the [research ledger](RESEARCH_LSP.md#retained-regressions-and-validation) for specific tests and the additional browser experiments.

Managed-tool tests cover version persistence, pending dependency updates, corrupt Phpactor downloads, failed updates retaining the active version, cancellation, duplicate suppression, custom configuration, offline metadata, and installation isolation. Chromium/WebKit regression tests cover the Settings controls and background activation events.

To exercise the real download/install path against the official providers:

```sh
MIER_LSP_INSTALL_SMOKE=1 go test -race ./internal/lspclient \
  -run '^TestRealManagedToolInstallation$' -count=1 -timeout 10m -v
```

This opt-in test requires PHP/Node/npm and installs outside the repository. To retain its managed manifest and installations for inspection, set `MIER_LSP_INSTALL_TOOLS` to a separate absolute tools directory.

### Optional real-server smoke

Real-server tests skip when `MIER_LSP_SMOKE_TOOLS` is unset. They create temporary Git repositories, configure temporary server settings/caches, and read tooling from the supplied directory. They do not use a user's repository or GitHub account.

With the official packages and optional `phpactor.phar` already installed in a separate tools directory:

```sh
MIER_LSP_SMOKE_TOOLS=/absolute/path/to/mier-lsp-tools go test . -run '^TestReal' -count=1 -v
```

For the complete tested set, that directory contains:

```text
phpactor.phar
node_modules/typescript/                         5.9.3
node_modules/typescript-language-server/         6.0.2
node_modules/@vue/language-server/               3.3.13
node_modules/@vue/typescript-plugin/             3.3.13
```

The npm packages can be installed outside a project with `npm install --prefix /absolute/path/to/mier-lsp-tools --no-save typescript@5.9.3 typescript-language-server@6.0.2 @vue/language-server@3.3.13 @vue/typescript-plugin@3.3.13`. Obtain the PHAR from [Phpactor's official installation instructions](https://phpactor.readthedocs.io/en/master/usage/standalone.html); the `latest` download is mutable, so record its reported version when reproducing. No PHAR means the PHP smoke skips while TypeScript/Vue can still run.

The real-server suite includes a dirty monorepo dependency-link regression, PHP hover/definition navigation after fetching a PR commit absent from the local clone through an SCP-style remote, navigation into an installed PHP dependency with over 128 MiB of vendor PHP source, and a cold/prepared/warm PHP timing check. The large-vendor test verifies a frozen dependency definition and unchanged tracked source/Git state. These fixtures do not establish complete Laravel/Symfony support or cross-platform server compatibility.

## Implementation map and extension seam

| Module | Responsibility |
| --- | --- |
| [lspservice.go](../lspservice.go) | Native service lifecycle and Git snapshot routing. |
| [types.go](../internal/lspclient/types.go) | Normalized server status, position, hover, definition, and read contracts. |
| [config.go](../internal/lspclient/config.go) | Supported languages, detection, configuration validation/persistence. |
| [tools_catalog.go](../internal/lspclient/tools_catalog.go), [tools_install.go](../internal/lspclient/tools_install.go) | Managed versions, runtime prerequisites, upstream checks, verified installation, cancellation, and activation. |
| [manager.go](../internal/lspclient/manager.go) | Cached revisions, sessions, leases, actual requests, and grants. |
| [warmup.go](../internal/lspclient/warmup.go) | Shared, app-owned document preparation independent of pointer cancellation. |
| [session.go](../internal/lspclient/session.go) | Initialization, versioned documents, content normalization, Vue peer broker. |
| [rpc.go](../internal/lspclient/rpc.go) | Bounded stdio JSON-RPC, response correlation, cancellation, process lifetime. |
| [workspace.go](../internal/lspclient/workspace.go), [dependencies.go](../internal/lspclient/dependencies.go) | Private source extraction, permitted file scopes, positions, dependency copies/rebasing. |
| [archive.go](../internal/gitclient/archive.go), [exact_tree.go](../internal/gitclient/exact_tree.go), [workspace_archive.go](../internal/gitclient/workspace_archive.go) | Commit/merge-base snapshots and mutable source capture. |
| [services.ts](../frontend/src/services.ts) | Browser-safe native calls; LSP-specific abort forwarding. |
| [CodeIntelligence.tsx](../frontend/src/CodeIntelligence.tsx) | DOM positions, availability, hover, cancellation, read-only definition navigation. |
| [SymbolInformation.tsx](../frontend/src/SymbolInformation.tsx) | Safe symbol documentation and syntax-highlighted signatures. |
| [LanguageServerSettings.tsx](../frontend/src/LanguageServerSettings.tsx) | Installation/update state, progress, retries, and explicit configuration UI. |

The native `main.LSPService` exposes `Servers(root)`, `CheckServerUpdates(root, force)`, `InstallServer(language)`, `CancelServerInstall(language)`, `ConfigureServer(language, configuration)`, `Warmup(positionRequest)`, `Hover(positionRequest)`, `Definition(positionRequest)`, and `ReadDefinition({root, revision, target})`. An empty root checks global setup without scanning a repository. Positions are zero-based UTF-16. A position request has root, revision, path or granted URI, optional comparison head and PR identity, line, character, and displayed line text. Hover/definition responses identify the actual captured revision; readers must keep that revision with their opaque target. Wails omits the first Go `context.Context` parameter from frontend arguments.

Future language/framework support can add detection/routing, setup fields, server-specific initialization/configuration, and tested dependency-resolution rules without changing the revision/grant boundary. Keep Vue's custom broker as a server adapter rather than a generic-LSP assumption. Currently `workspace/configuration` returns empty configuration objects; diagnostics are not normalized or sent to the UI. Add capability-specific contracts and real fixtures before advertising completion, diagnostics, framework stubs, or additional navigation features.
