import {
  lazy,
  Suspense,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import Modal from "./Modal";
import { Markdown } from "./Markdown";
import { AIModelLogo, AIModelPicker } from "./AIModelPicker";
import { AIReviewActivity } from "./AIReviewActivity";
import {
  aiIntensities,
  aiProviderName,
  aiReviewActive,
  type AIPreferences,
  type AIProvider,
  type AIProviderStatus,
} from "./aiTypes";
import {
  aiActionScopeKey,
  type AIActionKind,
  type AIActionProposal,
  type AIActionScope,
  type AIActionSnapshot,
} from "./aiActionTypes";
import { services } from "./services";
import "./AIReview.css";
import "./AIActions.css";

const AIReviewChat = lazy(async () => ({
  default: (await import("./AIReviewChat")).AIReviewChat,
}));
const retained = new Map<string, AIActionSnapshot>();
const preparing = new Set<string>();
const observers = new Set<() => void>();
const subscribe = (listener: () => void) => {
  observers.add(listener);
  return () => {
    observers.delete(listener);
  };
};
const changed = () => {
  for (const listener of observers) listener();
};
const instructions = new Map<string, string>();
const titles: Record<AIActionKind, string> = {
  commit: "Draft a commit message",
  pull_request: "Draft a pull request",
  explain: "Explain branch changes",
  conflict: "Propose a conflict resolution",
  code: "Coding worktree",
};
const prompts: Record<AIActionKind, string> = {
  commit: "Write a concise conventional commit message for the staged changes.",
  pull_request:
    "Write a clear pull request title and description for this branch comparison.",
  explain: "Explain the branch changes, their behavior, and areas to check.",
  conflict:
    "Propose a complete merged file that preserves the intended behavior of both versions.",
  code: "Inspect this worktree and summarize the changes you recommend.",
};
const descriptions: Record<AIActionKind, string> = {
  commit:
    "Uses staged changes and captured source. Choose Use commit draft when it is ready; then edit and commit normally.",
  pull_request:
    "Uses the committed branch comparison. Choose Use PR draft when it is ready; then edit and create the pull request normally.",
  explain:
    "Read an explanation of the committed changes since the common ancestor, grounded in the captured diff and source.",
  conflict:
    "Compare the proposal with your current result. Using it fills the result editor; Save result and stage remains a separate action.",
  code: "Describe a coding task. Edits stay in a separate worktree on its own branch. Open it to review, test, stage, and commit through the normal Git tools.",
};
const errorText = (issue: unknown) =>
  issue instanceof Error ? issue.message : String(issue);

type Props = {
  scope: AIActionScope;
  initialActionId?: string;
  disabled?: boolean;
  applyLabel?: string;
  onApply?: (proposal: AIActionProposal) => void | Promise<void>;
  onWorktree?: () => void;
  onOpenWorktree?: (path: string) => void;
  onOpenSettings?: () => void;
};
export function AIActionDialog(props: Props & { onClose: () => void }) {
  return (
    <Modal title={titles[props.scope.kind]} onClose={props.onClose}>
      <div className="ai-action-dialog">
        <AIActionPanel {...props} />
      </div>
    </Modal>
  );
}
export default function AIActionPanel(props: Props) {
  return <TaskPanel key={aiActionScopeKey(props.scope)} {...props} />;
}
function TaskPanel({
  scope,
  initialActionId,
  disabled = false,
  applyLabel,
  onApply,
  onWorktree,
  onOpenWorktree,
  onOpenSettings,
}: Props) {
  const key = aiActionScopeKey(scope);
  const task = useSyncExternalStore(
    subscribe,
    () => retained.get(key),
    () => undefined,
  );
  const starting = useSyncExternalStore(
    subscribe,
    () => preparing.has(key),
    () => false,
  );
  const [preferences, setPreferences] = useState<AIPreferences>({
    provider: "codex",
    model: "",
    codexPath: "",
    copilotPath: "",
    intensity: "medium",
  });
  const [status, setStatus] = useState<AIProviderStatus>();
  const [checking, setChecking] = useState(true);
  const [busy, setBusy] = useState(false);
  const [applying, setApplying] = useState(false);
  const [applied, setApplied] = useState("");
  const [error, setError] = useState("");
  const [pollError, setPollError] = useState("");
  const [retry, setRetry] = useState(0);
  const [ready, setReady] = useState(false);
  const mounted = useRef(true),
    guard = useRef(false);
  const active = aiReviewActive(task?.run);
  const callback = useRef(onWorktree);
  callback.current = onWorktree;
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const accept = (value: AIActionSnapshot) => {
    if (value.kind !== scope.kind || value.root !== scope.root)
      throw new Error(
        "This task belongs to another context. Start a new task.",
      );
    retained.set(key, value);
    while (retained.size > 40) retained.delete(retained.keys().next().value!);
    changed();
  };
  useEffect(() => {
    let canceled = false;
    void services.ai
      .preferences()
      .then((next) => {
        if (!canceled) {
          const run = retained.get(key)?.run;
          setPreferences(
            run && (run.provider === "codex" || run.provider === "copilot")
              ? {
                  ...next,
                  provider: run.provider,
                  model: run.model,
                  intensity: run.intensity || next.intensity || "medium",
                }
              : next,
          );
        }
      })
      .catch((issue) => {
        if (!canceled) setError(errorText(issue));
      })
      .finally(() => {
        if (!canceled) setReady(true);
      });
    return () => {
      canceled = true;
    };
  }, []);
  useEffect(() => {
    const run = task?.run;
    if (run && (run.provider === "codex" || run.provider === "copilot")) {
      const provider = run.provider;
      setPreferences((value) => ({
        ...value,
        provider,
        model: run.model,
        intensity: run.intensity || value.intensity || "medium",
      }));
    }
  }, [task?.run.id]);
  useEffect(() => {
    if (!ready) return;
    let canceled = false;
    setChecking(true);
    setStatus(undefined);
    void services.ai
      .status(preferences.provider)
      .then((value) => {
        if (!canceled) setStatus(value);
      })
      .catch((issue) => {
        if (!canceled) setError(errorText(issue));
      })
      .finally(() => {
        if (!canceled) setChecking(false);
      });
    return () => {
      canceled = true;
    };
  }, [ready, preferences.provider, retry]);
  useEffect(() => {
    if (!initialActionId || task) return;
    let canceled = false;
    void services.ai
      .getAction(initialActionId)
      .then((value) => {
        if (!canceled) accept(value);
      })
      .catch(() => {
        /* Worktree edits survive application restarts; transient chat results do not. */
      });
    return () => {
      canceled = true;
    };
  }, [initialActionId]);
  useEffect(() => {
    if (!task || !active) return;
    let canceled = false,
      timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const next = await services.ai.getAction(task.run.id);
        if (!canceled) {
          accept(next);
          setPollError("");
        }
      } catch (issue) {
        if (!canceled) setPollError(errorText(issue));
      }
      if (!canceled) timer = setTimeout(() => void poll(), 1000);
    };
    timer = setTimeout(() => void poll(), 500);
    return () => {
      canceled = true;
      clearTimeout(timer);
    };
  }, [task?.run.id, active, retry]);
  useEffect(() => {
    if (task?.worktree) callback.current?.();
  }, [task?.run.id, task?.run.state]);
  const start = async (focus: string) => {
    if (
      guard.current ||
      preparing.has(key) ||
      active ||
      disabled ||
      checking ||
      !status?.installed ||
      !status.authenticated
    )
      return false;
    if (scope.kind === "code" && !focus.trim()) {
      setError("Describe the coding task before starting.");
      return false;
    }
    guard.current = true;
    preparing.add(key);
    changed();
    setBusy(true);
    setError("");
    setApplied("");
    try {
      const next = await services.ai.startAction({
        ...scope,
        worktreeId: task?.worktree?.id || scope.worktreeId,
        provider: preferences.provider,
        model: preferences.model,
        intensity: preferences.intensity || "medium",
        instructions: focus,
      });
      accept(next);
      instructions.delete(key);
      return true;
    } catch (issue) {
      if (mounted.current) setError(errorText(issue));
      return false;
    } finally {
      guard.current = false;
      preparing.delete(key);
      changed();
      if (mounted.current) setBusy(false);
    }
  };
  const stop = async () => {
    if (!task || guard.current) return;
    guard.current = true;
    setBusy(true);
    setError("");
    try {
      await services.ai.cancelAction(task.run.id);
      accept(await services.ai.getAction(task.run.id));
    } catch (issue) {
      if (mounted.current) setError(errorText(issue));
    } finally {
      guard.current = false;
      if (mounted.current) setBusy(false);
    }
  };
  const apply = async () => {
    if (
      !task?.proposal ||
      task.run.state !== "completed" ||
      !onApply ||
      guard.current ||
      disabled
    )
      return;
    guard.current = true;
    setApplying(true);
    setError("");
    try {
      await services.ai.validateAction(task.run.id);
      if (mounted.current) {
        await onApply(task.proposal);
        if (mounted.current)
          setApplied("Applied to your draft. Review it before saving.");
      }
    } catch (issue) {
      if (mounted.current) setError(errorText(issue));
    } finally {
      guard.current = false;
      if (mounted.current) setApplying(false);
    }
  };
  const lock = busy || starting || active || applying;
  const proposal = task?.proposal;
  return (
    <section className="ai-action-panel" aria-label={titles[scope.kind]}>
      {(error || pollError) && (
        <div className="ai-review-error" role="alert">
          {error || pollError}
          {pollError && (
            <button
              type="button"
              className="btn"
              onClick={() => setRetry((n) => n + 1)}
            >
              Retry task status
            </button>
          )}
        </div>
      )}
      <Suspense fallback={<p role="status">Loading AI chat…</p>}>
        <AIReviewChat
          run={task?.run}
          active={active}
          busy={busy || starting}
          disabled={
            disabled ||
            applying ||
            checking ||
            !status?.installed ||
            !status.authenticated
          }
          initialDraft={instructions.get(key) || ""}
          onDraftChange={(value) => {
            if (value) instructions.set(key, value);
            else instructions.delete(key);
            while (instructions.size > 40)
              instructions.delete(instructions.keys().next().value!);
          }}
          onStart={start}
          onCancel={stop}
          copy={{
            prompt: prompts[scope.kind],
            label: "AI task chat",
            options: "Task options",
            welcome: titles[scope.kind],
            description: descriptions[scope.kind],
            focus:
              scope.kind === "code" ? "Coding task" : "Instructions (optional)",
            placeholder:
              scope.kind === "code"
                ? "Add input validation and describe what needs testing…"
                : "Style, focus, or extra context…",
            hint: "Enter to start · Shift+Enter for a new line",
            stop: "Stop task",
            preparing: "Preparing task…",
            start: "Start AI task",
            again:
              scope.kind === "code" ? "Run follow-up task" : "Generate again",
          }}
          controls={
            <>
              <div className="ai-review-selectors">
                <label>
                  Provider
                  <div className="ai-provider-select">
                    <AIModelLogo provider={preferences.provider} />
                    <select
                      aria-label="AI task provider"
                      value={preferences.provider}
                      disabled={lock}
                      onChange={(event) =>
                        setPreferences((value) => ({
                          ...value,
                          provider: event.target.value as AIProvider,
                          model: "",
                        }))
                      }
                    >
                      <option value="codex">ChatGPT · Codex</option>
                      <option value="copilot">GitHub Copilot</option>
                    </select>
                  </div>
                </label>
                <AIModelPicker
                  models={status?.models || []}
                  value={preferences.model}
                  provider={preferences.provider}
                  disabled={lock || checking}
                  label="AI task model"
                  onChange={(model) =>
                    setPreferences((value) => ({ ...value, model }))
                  }
                />
              </div>
              <fieldset className="ai-intensity" disabled={lock}>
                <legend>Task intensity</legend>
                <div>
                  {aiIntensities.map((option) => (
                    <label key={option.id}>
                      <input
                        type="radio"
                        name="task-intensity"
                        checked={
                          (preferences.intensity || "medium") === option.id
                        }
                        onChange={() =>
                          setPreferences((value) => ({
                            ...value,
                            intensity: option.id,
                          }))
                        }
                      />
                      <span>{option.name}</span>
                    </label>
                  ))}
                </div>
              </fieldset>
            </>
          }
          result={
            task && (
              <div className="ai-action-result">
                <div className="ai-action-state">
                  <AIModelLogo
                    model={task.run.model}
                    provider={task.run.provider}
                  />
                  <strong>{aiProviderName(task.run.provider)}</strong>
                  <span>{task.run.state}</span>
                </div>
                {active && (
                  <p role="status">
                    {task.run.progress || "Preparing captured context…"}
                  </p>
                )}
                {task.run.error && (
                  <p className="ai-review-error" role="alert">
                    {task.run.error}
                  </p>
                )}
                {!!task.run.warnings?.length && (
                  <ul className="ai-action-warnings">
                    {task.run.warnings.map((warning, index) => (
                      <li key={index}>{warning}</li>
                    ))}
                  </ul>
                )}
                {proposal && (
                  <div className="ai-action-proposal" aria-label="AI proposal">
                    {proposal.title && <h3>{proposal.title}</h3>}
                    {proposal.body && <Markdown>{proposal.body}</Markdown>}
                    {proposal.text &&
                      (scope.kind === "commit" ? (
                        <pre>{proposal.text}</pre>
                      ) : (
                        <Markdown>{proposal.text}</Markdown>
                      ))}
                    {proposal.content !== undefined && (
                      <pre
                        className="ai-conflict-proposal"
                        tabIndex={0}
                        aria-label="Proposed merged source"
                      >
                        <code>{proposal.content}</code>
                      </pre>
                    )}
                  </div>
                )}
                {onApply && proposal && (
                  <div className="ai-action-apply">
                    <p className="muted">
                      {scope.kind === "conflict"
                        ? "Replaces the result editor. Undo and the separate Save result and stage action stay available."
                        : "Replaces the draft fields. Review and edit them before saving."}
                    </p>
                    <button
                      type="button"
                      className="btn btn-primary"
                      disabled={
                        lock || disabled || task.run.state !== "completed"
                      }
                      onClick={() => void apply()}
                    >
                      {applying
                        ? "Checking captured changes…"
                        : applyLabel || "Use proposal"}
                    </button>
                  </div>
                )}
                {applied && (
                  <p className="text-green" role="status">
                    {applied}
                  </p>
                )}
                {task.worktree && (
                  <div className="ai-action-worktree">
                    <strong>{task.worktree.branch}</strong>
                    <code>{task.worktree.path}</code>
                    <span>
                      {task.writes}{" "}
                      {task.writes === 1 ? "file write" : "file writes"}
                    </span>
                    {onOpenWorktree && (
                      <button
                        type="button"
                        className="btn"
                        disabled={active || busy}
                        onClick={() => onOpenWorktree(task.worktree!.path)}
                      >
                        Open worktree to review
                      </button>
                    )}
                  </div>
                )}
                <AIReviewActivity run={task.run} task />
              </div>
            )
          }
          extras={
            <>
              {checking ? (
                <p role="status">Checking provider…</p>
              ) : status?.authenticated ? (
                <p className="ai-provider-status text-green">
                  Connected{status.account ? ` · ${status.account}` : ""}
                </p>
              ) : (
                <p>
                  {status?.error || "Connect this provider in Settings → AI."}{" "}
                  {onOpenSettings && (
                    <button
                      type="button"
                      className="btn btn-ghost"
                      onClick={onOpenSettings}
                    >
                      AI provider settings
                    </button>
                  )}
                  <button
                    type="button"
                    className="btn btn-ghost"
                    onClick={() => setRetry((n) => n + 1)}
                  >
                    Retry provider
                  </button>
                </p>
              )}
              <p className="ai-review-disclosure">
                Starting sends the captured Git context and requested source to{" "}
                {aiProviderName(preferences.provider)} using your account.
                {scope.kind === "code"
                  ? " File tools can read and edit the owned worktree. Review and run checks yourself before committing."
                  : " Each request creates a proposal for you to review."}
              </p>
            </>
          }
        />
      </Suspense>
    </section>
  );
}
