# Review providers

Open **Settings → Accounts → Review providers** to connect GitLab, Bitbucket Cloud, or Azure DevOps Services. GitHub and GitHub Enterprise retain the GitHub CLI / keychain account controls above them. Tokens are validated with the provider before being saved in the OS keychain. Password fields are never saved in browser storage or repository configuration.

Open a local repository with a matching remote. M.I.E.R. uses the provider's native API for PR lists, review details, files, commits, conversations, checks, and stacks. GitLab's nested namespaces, Azure project paths, Git URL rewrites, and SSH host aliases are recognized. A provider token is only used for its HTTPS origin; SSH still uses the configured SSH transport.

## Authentication

| Provider | Connection | Token permissions |
| --- | --- | --- |
| GitLab.com / self-managed GitLab | Hostname and personal access token | `api` for review writes; `read_api` for read-only browsing. `read_repository` for Git over HTTPS; `write_repository` when pushing. Existing project membership still applies. |
| Bitbucket Cloud | Atlassian account email and a scoped API token | `read:user:bitbucket`, repository read, and pull-request read. Add separate repository write and pull-request write scopes for publishing. Write does not imply read. |
| Azure DevOps Services | Organization name and personal access token | **Code (Read & write)**, or **Code (Read)** for a read-only connection, in the matching organization. |

Choose **Read-only connection** to disable API writes in M.I.E.R. even if the token has broader permissions. Disconnect removes the keychain token and retains the hostname's provider identity, so it cannot accidentally be queried as GitHub Enterprise. Saved local review drafts remain on this device. Reconnect before publishing them.

Create tokens using the [GitLab instructions](https://docs.gitlab.com/user/profile/personal_access_tokens/), [Bitbucket API token permissions](https://support.atlassian.com/bitbucket-cloud/docs/api-token-permissions/), or [Azure PAT instructions](https://learn.microsoft.com/en-us/azure/devops/organizations/accounts/use-personal-access-tokens-to-authenticate).

## Native behavior

| Capability | GitLab | Bitbucket Cloud | Azure DevOps |
| --- | --- | --- | --- |
| Browse, filter, paginate, create, edit and close reviews | Yes | Yes | Yes |
| Drafts, general comments, inline comments, replies, resolved conversations | Yes | Yes | Yes |
| Approve | Yes | Yes | Yes, native vote |
| Request changes | Leave a comment; no supported REST mutation | Yes | Yes, reject vote |
| Reviewers | Usernames | Bitbucket UUIDs | Azure identity IDs |
| Assignees | Yes | Not in the PR API | Not in the PR API |
| Labels | Yes | Not in the PR API | Yes |
| Related PR stacks and guarded retargeting | Yes | Yes | Yes |
| Merge with an expected source head | Yes | Complete in Bitbucket | Yes, branch policies enforced |
| Reopen closed reviews | Yes | Create a new PR after declining | Yes |

The UI disables unsupported operations. Bitbucket's REST merge endpoint has no expected-head precondition; the **Open pull request** button keeps final merges in Bitbucket. GitHub Actions job logs and run controls remain specific to GitHub. Other providers' check results and links appear in the Checks tab.

Azure's API has no full-text PR search. M.I.E.R. filters each fetched page by title/description and draft status, while preserving pagination through the underlying results. Searching `#123` loads that exact PR. Counts remain unknown when the host does not supply a reliable total. Azure patches use immutable source and merge-base blobs, at most four simultaneous reads, 2 MiB per blob, 32 MiB of source per review, and text patches for the first 200 files. Additional files remain listed with an omission warning. Other API pagination and response limits are reported instead of silently hiding data.

Reviews on these providers require multiple native calls. Each pending comment has a stable ID and an unobtrusive publication marker. Private receipts record hashes and native IDs, not tokens or comment contents. If a request is interrupted, retry continues the remaining comments before applying the approval/vote. Comment-only reviews preserve an existing Azure vote. A published draft whose body was subsequently edited must be replaced with a new draft comment.

PR checkout verifies the displayed source head before switching. Source capture fetches the actual source branch/fork and the target independently, preserving the current checkout, tracking branches, and `FETCH_HEAD`. Azure uses its source commit rather than its generated merge-preview commit. Existing code intelligence and read-only review tools use the same provider sources.

Bitbucket Data Center and Azure DevOps Server are separate APIs and are not supported by these Cloud adapters.
