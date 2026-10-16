# AI pull request reviews

M.I.E.R. can review a PR from a [connected review provider](REVIEW_PROVIDERS.md) with your ChatGPT subscription through Codex, or your GitHub Copilot account through the official Copilot SDK. Copilot still uses your separate GitHub account. Open a PR, choose **Review with AI**, select a provider/model and intensity, and start. Use **Expand code area** for more code space. The assistant docks beside the editor when there is room and below it in smaller review areas.

The chat uses [assistant-ui](https://www.assistant-ui.com/docs/runtimes/custom/external-store) with M.I.E.R.'s native review state. Enter starts a review with your focus; Shift+Enter adds a line. An empty focus starts a general review. Each message starts a fresh review of the captured PR, rather than resuming an unrestricted coding conversation. Only the latest run is shown. The provider, model, and intensity are remembered; unfinished focus text stays in memory while hiding the pane. Model choices include locally bundled brand marks from [Lobe Icons](https://github.com/lobehub/lobe-icons) (the asset license is in `frontend/public/ai/LICENSE`); unknown model families use a neutral icon.

Choose **Quick** for high-impact correctness bugs, **Balanced** for behavior and error paths, or **Thorough** for callers, dependencies, boundary cases and test coverage. The choice changes the review instructions for every model. Codex and Copilot also receive the corresponding low/medium/high reasoning effort when their live model catalog advertises it; otherwise the model keeps its own reasoning default. These are review-depth preferences, not speed, cost, coverage or accuracy guarantees. External MCP clients receive the chosen intensity and focus in the review context and control their own inference settings.

See the review chat in [light](../art/mier-ai-chat-light.png) and [dark](../art/mier-ai-chat-dark.png) appearance. These screenshots use a sample PR and simulated provider responses.

Starting a review sends the captured PR description, changes, requested source, and requested linked ticket content from connected Jira or Linear accounts to your selected provider. Review only code and ticket information you are allowed to share with that provider. Nothing runs until you start a review.

## Linked ticket context

The reviewer can read Jira and Linear tickets linked through the PR branch, title or description, plus tickets you added manually. Only connected providers participate. Repository project/team filters and personally hidden tickets are respected when the review starts. `get_review_context` lists the captured provider/key pairs; `read_review_ticket` reads one of those pairs. Arbitrary ticket searches, unrelated keys, attachments, linked web pages and ticket changes are unavailable.

Ticket reads include the summary, status, assignee, priority, labels, description, configured Jira acceptance criteria and up to ten recent comments. Jira document content is converted to readable text; Linear Markdown is preserved. Reads are lazy and use the existing memory-only provider caches, which can retain details for two minutes. Each result includes its read time and the provider's update time when available. Ticket contents can change independently of the captured Git commit. Missing comments and truncated content are marked explicitly; inaccessible tickets do not prevent code review.

There are at most 25 captured references per provider, 30 ticket reads per review and 32 KiB of ticket text per read, with smaller individual field limits. Changing the linked site/workspace, repository filters or authorization invalidates the existing provider access, including cached details. Start a new review to capture new links or changed settings. Ticket text is untrusted context, never agent instructions. Read access cannot create, edit, transition or comment on tickets, or change their personal links.

## Connect your provider

### ChatGPT / Codex

Install the official CLI:

```sh
npm install -g @openai/codex
```

In **Settings → AI review**, choose **Sign in with ChatGPT** and complete the displayed device code in your browser. An existing ChatGPT login in Codex also works. M.I.E.R. uses Codex's supported local `app-server` protocol; Codex owns login, credential storage, and refresh. M.I.E.R. does not copy ChatGPT cookies or read Codex's credential file. An API-key-only Codex login does not enable this subscription provider.

### GitHub Copilot

Install the official CLI:

```sh
npm install -g @github/copilot
```

Connect your GitHub account to M.I.E.R. through **Accounts**, using a GitHub CLI login (`gh auth login`) or a supported OAuth token. The Copilot SDK receives that credential in Go, outside the webview, and uses the account's Copilot access. GitHub OAuth tokens and appropriately permitted fine-grained tokens are supported; classic `ghp_` PATs are rejected. A regular GitHub account alone does not guarantee Copilot model access.

Both providers need their CLIs available to the desktop application. **Settings → AI review → Executable paths** accepts an absolute path if automatic detection fails, including applications launched from the macOS Dock. Update the provider CLI if its local protocol is unsupported. Provider model lists are discovered dynamically; quotas and organization policies still apply.

## Suggestions and review control

- Findings stay in the assistant. After completion, cancellation or failure, choose **View code**, **Edit comment**, **Add to review**, or **Dismiss**. Check individual findings or **Select all**, then **Add selected** for a batch. Selection starts empty. Dismissed findings can be restored; rerunning starts a fresh set of decisions.
- Only findings you explicitly accept become native **private pending comments**, with provider attribution and severity. An edited comment retains its original finding ID and captured revision. Deleting an accepted comment never silently adds it again. **Go to comment** opens an accepted draft; **View code** highlights a current-revision location in blue. Earlier-revision findings can be added as outdated drafts.
- Human and AI comments share the existing draft store. Accepting a finding does not replace your own pending comments.
- Only your existing **Finish review** action publishes comments to GitHub, under your account. The agent has no publishing, merge, shell, or file-edit tool.
- A review captures immutable head, base and merge-base commits. Old-side source comes from the merge base; a fork's new-side source comes from that fork. Findings must reference real lines in the captured diff. Comments from an earlier head remain outdated drafts after you accept new commits.
- **Stop review** terminates the provider session and revokes its local tools. Partial suggestions are retained. Provider errors and quota failures are shown explicitly.
- Reviews continue while you navigate inside the app. Reopening that PR restores status and suggestions. Quitting stops active reviews; already accepted comments persist as normal review drafts. Provider session history and MCP credentials are not persisted by M.I.E.R.

Reviews are bounded to 64 files, 384 KiB of patch, 50 comments, 100 source reads and 30 minutes. Each source read returns at most 250 lines and 64 KiB; files larger than 2 MiB and binary content are excluded. Missing/truncated patches and scope limits are reported for manual review. Up to four reviews can run at once, with one reviewer per captured PR revision. Reviewers inspect code; they cannot execute tests or validate a working checkout.

See [manual finding actions in dark appearance](../art/mier-ai-actions-dark.png) and [activity and usage in light appearance](../art/mier-ai-activity-light.png). These use a sample PR and simulated provider telemetry; real-provider validation is described below.

## Activity and usage

Expand **Activity and usage** in a review to see executed review tools, running/completed/failed/cancelled states, durations, repository paths and requested line ranges. Successful source reads report the actual returned lines and bytes. Ticket reads show the provider, key and returned text bytes, with separate totals for successful reads and unique tickets. Aggregate counts include all calls; the timeline retains the latest 200. File-read totals count successful reads, not the patches returned with PR context. Raw tool arguments/results, source and ticket text, provider logs and credentials are excluded from telemetry.

Token and billing values come from each provider rather than estimates:

- Codex's [`thread/tokenUsage/updated`](https://learn.chatgpt.com/docs/app-server) supplies cumulative input, output, total, cache and reasoning tokens. Cache and reasoning counts are not added to the total again. Repeated notifications replace cumulative values.
- [Copilot usage events and session metrics](https://docs.github.com/en/copilot/how-tos/copilot-sdk/features/usage-and-billing) supply input/output/cache tokens, model calls, API time and premium request units or nano-AI units when available. Per-call events are deduplicated; final session metrics replace the observed subtotals. Missing or unsupported final metrics leave the event values labelled as subtotals. Partial AI-unit billing remains labelled as a subtotal even when token totals are available. These billing units are not dollars.
- **Dollar cost: Not reported** is intentional: these subscription connections do not supply a dollar charge for an individual review. Missing token/cost fields, including external MCP client usage, remain **Not reported**. A provider-reported zero is displayed as zero. M.I.E.R. does not infer API prices or subscription overage charges.

Activity and usage stay in memory with the review while the app is running. Browser storage contains only the session ID and dismissed finding IDs. Explicitly accepted comments use the normal persisted draft store. Quitting removes the activity timeline and unaccepted proposals.

## Connect an external MCP client

In a PR's assistant choose **Connect an MCP client… → Create MCP connection**. Copy the generated configuration into a local client supporting Streamable HTTP and bearer headers. M.I.E.R. exposes exactly five tools:

| Tool | Access |
| --- | --- |
| `get_review_context` | Captured PR metadata, patches and allowed ticket references |
| `read_review_file` | Bounded source at captured revisions |
| `read_review_ticket` | Read-only content for a captured linked Jira/Linear ticket |
| `add_review_comment` | Stage an anchored finding in the assistant for human approval |
| `finish_review` | Record a summary and finish the external review |

The loopback endpoint and bearer are unique to that review. Keep the connection configuration private: it permits reading that repository's captured source and linked tickets, and proposing findings until **Stop review**, completion, app exit, or the 30-minute expiry. Reopening the pane restores the same active connection. Tokens stay in memory and are not saved to browser storage. This is a local review server, not a remotely reachable general workspace server.

Codex consumes the MCP bridge directly. The Copilot SDK registers these five capabilities as explicitly allowed custom tools, forwarding their calls through an authenticated MCP client. Copilot's built-in MCP servers, commands, filesystem tools, repository instructions, plugins and remote session export are disabled for this reviewer. Starting a review authorizes only these scoped proposal tools; it does not authorize importing findings or publishing comments.

## Integration references and validation

This local CLI approach follows the official [Codex app-server](https://learn.chatgpt.com/docs/app-server) and [authentication](https://learn.chatgpt.com/docs/auth) documentation, the [Copilot SDK authentication guide](https://docs.github.com/en/copilot/how-tos/copilot-sdk/auth/authenticate), and the official [Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk). T3 Code's [Codex adapter](https://github.com/pingdotgg/t3code/blob/main/apps/server/src/orchestration-v2/Adapters/CodexAdapterV2.ts) informed the long-lived control connection and per-review tool scoping. Hosted/commercial deployments need to evaluate OpenAI's current [Sign in with ChatGPT integration requirements](https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server) before reusing this local-desktop flow.

Real Linux provider smoke reviews passed with Codex 0.162.1 and Copilot CLI 1.0.98 / Go SDK 1.0.22: both read a disposable diff, found its retry bug, and proposed a finding and summary through the scoped tools. CI uses deterministic fake-provider protocol tests and real local MCP transports; it does not require subscriptions or send repository code to an AI service. macOS/Windows provider runtime interaction requires separate native qualification.
