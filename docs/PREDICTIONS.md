# Jev assessments

M.I.E.R. uses TypeSafe's [System One API](https://api.typesafe.ai/redoc) for optional structured assessments. Jev returns choices, scores and probabilities. The existing Codex/Copilot reviewer supplies explanations, code proposals and private review comments.

Connect your own API key in **Settings → AI review → TypeSafe · Jev**. Obtain it from the [TypeSafe console](https://console.typesafe.ai). The key stays in the OS keychain; it is not written to application preferences or browser storage. **Refresh models** discovers the account's available names. The default is pinned to `jev-1.13.0`; choose a different available model when ready to evaluate it. Moving aliases can change behavior without an app update.

Enable each hosted or local repository separately under **Configure Jev**, then choose **Start assessment**. Hosted and local consent are separate. Opening a PR or enabling a repository never starts model inference automatically. An assessment shares bounded source, linked ticket requirements, or log excerpts with TypeSafe. Its published policies describe [data handling and enterprise zero data retention](https://docs.typesafe.ai/legal).

| Where | Assessment | Result and control |
| --- | --- | --- |
| PR changes → **Review priorities and ticket coverage** | Review order and specialist routing | Attention scores and suggested review specialties. **Suggested review order** sorts the file navigator only when checked. Every file remains available. |
| The same panel → **Ticket acceptance coverage** | Jira/Linear requirements | One judgment per captured criterion, or a broad ticket-intent check when no acceptance field exists. **View ticket** opens the existing ticket dock. No ticket changes are possible. |
| Completed/stopped AI review → **Assess finding evidence with Jev** | Source support and possible duplicates | Annotates the original findings. Accepting, editing, dismissing and publishing remain explicit review actions. |
| Actions job logs → **Assess job failure with Jev** | Failure category and relevant excerpts | Displays likely cause categories and bounded evidence. It cannot rerun or cancel jobs. |
| Conflict editor → **Conflict investigation with Jev** | Next investigation step and behavior disagreement | Compares base/current/incoming text at the displayed conflict token. It cannot modify the result, stage a file or continue Git. |

## Scope, uncertainty and usage

Assessments are advisory. A score out of two prioritizes attention; it is not the probability that a file contains a bug. **Model certainty** summarizes the returned option distribution and is not a measured correctness rate for your repository. See [TypeSafe's confidence documentation](https://docs.typesafe.ai/confidence). Missing code, tests and provider data remain limitations rather than proof that a change is safe or a requirement is complete.

PR inputs use displayed immutable head/base commits and the provider's merge base. Findings must belong to that PR revision. Conflict inputs must match the current conflict token. Tickets are restricted to linked, visible Jira/Linear references and retain provider update times; ticket contents can change independently of Git revisions. Provider filters and hidden ticket choices are respected.

An assessment has a 75-second total deadline, a 25-second model request timeout and a 24 KiB state limit. At most two run together. Review/routing assesses up to 24 text patches within a 14 KiB patch budget; findings assess up to 12 captured findings with bounded diff evidence; ticket coverage reads up to four tickets and 12 criteria; conflict versions are limited to 5,000 bytes each. CI sends at most ten bounded error excerpts, or a bounded tail when no error markers are found. Omitted and truncated context is reported. Binary/missing text is never treated as reviewed evidence.

All questions using the same captured state are batched. The native client validates answer names, types, probabilities, score consistency and token usage. It follows no credential redirects, performs no automatic inference retries, and reports sanitized errors. Successful results stay in memory for five minutes, with at most 32 retained. Reusing a result is labelled **cached**; its displayed tokens belong to the original call. Settings changes and disconnect cancel active assessments and clear results. **Cancel assessment** cancels the native request; TypeSafe may already have processed or billed it.

Token counts and the answering model come from TypeSafe. A dollar charge is not supplied by this API, so M.I.E.R. displays **Charge not reported**. **Useful / Not useful** stores at most 200 ratings in memory. **Local evaluation feedback → Load evaluation metadata** exposes only assessment ID, kind, model, rating and timing. This feedback is not a measurement of calibration, bug recall or missed findings. It is cleared when the application exits.

Automated validation uses deterministic transport fixtures, linked-ticket readers and actual disposable Git conflicts. It does not send project source to TypeSafe or establish Jev's review accuracy or latency on your project. Qualify a chosen model with historical labelled PRs before relying on its ranking.
