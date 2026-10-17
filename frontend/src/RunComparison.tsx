import { useState } from "react";
import type { WorkflowRun, WorkflowJob } from "./actionsTypes";
import { services, type RepositoryIdentity } from "./services";
import { useActionsResource } from "./useActionsResource";
import { ActionsError } from "./Actions";
import { compareWorkflowJobs } from "./workflowJobComparison";
const statuses = (jobs: WorkflowJob[], identify: boolean) =>
  jobs
    .map(
      (job) =>
        `${job.conclusion || job.status}${identify ? ` · #${job.id}` : ""}`,
    )
    .join("; ") || "Absent";
const failures = (jobs: WorkflowJob[], identify: boolean) =>
  jobs
    .flatMap((job) =>
      (job.steps ?? [])
        .filter((step) => step.conclusion === "failure")
        .map((step) => `${identify ? `#${job.id}: ` : ""}${step.name}`),
    )
    .join(", ") || "None";
type Props = {
  repository: RepositoryIdentity;
  run: WorkflowRun;
  jobs: WorkflowJob[];
  latestAttempt: number;
};
export default function RunComparison(props: Props) {
  const [opened, setOpened] = useState(false);
  return (
    <details
      className="run-comparison"
      onToggle={(event) => {
        if (event.currentTarget.open) setOpened(true);
      }}
    >
      <summary>Compare runs and attempts</summary>
      {opened && <RunComparisonContents {...props} />}
    </details>
  );
}
function RunComparisonContents({
  repository,
  run,
  jobs,
  latestAttempt,
}: {
  repository: RepositoryIdentity;
  run: WorkflowRun;
  jobs: WorkflowJob[];
  latestAttempt: number;
}) {
  const [baseline, setBaseline] = useState("");
  const runs = useActionsResource(
    `compare-runs:${run.workflowId}:${run.headBranch}`,
    (signal) =>
      services.actions.runs(
        repository,
        {
          workflowId: run.workflowId,
          branch: run.headBranch,
          status: "completed",
          page: 1,
        },
        signal,
      ),
  );
  const previous = useActionsResource(`compare:${baseline}`, (signal) => {
    if (!baseline) return Promise.resolve(undefined);
    const [id, attempt] = baseline.split(":").map(Number);
    return services.actions.run(repository, id, attempt, signal);
  });
  const comparison = compareWorkflowJobs(previous.data?.jobs ?? [], jobs);
  return (
    <div>
      <label>
        Baseline
        <select
          aria-label="Compare workflow baseline"
          value={baseline}
          onChange={(e) => setBaseline(e.target.value)}
        >
          <option value="">Select a run or attempt…</option>
          {Array.from({ length: Math.max(0, latestAttempt) }, (_, i) => i + 1)
            .filter((attempt) => attempt !== run.runAttempt)
            .map((attempt) => (
              <option
                key={`${run.id}:${attempt}`}
                value={`${run.id}:${attempt}`}
              >
                This run · attempt {attempt}
              </option>
            ))}
          {runs.data?.items
            ?.filter((value) => value.id !== run.id)
            .map((value) => (
              <option key={value.id} value={`${value.id}:${value.runAttempt}`}>
                #{value.runNumber} · {value.conclusion} ·{" "}
                {value.headSha.slice(0, 7)} · attempt {value.runAttempt}
              </option>
            ))}
        </select>
      </label>
      <ActionsError
        error={runs.error || previous.error}
        retry={() => {
          runs.refresh();
          previous.refresh();
        }}
        loading={runs.loading || previous.loading}
      />
      {previous.data && (
        <>
          <p>
            <code>{previous.data.run.headSha.slice(0, 7)}</code> (
            {previous.data.run.conclusion}) →{" "}
            <code>{run.headSha.slice(0, 7)}</code> (
            {run.conclusion || run.status}).{" "}
            {previous.data.run.headSha !== run.headSha
              ? "These runs have different source commits."
              : ""}
          </p>
          {comparison.some((group) => group.ambiguous) && (
            <p className="text-muted">
              Repeated job names are grouped. Duration changes are shown only
              when a name identifies one job in each run.
            </p>
          )}
          <table>
            <thead>
              <tr>
                <th>Job</th>
                <th>Baseline</th>
                <th>Current</th>
                <th>Duration change</th>
                <th>Failed steps before → after</th>
              </tr>
            </thead>
            <tbody>
              {comparison.map(
                ({ name, before, after, ambiguous, durationChange }) => {
                  return (
                    <tr key={name}>
                      <td>
                        {name}
                        {ambiguous
                          ? ` (${before.length} → ${after.length} jobs)`
                          : ""}
                      </td>
                      <td>{statuses(before, ambiguous)}</td>
                      <td>{statuses(after, ambiguous)}</td>
                      <td>
                        {durationChange !== undefined
                          ? `${durationChange >= 0 ? "+" : ""}${durationChange.toFixed(0)}s`
                          : "—"}
                      </td>
                      <td>
                        {failures(before, ambiguous)} →{" "}
                        {failures(after, ambiguous)}
                      </td>
                    </tr>
                  );
                },
              )}
            </tbody>
          </table>
        </>
      )}
    </div>
  );
}
