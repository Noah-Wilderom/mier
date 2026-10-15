import {
  lazy,
  Suspense,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { PullRequest } from "./data";
import { services } from "./services";
import { Markdown } from "./Markdown";
import { Icon } from "./Icon";
import {
  aiProviderName,
  aiIntensities,
  type AIIntensity,
  aiReviewActive,
  type AIConnection,
  type AIFinding,
  type AIPreferences,
  type AIProvider,
  type AIProviderStatus,
  type AIReview,
  type AIReviewRequest,
} from "./aiTypes";
import "./AIReview.css";
import { AIModelLogo, AIModelPicker } from "./AIModelPicker";
const AIReviewChat = lazy(async () => ({
  default: (await import("./AIReviewChat")).AIReviewChat,
}));

type Props = {
  pr: PullRequest;
  enabled: boolean;
  children: ReactNode;
  onImport: (run: AIReview, findings: AIFinding[]) => Promise<void>;
  onOpenSettings: () => void;
  expanded: boolean;
  onExpand: () => void;
  onOpenComment: (id: string, path: string) => void;
};
export default function ReviewAssistant(props: Props) {
  return props.enabled && props.pr.provider ? (
    <LiveAssistant {...props} />
  ) : (
    <>{props.children}</>
  );
}
type Saved = { id: string; seen: string[] };
const storagePrefix = "mier.ai-review.";
const read = (key: string): Saved | undefined => {
  try {
    const value = JSON.parse(
      localStorage.getItem(storagePrefix + key) ?? "null",
    );
    if (
      typeof value?.id === "string" &&
      Array.isArray(value.seen) &&
      value.seen.every((id: unknown) => typeof id === "string")
    )
      return value;
  } catch {
    /* keep code review usable */
  }
};
function LiveAssistant({
  pr,
  children,
  onImport,
  onOpenSettings,
  expanded,
  onExpand,
  onOpenComment,
}: Props) {
  const identity = pr.provider!;
  const scope = JSON.stringify([
    identity.host,
    identity.owner,
    identity.repo,
    pr.number,
  ]);
  const [open, setOpen] = useState(false);
  const [saved, setSaved] = useState<Saved | undefined>(() => read(scope));
  const [run, setRun] = useState<AIReview>();
  const [preferences, setPreferences] = useState<AIPreferences>({
    provider: "codex",
    model: "",
    intensity: "medium",
    codexPath: "",
    copilotPath: "",
  });
  const [status, setStatus] = useState<AIProviderStatus>();
  const [connection, setConnection] = useState<AIConnection>();
  const [instructions, setInstructions] = useState("");
  const [error, setError] = useState(""),
    [pollError, setPollError] = useState("");
  const [busy, setBusy] = useState(false),
    [checking, setChecking] = useState(false),
    [retry, setRetry] = useState(0);
  const [confirmExternal, setConfirmExternal] = useState(false),
    [copied, setCopied] = useState(false);
  const seen = useRef(new Set(saved?.seen ?? []));
  const importRef = useRef(onImport);
  importRef.current = onImport;
  const guard = useRef(false);
  const preferencesLoaded = useRef(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const persist = (value: Saved) => {
    setSaved(value);
    try {
      localStorage.setItem(storagePrefix + scope, JSON.stringify(value));
    } catch {
      /* native draft persistence still reports its own failures */
    }
  };
  useEffect(() => {
    if (!open || preferencesLoaded.current) return;
    let stale = false;
    void services.ai
      .preferences()
      .then((value) => {
        if (!stale) {
          setPreferences(value);
          preferencesLoaded.current = true;
        }
      })
      .catch((issue) => {
        if (!stale) setError(String(issue));
      });
    return () => {
      stale = true;
    };
  }, [open]);
  useEffect(() => {
    if (!open) return;
    let stale = false;
    setChecking(true);
    setStatus(undefined);
    void services.ai
      .status(preferences.provider)
      .then((value) => {
        if (!stale) setStatus(value);
      })
      .catch((issue) => {
        if (!stale)
          setError(issue instanceof Error ? issue.message : String(issue));
      })
      .finally(() => {
        if (!stale) setChecking(false);
      });
    return () => {
      stale = true;
    };
  }, [open, preferences.provider, retry]);
  useEffect(() => {
    if (!saved?.id) return;
    let stale = false,
      timer: ReturnType<typeof setTimeout>;
    setPollError("");
    const poll = async () => {
      try {
        const value = await services.ai.get(saved.id);
        if (stale) return;
        setRun(value);
        const additions = (value.findings ?? []).filter(
          (finding) => !seen.current.has(finding.id),
        );
        if (additions.length) {
          await importRef.current(value, additions);
          if (stale) return;
          for (const finding of additions) seen.current.add(finding.id);
          persist({ id: value.id, seen: [...seen.current] });
        }
        if (aiReviewActive(value) && !stale)
          timer = setTimeout(() => void poll(), document.hidden ? 2500 : 800);
      } catch (issue) {
        if (!stale)
          setPollError(issue instanceof Error ? issue.message : String(issue));
      }
    };
    void poll();
    return () => {
      stale = true;
      clearTimeout(timer);
    };
  }, [saved?.id, retry]);
  const active = aiReviewActive(run);
  useEffect(() => {
    if (
      !open ||
      !run ||
      run.provider !== "external" ||
      !aiReviewActive(run) ||
      connection?.run.id === run.id
    )
      return;
    let stale = false;
    void services.ai
      .connection(run.id)
      .then((value) => {
        if (!stale) setConnection(value);
      })
      .catch((issue) => {
        if (!stale) setError(String(issue));
      });
    return () => {
      stale = true;
    };
  }, [open, run?.id, run?.state, connection?.run.id]);
  const request = (focus = instructions): AIReviewRequest => ({
    host: identity.host,
    owner: identity.owner,
    repository: identity.repo,
    pullRequest: pr.number,
    headSha: pr.headSha,
    baseSha: pr.baseSha ?? "",
    provider: preferences.provider,
    model: preferences.model,
    instructions: focus,
    intensity: preferences.intensity || "medium",
  });
  const start = async (
    external = false,
    focus = instructions,
  ): Promise<boolean> => {
    if (guard.current || active) return false;
    guard.current = true;
    setBusy(true);
    setError("");
    setPollError("");
    try {
      const next = external
        ? await services.ai.startMCP(request(focus))
        : await services.ai.start(request(focus));
      const review = external ? (next as AIConnection).run : (next as AIReview);
      try {
        localStorage.setItem(
          storagePrefix + scope,
          JSON.stringify({ id: review.id, seen: [] }),
        );
      } catch {
        /* session metadata is best-effort */
      }
      if (!mounted.current) return true;
      seen.current = new Set();
      setRun(review);
      persist({ id: review.id, seen: [] });
      setConfirmExternal(false);
      setConnection(external ? (next as AIConnection) : undefined);
      if (!external)
        void services.ai.savePreferences(preferences).catch(() => {
          /* a saved review is not repeated because preferences failed */
        });
      return true;
    } catch (issue) {
      if (mounted.current)
        setError(issue instanceof Error ? issue.message : String(issue));
      return false;
    } finally {
      guard.current = false;
      if (mounted.current) setBusy(false);
    }
  };
  const cancel = async () => {
    if (!run || guard.current) return;
    guard.current = true;
    setBusy(true);
    setError("");
    try {
      await services.ai.cancel(run.id);
      setConnection(undefined);
      setRetry((n) => n + 1);
    } catch (issue) {
      setError(issue instanceof Error ? issue.message : String(issue));
    } finally {
      guard.current = false;
      setBusy(false);
    }
  };
  const copiedConfig = connection
    ? JSON.stringify(
        {
          mcpServers: {
            mier: {
              type: "http",
              url: connection.url,
              headers: { Authorization: `Bearer ${connection.token}` },
            },
          },
        },
        null,
        2,
      )
    : "";
  const forget = () => {
    setSaved(undefined);
    setRun(undefined);
    setConnection(undefined);
    setPollError("");
    seen.current.clear();
    try {
      localStorage.removeItem(storagePrefix + scope);
    } catch {}
  };
  return (
    <section
      className={`ai-review-layout${open ? " has-ai-pane" : ""}`}
      aria-label="Pull request AI review"
    >
      <div className="ai-review-toolbar">
        <strong>AI review</strong>
        <span className="muted">
          {active
            ? run?.progress || "Reviewing…"
            : run
              ? `${run.findings?.length ?? 0} suggestions · ${run.state}`
              : "Private draft suggestions"}
        </span>
        {open && !expanded && (
          <button className="btn btn-ghost" onClick={onExpand}>
            Expand code area
          </button>
        )}
        <button
          className="btn btn-ghost"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
        >
          {open ? "Hide AI review" : "Review with AI"}
        </button>
      </div>
      <div className="ai-review-body">
        <div className="ai-review-code">{children}</div>
        {open && (
          <aside className="ai-review-pane" aria-label="AI reviewer">
            <header>
              <strong>Review assistant</strong>
              <button
                className="icon-button"
                aria-label="Close AI review pane"
                onClick={() => setOpen(false)}
              >
                <Icon name="close" size={14} />
              </button>
            </header>
            <div className="ai-review-pane-scroll">
              {(error || pollError) && (
                <div className="ai-review-error" role="alert">
                  {error || pollError}
                  {pollError && (
                    <button
                      className="btn"
                      disabled={busy}
                      onClick={() => setRetry((n) => n + 1)}
                    >
                      Retry review status
                    </button>
                  )}
                  {pollError.toLowerCase().includes("not found") && (
                    <button className="btn-ghost" onClick={forget}>
                      Forget ended session
                    </button>
                  )}
                </div>
              )}

              <Suspense
                fallback={
                  <p className="ai-provider-status" role="status">
                    Loading review chat…
                  </p>
                }
              >
                <AIReviewChat
                  run={run}
                  active={active}
                  busy={busy}
                  disabled={
                    checking ||
                    !status?.authenticated ||
                    pr.provider?.detailLoaded === false
                  }
                  onStart={(focus) => start(false, focus)}
                  onCancel={cancel}
                  onDraftChange={setInstructions}
                  initialDraft={instructions}
                  result={
                    run && (
                      <div className="ai-run-result">
                        <div className="ai-run-heading">
                          <strong className="ai-provider-brand">
                            <AIModelLogo
                              model={run.model}
                              provider={run.provider}
                            />
                            {aiProviderName(run.provider)}
                          </strong>
                          <span>{run.state}</span>
                        </div>
                        <code title={run.headSha}>
                          Reviewing {run.headSha.slice(0, 8)}
                          {run.model ? ` · ${run.model}` : ""}
                          {` · ${aiIntensities.find((option) => option.id === (run.intensity || "medium"))?.name}`}
                        </code>
                        {run.headSha !== pr.headSha && (
                          <p className="ai-review-error">
                            This review targets an earlier commit. Its comments
                            are retained as outdated drafts.
                          </p>
                        )}
                        {active && (
                          <p role="status" className="ai-run-progress">
                            {run.progress || "Reviewing source…"}
                          </p>
                        )}
                        {!!run.error && (
                          <p className="ai-review-error" role="alert">
                            {run.error}
                          </p>
                        )}
                        {run.warnings?.map((warning, i) => (
                          <p className="ai-review-warning" key={i}>
                            {warning}
                          </p>
                        ))}
                        {!!run.findings?.length && (
                          <div className="ai-findings">
                            {run.findings.map((finding) => {
                              const id = `ai:${run.id}:${finding.id}`,
                                added = pr.pendingComments.some(
                                  (comment) => comment.id === id,
                                );
                              return (
                                <article key={finding.id}>
                                  <header>
                                    <span
                                      className={`ai-severity ai-severity-${finding.severity}`}
                                    >
                                      {finding.severity}
                                    </span>
                                    <button
                                      className="btn-ghost"
                                      onClick={() =>
                                        onOpenComment(id, finding.path)
                                      }
                                    >
                                      {finding.path.split("/").pop()}:
                                      {finding.line}
                                    </button>
                                  </header>
                                  <Markdown>{finding.body}</Markdown>
                                  <span className="muted">
                                    {added
                                      ? "Added to your pending review"
                                      : "Removed from pending review"}
                                  </span>
                                  {!added && (
                                    <button
                                      className="btn-ghost"
                                      disabled={busy}
                                      onClick={() =>
                                        void importRef
                                          .current(run, [finding])
                                          .catch((issue) =>
                                            setError(String(issue)),
                                          )
                                      }
                                    >
                                      Add suggestion back
                                    </button>
                                  )}
                                </article>
                              );
                            })}
                          </div>
                        )}
                        {!!run.summary && (
                          <div className="ai-review-summary">
                            <h3>Review summary</h3>
                            <Markdown>{run.summary}</Markdown>
                          </div>
                        )}
                        {run.state === "completed" && !run.findings?.length && (
                          <p className="muted">
                            No inline suggestions were added. Read the summary
                            for the review’s scope and limitations.
                          </p>
                        )}
                      </div>
                    )
                  }
                  controls={
                    <>
                      <div className="ai-review-selectors">
                        <label>
                          Provider
                          <div className="ai-provider-select">
                            <AIModelLogo provider={preferences.provider} />
                            <select
                              aria-label="AI review provider"
                              value={preferences.provider}
                              disabled={busy || active}
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
                          disabled={busy || active || checking}
                          onChange={(model) =>
                            setPreferences((value) => ({ ...value, model }))
                          }
                        />
                      </div>
                      <fieldset
                        className="ai-intensity"
                        disabled={busy || active}
                      >
                        <legend>Review intensity</legend>
                        <div>
                          {aiIntensities.map((option) => (
                            <label key={option.id} title={option.detail}>
                              <input
                                type="radio"
                                name="review-intensity"
                                value={option.id}
                                checked={
                                  (preferences.intensity || "medium") ===
                                  option.id
                                }
                                onChange={(event) =>
                                  setPreferences((value) => ({
                                    ...value,
                                    intensity: event.target
                                      .value as AIIntensity,
                                  }))
                                }
                              />
                              <span>{option.name}</span>
                            </label>
                          ))}
                        </div>
                      </fieldset>
                      <p className="ai-intensity-detail muted">
                        {
                          aiIntensities.find(
                            (option) =>
                              option.id === (preferences.intensity || "medium"),
                          )?.detail
                        }
                      </p>
                    </>
                  }
                  extras={
                    <>
                      {checking ? (
                        <p role="status">Checking provider…</p>
                      ) : status?.authenticated ? (
                        <p className="ai-provider-status text-green">
                          Connected
                          {status.account ? ` · ${status.account}` : ""}
                          {status.plan ? ` · ${status.plan}` : ""}
                        </p>
                      ) : (
                        <p className="ai-provider-status">
                          {status?.error ||
                            "Connect this provider in Settings."}
                          <button
                            className="btn-ghost"
                            onClick={onOpenSettings}
                          >
                            AI provider settings
                          </button>
                          <button
                            className="btn-ghost"
                            onClick={() => setRetry((n) => n + 1)}
                          >
                            Retry provider
                          </button>
                        </p>
                      )}
                      <p className="ai-review-disclosure">
                        Starting sends this PR’s changes and requested source
                        files to {aiProviderName(preferences.provider)} using
                        your account. Suggestions stay private until you submit
                        your review. Each message starts a new review of this
                        PR.
                      </p>
                      {connection && active && (
                        <div className="ai-mcp-connection">
                          <strong>MCP connection ready</strong>
                          <p>
                            Copy this configuration into your local MCP client.
                            It grants access to this PR revision until you stop
                            the review or it expires in 30 minutes.
                          </p>
                          <code>{connection.url}</code>
                          <button
                            className="btn"
                            onClick={() =>
                              void navigator.clipboard
                                .writeText(copiedConfig)
                                .then(() => setCopied(true))
                                .catch(() =>
                                  setError(
                                    "Clipboard unavailable. Copy the configuration below.",
                                  ),
                                )
                            }
                          >
                            {copied
                              ? "Configuration copied"
                              : "Copy MCP configuration"}
                          </button>
                          <details>
                            <summary>Show connection configuration</summary>
                            <pre>{copiedConfig}</pre>
                          </details>
                        </div>
                      )}
                      {!active && !busy && (
                        <div className="ai-external">
                          <button
                            className="btn btn-ghost"
                            onClick={() =>
                              setConfirmExternal((value) => !value)
                            }
                          >
                            Connect an MCP client…
                          </button>
                          {confirmExternal && (
                            <div className="ai-mcp-confirm">
                              <p>
                                Create a temporary local connection so your own
                                agent can read this PR and add private review
                                comments?
                              </p>
                              <button
                                className="btn"
                                onClick={() => void start(true)}
                              >
                                Create MCP connection
                              </button>
                            </div>
                          )}
                        </div>
                      )}
                    </>
                  }
                />
              </Suspense>
            </div>
          </aside>
        )}
      </div>
    </section>
  );
}
