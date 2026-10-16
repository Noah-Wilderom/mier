import { useState } from "react";
import type { Check, PullRequest } from "./data";
import type { RepositoryIdentity } from "./services";
import { services } from "./services";
import { Markdown } from "./Markdown";
import { Icon } from "./Icon";
import { ActionsError, ActionsLink, ActionsStatus, JobLogs } from "./Actions";
import { useActionsResource } from "./useActionsResource";

function CheckLogs({
  repository,
  check,
}: {
  repository: RepositoryIdentity;
  check: Check;
}) {
  const job = useActionsResource(
    `check:${check.checkRunId}:${check.rawStatus}`,
    (signal) =>
      services.actions.checkJob(repository, check.checkRunId!, signal),
  );
  return (
    <>
      <ActionsError
        error={job.error}
        retry={job.refresh}
        loading={job.loading}
      />
      {job.loading && !job.data && <p role="status">Finding Actions job…</p>}
      {job.data && <JobLogs repository={repository} job={job.data} />}
    </>
  );
}
export default function Checks({
  pr,
  repository,
  account,
}: {
  pr: PullRequest;
  repository?: RepositoryIdentity;
  account?: string;
}) {
  const [expanded, setExpanded] = useState<string | null>(null);
  return (
    <div className="checks-pane">
      <div className="section-intro">
        <h2>
          {!pr.checks.length
            ? "No checks reported"
            : pr.checks.every((check) => check.status === "passed")
              ? "All checks passed"
              : pr.checks.some((check) => check.status === "failed")
                ? "Some checks need attention"
                : "Checks are in progress"}
        </h2>
        <span>
          Results for commit{" "}
          <code>{(pr.checkHeadSha || pr.headSha).slice(0, 7)}</code>
        </span>
      </div>
      {pr.checks.map((check) => (
        <div className="check-entry" key={check.id}>
          <button
            className="check-row"
            aria-expanded={expanded === check.id}
            onClick={() => setExpanded(expanded === check.id ? null : check.id)}
          >
            <ActionsStatus
              status={
                check.rawStatus ||
                (check.status === "running" ? "in_progress" : "completed")
              }
              conclusion={
                check.rawStatus
                  ? check.conclusion
                  : check.status === "passed"
                    ? "success"
                    : check.status === "failed"
                      ? "failure"
                      : undefined
              }
            />
            <div>
              <strong>{check.name}</strong>
              <span>{check.required ? "Required" : "Optional"}</span>
            </div>
            <span>{check.duration}</span>
            <Icon
              name={expanded === check.id ? "chevron-down" : "chevron-right"}
            />
          </button>
          {expanded === check.id && (
            <div className="check-details">
              {!!check.output.length && (
                <Markdown attachmentsAsLinks>
                  {check.output.join("\n\n")}
                </Markdown>
              )}
              <ActionsLink url={check.detailsUrl} label="View check details" />
              {repository &&
              check.checkRunId &&
              check.appSlug === "github-actions" && pr.provider?.capabilities?.actions !== false && (!repository.kind || repository.kind === "github") ? (
                <CheckLogs
                  key={`${check.id}:${repository.host}:${repository.owner}:${repository.repo}:${account}`}
                  repository={repository}
                  check={check}
                />
              ) : (
                !check.output.length && (
                  <p className="text-muted">
                    This provider did not publish a check summary. Open the
                    check for its output.
                  </p>
                )
              )}
            </div>
          )}
        </div>
      ))}
      {!repository && (
        <div className="checks-note">
          <Icon name="shield" size={16} />
          Sample checks illustrate merge requirements.
        </div>
      )}
    </div>
  );
}
