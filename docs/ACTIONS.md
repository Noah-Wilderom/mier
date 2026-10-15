# GitHub Actions

Open a GitHub repository, then use **Cmd/Ctrl+4** or choose **GitHub Actions** in the command palette (**Cmd/Ctrl+K**). The view uses the same GitHub account and remote as the pull request inbox, including Enterprise hosts.

Filter runs by workflow, status or branch, and page through older runs. Select a run to inspect its commit, event, actor, jobs and steps. **Run attempt** lets you inspect earlier reruns without replacing their logs with a newer attempt's output.

The run list and selected run refresh every 15 seconds while the view is visible. Refresh requests do not overlap. Switching repositories, accounts, filters, runs or attempts cancels obsolete requests; temporary failures retain the last successful results and offer a retry.

## Job logs

Select a job to load its actual Actions logs. Logs appear in searchable, collapsible output groups, with failed groups marked in red. Terminal escape sequences are removed, and log content remains plain text.

GitHub's downloadable job logs become available after that job completes, even if other jobs are still running. Until then, M.I.E.R. shows the changing step states and polls for the archive. Newly completed jobs get a short retry window while GitHub publishes their logs. Expired or deleted archives and permission failures have separate messages and a link to the job on GitHub.

Downloads request a fresh signed URL each time and retry once if it expires. The GitHub token stays on the API request and is never forwarded to log storage. To keep large logs responsive, the viewer retains up to 2 MiB of the end of the log. If storage ignores range requests, downloads are capped at 64 MiB; the viewer explicitly identifies partial output instead of claiming to show the final result.

The PR **Checks** tab also loads Actions job logs when you expand a check. Third-party checks retain their Markdown summaries and provider links. Check IDs are resolved to verified Actions job IDs, preserving the attempt associated with the original check.

## Workflow controls

- **Rerun all jobs**, **Rerun failed jobs**, and **Rerun job** are available for the latest completed attempt. A confirmation shows the repository, workflow, attempt and original commit before changing anything.
- **Cancel run** requests cancellation for an active run. GitHub controls when its jobs actually stop.
- Select an active workflow, then choose **Run workflow** to dispatch it manually. The selected branch or tag must declare `workflow_dispatch`. Input controls come from that workflow file, including required fields, booleans, numbers, choices and environment names. After changing the ref, use **Load inputs for this ref** before submitting.

GitHub rechecks repository permissions. M.I.E.R. also verifies the current attempt before rerunning or canceling, and refuses a job from another run or an older attempt. Write operations are never automatically retried.

## Account permissions

Fine-grained tokens need repository **Actions: read** for runs, jobs and logs, **Actions: write** for rerun/cancel/manual dispatch, and **Contents: read** to read manual workflow inputs. Reading PR checks also needs the existing **Checks: read** permission. Classic tokens need `repo` for private repositories. Organization SSO, repository access, approval requirements and Actions policies still apply.

Official API references: [workflow runs](https://docs.github.com/en/rest/actions/workflow-runs), [workflow jobs and logs](https://docs.github.com/en/rest/actions/workflow-jobs), and [workflow dispatch](https://docs.github.com/en/rest/actions/workflows#create-a-workflow-dispatch-event).
