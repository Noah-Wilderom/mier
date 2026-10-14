# Frontend research and validation

Recorded on **2026-10-13** for M.I.E.R. (Mag ik een review). This note collects the frontend research performed during implementation. External documentation, local observations, and product choices are identified separately. Linked upstream documentation can change; installed versions below come from this project's lockfile.

Backend/provider and Wails research belongs in [RESEARCH_GIT_GITHUB_WAILS.md](RESEARCH_GIT_GITHUB_WAILS.md). Language-server facts and experiments are maintained in [RESEARCH_LSP.md](RESEARCH_LSP.md), with operational guidance in [LANGUAGE_INTELLIGENCE.md](LANGUAGE_INTELLIGENCE.md); this note records the frontend seams used by that implementation rather than repeating server/protocol claims. Product intent and interface decisions are maintained in [PRODUCT.md](../PRODUCT.md) and [DESIGN.md](../DESIGN.md).

## Versions observed locally

The following exact versions were read from [frontend/package-lock.json](../frontend/package-lock.json), rather than inferred from the compatible ranges in `package.json`.

| Package | Installed version |
| --- | --- |
| React / React DOM | 18.3.1 |
| TypeScript | 5.9.3 |
| Vite | 8.3.4 |
| `@vitejs/plugin-react` | 6.1.2 |
| `@wailsio/runtime` | 3.0.0-beta.29 |
| `react-markdown` | 10.1.0 |
| `remark-gfm` | 4.0.1 |
| `remark-gemoji` | 8.0.0 |
| `shiki`, `@shikijs/langs`, `@shikijs/themes` | 4.5.0 |

## Product references and native appearance

**External facts.** SourceGit's own repository documents a cross-platform Git GUI with light/dark themes, a commit graph, repository operations, and diffs, and links its light/dark screenshots. This is a feature description from the project, not a performance measurement of SourceGit. [SourceGit repository](https://github.com/sourcegit-scm/sourcegit).

GitKraken's provider documentation describes a GitHub pull-request view for metadata, changes, comments, and code suggestions, alongside a browser review experience. That documentation informed the breadth of the PR workflow being considered; it does not establish exhaustive feature parity for M.I.E.R. [GitKraken Desktop pull requests](https://help.gitkraken.com/gitkraken-desktop/pull-requests/).

**Product choices.** The accepted direction was a review inbox for developers, with SourceGit-like information density, system/light/dark appearance, an expandable code review, and a separate local Changes workspace. Native titlebars and OS menus are shell concerns; Wails remains the desktop host while the React interface can run independently as a browser demo. The project name evolved from OpenGit to M.I.E.R.; historical QA reports therefore include earlier prototype terminology.

**Local findings.** Early browser QA exercised normal and expanded layouts at 1440×900, compact desktop at 1024×768, and containment at 390×844. Page width stayed within the viewport. A small clipped “Changed” label was observed in the compact/mobile historical layout; the report preserves that observation. This was a desktop-first containment check, not a claim of a complete mobile application. The historical [prototype report](validation/prototype-report.json) records 11 passing scenarios using sample data.

## Markdown, GFM, emoji, and navigation

