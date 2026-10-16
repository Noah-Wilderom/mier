import { useEffect, useMemo, useRef, useState } from "react";
import type { RepositoryIdentity } from "./services";
import { services } from "./services";
import type {
  Workflow,
  WorkflowAction,
  WorkflowJob,
  WorkflowRun,
} from "./actionsTypes";
import { useActionsResource } from "./useActionsResource";
import { actionStatus, jobLogSections } from "./actionsLog";
import { Icon } from "./Icon";
import Modal from "./Modal";
import { openWebURL } from "./native";
import "./Actions.css";
import PredictionPanel from "./PredictionPanel";

export function ActionsStatus({
  status,
  conclusion,
}: {
  status: string;
  conclusion?: string;
}) {
  const value = actionStatus(status, conclusion);
  return (
    <span className={`actions-status ${value.color}`}>
      <Icon name={value.icon} size={16} />
      {value.label}
    </span>
  );
}
export function ActionsLink({
  url,
  label = "Open on GitHub",
}: {
  url?: string;
  label?: string;
}) {
  const [error, setError] = useState("");
  if (!url) return null;
  return (
    <>
      <button
        className="btn btn-small"
        type="button"
        onClick={() => {
          setError("");
          void openWebURL(url).catch((error) => setError(String(error)));
        }}
      >
        <Icon name="external" size={14} />
        {label}
      </button>
      {error && (
        <span role="alert" className="text-red">
          {error}
        </span>
      )}
    </>
  );
}
export function ActionsError({
  error,
  retry,
  loading,
}: {
  error: string;
  retry: () => void;
  loading: boolean;
}) {
  return error ? (
    <div className="actions-error" role="alert">
      <Icon name="alert-circle" />
      <span>{error}</span>
      <button
        type="button"
        className="btn btn-small"
        onClick={retry}
        disabled={loading}
      >
        Retry
      </button>
    </div>
  ) : null;
}

export function JobLogs({
  repository,
  job,
}: {
  repository: RepositoryIdentity;
  job: WorkflowJob;
}) {
  const log = useActionsResource(
    `log:${job.id}:${job.status}`,
    (signal) => services.actions.log(repository, job.id, signal),
    (data) =>
      data?.state === "pending" || (!data && job.status !== "completed")
        ? 15000
        : 0,
  );
  const [query, setQuery] = useState("");
  const sections = useMemo(
    () => jobLogSections(log.data?.text || ""),
    [log.data?.text],
  );
  const visible = useMemo(
    () =>
      sections.filter(
        (section) =>
          !query ||
          (section.title + "\n" + section.text)
            .toLowerCase()
            .includes(query.toLowerCase()),
      ),
    [sections, query],
  );
  return (
    <section className="actions-logs" aria-label={`Logs for ${job.name}`}>
      <div className="actions-log-toolbar">
        <strong>Job logs</strong>
        <label className="actions-log-search">
          <Icon name="search" size={14} />
          <input
            type="search"
            aria-label="Search job logs"
            placeholder="Search logs…"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </label>
        <button
          className="btn btn-small"
          disabled={log.loading}
          onClick={log.refresh}
        >
          <Icon name="refresh" size={14} />
          {log.loading ? "Loading logs…" : "Refresh logs"}
        </button>
        <ActionsLink url={job.url} label="View job on GitHub" />
      </div>
      {log.data?.state==="available"&&job.conclusion!=="success"&&<PredictionPanel label="Assess job failure with Jev" request={{kind:"ci",scope:repository,jobId:job.id}}/>}
      <ActionsError
        error={log.error}
        retry={log.refresh}
        loading={log.loading}
      />
      {!log.data && log.loading && (
        <p className="actions-empty" role="status">
          Loading job logs…
        </p>
      )}
      {log.data?.message && (
        <p className="actions-log-message" role="status">
          {log.data.message}
        </p>
      )}
      {log.data?.truncated && (
        <p className="actions-log-message">
          {log.data.tail
            ? "Showing the last 2 MiB of this log, including the final result."
            : "This log exceeds the download limit. Showing part of the log; open it on GitHub for the full output."}
        </p>
      )}
      {log.data?.state === "available" &&
        (!sections.length ? (
          <p className="actions-empty">This job produced no output.</p>
        ) : !visible.length ? (
          <p className="actions-empty">No log sections match “{query}”.</p>
        ) : (
          <div className="actions-log-output">
            {visible.map((section, index) => (
              <details
                className={`actions-log-section ${section.error ? "has-error" : ""}`}
                key={`${index}:${section.title}`}
                open={!!query || section.error || sections.length < 12}
              >
                <summary>
                  {section.error && <Icon name="alert-circle" size={14} />}
                  <span>{section.title}</span>
                </summary>
                <pre>{section.text}</pre>
              </details>
            ))}
          </div>
        ))}
    </section>
  );
}

