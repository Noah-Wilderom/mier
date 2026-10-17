import { useEffect, useRef, useState } from "react";
import { services } from "./services";
import type { AIWorktreeInfo } from "./aiActionTypes";
import type { CommandRun, CustomCommand } from "./repositoryToolTypes";
import { useActionsResource } from "./useActionsResource";
export interface CheckProposal extends Omit<CustomCommand, "id"> {
  id?: string;
  reason?: string;
}
export interface WorktreeCheck {
  id: string;
  worktreeId: string;
  actionId: string;
  command: CustomCommand;
  run: CommandRun;
  sourceStamp: string;
  afterStamp: string;
  stale: boolean;
  error: string;
}
export interface CheckApproval {
  id: string;
  worktree: AIWorktreeInfo;
  command: CustomCommand;
  sourceStamp: string;
  expiresAt: string;
}
export default function WorktreeVerification({
  worktree,
  proposals = [],
  disabled,
  version,
}: {
  worktree: AIWorktreeInfo;
  proposals?: CheckProposal[];
  disabled: boolean;
  version: number;
}) {
  const [commands, setCommands] = useState<CustomCommand[]>([]),
    [selected, setSelected] = useState("manual"),
    [executable, setExecutable] = useState(""),
    [argumentsText, setArguments] = useState(""),
    [timeout, setTimeoutSeconds] = useState(300),
    [approval, setApproval] = useState<CheckApproval>(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const guard = useRef(false);
  const checks = useActionsResource(
    `worktree-checks:${worktree.id}:${version}`,
    (signal) => services.ai.worktreeChecks(worktree.id, signal),
    (data) => (data?.some((check) => check.run.status === "running") ? 800 : 0),
  );
  useEffect(() => {
    let canceled = false;
    void services.commands
      .list()
      .then((value) => {
        if (!canceled) setCommands(value);
      })
      .catch((issue) => {
        if (!canceled) setError(String(issue));
      });
    return () => {
      canceled = true;
    };
  }, []);
  const running =
    checks.data?.some((check) => check.run.status === "running") ?? false;
  const options = [
    ...proposals.map((command, i) => ({
      id: `proposal:${i}`,
      command: { ...command, id: command.id ?? "" },
    })),
    ...commands.map((command) => ({ id: `saved:${command.id}`, command })),
  ];
  const definition = options.find((option) => option.id === selected)
    ?.command ?? {
    id: "",
    name: "Worktree check",
    executable: executable.trim(),
    arguments: argumentsText ? argumentsText.split("\n") : [],
    timeoutSeconds: timeout,
  };
  const prepare = async () => {
    if (guard.current) return;
    guard.current = true;
    setBusy(true);
    setError("");
    try {
      setApproval(
        await services.ai.prepareWorktreeCheck(worktree.id, definition),
      );
    } catch (issue) {
      setError(String(issue));
    } finally {
      guard.current = false;
      setBusy(false);
    }
  };
  const start = async () => {
    if (guard.current || !approval) return;
    guard.current = true;
    setBusy(true);
    setError("");
    try {
      await services.ai.startWorktreeCheck(approval.id);
      setApproval(undefined);
      checks.refresh();
    } catch (issue) {
      setError(String(issue));
      setApproval(undefined);
    } finally {
      guard.current = false;
      setBusy(false);
    }
  };
  return (
    <section
      className="ai-worktree-verification"
      aria-label="Worktree verification"
    >
      <header>
        <strong>Verify this worktree</strong>
        <button
          className="btn btn-ghost"
          disabled={checks.loading}
          onClick={checks.refresh}
        >
          Refresh results
        </button>
      </header>
      <p>
        Choose a saved command, an AI-proposed check, or enter a command. Review
        it before execution.
      </p>
      {(error || checks.error) && (
        <p role="alert" className="text-red">
          {error || checks.error}
        </p>
      )}
      <label>
        Check
        <select
          aria-label="Worktree check"
          disabled={busy || running || disabled}
          value={selected}
          onChange={(e) => {
            setSelected(e.target.value);
            setApproval(undefined);
          }}
        >
          <option value="manual">Custom check…</option>
          {options.map((option) => (
            <option value={option.id} key={option.id}>
              {option.id.startsWith("proposal:") ? "AI proposal · " : ""}
              {option.command.name}
            </option>
          ))}
        </select>
      </label>
      {selected === "manual" && (
        <div className="ai-check-command">
          <label>
            Executable
            <input
              aria-label="Check executable"
              placeholder="go, npm, cargo, or full executable path"
              disabled={busy || running || disabled}
              value={executable}
              onChange={(e) => {
                setExecutable(e.target.value);
                setApproval(undefined);
              }}
            />
          </label>
          <label>
            Arguments · one per line
            <textarea
              aria-label="Check arguments"
              rows={3}
              placeholder={"test\n./..."}
              disabled={busy || running || disabled}
              value={argumentsText}
              onChange={(e) => {
                setArguments(e.target.value);
                setApproval(undefined);
              }}
            />
          </label>
          <label>
            Timeout (seconds)
            <input
              aria-label="Check timeout"
              type="number"
              min={1}
              max={900}
              value={timeout}
              disabled={busy || running || disabled}
              onChange={(e) => {
                setTimeoutSeconds(Number(e.target.value));
                setApproval(undefined);
              }}
            />
          </label>
        </div>
      )}
      {"reason" in definition && !!definition.reason && (
        <p>{String(definition.reason)}</p>
      )}
      <button
        type="button"
        className="btn"
        disabled={
          busy || running || disabled || !definition.executable || !!approval
        }
        onClick={() => void prepare()}
      >
        {busy ? "Preparing check…" : "Review command"}
      </button>
      {approval && (
        <div
          className="ai-check-approval"
          role="group"
          aria-label="Approve worktree check"
        >
          <strong>{approval.command.name}</strong>
          <dl>
            <dt>Executable</dt>
            <dd>
              <code>{approval.command.executable}</code>
            </dd>
            <dt>Arguments</dt>
            <dd>
              <code>{JSON.stringify(approval.command.arguments)}</code>
            </dd>
            <dt>Directory</dt>
            <dd>
              <code>{approval.worktree.path}</code>
            </dd>
            <dt>Branch</dt>
            <dd>
              {approval.worktree.branch} ·{" "}
              {approval.worktree.headSha.slice(0, 8)}
            </dd>
            <dt>Timeout</dt>
            <dd>{approval.command.timeoutSeconds}s</dd>
          </dl>
          <p>
            Runs with your normal OS permissions. Project scripts can execute
            code. Approval expires in 10 minutes and is invalidated by source
            changes.
          </p>
          <button
            className="btn"
            disabled={busy}
            onClick={() => setApproval(undefined)}
          >
            Cancel approval
          </button>
          <button
            className="btn btn-primary"
            disabled={busy || disabled}
            onClick={() => void start()}
          >
            Approve and run check
          </button>
        </div>
      )}
      <div className="ai-check-results">
        {checks.data?.map((check) => (
          <article key={check.id}>
            <div>
              <strong>{check.command.name}</strong>
              <span
                className={
                  check.run.status === "completed" &&
                  check.run.exitCode === 0 &&
                  !check.stale
                    ? "text-green"
                    : "text-red"
                }
              >
                {check.run.status === "running"
                  ? "Running…"
                  : check.stale
                    ? "Source changed · recheck required"
                    : check.run.status === "completed" &&
                        check.run.exitCode === 0
                      ? "Passed"
                      : `${check.run.status} · exit ${check.run.exitCode}`}
              </span>
              {check.run.status === "running" && (
                <button
                  className="btn"
                  onClick={() =>
                    void services.ai
                      .cancelWorktreeCheck(check.id)
                      .then(checks.refresh)
                      .catch((issue) => setError(String(issue)))
                  }
                >
                  Cancel check
                </button>
              )}
            </div>
            <time>{new Date(check.run.startedAt).toLocaleString()}</time>
            {(check.error || check.run.error) && (
              <p role="alert">{check.error || check.run.error}</p>
            )}
            <details>
              <summary>
                Command and output
                {check.run.truncated
                  ? " · Output truncated at 512 KiB per stream"
                  : ""}
              </summary>
              <code>
                {check.command.executable}{" "}
                {JSON.stringify(check.command.arguments)}
              </code>
              <h4>Standard output</h4>
              <pre>{check.run.stdout || "No output yet."}</pre>
              <h4>Standard error</h4>
              <pre>{check.run.stderr || "No standard error."}</pre>
            </details>
          </article>
        ))}
      </div>
    </section>
  );
}
