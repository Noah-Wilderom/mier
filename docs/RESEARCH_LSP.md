# Language intelligence research ledger

Research and validation captured during the M.I.E.R. implementation, **2026-10-13**. This records sources, observed behavior, failed assumptions, resulting decisions, and retained regressions. Use [LANGUAGE_INTELLIGENCE.md](LANGUAGE_INTELLIGENCE.md) for current setup, API behavior, and limits; [RESEARCH.md](RESEARCH.md) indexes the broader project research.

The evidence has three distinct origins: protocol/upstream documentation, the local implementation and retained tests, and isolated experiments against real installed servers. Test fixtures and browser mocks establish the behavior they exercised; they do not imply broad framework or platform parity.

## Primary sources

| Source | Fact used in the implementation |
| --- | --- |
| [LSP 3.17 position specification](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#position), [source](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.17/types/position.md) | Lines/characters are zero-based; negotiated encoding defines characters. UTF-16 is the default and must be supported by servers. |
| [LSP cancellation specification](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#cancelRequest), [source](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.17/specification.md) | Cancellation uses `$/cancelRequest` with the request ID. Cancelled requests still receive responses, so clients must handle late replies. |
| [LSP hover specification](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#textDocument_hover), [source](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.17/language/hover.md) | Hover can contain MarkedString, an array, or MarkupContent, plus an optional source range. |
| [LSP MarkupContent](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#markupContent), [source](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.17/types/markupContent.md) | `plaintext` and `markdown` are distinct formats; rendering must respect the returned kind. |
| [TypeScript language server 6.0.2 README](https://github.com/typescript-language-server/typescript-language-server/blob/v6.0.2/README.md) | It is an LSP wrapper over TypeScript's tsserver, requires `--stdio`, and exposes `typescript.tsserverRequest` through `workspace/executeCommand`. |
| [TypeScript language server 6.0.2 package manifest](https://github.com/typescript-language-server/typescript-language-server/blob/v6.0.2/package.json) | The tested package declares Node `>=22.22.2`. |
| [Vue language server 3.3.13 README](https://github.com/vuejs/language-tools/blob/v3.3.13/packages/language-server/README.md) | The server accepts `--tsdk` pointing to TypeScript's `lib` directory and communicates with `@vue/typescript-plugin` through custom notifications. |
| [Vue server 3.3.13 implementation](https://github.com/vuejs/language-tools/blob/v3.3.13/packages/language-server/lib/server.ts) | Project information and semantic requests are sent via `tsserver/request`; responses correlate IDs through `tsserver/response`. |
| [Phpactor standalone installation](https://phpactor.readthedocs.io/en/master/usage/standalone.html), [language-server documentation](https://phpactor.readthedocs.io/en/master/usage/language-server.html) | Official PHAR/Composer installation and PHP requirements; Phpactor supplies the PHP LSP backend. |
| [Git archive manual](https://git-scm.com/docs/git-archive#_attributes) | `export-ignore` excludes source from packaging archives; `export-subst` can rewrite placeholders. Packaging output cannot be assumed to preserve a commit's analysis bytes. |
| [Git diff manual](https://git-scm.com/docs/git-diff) | Three-dot comparison uses a merge base rather than treating the latest base branch tip as its old side. |

The project's exact cache, permission, discovery, and UI policies are implementation decisions supported by its own source/tests, rather than promises made by those upstream projects.

## Verified versions and scope

| Component | Version observed in the real-server probes |
| --- | --- |
| Phpactor PHAR | `2026.10.07.0` |
| PHP runtime | `8.5.10` |
| TypeScript SDK | `5.9.3` |
| `typescript-language-server` | `6.0.2` |
| `@vue/language-server` | `3.3.13` |
| `@vue/typescript-plugin` | `3.3.13` |
| Node runtime | `26.1.0` |

The npm packages were installed in an isolated tools directory, not added to the application's production dependencies or user repositories. Real tests used disposable Git fixtures and temporary server configuration/cache directories. The final native build and real-server runs were on Linux amd64. A newer upstream README's recommended SDK version is not substituted for the SDK actually tested here.

## Experiments and decisions

### 1. Commit source must remain independent of the checkout

**Question:** Can a review hover accidentally describe dirty checkout source?

**Experiment:** A temporary TypeScript repository committed a function returning `string`, then changed the working file to return a number. Hover and definition requests used the committed revision while that incompatible working edit remained present. The real server reported the committed signature and opened the selected source, not the dirty file. A definition for `String.charAt` also opened an actual installed SDK `.d.ts` file with the dependency label.

**Decision:** Every source session uses a captured revision workspace; requests are not sent against the original project checkout. The SDK/dependency reader is separately scoped and frozen when a definition is granted. Retained evidence: [TestRealTypeScriptRevisionHoverAndDependencyDefinition](../lsp_real_test.go) and [manager grant tests](../internal/lspclient/manager_test.go).

### 2. Packaging archives are unsuitable for analysis

**Failed assumption:** `git archive <sha>` always yields the source bytes represented by that commit.

**Experiment:** A disposable commit added `.gitattributes` with `hidden.ts export-ignore` and `substitute.ts export-subst`. The packaging archive omitted `hidden.ts` and replaced a literal `$Format:%h$` string in the second file with a short commit ID. This matched the documented [Git archive attribute behavior](https://git-scm.com/docs/git-archive#_attributes).

**Correction:** Capture uses a complete `ls-tree` listing and bounded `cat-file` batches over recorded object IDs. Analysis source remains literal regardless of packaging attributes. Retained regression: `TestCodeArchivesIgnoreExportPackagingAttributes` in [archive_test.go](../internal/gitclient/archive_test.go); implementation: [exact_tree.go](../internal/gitclient/exact_tree.go).

### 3. PR old source is the merge base

**Failed assumption:** A PR's `base.sha` is always the source displayed in the old diff column.

**Experiment:** The base branch advanced independently of a PR head. The latest base tree contained changes absent from the three-dot comparison's old side.

**Correction:** Old-side requests carry both the complete base SHA and the displayed comparison head. The backend resolves their actual merge base; the returned analysis revision can legitimately differ from the requested base tip. Frontend validity is guarded by the displayed context/epoch rather than rejecting that resolved SHA. Retained regression: `TestArchiveComparisonUsesMergeBaseRatherThanLatestBase` in [archive_test.go](../internal/gitclient/archive_test.go). [Git's diff documentation](https://git-scm.com/docs/git-diff) supplies the comparison semantics.

### 4. Working and staged source require distinct captures

**Failed assumptions:** Only Git-reported dirty files need an overlay; repeatedly reading `:0:path` is equivalent to one coherent index snapshot.

**Review/probe findings:** Working edits can be hidden by `assume-unchanged` or `skip-worktree`. A later mutable index lookup can observe a different staging operation from the initial file listing. An unborn repository has working/index source even without a HEAD commit.

**Correction:** Index capture records one stage-0 listing and reads its blob IDs. Working capture reads actual tracked files plus nonignored untracked paths, checks for changes during capture, and preserves missing sparse files from recorded index blobs. Each capture is content-addressed; `working`/`index` requests return a digest and recursive grants keep that digest. Index conflicts produce an actionable rejection.

**Evidence:** [workspace_archive_test.go](../internal/gitclient/workspace_archive_test.go), `TestMutableRequestsRecaptureDigestRejectStaleLinesAndKeepFrozenGrants` in [manager_test.go](../internal/lspclient/manager_test.go), and `TestRealTypeScriptWorkingAndStagedSource` in [lsp_real_test.go](../lsp_real_test.go). Local UI source fingerprints are covered separately in [frontend research](RESEARCH_FRONTEND.md).

### 5. Vue needs a live TypeScript peer and broker

**Initial result:** Starting the Vue executable and passing an SDK alone did not produce the expected TypeScript semantic hover/definition. The real Vue probe returned no hover and an empty definition list for a symbol in `<script setup lang="ts">`.

**Primary-source finding:** The versioned [Vue README](https://github.com/vuejs/language-tools/blob/v3.3.13/packages/language-server/README.md) describes cooperation with `@vue/typescript-plugin`; its [server source](https://github.com/vuejs/language-tools/blob/v3.3.13/packages/language-server/lib/server.ts) requests project/semantic information through custom notifications. TypeScript's [command bridge](https://github.com/typescript-language-server/typescript-language-server/blob/v6.0.2/README.md#send-tsserver-command) can carry those tsserver requests.

**Experiment:** A real TypeScript peer loaded the Vue plugin, received the same versioned `.vue` document, and successfully answered the Vue project-info request. Direct TypeScript-peer hover at the selected Vue position returned the imported function's `string` signature, and definition returned the real `src/lib.ts` target. The browser/protocol harness also exercised flat and nested custom-message tuple shapes; the backend handles both and preserves their response envelope.

**Decision:** A Vue session owns a plugin-enabled TypeScript peer. It verifies the peer's advertised `typescript.tsserverRequest`, forwards notification requests to `workspace/executeCommand`, extracts the response body, and replies with the matching ID. Vue-specific results take precedence; empty or unsupported semantic requests can fall back to the real TypeScript peer. Session reuse checks both processes, so a dead peer does not leave a live Vue process waiting indefinitely.

**Evidence:** [session.go](../internal/lspclient/session.go), `TestVueUsesPeerTypeScriptBrokerAndPlugin` in [manager_test.go](../internal/lspclient/manager_test.go), and `TestRealVueHoverAndTypeScriptDefinition` in [lsp_real_test.go](../lsp_real_test.go). The minimum-version setup message does not replace the runtime capability check or promise compatibility with every Vue/TypeScript combination.

### 6. PHP indexing can initially produce a partial authentic hover

**Experiment:** With Phpactor `2026.10.07.0`, the first hover for a cross-file method returned a short method description while project indexing was still starting. A second request about one second later returned the complete typed signature. Definitions and the eventual result came from Phpactor, not a local token approximation.

**Follow-up observation (2026-10-14):** The user reported roughly 20 seconds for first symbol inspection, with subsequent symbols answering immediately. The cold request path captured a revision, copied dependencies, and initialized a server only when the pointer requested a symbol. Cancellation could interrupt that preparation. A small isolated PHP fixture measured 3.254 seconds to its first partial hover, 5.649 seconds to the full signature, and 2 milliseconds for a warmed hover. That fixture is not a benchmark of the user's larger project; a single one-second refresh was insufficient even here.

**Current decision:** Prepare the selected file when the review opens. Share active preparation and keep it alive when a pointer request is cancelled, while bounding it and cancelling on application shutdown. Preserve requests when the pointer moves within an identifier. Display actual server content immediately, with highlighted signature blocks and literal plaintext. For an active PHP symbol, refresh up to four times with delays of 500 ms, 1 s, 2 s, and 4 s, reusing the first response's captured revision. Keep the previous result during refresh/failure, preserve viewport placement, and abort on dismissal or view changes. Disable Phpactor's unused on-open/update/save diagnostics and document highlighting, following [its performance guidance](https://phpactor.readthedocs.io/en/master/tips/performance.html). There is no fixed backend sleep in production.

**Evidence:** `TestRealPHPColdAndWarmHoverTiming` in [lsp_real_test.go](../lsp_real_test.go) records document preparation, first/full hover, and warmed hover separately. [warmup_test.go](../internal/lspclient/warmup_test.go) verifies shared capture, cancelled waiters, failure retry, application shutdown, and PHP initialization options. [codeIntelligence.spec.ts](../frontend/tests/codeIntelligence.spec.ts) retains browser checks for preparation before hover, highlighted signatures, definitions, identifier movement, late refreshes, viewport resizing, plaintext, retry, and dismissal/navigation guards. Preparation completion means the document is open, not that Phpactor has finished indexing every dependency; cold cost still depends on the project and machine.

### 7. Installed dependency links can leak the wrong project revision

**Red experiment:** `node_modules/@team/foo` was a workspace symlink into a tracked `packages/foo` folder. The committed package returned `string`; the original checkout was edited to return a number. Linking the original `node_modules` directory wholesale into a supposedly frozen review workspace let TypeScript resolve the dirty original package, producing the wrong type.

**Correction:** Copy filtered installed source/metadata into the private workspace. Rebase links whose targets are inside the original repository into the corresponding snapshot path; omit arbitrary external targets. Selected tracked source takes precedence over installed copies. Rebase absolute project paths in generated Composer autoload metadata as well.

**Green result:** The same real TypeScript experiment returned the committed `string` signature and a definition in the captured repository package with `external=false`. The unit fixture also verified Composer metadata rebasing and frozen installed source. Retained evidence: [lsp_workspace_dependency_test.go](../lsp_workspace_dependency_test.go), [dependencies_test.go](../internal/lspclient/dependencies_test.go), and [dependencies.go](../internal/lspclient/dependencies.go).

**Boundary:** Installed dependency versions are captured, not reconstructed from a historical lockfile. The test establishes this workspace-link case and generated Composer path handling; it is not a real Laravel/Symfony integration test. Configured SDK/plugin paths and known installations remain permitted read-only definition scopes, with returned text frozen at grant time.

### 8. Positions must count UTF-16 across highlighted DOM nodes

**Problem:** Syntax tokens create nested spans, and a Unicode emoji occupies two UTF-16 units. Counting only a hovered span, guessed token boundaries, visible columns, or Unicode code points can ask for the wrong symbol.

**Decision:** Use the browser caret API and a DOM Range from the complete code-line start to the caret, then use JavaScript string length. Convert displayed one-based line numbers to zero-based LSP lines. Send the exact displayed line text; validate it and reject negative/out-of-range offsets and half-surrogate boundaries in Go. The client advertises UTF-16 and rejects a server choosing another encoding. This follows the [LSP position contract](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#position).

**Evidence:** `TestStaleLinesAndUTF16SurrogateBoundaries` in [manager_test.go](../internal/lspclient/manager_test.go), plus real-DOM browser tests across nested syntax spans, whitespace, and emoji. [Frontend research](RESEARCH_FRONTEND.md) records exact-text highlighting and anchor behavior.

### 9. Cancellation must reach the native promise

**Review finding:** A generic async wrapper could await the Wails call and discard the runtime promise's `.cancel()` method. UI epoch checks prevented stale display but allowed abandoned RPC work to occupy the backend.

**Correction:** The LSP-specific wrapper retains the actual cancellable native promise and attaches/removes an AbortSignal listener. Pre-aborted requests avoid invoking Go; in-flight abort calls the promise's cancellation method. Go request cancellation sends LSP `$/cancelRequest`, while response IDs and UI generations still guard late results. Initialization respects the requesting context; server ownership remains with the service.

**Evidence:** [services.ts](../frontend/src/services.ts), [CodeIntelligence.tsx](../frontend/src/CodeIntelligence.tsx), and `TestRPCResponseCorrelationCancellationAndBoundedShutdown` in [rpc_test.go](../internal/lspclient/rpc_test.go). An isolated runtime-promise harness exercised pre-abort, in-flight cancel, listener removal, genuine backend errors, and browser rejection. The [LSP cancellation specification](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#cancelRequest) explains why late responses still need handling.

### 10. Definition navigation needs scoped frozen grants

**Question:** Can server-returned definition paths become an arbitrary frontend file reader, or silently change while being reviewed?

**Decision:** Normalize Location/LocationLink targets, resolve them only within the captured project and explicitly known dependency/tool scopes, validate text/ranges, and issue opaque grants tied to canonical root/revision. ReadDefinition accepts the grant rather than a caller-chosen path. Freeze granted text, retain its actual revision for recursive navigation, reject source-edit requests, and expire grants with bounded caches.

**Evidence:** `TestManagerRejectsTraversalAndUnsafeServerDefinitionTargets`, `TestDependencyDefinitionsAreConfinedAndFrozenWhenGranted`, `TestManagerUsesFullSelectedSourceAndOpaqueRecursiveDefinitionGrants`, and archive-extraction tests in [manager_test.go](../internal/lspclient/manager_test.go). Browser tests confirmed read-only source navigation, preserved review drafts, dependency labels, and no stale response after head/context changes. This is a reader boundary; the external server process itself is not OS-sandboxed.

### 11. Plaintext must stay literal; dismiss/re-entry must remain usable

**Rendering defect:** Treating a plaintext hover as Markdown changed strings such as `List<T>` and `**literal**`. The backend now preserves single plaintext MarkupContent kind and the frontend renders it literally; Markdown content still uses the safe Markdown component. [The protocol explicitly distinguishes the formats](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#markupContent).

**Interaction defect:** Dismissal left the last pointer position as the hover deduplication key. Re-entering the exact same character returned early even though the panel was gone. Separate active-hover position from the last keyboard-navigation location and clear the former on dismissal.

**Evidence:** `TestPlaintextHoverPreservesLiteralMarkupCharacters` in [dependencies_test.go](../internal/lspclient/dependencies_test.go) and isolated browser tests for literal content, Escape/close dismissal, and same-symbol re-entry.

## Retained regressions and validation

The final verification reported a passing full Go race run, all five opt-in real LSP tests under the race detector, frontend tests/typechecking, and the Linux amd64 native Wails build. Reproduction commands and optional-tool layout are in [the handoff](LANGUAGE_INTELLIGENCE.md#validation).

| Retained source | What it proves |
| --- | --- |
| [archive_test.go](../internal/gitclient/archive_test.go) | Exact selected commit, checkout preservation, merge-base old source, export packaging attributes. |
| [workspace_archive_test.go](../internal/gitclient/workspace_archive_test.go) | HEAD/index/working separation, unborn source, rename/delete, sparse/hidden edits, external symlink exclusion. |
| [manager_test.go](../internal/lspclient/manager_test.go) | Actual request routing through test servers; positions, stale text, grants/scopes, configuration, Vue peer, cache eviction and recursion. |
| [warmup_test.go](../internal/lspclient/warmup_test.go) | App-owned preparation, cancellation sharing, retries, shutdown, and unused PHP work disabled. |
| [codeIntelligence.spec.ts](../frontend/tests/codeIntelligence.spec.ts) | Early preparation, signature formatting, definition navigation, stable hover, bounded refreshes, resize placement, retry, and stale-view guards. |
| [dependencies_test.go](../internal/lspclient/dependencies_test.go) | Copied dependency isolation/rebasing and plaintext preservation. |
| [rpc_test.go](../internal/lspclient/rpc_test.go) | Request correlation, cancellation, malformed/oversized transport, bounded shutdown. |
| [lsp_real_test.go](../lsp_real_test.go) | Real PHP, TS commit/dependency, TS working/index, and Vue semantic hover/definition. |
| [lsp_workspace_dependency_test.go](../lsp_workspace_dependency_test.go) | Real TypeScript workspace package isolation with a dirty original package. |

Additional isolated browser/protocol harnesses were run from temporary paths; they were not committed as portable project tests. Their coverage is recorded here so it is not confused with the retained suite:

| Temporary harness | Coverage observed |
| --- | --- |
| `/tmp/mier-lsp-ui-qa.cjs` | Nested-span/emoji UTF-16 positions; click/F12/gD; multiple/empty/error targets; merge-base revisions; opaque recursive readers; drafts/read-only state; missing/unsupported servers; new-head/local-context cancellation; unborn working/index; exact-symbol re-entry. |
| `/tmp/mier-php-hover-qa.cjs` | Authentic first result; one warmup refresh; retained content while pending/on failure; timer/RPC abort on dismissal/context/unmount; plaintext literal rendering. |
| `/tmp/mier-lsp-settings-qa.cjs` | Real-status loading states, literal argument arrays, rejected-save draft retention, busy controls, configuration-change events, truthful refresh failures, narrow viewport. |
| `/tmp/mier-vue-broker-probe.cjs` | Real Node server initialization/capabilities, TypeScript/Vue plugin cooperation, project-info broker traffic and semantic requests in a disposable fixture. |

These temporary paths may not survive the development machine/session. Promote representative browser cases into a retained test harness before relying on them in CI.

## Remaining research seams

Framework-specific indexing and configuration are separate work. Composer path rebasing was tested, but Laravel/Symfony projects were not exercised. Vue was validated against the recorded server/plugin/SDK combination and a small SFC fixture, not every template/framework build configuration. Yarn PnP, arbitrary package-manager layouts, and all monorepo alias strategies are not established by the workspace-link regression.

Future work should preserve the existing source-revision, UTF-16, cancellation, and grant contracts while adding explicit server adapters and real fixtures. Completion/diagnostics require new normalized APIs and UI behavior; merely launching a server that offers them does not implement those features. General product setup and known operational limits remain in [LANGUAGE_INTELLIGENCE.md](LANGUAGE_INTELLIGENCE.md).