const actionLabels: Record<WorkflowAction, string> = {
  rerun: "Rerun all jobs",
  "rerun-failed": "Rerun failed jobs",
  "rerun-job": "Rerun job",
  cancel: "Cancel run",
};
function RunControls({
  repository,
  run,
  job,
  latest,
  onChange,
}: {
  repository: RepositoryIdentity;
  run: WorkflowRun;
  job?: WorkflowJob;
  latest: boolean;
  onChange: () => void;
}) {
  const [confirmation, setConfirmation] = useState<WorkflowAction>();
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const mutate = async () => {
    if (!confirmation || busy) return;
    setBusy(true);
    setError("");
    try {
      await services.actions.mutate(repository, {
        action: confirmation,
        runId: run.id,
        expectedAttempt: run.runAttempt,
        jobId: job?.id || 0,
      });
      setConfirmation(undefined);
      onChange();
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  };
  if (!latest)
    return <span className="text-muted">Viewing an earlier attempt.</span>;
  const allowed: WorkflowAction[] =
    run.status !== "completed"
      ? ["cancel"]
      : [
          "rerun",
          ...(["failure", "timed_out", "cancelled", "action_required"].includes(
            run.conclusion,
          )
            ? ["rerun-failed" as const]
            : []),
          ...(job ? ["rerun-job" as const] : []),
        ];
  return (
    <>
      <div className="actions-run-controls">
        {allowed.map((action) => (
          <button
            key={action}
            className="btn btn-small"
            onClick={() => {
              setError("");
              setConfirmation(action);
            }}
          >
            {actionLabels[action]}
          </button>
        ))}
      </div>
      {confirmation && (
        <Modal
          title={actionLabels[confirmation]}
          onClose={() => {
            if (!busy) setConfirmation(undefined);
          }}
        >
          <div className="actions-confirm">
            <p>
              {actionLabels[confirmation]} for{" "}
              <strong>
                {run.name} #{run.runNumber}
              </strong>
              , attempt {run.runAttempt}
              {confirmation === "rerun-job" ? ` · ${job?.name}` : ""}?
            </p>
            <p className="text-muted">
              {repository.owner}/{repository.repo} · {run.headBranch} ·{" "}
              <code>{run.headSha.slice(0, 7)}</code>
            </p>
            <p>
              {confirmation === "cancel"
                ? "Running jobs will be asked to stop."
                : "GitHub reruns the original commit and workflow. Repository Actions write access is required."}
            </p>
            <ActionsError error={error} retry={mutate} loading={busy} />
            <div className="actions-confirm-buttons">
              <button
                className="btn"
                disabled={busy}
                onClick={() => setConfirmation(undefined)}
              >
                Back
              </button>
              <button
                className={`btn ${confirmation === "cancel" ? "btn-danger" : "btn-primary"}`}
                disabled={busy}
                onClick={() => void mutate()}
              >
                {busy ? "Working…" : actionLabels[confirmation]}
              </button>
            </div>
          </div>
        </Modal>
      )}
    </>
  );
}

function RunDetails({
  repository,
  selected,
  onRefresh,
}: {
  repository: RepositoryIdentity;
  selected: WorkflowRun;
  onRefresh: () => void;
}) {
  const [attempt, setAttempt] = useState(0),
    [selectedJob, setSelectedJob] = useState<number>();
  const latestSeen = useRef(Math.max(selected.runAttempt, 1));
  const detail = useActionsResource(
    `run:${selected.id}:${attempt}:${attempt === 0 ? selected.runAttempt : ""}`,
    (signal) => services.actions.run(repository, selected.id, attempt, signal),
    () => 15000,
  );
  const run = detail.data?.run || selected;
  const jobs = detail.data?.jobs || [];
  const job =
    jobs.find((job) => job.id === selectedJob) ||
    jobs.find((job) => job.conclusion === "failure") ||
    jobs[0];
  latestSeen.current = Math.max(
    latestSeen.current,
    selected.runAttempt,
    attempt === 0 ? run.runAttempt : 0,
  );
  const latestAttempt = latestSeen.current;
  return (
    <section
      className="actions-run-detail"
      aria-label={`Workflow run ${run.runNumber}`}
    >
      <header className="actions-run-heading">
        <div>
          <span className="actions-eyebrow">
            {run.name} #{run.runNumber}
          </span>
          <h2>{run.displayTitle || run.name}</h2>
          <div className="actions-run-meta">
            <ActionsStatus status={run.status} conclusion={run.conclusion} />
            <span>
              <Icon name="branch" size={14} />
              {run.headBranch || "Detached commit"}
            </span>
            <code title={run.headSha}>{run.headSha.slice(0, 7)}</code>
            <span>{run.event.replace(/_/g, " ")}</span>
            <span>{run.actor?.login}</span>
          </div>
        </div>
        <ActionsLink url={run.url} />
      </header>
      <div className="actions-run-toolbar">
        <label>
          Attempt{" "}
          <select
            aria-label="Run attempt"
            value={attempt}
            onChange={(event) => {
              setAttempt(Number(event.target.value));
              setSelectedJob(undefined);
            }}
          >
            <option value={0}>Latest (attempt {latestAttempt})</option>
            {Array.from(
              { length: latestAttempt },
              (_, index) => latestAttempt - index,
            ).map((value) => (
              <option key={value} value={value}>
                Attempt {value}
              </option>
            ))}
          </select>
        </label>
        <button
          className="btn btn-small"
          disabled={detail.loading}
          onClick={detail.refresh}
        >
          <Icon name="refresh" size={14} />
          Refresh run
        </button>
        <RunControls
          repository={repository}
          run={run}
          job={job}
          latest={attempt === 0 || attempt === latestAttempt}
          onChange={() => {
            setAttempt(0);
            setSelectedJob(undefined);
            detail.refresh();
            onRefresh();
          }}
        />
      </div>
      <ActionsError
        error={detail.error}
        retry={detail.refresh}
        loading={detail.loading}
      />
      {!detail.data && detail.loading && (
        <p className="actions-empty" role="status">
          Loading jobs…
        </p>
      )}
      {detail.data && !jobs.length && (
        <p className="actions-empty">
          No jobs have started. This run may be queued, waiting for approval, or
          have failed before starting.
        </p>
      )}
      {!!jobs.length && (
        <div className="actions-job-layout">
          <nav className="actions-jobs" aria-label="Workflow jobs">
            {jobs.map((value) => (
              <button
                key={value.id}
                aria-pressed={job?.id === value.id}
                onClick={() => setSelectedJob(value.id)}
              >
                <strong>{value.name}</strong>
                <ActionsStatus
                  status={value.status}
                  conclusion={value.conclusion}
                />
                {value.runnerName && (
                  <span className="text-muted">{value.runnerName}</span>
                )}
              </button>
            ))}
          </nav>
          {job && (
            <div className="actions-job-detail">
              <h3>{job.name}</h3>
              <ol className="actions-steps" aria-label="Job steps">
                {(job.steps || []).map((step) => (
                  <li key={step.number}>
                    <span>{step.name}</span>
                    <ActionsStatus
                      status={step.status}
                      conclusion={step.conclusion}
                    />
                  </li>
                ))}
              </ol>
              <JobLogs key={job.id} repository={repository} job={job} />
            </div>
          )}
        </div>
      )}
    </section>
  );
}

function RepositoryActions({ repository }: { repository: RepositoryIdentity }) {
  const [workflowId, setWorkflowId] = useState(0),
    [status, setStatus] = useState(""),
    [branch, setBranch] = useState(""),
    [branchInput, setBranchInput] = useState(""),
    [page, setPage] = useState(1);
  const [selected, setSelected] = useState<WorkflowRun>();
  const [dispatch, setDispatch] = useState<Workflow>(),
    [message, setMessage] = useState("");
  const workflows = useActionsResource("workflows", (signal) =>
    services.actions.workflows(repository, signal),
  );
  const request = { workflowId, status, branch, page };
  const runs = useActionsResource(
    JSON.stringify(request),
    (signal) => services.actions.runs(repository, request, signal),
    () => 15000,
  );
  const items = runs.data?.items || [];
  const activeRun = items.find((run) => run.id === selected?.id) || selected;
  const workflow = workflows.data?.find((value) => value.id === workflowId);
  const reset = () => {
    setPage(1);
    setSelected(undefined);
  };
  const refresh = () => {
    runs.refresh();
    workflows.refresh();
  };
  return (
    <div className="actions-workspace">
      <header className="actions-page-heading">
        <div>
          <h1>Actions</h1>
          <p>
            {repository.owner}/{repository.repo}
          </p>
        </div>
        <div className="actions-run-controls">
          <button
            className="btn"
            disabled={!workflow || workflow.state !== "active"}
            title={
              workflow
                ? "Run this workflow manually"
                : "Select a workflow first"
            }
            onClick={() => {
              setMessage("");
              setDispatch(workflow);
            }}
          >
            <Icon name="play" size={16} />
            Run workflow
          </button>
          <button className="btn" disabled={runs.loading} onClick={refresh}>
            <Icon name="refresh" size={16} />
            Refresh Actions
          </button>
        </div>
      </header>
      {dispatch && (
        <DispatchWorkflow
          repository={repository}
          workflow={dispatch}
          onClose={() => setDispatch(undefined)}
          onDispatched={() => {
            setDispatch(undefined);
            setMessage(
              "Workflow requested. Its run will appear once GitHub queues it.",
            );
            setPage(1);
            runs.refresh();
          }}
        />
      )}
      {message && (
        <p className="actions-log-message" role="status">
          {message}
        </p>
      )}
      <form
        className="actions-filters"
        onSubmit={(event) => {
          event.preventDefault();
          setBranch(branchInput.trim());
          reset();
        }}
      >
        <label>
          Workflow
          <select
            aria-label="Workflow"
            value={workflowId}
            onChange={(event) => {
              setWorkflowId(Number(event.target.value));
              reset();
            }}
          >
            <option value={0}>All workflows</option>
            {(workflows.data || []).map((workflow) => (
              <option value={workflow.id} key={workflow.id}>
                {workflow.name}
                {workflow.state !== "active" ? " (disabled)" : ""}
              </option>
            ))}
          </select>
        </label>
        <label>
          Status
          <select
            aria-label="Run status"
            value={status}
            onChange={(event) => {
              setStatus(event.target.value);
              reset();
            }}
          >
            <option value="">All statuses</option>
            <option value="in_progress">Running</option>
            <option value="queued">Queued</option>
            <option value="waiting">Waiting</option>
            <option value="success">Passed</option>
            <option value="failure">Failed</option>
            <option value="cancelled">Cancelled</option>
          </select>
        </label>
        <label>
          Branch
          <input
            aria-label="Filter runs by branch"
            value={branchInput}
            onChange={(event) => setBranchInput(event.target.value)}
            placeholder="All branches"
          />
        </label>
        <button className="btn" type="submit">
          Filter runs
        </button>
      </form>
      <ActionsError
        error={workflows.error}
        retry={workflows.refresh}
        loading={workflows.loading}
      />
      <ActionsError
        error={runs.error}
        retry={runs.refresh}
        loading={runs.loading}
      />
      <div className="actions-browser">
        <section className="actions-run-list" aria-label="Workflow runs">
          <div className="actions-list-heading">
            <strong>Workflow runs</strong>
            <span className="text-muted">
              {runs.data
                ? `${runs.data.totalCount} ${runs.data.totalCount === 1 ? "result" : "results"}`
                : "Loading…"}
            </span>
          </div>
          {!runs.data && runs.loading && (
            <p className="actions-empty" role="status">
              Loading workflow runs…
            </p>
          )}
          {runs.data && !items.length && (
            <p className="actions-empty">
              No workflow runs match these filters.
            </p>
          )}
          {items.map((run) => (
            <button
              className="actions-run-card"
              key={run.id}
              aria-pressed={selected?.id === run.id}
              onClick={() => setSelected(run)}
            >
              <span className="actions-run-card-title">
                {run.displayTitle || run.name}
              </span>
              <span>
                {run.name} #{run.runNumber} · Attempt {run.runAttempt}
              </span>
              <ActionsStatus status={run.status} conclusion={run.conclusion} />
              <span className="actions-run-card-ref">
                <Icon name="branch" size={13} />
                {run.headBranch} · {run.headSha.slice(0, 7)}
              </span>
              <time dateTime={run.createdAt}>
                {new Date(run.createdAt).toLocaleString()}
              </time>
            </button>
          ))}
          <div className="actions-pagination">
            <button
              className="btn btn-small"
              disabled={page === 1 || runs.loading}
              onClick={() => setPage((value) => value - 1)}
            >
              Previous
            </button>
            <span>Page {page}</span>
            <button
              className="btn btn-small"
              disabled={!runs.data?.hasMore || runs.loading}
              onClick={() => setPage((value) => value + 1)}
            >
              Next
            </button>
          </div>
        </section>
        {activeRun ? (
          <RunDetails
            key={activeRun.id}
            repository={repository}
            selected={activeRun}
            onRefresh={runs.refresh}
          />
        ) : (
          <section className="actions-select-run">
            <Icon name="play" size={30} />
            <h2>Select a workflow run</h2>
            <p>Inspect jobs, steps and logs, or rerun and cancel workflows.</p>
          </section>
        )}
      </div>
      <p className="actions-footnote">
        Runs and steps refresh every 15 seconds while this view is visible.
        Downloadable logs become available when each job completes.
      </p>
    </div>
  );
}

function DispatchWorkflow({
  repository,
  workflow,
  onClose,
  onDispatched,
}: {
  repository: RepositoryIdentity;
  workflow: Workflow;
  onClose: () => void;
  onDispatched: () => void;
}) {
  const [reference, setReference] = useState(""),
    [schemaRef, setSchemaRef] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const config = useActionsResource(
    `dispatch:${workflow.id}:${schemaRef}`,
    (signal) =>
      services.actions.dispatchConfig(
        repository,
        workflow.id,
        schemaRef,
        signal,
      ),
  );
  useEffect(() => {
    if (!config.data) return;
    setReference(config.data.ref);
    setValues(
      Object.fromEntries(
        (config.data.inputs || []).map((input) => [
          input.name,
          input.default || (input.type === "boolean" ? "false" : ""),
        ]),
      ),
    );
  }, [config.data]);
  const submit = async () => {
    if (busy || !config.data?.enabled || reference !== config.data.ref) return;
    setBusy(true);
    setError("");
    try {
      await services.actions.dispatch(repository, {
        workflowId: workflow.id,
        ref: reference,
        inputs: values,
      });
      onDispatched();
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal
      title={`Run ${workflow.name}`}
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <form
        className="actions-dispatch-form"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <p>
          {repository.owner}/{repository.repo} · Actions write access is
          required.
        </p>
        <label>
          Branch or tag
          <input
            required
            aria-label="Workflow branch or tag"
            value={reference}
            disabled={busy}
            onChange={(event) => setReference(event.target.value)}
          />
        </label>
        <button
          className="btn btn-small"
          type="button"
          disabled={busy || config.loading || !reference.trim()}
          onClick={() => {
            setError("");
            if (schemaRef === reference.trim()) config.refresh();
            else setSchemaRef(reference.trim());
          }}
        >
          Load inputs for this ref
        </button>
        <ActionsError
          error={config.error}
          retry={config.refresh}
          loading={config.loading}
        />
        {config.loading && <p role="status">Reading workflow inputs…</p>}
        {config.data && !config.data.enabled && (
          <p>
            This workflow does not support manual runs on this ref. It needs an
            active <code>workflow_dispatch</code> trigger.
          </p>
        )}
        {(config.data?.inputs || []).map((input) => (
          <label key={input.name}>
            {input.name}
            {input.required ? " *" : ""}
            {input.type === "boolean" ? (
              <select
                aria-label={input.name}
                disabled={busy}
                value={values[input.name] || "false"}
                onChange={(event) =>
                  setValues((previous) => ({
                    ...previous,
                    [input.name]: event.target.value,
                  }))
                }
              >
                <option value="false">False</option>
                <option value="true">True</option>
              </select>
            ) : input.type === "choice" ? (
              <select
                aria-label={input.name}
                disabled={busy}
                required={input.required}
                value={values[input.name] || ""}
                onChange={(event) =>
                  setValues((previous) => ({
                    ...previous,
                    [input.name]: event.target.value,
                  }))
                }
              >
                <option value="">Choose…</option>
                {(input.options || []).map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            ) : (
              <input
                aria-label={input.name}
                disabled={busy}
                required={input.required}
                type={input.type === "number" ? "number" : "text"}
                step={input.type === "number" ? "any" : undefined}
                value={values[input.name] || ""}
                placeholder={
                  input.type === "environment" ? "Environment name" : ""
                }
                onChange={(event) =>
                  setValues((previous) => ({
                    ...previous,
                    [input.name]: event.target.value,
                  }))
                }
              />
            )}
            {input.description && (
              <span className="text-muted">{input.description}</span>
            )}
          </label>
        ))}
        {config.data && reference !== config.data.ref && (
          <p role="status">
            Load inputs for the selected branch or tag before running it.
          </p>
        )}
        <ActionsError error={error} retry={submit} loading={busy} />
        <div className="actions-confirm-buttons">
          <button
            className="btn"
            type="button"
            disabled={busy}
            onClick={onClose}
          >
            Back
          </button>
          <button
            className="btn btn-primary"
            disabled={
              busy ||
              config.loading ||
              !!config.error ||
              !config.data?.enabled ||
              reference !== config.data.ref
            }
          >
            {busy ? "Requesting…" : "Run workflow"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

export default function ActionsPage({
  repository,
  account,
}: {
  repository?: RepositoryIdentity;
  account?: string;
}) {
  if (!repository?.owner || !repository.repo)
    return (
      <section className="actions-empty actions-workspace">
        <Icon name="github" size={32} />
        <h2>Open a GitHub repository to view Actions</h2>
        <p>Workflow runs use the repository’s GitHub account.</p>
      </section>
    );
  return (
    <RepositoryActions
      key={`${repository.host}:${repository.owner}:${repository.repo}:${account}`}
      repository={repository}
    />
  );
}
