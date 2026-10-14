# Jira Cloud tickets

M.I.E.R. discovers Jira issue keys in a pull request's branch, title, and description. Ticket cards show their source, summary, status, and assignee. Open a card to read the description, configured acceptance criteria, and ten recent comments while keeping the review open. **Add ticket** searches Jira; **Hide** and **Restore** keep personal selections for that PR.

The ticket reader opens in a pane beside the editor, with its own scrolling area. Drag the divider to adjust its width, or focus the divider and use the arrow keys, Home, or End. The editor remains interactive, and opening, resizing, or closing the ticket preserves an in-progress review comment. Selecting the already-open ticket focuses its reader and keeps the loaded details. Close it with the header button or Escape while focused inside the reader. When the review area is too narrow for both columns, the ticket appears below the editor in a scrollable layout.

## Connect your app

Jira is optional. Open the desktop application's **Settings → Apps → Configure Jira** during setup or connect later from a PR's **Connect Jira** button. The registration guide and credential fields are inside the application. Standalone browser previews cannot connect an account.

1. Open the [Atlassian developer console](https://developer.atlassian.com/console/myapps/) and create an **OAuth 2.0 integration**. Choose the account-level grant if the console asks for a grant type.
2. Under **Permissions**, select **Add** next to the **Jira API**. Then select **Configure** for that API, add the classic **`read:jira-work`** scope (**View Jira issue data**), and save your changes. Creating the OAuth app and registering a callback do not add Jira API access automatically.
3. Under **Authorization → OAuth 2.0**, register the exact callback URL displayed by M.I.E.R. The default is:

   ```text
   http://localhost:43827/oauth/jira/callback
   ```

4. Copy your app's **Client ID** and **Secret** from its Settings into M.I.E.R. and select **Save app credentials**.
5. Select **Authorize Jira**, complete consent in your browser, and return to M.I.E.R. Choose your default Jira site from the authorized sites.

M.I.E.R. adds `offline_access` to the authorization request automatically for refresh tokens. It is separate from the Jira API scope configured in Permissions. These steps follow the [Atlassian OAuth guide](https://developer.atlassian.com/cloud/jira/platform/oauth-2-3lo-apps/); the classic permission is described in [Jira's scope reference](https://developer.atlassian.com/cloud/jira/platform/scopes-for-oauth-2-3LO-and-forge-apps/). The callback must match the registered URL, including the port and path. Changing credentials or the callback port requires authorization again. Leaving the secret blank when editing the same client ID keeps the saved secret.

Each installation uses the app credentials supplied locally. Jira credentials belong in the application's setup form; no Jira repository or GitHub Actions secret is required, and M.I.E.R. supplies no shared Jira app.

Atlassian's current cloud-app requirements prohibit distributed integrations that require customers to create individual OAuth apps. This implementation follows the maintainer's requested credentials model; it should not be described as satisfying Atlassian's public distribution requirements. See the notice in [Atlassian's OAuth documentation](https://developer.atlassian.com/cloud/jira/platform/oauth-2-3lo-apps/).

## Browser authorization errors

If the developer console's Authorization page says **Your app doesn't have any APIs**, the app has no Jira API configured. This can leave Atlassian's consent page displaying **Something went wrong** before it redirects back to M.I.E.R. Follow the Permissions step above to add the Jira API and its classic `read:jira-work` scope, save, then **Cancel authorization** in M.I.E.R. and start **Authorize Jira** again. The same recovery instructions are available under **Browser authorization failed?** while a connection attempt is active.

If the browser still shows an error, expand **Information for the owner** on Atlassian's error page for the provider's specific cause. Check that the saved callback exactly matches M.I.E.R.'s displayed URL and that the Client ID came from this OAuth app's Settings. For a private app, sign in as the app owner; using a different Atlassian account requires enabling sharing under Distribution. Atlassian documents these requirements in its [OAuth guide](https://developer.atlassian.com/cloud/jira/platform/oauth-2-3lo-apps/). Keep M.I.E.R. open until consent finishes so its temporary callback listener is available.

If the browser reaches M.I.E.R.'s callback and says **Jira could not be connected**, return to Settings for the specific error. Earlier builds incorrectly described every keychain write failure as a locked keychain. The [macOS keyring backend](https://github.com/zalando/go-keyring/blob/v0.2.8/keyring_darwin.go) rejects encoded commands over 4096 bytes, and the [Windows backend](https://github.com/zalando/go-keyring/blob/v0.2.8/keyring_windows.go) limits individual values to 2560 bytes; a combined OAuth token record can exceed those limits even though the client secret alone fits. That rejection happens without a permission prompt. Update or rebuild M.I.E.R. and authorize again; existing app credentials remain readable.

Current builds store larger records as bounded keychain entries and distinguish size-limit errors from access failures. If an access failure remains on macOS, open **Keychain Access**, check that the **login** keychain is unlocked, and inspect access permissions for the `mag-ik-een-review.jira` entries.

## Match tickets to a repository

Open a PR and select **Jira repository settings** beside its Tickets row.

- **Jira site:** choose the authorized site containing this repository's tickets. The default site is used until a repository is explicitly mapped.
- **Project keys:** enter keys such as `FAN, APP` to limit discovery and search. Leave blank to include all projects visible to your account.
- **Acceptance criteria field:** optionally enter its Jira field ID, such as `customfield_10001`. Your Jira administrator can identify the field. Otherwise, criteria in the ordinary description remain visible there.

Mappings use the GitHub host, owner, and repository, so different local clones share them. Selections additionally use the PR number. Manually added tickets are shown first; repeated keys combine their source labels. Hiding and restoring a manual ticket preserves its selection. These preferences are local to M.I.E.R.; they do not edit Jira issues or the PR.

Discovery reads references in PR text. It does not query Jira's native development panel to prove that a ticket is linked, and it does not inspect commit messages. Search uses Jira's [enhanced JQL search API](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-search/) and returns up to 15 title matches or an exact issue key. Your Jira permissions and app access restrictions still apply.

## Storage and recovery

The client secret, access token, rotating refresh token, and token expiry live in the OS keychain under `mag-ik-een-review.jira`. Small records retain the existing single-item format. Larger records use a small manifest in that item and bounded, encoded parts under `mag-ik-een-review.jira.parts`, with an overall 64 KiB credential limit. Each update writes a new generation of parts before publishing its manifest; incomplete writes leave the previous complete record in use. Reads validate the bounded manifest, part sizes, and checksum. Replaced generations and failed staging writes are cleaned up where keychain access permits; a process interruption before cleanup can leave unused parts in the keychain. All parts remain in the OS credential store. Secrets are not returned to React or written to browser storage. Jira site metadata, client ID, callback port, repository mappings, and personal PR selections live in the user configuration directory's `mag-ik-een-review/jira.json`, using atomic replacement and private file permissions where supported.

The authorization listener binds only to IPv4 loopback while a connection attempt is active. Attempts last up to five minutes and continue when navigating away from Settings. **Cancel authorization**, application shutdown, or completion closes the listener. An occupied port produces a setup error; choose another port in M.I.E.R. and update the registered callback URL before trying again.

Refreshes are serialized, and a newly rotated refresh token is saved before another provider request. If that write fails, the running application retains the new token in memory and retries saving it after the keychain is unlocked. Restarting before that save can require fresh authorization. Expired or revoked grants require **Authorize again** in Settings. **Disconnect** removes locally saved account tokens and ticket caches, while retaining your app credentials and mappings. Revoke the grant in your Atlassian account if you also want to remove provider-side access.

Ticket data is cached only in memory: up to 64 entries for two minutes, invalidated when the account or repository mapping changes. Discovery displays up to 25 matches, with at most four concurrent summary requests. Full descriptions and comments load when a card is opened. Responses are bounded to 2 MiB and individual rich-text fields to 64 KiB. Lists, tables, formatted text, code, and safe web links render from Atlassian Document Format; attachments open in Jira rather than loading external images inside the app. Comments have a separate five-second timeout, so a comment failure can retain the ticket description.

Refresh failures retain the currently displayed cards and offer a retry. A missing or restricted issue has its own unavailable message. Rate limits honor bounded numeric `Retry-After` values. Closing a reader or changing the PR cancels its requests; obsolete account results are discarded. Jira failures do not block the GitHub review.

## Verification

Retained Go tests exercise loopback callbacks, state validation, denied consent, occupied ports, configuration corruption, secure-store failures, rotating tokens, revoked grants, redirects, cancellation, rate limits, ticket matching, project/site mappings, personal selections, and stale account results. Chromium and WebKit tests exercise the setup form and ticket reader through the Wails RPC boundary.

Keychain regressions reproduce the original callback failure using the macOS command and Windows entry limits. They cover larger authorization/refresh records, restart, legacy single-item reads, maximum client ID length, Unicode boundaries, interrupted staging and publication, corrupt manifests/parts, account removal, and disconnect cleanup. The macOS and Windows CI jobs also opt into `MIER_JIRA_KEYCHAIN_SMOKE=1 go test -run '^TestRealJiraKeychainLargeRecord$' -count=1 -timeout 90s .`, which round-trips synthetic data through the real OS keychain using a temporary account and cleans it up. This smoke test does not authorize a Jira account or establish behavior of a user's locked keychain.

These fixtures do not establish live Jira account access. A real OAuth consent flow, your site's field IDs and permissions, and macOS/Windows keychain and callback behavior still need qualification with your own registered app. See [the validation record](VALIDATION.md).
