# AI pull request reviews

M.I.E.R. can review a GitHub PR with your ChatGPT subscription through Codex, or your GitHub Copilot account through the official Copilot SDK. Open a PR, choose **Review with AI**, select a provider/model, and start. Use **Expand review** for more code space. The assistant docks beside the editor when there is room and below it in smaller review areas.

Starting a review sends the captured PR description, changes, and source requested by the reviewer to your selected provider. Review only repositories you are allowed to share with that provider. Nothing runs until you start a review.

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

- Findings become native **private pending comments**, with provider attribution and severity. Inspect them, delete them, or add a removed suggestion back from the assistant.
- Human and AI comments share the existing draft store. Receiving a suggestion does not replace your own pending comments.
- Only your existing **Finish review** action publishes comments to GitHub, under your account. The agent has no publishing, merge, shell, or file-edit tool.
- A review captures immutable head, base and merge-base commits. Old-side source comes from the merge base; a fork's new-side source comes from that fork. Findings must reference real lines in the captured diff. Comments from an earlier head remain outdated drafts after you accept new commits.
- **Stop review** terminates the provider session and revokes its local tools. Partial suggestions are retained. Provider errors and quota failures are shown explicitly.
- Reviews continue while you navigate inside the app. Reopening that PR restores status and suggestions. Quitting stops active reviews; already imported comments persist as normal review drafts. Provider session history and MCP credentials are not persisted by M.I.E.R.

Reviews are bounded to 64 files, 384 KiB of patch, 50 comments, 100 source reads and 30 minutes. Each source read returns at most 250 lines and 64 KiB; files larger than 2 MiB and binary content are excluded. Missing/truncated patches and scope limits are reported for manual review. Up to four reviews can run at once, with one reviewer per captured PR revision. Reviewers inspect code; they cannot execute tests or validate a working checkout.

## Connect an external MCP client

In a PR's assistant choose **Connect an MCP client… → Create MCP connection**. Copy the generated configuration into a local client supporting Streamable HTTP and bearer headers. M.I.E.R. exposes exactly four tools:

| Tool | Access |
| --- | --- |
| `get_review_context` | Captured PR metadata and patches |
| `read_review_file` | Bounded source at captured revisions |
| `add_review_comment` | Add an anchored, private draft suggestion |
| `finish_review` | Record a summary and finish the external review |

The loopback endpoint and bearer are unique to that review. Keep the connection configuration private: it permits reading that repository's captured source and adding draft suggestions until **Stop review**, completion, app exit, or the 30-minute expiry. Reopening the pane restores the same active connection. Tokens stay in memory and are not saved to browser storage. This is a local review server, not a remotely reachable general workspace server.

Codex consumes the MCP bridge directly. The Copilot SDK registers these four capabilities as explicitly allowed custom tools, forwarding their calls through an authenticated MCP client. Copilot's built-in MCP servers, commands, filesystem tools, repository instructions, plugins and remote session export are disabled for this reviewer. Starting a review authorizes only these additive private draft tools.

## Integration references and validation

This local CLI approach follows the official [Codex app-server](https://learn.chatgpt.com/docs/app-server) and [authentication](https://learn.chatgpt.com/docs/auth) documentation, the [Copilot SDK authentication guide](https://docs.github.com/en/copilot/how-tos/copilot-sdk/auth/authenticate), and the official [Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk). T3 Code's [Codex adapter](https://github.com/pingdotgg/t3code/blob/main/apps/server/src/orchestration-v2/Adapters/CodexAdapterV2.ts) informed the long-lived control connection and per-review tool scoping. Hosted/commercial deployments need to evaluate OpenAI's current [Sign in with ChatGPT integration requirements](https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server) before reusing this local-desktop flow.

Real Linux provider smoke reviews passed with Codex 0.162.1 and Copilot CLI 1.0.98 / Go SDK 1.0.22: both read a disposable diff, found its retry bug, and added a native comment and summary. CI uses deterministic fake-provider protocol tests and real local MCP transports; it does not require subscriptions or send repository code to an AI service. macOS/Windows provider runtime interaction requires separate native qualification.
