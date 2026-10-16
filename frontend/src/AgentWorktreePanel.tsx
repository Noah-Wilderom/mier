import { useCallback, useEffect, useRef, useState } from "react";
import AIActionPanel from "./AIActionPanel";
import type { AIWorktreeInfo } from "./aiActionTypes";
import { services, type RepositorySnapshot } from "./services";

export default function AgentWorktreePanel({
  repository,
  locked,
  onOpen,
}: {
  repository: RepositorySnapshot;
  locked: boolean;
  onOpen?: (path: string) => void;
}) {
  const [items, setItems] = useState<AIWorktreeInfo[]>([]);
  const [selected, setSelected] = useState("");
  const [clientId, setClientId] = useState(() => crypto.randomUUID());
  const [reload, setReload] = useState(0);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [remove, setRemove] = useState<AIWorktreeInfo>();
  const guard = useRef(false);
  const current = items.find((item) => item.id === selected);
  const refresh = useCallback(() => setReload((n) => n + 1), []);
  useEffect(() => {
    let canceled = false;
    setLoading(true);
    void services.ai
      .worktrees(repository.root)
      .then((next) => {
        if (!canceled) {
          setItems(next || []);
          setError("");
        }
      })
      .catch((issue) => {
        if (!canceled)
          setError(issue instanceof Error ? issue.message : String(issue));
      })
      .finally(() => {
        if (!canceled) setLoading(false);
      });
    return () => {
      canceled = true;
    };
  }, [repository.root, reload]);
  const removeWorktree = async () => {
    if (!remove || pending || locked || guard.current) return;
    guard.current = true;
    setPending(true);
    setError("");
    try {
      await services.ai.removeWorktree(remove.id);
      if (selected === remove.id) {
        setSelected("");
        setClientId(crypto.randomUUID());
      }
      setRemove(undefined);
      refresh();
    } catch (issue) {
      setError(issue instanceof Error ? issue.message : String(issue));
    } finally {
      guard.current = false;
      setPending(false);
    }
  };
  return (
    <div className="ai-agent-panel">
      <div className="git-tool-actions">
        <h3>Coding worktrees</h3>
        <button
          type="button"
          className="btn"
          disabled={loading || pending}
          onClick={refresh}
        >
          Refresh agents
        </button>
      </div>
      <p className="muted">
        Each task starts from a committed revision in a separate branch and
        directory. Your current checkout and its uncommitted changes stay here.
        Worktrees remain on disk when this window closes.
      </p>
      {!current && (!repository.hasHead || repository.operation) && (
        <p className="git-operation-error" role="status">
          {repository.operation
            ? "Finish the current Git operation before starting a coding task."
            : "Create an initial commit before starting a coding task."}
        </p>
      )}
      {error && (
        <p className="git-operation-error" role="alert">
          {error}
        </p>
      )}
      {remove && (
        <div className="repository-tool-confirm" role="alert">
          <p>
            Remove the clean worktree <strong>{remove.branch}</strong>? Its
            branch and commits remain in Git. Dirty worktrees must be reviewed
            and committed or cleaned first.
          </p>
          <code>{remove.path}</code>
          <div className="git-tool-actions">
            <button
              type="button"
              className="btn"
              disabled={pending}
              onClick={() => setRemove(undefined)}
            >
              Keep worktree
            </button>
            <button
              type="button"
              className="btn"
              disabled={pending || locked}
              onClick={() => void removeWorktree()}
            >
              {pending ? "Removing…" : "Remove clean worktree"}
            </button>
          </div>
        </div>
      )}
      <div className="ai-agent-layout">
        <nav className="ai-agent-list" aria-label="Coding worktrees">
          <button
            type="button"
            className={!selected ? "selected" : ""}
            disabled={pending}
            onClick={() => {
              setSelected("");
              setClientId(crypto.randomUUID());
            }}
          >
            <strong>New coding task</strong>
            <span>Create a separate worktree</span>
          </button>
          {loading && !items.length && <p role="status">Loading worktrees…</p>}
          {items.map((item) => (
            <button
              type="button"
              key={item.id}
              className={selected === item.id ? "selected" : ""}
              disabled={pending}
              onClick={() => setSelected(item.id)}
            >
              <strong>{item.branch}</strong>
              <span>{item.state}</span>
            </button>
          ))}
        </nav>
        <div className="ai-agent-task">
          <header>
            <strong>{current?.branch || "New coding task"}</strong>
            {current && (
              <>
                <button
                  type="button"
                  className="btn btn-ghost"
                  disabled={locked || pending || current.state === "running"}
                  onClick={() => onOpen?.(current.path)}
                >
                  Open worktree
                </button>
                <button
                  type="button"
                  className="btn btn-ghost"
                  disabled={locked || pending || current.state === "running"}
                  onClick={() => setRemove(current)}
                >
                  Remove…
                </button>
              </>
            )}
          </header>
          {selected && !current ? (
            <p className="muted">
              This worktree is no longer listed. Refresh the agents or start a
              new task.
            </p>
          ) : (
            <AIActionPanel
              key={current?.id || clientId}
              scope={{
                root: repository.root,
                kind: "code",
                expectedHead: repository.headSha,
                worktreeId: current?.id,
                clientId: current ? undefined : clientId,
              }}
              initialActionId={current?.lastActionId}
              disabled={
                locked ||
                pending ||
                (!current && (!repository.hasHead || !!repository.operation))
              }
              onWorktree={refresh}
              onOpenWorktree={onOpen}
            />
          )}
        </div>
      </div>
    </div>
  );
}