**External facts.** `react-markdown` renders Markdown as React elements, supports custom element components and plugins, and exposes `skipHtml` and `urlTransform`. Its own security guidance notes that the default behavior can be changed by plugins or custom handling. This led to explicit project URL handling rather than treating any parser configuration as a blanket security guarantee. [react-markdown documentation](https://github.com/remarkjs/react-markdown).

`remark-gfm` supplies GitHub-style autolinks, footnotes, strikethrough, tables, and task lists. [remark-gfm documentation](https://github.com/remarkjs/remark-gfm). `remark-gemoji` turns recognized GitHub emoji shortcodes into Unicode emoji in Markdown text while excluding code nodes; it is separate from GFM syntax support. [remark-gemoji documentation](https://github.com/remarkjs/remark-gemoji).

**Implementation choices.** [Markdown.tsx](../frontend/src/Markdown.tsx) composes these packages and renders safe React nodes without `dangerouslySetInnerHTML`. Raw HTML is skipped. HTTP/HTTPS links and local footnote anchors are accepted; executable, file, data, relative, malformed, whitespace-obfuscated, and credential-bearing URLs are not navigable. Web links retain a readable label, an external-link note, and `noopener noreferrer`. Images use the same web URL restrictions and request no referrer. Code blocks and wide tables have keyboard-focusable scrolling containers. Fences remain readable plain code; no additional fence-highlighting dependency was added.

Native web-link clicks dynamically use Wails `Browser.OpenURL`; an ordinary browser uses normal external navigation, and the Markdown native-call failure path falls back to a browser window. The installed runtime API was inspected during implementation; provider and runtime details are documented in the backend/Wails note. The shared renderer is used for PR descriptions, conversation comments, review bodies, inline review threads, and replies.

**Local experiments.** [Markdown.test.ts](../frontend/src/Markdown.test.ts) contains five persistent tests covering headings, emphasis, lists, quotes, fences, escaping, GFM, Unicode/shortcode emoji, URL filtering, and footnotes. A separate isolated browser check covered eight groups, including native `OpenURL` through a mock, browser fallback, keyboard scrolling, light/dark appearance, and a narrow viewport. At the 348px content width measured in that fixture, wide code scrolled inside its container without widening the page. [Markdown browser report](validation/markdown-report.json).

## PHP syntax diagnosis and grammar highlighting

**Observed failure.** The original review renderer applied a generic regular expression to each line independently. A reproduction with PHPDoc `@return array{name: string}` found `return` and `string` rendered as generic code keywords inside the comment. The failing browser assertion was captured before changing the implementation. A per-line expression also had no retained state for multiline comments or strings. PHP fragments without an opening PHP tag needed explicit grammar context. These were separate concerns; changing a keyword color alone would not repair the parser state.

**External facts.** Shiki provides token output so an application can render the result itself, and recommends reusing a highlighter instance. [Shiki installation and usage](https://shiki.style/guide/install). Its fine-grained core accepts explicitly imported grammar and theme modules, allowing applications to avoid including every supported language/theme in the build. [Shiki bundles](https://shiki.style/guide/bundles). Its grammar-state and context-code APIs allow a snippet to continue from an existing parsing context. [Shiki grammar state](https://shiki.style/guide/grammar-state). The singleton and restricted import approach also follows its performance guidance. [Shiki performance practices](https://shiki.style/guide/best-performance).

**Implementation choices.** [syntaxHighlighter.ts](../frontend/src/syntaxHighlighter.ts) uses the Oniguruma engine with a lazily initialized core and literal lazy module imports. It produces token data rendered as React spans, preserving ordinary DOM text selection. Old and new diff sides are tokenized independently. State continues within contiguous side-specific hunk sequences and across bounded 200-line chunks; it resets at omitted-line gaps or hunk boundaries. This avoids mixing the removed and added revisions into one parser stream.

For PHP snippets without `<?php` or `<?=`, `grammarContextCode` supplies `<?php\n` only to the grammar; that prefix never becomes displayed code or changes line numbers. Every tokenized line must concatenate back to its exact original text. Tabs, spaces, Unicode, and blank lines therefore survive; if a tokenizer result fails that check, the line is rendered verbatim.

The module supports a constrained set of 29 lazy grammars: PHP, JavaScript, JSX, TypeScript, TSX, JSON, JSONC, CSS, SCSS, HTML, XML, Markdown, Go, Python, Ruby, Rust, Java, C, C++, C#, shell scripts, YAML, SQL, Vue, Dockerfile, TOML, INI, Swift, and Kotlin. Unsupported formats use plain text. Source over 6,000 lines or 300,000 characters also uses exact plain text with the selected theme palette. These are application bounds, not upstream Shiki limits.

The application yields between chunks, restricts per-line tokenization, and caches up to 24 source/theme entries within a one-million-character source budget. Failed engine/module loads can retry; supported-language fallback results from transient failure are not kept indefinitely. Identical source content from a background poll shares the existing highlight result. The full-file `highlightCodeLines(path, lines, theme)` API is also used by read-only definition documents.

**Local experiments.** Five [syntaxHighlighter.test.ts](../frontend/src/syntaxHighlighter.test.ts) cases exercised PHPDoc scope, real keywords and strings, multiline escaped strings/comments across more than 200 lines, independent old/new states, omitted hunk resets, exact blank/tab/Unicode text, all eight theme modules, and unsupported/oversized fallback. Together with three preference tests, the focused suite passed **8/8**. The original PHP browser feedback loop became green: PHPDoc token spans retained comment scope. [Recorded bug feedback loops](validation/frontend-bug-loops.json).

The actual review component was checked in split and unified modes. In an asymmetric wrap fixture, both sides measured 115px, the next row started at the previous row's bottom, and no additional gap existed. No `.review-comment-gap` spacer was present in the current implementation. This fixture established that paired-row alignment itself was correct; it does not prove the cause of every earlier screenshot. [Code review browser report](validation/code-review-report.json).

A later read-only source review found that whitespace-ignore conversion searched all split rows for each changed line, even when split view was displayed. It was replaced with per-side hunk/line maps built once, making that pairing pass linear. Grammar and revision-bound draft regression checks remained green afterward. No numerical speedup was claimed because a before/after benchmark was not recorded.

## Syntax themes, font size, and persistence

**External facts.** Shiki supports independently chosen light/dark themes and documents theme-dependent token colors. [Shiki dual themes](https://shiki.style/guide/dual-themes), [Shiki theme reference](https://shiki.style/themes). The public theme page did not expose every theme ID as searchable text during this research; each selected ID below was additionally verified as an installed `@shikijs/themes` 4.5.0 module and loaded in the persistent test.

| Appearance | UI label | Exact module ID |
| --- | --- | --- |
| Light | GitHub Light | `github-light` |
| Light | Light Plus | `light-plus` |
| Light | One Light | `one-light` |
| Light | Catppuccin Latte | `catppuccin-latte` |
| Dark | GitHub Dark | `github-dark` |
| Dark | Dark Plus | `dark-plus` |
| Dark | One Dark Pro | `one-dark-pro` |
| Dark | Nord | `nord` |

**Product choices.** [CodeReviewSettings.tsx](../frontend/src/CodeReviewSettings.tsx) offers four theme cards for each appearance with actual PHP grammar previews. Font size is adjustable from 12–20px, defaulting to 13px. Light and dark preferences are separate, so changing one does not overwrite the other. The effective application appearance selects the corresponding code theme; system appearance changes also update code colors.

[codeReviewPreferences.ts](../frontend/src/codeReviewPreferences.ts) uses the device-local key `mier.code-review.preferences.v1`, validates stored values, synchronizes other windows through storage events, and supplies shared font/line-height variables to PR reviews and local Changes. This storage contains display preferences, not credentials. Line height is `ceil(fontSize × 1.75)`. The highlighter can emit eight-digit hex colors, as observed for Nord; color checks use valid CSS color values rather than assuming six digits.

**Local experiments.** The settings tests covered clamping, invalid IDs/JSON, independent light/dark values, session operation with unavailable storage, module reload, and cross-window storage events. Browser QA verified all eight real preview cards, font size 20px, saved Latte/Nord preferences after reload, immediate appearance updates, and that a wide viewport did not silently reset the chosen font. [Preference tests](../frontend/src/codeReviewPreferences.test.ts), [code review report](validation/code-review-report.json).

## Live loading, local changes, and revision safety

**Product choices.** A local snapshot becomes usable before account/network requests finish. Repository and PR loading indicators communicate pending data; an unavailable provider count remains unknown instead of pretending to be zero. The backend's 15-second synchronization is reflected through events, rather than a second independent frontend polling loop. Recent repositories begin watching at native startup so inactive repository counts and notifications can update before selection.

Local diff loading is keyed by selected path and staging mode. Switching files clears the old file immediately and shows a skeleton until the new result arrives. Re-fetching the same request keeps useful code visible with a small “Refreshing…” status. A background read error leaves the existing code visible with an inline retry. State is canceled/ignored when the relevant request changes. The actual diff source, rather than a loading flag, drives the language-intelligence revision key.

**Local experiments.** The current [LocalChangesView](../frontend/src/LocalChangesView.tsx) fixture used delayed diff, stage, and commit promises. It verified no old code under a new filename, retained code throughout a same-file poll, retained code after a failed background refresh, successful retry, independent rename-side paths, PHPDoc scopes, blank row height, selection without a generated `+` marker, immediate Nord/20px updates, and loading the staged index after staging. Stage failure remained retryable; commit failure retained the user's message; success cleared it. [Local changes report](validation/local-code-report.json).

The current live workspace was separately tested with the installed Wails frontend transport replaced by an isolated fake transport. Three recent paths each received one `main.WorkspaceSyncService.StartWatch` at startup despite three runtime-ready events. An inactive repository's event changed its count to two open/draft PRs. Another repository's provider error cleared its count skeleton while preserving the unknown count. A selected detail-loaded PR received updated assignees through summary refresh; the “Assigned to you” inbox moved from fixture PR #501 to #502 and then to zero items. These are frontend integration observations with fake data, not live provider mutation tests. [Editor/profile/live report](validation/editor-avatar-live-report.json).

**Revision-bound drafts.** An inline composer records the head SHA and path when typing begins. Explicitly accepting a newer head keeps the body and old anchor, warns that the draft started on an earlier commit, and saves it as outdated unless the user explicitly repositions it. If the original line/file disappears, a detached composer retains the text. Async save errors also retain the text. Browser QA saved `old-review-head` after changing the PR to `new-review-head`, proving that refresh did not silently stamp a new revision. Expansion, collapse, and layout changes preserve the current selected file and draft. Navigating to another file still closes the unsent inline composer; that remains a narrower preservation behavior than a global multi-file unsent-draft store. [ReviewWorkspace source](../frontend/src/ReviewWorkspace.tsx), [code review report](validation/code-review-report.json).

## Language-intelligence frontend seam

**Implementation choices, not a server capability claim.** Code tokens remain ordinary text inside `.review-code-text` elements. Side-specific `data-code-path`, `data-code-line`, and `data-code-side` attributes identify the actual path and one-based source line; removed lines use the old/previous path. Diff markers and line-number gutters remain outside the selectable code so they do not corrupt language-server character offsets. The LSP wrapper handles traversal across nested token spans and its own requests; the highlighter does not make language-server calls.

For local unstaged changes, the frontend requests new=`working`, old=`index`; for staged changes, new=`index`, old=the current head SHA. Mutable local content gets its own content identity through the backend contract. A source-content revision key invalidates replies when the actual diff changes, while an unchanged background poll leaves the read-only dependency viewer mounted. PHP, TypeScript/JavaScript, and Vue were the first language-intelligence scope; server installation, materialization, dependency access, cancellation, and actual server smoke results are documented in [RESEARCH_LSP.md](RESEARCH_LSP.md), [LANGUAGE_INTELLIGENCE.md](LANGUAGE_INTELLIGENCE.md), and [README](../README.md).

## Official logos, editor actions, and Jira preview

**External sources.** Assets identify the corresponding applications and keep their official artwork. The asset inventory is also maintained in [ASSET_ATTRIBUTIONS.md](../ASSET_ATTRIBUTIONS.md).

| Local asset | Official portal and exact source |
| --- | --- |
| `frontend/public/editors/vscode.svg` | [VS Code brand guidance](https://code.visualstudio.com/brand); `visual-studio-code-icons/vscode.svg` in the [official icon archive](https://code.visualstudio.com/assets/branding/visual-studio-code-icons.zip) |
| `frontend/public/editors/phpstorm.svg` | [JetBrains brand resources](https://www.jetbrains.com/company/brand/); [official PhpStorm SVG](https://resources.jetbrains.com/storage/logos/web/phpstorm/phpstorm.svg) |
| `frontend/public/editors/zed.png` | [Zed brand resources](https://zed.dev/brand); [stable application icon](https://zed.dev/_next/static/media/stable-app-logo.0lgsg40_u_1r5.png) |
| `frontend/public/integrations/jira.svg` | [Atlassian logo guidance](https://atlassian.design/foundations/logos); original `Jira_icon.svg` in the [Jira application archive](https://atlassian.design/assets/700bfde52eb7/logos/jira_app.zip) |

**Product choices.** The editor split button defaults to VS Code, reads the saved preference in desktop mode, and offers VS Code, PhpStorm, and Zed with their real logos. A separate GitHub button opens the repository web URL. Desktop launch is unavailable in a browser or without a selected local root. While opening, a ref guards duplicate requests; both split controls are disabled with a normal cursor and no pointer events. Errors return the control to a retryable state.

**Observed timing bug and fix.** A delayed preferred-editor result replaced a user selection: selecting PhpStorm before the saved Zed preference resolved changed the label to “Open in Zed.” The same test became green after the component gave explicit user choice priority and ignored canceled preference requests. [Recorded feedback loop](validation/frontend-bug-loops.json), [EditorActions source](../frontend/src/EditorActions.tsx).

The broader editor check used actual `Call.ByName` serialization through a fake transport. It verified `main.EditorService.OpenRepository('/qa/alpha', 'phpstorm')`, only one call on a double click, loading cursor behavior, failure/retry, saved Zed selection on mount, browser/no-root disabling, successfully decoded logo assets, and separate mocked `Browser.OpenURL`. No actual editor process was launched by this browser QA. [Editor/profile/live report](validation/editor-avatar-live-report.json).

The original Jira installation preview and its three-second timing measurements are historical. That simulation was subsequently disabled and has now been replaced by real OAuth configuration and read-only ticket APIs. The old [Jira report](validation/jira-report.json) is evidence only for that preview; [Jira setup](JIRA.md) and [the validation record](VALIDATION.md) describe the current implementation and its qualification limits.

## Profile photos and role badges

**Implementation choices.** GitHub DTO mapping registers real avatar URLs for authors, reviewers, assignees, commenters, and the authenticated account. Shared [Avatar and ActorName](../frontend/src/Avatar.tsx) components validate image protocols, fall back to initials after load failure, strip `[bot]` from the visible name, show a separate Bot badge, and show Author on comments/reviews by the PR author. Inline review avatars retain the compact 25px size. The current frontend bot mapping recognizes the `[bot]` suffix/name and the shared model's `bot` flag; it does not claim independent inference of every possible automation account.

**Local experiments.** Fake provider profiles exercised a loaded image, Author badge, Bot badge without the literal suffix, 25px avatar size, failed-image initials, and rejection of a JavaScript image URL. The live fixture also verified the connected account's photo/name in the sidebar and author/bot presentation in conversation comments. [Editor/profile/live report](validation/editor-avatar-live-report.json), [liveData mapping](../frontend/src/liveData.ts).

## Durable evidence and reproduction

The JSON files in [validation](validation/) are copied from the isolated browser runs. Each records its test context and whether it represents the earlier demo or the later mocked live frontend. Fixtures contain invented accounts, repository paths, content, and revisions. They contain no saved token, private repository content, or real account mutation. Original `/tmp/opengit-qa` screenshot paths in the historical report describe where those images were captured; the screenshots themselves are not copied by this evidence archive.

| Evidence | Scope |
| --- | --- |
| [prototype-report.json](validation/prototype-report.json) | Historical sample-data workflow and layout checks, 11 passing scenarios |
| [prototype-retry-report.json](validation/prototype-retry-report.json) | Historical stage/commit selector correction and passing retry |
| [markdown-report.json](validation/markdown-report.json) | Current Markdown renderer, navigation mocks, scrolling and appearance |
| [jira-report.json](validation/jira-report.json) | Three-second preview and duplicate/error behavior |
| [code-review-report.json](validation/code-review-report.json) | PHP grammar, exact text, paired rows, preferences and earlier-head composer |
| [local-code-report.json](validation/local-code-report.json) | Local diff loading, refresh retention, grammar, stage/commit failure recovery |
| [editor-avatar-live-report.json](validation/editor-avatar-live-report.json) | Native service mocks, profile/badges, assigned membership, recent repository events |
| [frontend-bug-loops.json](validation/frontend-bug-loops.json) | Recorded failing and passing PHP/editor observations |

Persistent tests can be run from `frontend/`:

```sh
npm test
npx tsc --noEmit
node --test src/syntaxHighlighter.test.ts src/codeReviewPreferences.test.ts
```

The focused grammar/preferences tests passed 8/8 and TypeScript checks passed after the final frontend source changes covered here. Headless Chromium contexts were isolated and the app was served locally at `http://127.0.0.1:9245`. Browser preview/mocked transport results establish frontend behavior; native builds, real language servers, keychains, provider schemas, and platform-specific operations need their corresponding evidence in the other research notes. This document does not claim cross-platform desktop launch validation or complete GitKraken feature parity.
