import { useEffect, useRef, useState } from "react";
import { services, type RepositorySnapshot } from "./services";
import { loadWorkspaceState, saveWorkspaceState } from "./workspaceState";
import { Icon } from "./Icon";
export interface ConflictForecastResult {
  head: string;
  target: string;
  base: string;
  clean: boolean;
  conflicts: string[];
  overlap: string[];
  rebase: { commit: string; clean: boolean; conflicts: string[] }[];
  rebaseComplete: boolean;
  note: string;
}
export default function ConflictForecast({
  repository,
}: {
  repository: RepositorySnapshot;
}) {
  const [settings, setSettings] = useState({ enabled: false, target: "" }),
    [ready, setReady] = useState(false),
    [open, setOpen] = useState(false),
    [result, setResult] = useState<ConflictForecastResult>(),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(false),
    [rebase, setRebase] = useState(false),
    [refresh, setRefresh] = useState(0);
  const epoch = useRef(0);
  const root = repository.root;
  const branches = (repository.branches ?? []).filter(
    (branch) => !branch.current && !!branch.commitSha,
  );
  const target = branches.find((branch) => branch.name === settings.target);
  const key = JSON.stringify([
    repository.headSha,
    repository.currentBranch,
    target?.commitSha,
    rebase,
    refresh,
  ]);
  useEffect(() => {
    let canceled = false;
    setReady(false);
    void loadWorkspaceState<typeof settings>(`forecast:${root}`)
      .then((value) => {
        if (!canceled) setSettings(value ?? { enabled: false, target: "" });
      })
      .catch((issue) => setError(String(issue)))
      .finally(() => {
        if (!canceled) setReady(true);
      });
    return () => {
      canceled = true;
    };
  }, [root]);
  useEffect(() => {
    if (ready) saveWorkspaceState(`forecast:${root}`, settings);
  }, [root, settings, ready]);
  useEffect(() => {
    if (!ready || !settings.enabled || !target || !repository.hasHead) return;
    let canceled = false;
    const controller = new AbortController();
    const sequence = ++epoch.current;
    setLoading(true);
    setError("");
    setResult(undefined);
    // Small delay coalesces a fetch's several branch/ref notifications.
    const timer = setTimeout(
      () =>
        void services.repository
          .forecastConflict(
            root,
            {
              target: target.name,
              expectedTarget: target.commitSha,
              expectedHead: repository.headSha ?? "",
              expectedBranch: repository.currentBranch,
              rebase,
            },
            controller.signal,
          )
          .then((value) => {
            if (!canceled && sequence === epoch.current) setResult(value);
          })
          .catch((issue) => {
            if (!canceled) setError(String(issue));
          })
          .finally(() => {
            if (!canceled) setLoading(false);
          }),
      500,
    );
    return () => {
      canceled = true;
      clearTimeout(timer);
      controller.abort();
    };
  }, [root, key, settings.enabled, ready]);
  const current =
    result &&
    result.head === repository.headSha &&
    result.target === target?.commitSha
      ? result
      : undefined;
  return (
    <div className="conflict-forecast">
      <div className="conflict-forecast-bar">
        <button
          className="btn-ghost"
          onClick={() => setOpen((value) => !value)}
          aria-expanded={open}
        >
          <Icon name="git-branch" size={13} />
          {!settings.enabled
            ? "Conflict forecast"
            : loading
              ? "Checking branch conflicts…"
              : error
                ? "Conflict forecast unavailable"
                : current
                  ? current.clean
                    ? "Merge preview is clean"
                    : `${current.conflicts.length} potential conflict files`
                  : "Choose a target branch"}
        </button>
        {settings.enabled && target && (
          <span className="muted">
            {repository.currentBranch} → {target.name} · local refs
          </span>
        )}
      </div>
      {open && (
        <section aria-label="Proactive conflict detection">
          <div className="conflict-forecast-controls">
            <label>
              <input
                type="checkbox"
                checked={settings.enabled}
                onChange={(e) =>
                  setSettings((value) => ({
                    ...value,
                    enabled: e.target.checked,
                  }))
                }
              />
              Check in background
            </label>
            <label>
              Target branch
              <select
                aria-label="Conflict forecast target"
                value={settings.target}
                onChange={(e) => {
                  setSettings((value) => ({
                    ...value,
                    target: e.target.value,
                  }));
                  setRebase(false);
                }}
              >
                <option value="">Select branch…</option>
                {branches.map((branch) => (
                  <option key={branch.name}>{branch.name}</option>
                ))}
              </select>
            </label>
            <button
              className="btn"
              disabled={!target || loading || !settings.enabled}
              onClick={() => setRefresh((value) => value + 1)}
            >
              Refresh forecast
            </button>
            <button
              className="btn"
              disabled={!target || loading || !settings.enabled}
              onClick={() => {
                setRebase(true);
                setRefresh((value) => value + 1);
              }}
            >
              Preview rebase
            </button>
          </div>
          <p className="muted">
            Checks committed changes against local branch refs. Fetch to include
            new remote commits. Working files remain untouched.
          </p>
          {error && (
            <p role="alert" className="text-red">
              {error}
            </p>
          )}
          {current && (
            <>
              <p>
                {current.clean
                  ? "The simulated merge completed without conflicts."
                  : "A merge is expected to stop for conflicts."}{" "}
                {current.overlap.length} files changed on both branches.
              </p>
              {!!current.conflicts.length && (
                <ul aria-label="Forecast conflict files">
                  {current.conflicts.map((path) => (
                    <li key={path}>
                      <code>{path}</code>
                    </li>
                  ))}
                </ul>
              )}
              <details>
                <summary>Overlapping files ({current.overlap.length})</summary>
                <ul>
                  {current.overlap.map((path) => (
                    <li key={path}>
                      <code>{path}</code>
                    </li>
                  ))}
                </ul>
              </details>
              {rebase && (
                <div>
                  <strong>
                    {current.rebaseComplete
                      ? "Rebase preview completed"
                      : "Rebase preview stopped at the first conflict"}
                  </strong>
                  <p>{current.note}</p>
                  <ol>
                    {current.rebase.map((step) => (
                      <li key={step.commit}>
                        <code>{step.commit.slice(0, 8)}</code> ·{" "}
                        {step.clean ? "clean" : step.conflicts.join(", ")}
                      </li>
                    ))}
                  </ol>
                </div>
              )}
            </>
          )}
        </section>
      )}
    </div>
  );
}
