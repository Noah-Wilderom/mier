# Linear tickets

M.I.E.R. finds Linear issue identifiers in a pull request’s branch, title, and description, including identifiers inside Linear issue links. Open a card to read its Markdown description, status, priority, assignee, team, project, cycle, labels, parent, and ten recent comments beside the editor. **Add ticket** searches by identifier or text; **Hide** and **Restore** save personal selections for that PR.

The ticket row lets you switch between Jira and Linear. Both use the same resizable side pane and keep your review and draft comment intact. Their accounts, settings, caches, and selections are independent. When only Linear is connected, it is selected automatically. A narrow review area places the ticket below the editor.

## Authentication

Use your own **OAuth 2.0 application with PKCE**. Linear supports exchanging and refreshing PKCE tokens with a client ID and no client secret. M.I.E.R. requests only `read`, with the user actor. Consent happens in your system browser; tokens remain in the OS keychain. See [Linear’s OAuth documentation](https://linear.app/developers/oauth-2-0-authentication).

1. In Linear, open **Settings → Administration → API** and create an OAuth application. Workspace admin access is needed to manage OAuth apps. [Linear API settings](https://linear.app/docs/api-and-webhooks).
2. Register the exact callback displayed in M.I.E.R.’s **Settings → Apps → Configure Linear**:

   ```text
   http://localhost:43828/oauth/linear/callback
   ```

3. Copy the app’s **Client ID** into M.I.E.R. and select **Save Linear app**. There is no client-secret field.
4. Select **Authorize Linear**, choose the workspace and grant read access in the browser, then return to M.I.E.R. Keep the app running until authorization finishes.

No Linear repository secret, GitHub Actions secret, hosted callback server, or webhook is required. Each installation supplies its own client ID. Linear also supports personal API keys; this integration uses OAuth so access can expire, refresh, and be revoked without handling a long-lived personal key. [Linear authentication options](https://linear.app/developers/graphql).

The callback listens only on IPv4 loopback during a five-minute authorization attempt. Random state prevents cross-request callbacks; an S256 PKCE challenge binds the code to the app’s verifier. Canceling, completion, or shutdown closes the listener. If port 43828 is occupied, change the port in the setup form and update the registered callback too. Jira uses a separate default port, so both can authorize independently. Changing the client ID or port requires fresh authorization.

Linear access tokens currently last 24 hours and use rotating refresh tokens. Refreshes are serialized and the replacement is saved before further API calls. If the OS keychain temporarily rejects that save, the replacement stays in memory and its save is retried before another request. Restarting before persistence succeeds may require authorization again. [Linear refresh behavior](https://linear.app/developers/oauth-2-0-authentication).

## Repository matching

Open a GitHub PR, choose **Linear** in the Tickets row, and select **Linear repository settings**. Enter team keys such as `ENG, APP` to restrict discovery and search; leave blank for all teams visible in the connected workspace. These keys are the prefix in issue identifiers, not a team’s display name.

Mappings use the Linear workspace ID and normalized GitHub host, owner, and repository. Selections additionally include the PR number. Switching workspaces never applies another workspace’s mappings or manual references to the new account. One Linear workspace is connected at a time; **Authorize Linear again** replaces the connection while preserving workspace-specific preferences.

Discovery finds references in PR text rather than verifying Linear’s native GitHub development links. It does not scan commit messages. Up to 25 related tickets are displayed, with manual additions first and combined source labels for repeated identifiers. Search returns up to 15 matches. These actions read Linear and save local preferences; they do not modify issues, comments, or PRs.

Descriptions and comments render safe Markdown, including lists, task lists, tables, and code. Attachments are browser links; the reader does not embed external images or load authenticated attachment URLs. Open the issue in Linear for attachments and full provider functionality.

## Storage and recovery

Access and refresh tokens live under `mag-ik-een-review.linear` in the OS credential store. The bounded storage used for larger Jira token records also handles Linear’s tokens and native macOS and Windows entry limits. Tokens never reach React or browser storage. Nonsecret app configuration, workspace/account metadata, team mappings, and PR selections live in the user configuration directory’s `mag-ik-een-review/linear.json`, with atomic replacement and private file permissions where supported.

Changing app configuration creates a new credential reference before removing the old record, so a locked keychain cannot cause an old grant to be reused. If removal is denied, an unused record can remain in the OS credential store. **Disconnect Linear** clears local tokens and the connected workspace while retaining the client ID and preferences. Revoke the integration in Linear as well to remove provider-side authorization.

Ticket caches are bounded to 64 entries for two minutes and remain in memory. Account or team changes invalidate them. Full content loads on selection; comments have a separate five-second timeout, so an unavailable comment API does not discard the description. Provider requests use the fixed Linear API host, refuse redirects, bound response sizes to 2 MiB, and discard obsolete account results. Individual Markdown fields are limited to 64 KiB.

Missing or restricted tickets show individual errors. A failed refresh retains the displayed cards. Rate limits respect a bounded `Retry-After` value and show a retry message; GraphQL errors are checked even when HTTP returns 200. Provider failures leave the GitHub review available.

## Verification

Automated tests use synthetic OAuth responses, loopback callbacks, temporary configuration, and fake secure stores. Browser fixtures exercise the Wails RPC boundary in Chromium and WebKit. They do not authorize a real Linear account. A live consent flow and workspace permissions must still be verified using your registered OAuth application.
