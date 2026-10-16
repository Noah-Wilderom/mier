import { useRef, useState } from "react";
import type { PullRequest } from "./data";
import { aiReviewActive, type AIFinding, type AIReview } from "./aiTypes";
import PredictionPanel from "./PredictionPanel";
import {predictionValue,type PredictionResult} from "./predictionTypes";
import { Markdown } from "./Markdown";

export function AIReviewFindings({
  run,
  pr,
  dismissed,
  onDismiss,
  onImport,
  onInspect,
  onOpenComment,
}: {
  run: AIReview;
  pr: PullRequest;
  dismissed: string[];
  onDismiss: (ids: string[]) => void;
  onImport: (run: AIReview, findings: AIFinding[]) => Promise<void>;
  onInspect: (finding: AIFinding) => void;
  onOpenComment: (id: string, path: string) => void;
}) {
  const [triage,setTriage]=useState<{runId:string;result:PredictionResult}>();
  const [selected, setSelected] = useState<string[]>([]);
  const [editing, setEditing] = useState<string>();
  const [body, setBody] = useState("");
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState("");
  const guard = useRef(false);
  const active = aiReviewActive(run);
  const commentID = (finding: AIFinding) => `ai:${run.id}:${finding.id}`;
  const added = (finding: AIFinding) =>
    pr.pendingComments.some((comment) => comment.id === commentID(finding));
  const visible = run.findings.filter(
    (finding) => !dismissed.includes(finding.id) || added(finding),
  );
  const hidden = run.findings.filter(
    (finding) => dismissed.includes(finding.id) && !added(finding),
  );
  const choices = visible.filter((finding) => !added(finding));
  const chosen = choices.filter((finding) => selected.includes(finding.id));
  const apply = async (findings: AIFinding[]) => {
    if (guard.current || active || !findings.length) return;
    guard.current = true;
    setAdding(true);
    setError("");
    try {
      await onImport(run, findings);
      setSelected((ids) =>
        ids.filter((id) => !findings.some((finding) => finding.id === id)),
      );
      setEditing(undefined);
    } catch (issue) {
      setError(issue instanceof Error ? issue.message : String(issue));
    } finally {
      guard.current = false;
      setAdding(false);
    }
  };
  if (!run.findings.length) return null;
  return (
    <section className="ai-findings" aria-label="AI findings">
      {!active&&pr.provider&&<PredictionPanel label="Assess finding evidence with Jev" request={{kind:"findings",scope:{host:pr.provider.host,owner:pr.provider.owner,repo:pr.provider.repo},pullRequest:pr.number,headSha:pr.headSha,baseSha:pr.baseSha,runId:run.id}} onResult={result=>setTriage(result?{runId:run.id,result}:undefined)} onInspect={row=>{const finding=run.findings.find(f=>f.id===row.findingId);if(finding)onInspect(finding)}}/>}
      <h3>Findings · {run.findings.length}</h3>
      <p className="muted">
        {active
          ? "Findings are arriving. Choose what to add after the review finishes or stops."
          : "Choose findings to turn into pending comments. Nothing is added automatically."}
      </p>
      {error && (
        <p role="alert" className="ai-review-error">
          {error}
        </p>
      )}
      {!active && !!choices.length && (
        <div className="ai-finding-batch">
          <label>
            <input
              type="checkbox"
              aria-label="Select all findings"
              disabled={adding || !!editing}
              checked={choices.every((finding) =>
                selected.includes(finding.id),
              )}
              onChange={(event) =>
                setSelected(
                  event.target.checked
                    ? choices.map((finding) => finding.id)
                    : [],
                )
              }
            />
            Select all
          </label>
          <button
            className="btn"
            disabled={adding || !chosen.length || !!editing}
            onClick={() => void apply(chosen)}
          >
            {adding ? "Adding…" : `Add selected (${chosen.length})`}
          </button>
        </div>
      )}
      {visible.map((finding) => {
        const isAdded = added(finding);
        const isEditing = editing === finding.id;
        return (
          <article
            key={finding.id}
            aria-label={`AI finding at ${finding.path}:${finding.line}`}
          >
            <header>
              {!active && !isAdded && (
                <input
                  type="checkbox"
                  aria-label={`Select finding at ${finding.path}:${finding.line}`}
                  checked={selected.includes(finding.id)}
                  disabled={adding || !!editing}
                  onChange={(event) =>
                    setSelected((ids) =>
                      event.target.checked
                        ? [...ids, finding.id]
                        : ids.filter((id) => id !== finding.id),
                    )
                  }
                />
              )}
              <span className={`ai-severity ai-severity-${finding.severity}`}>
                {finding.severity}
              </span>
              <span className="ai-finding-location" title={finding.path}>
                {finding.path.split("/").pop()}:{finding.line} ·{" "}
                {finding.side === "old" ? "Original" : "Changed"}
              </span>
            </header>
            {triage?.runId===run.id&&triage.result.rows.filter(row=>row.findingId===finding.id).map(row=><p className="ai-finding-state" key={row.id}>Jev · {row.label}: {predictionValue(row)}</p>)}
            <Markdown>{finding.body}</Markdown>
            {isAdded ? (
              <p className="ai-finding-state" role="status">
                Added to your pending review
              </p>
            ) : null}
            {isEditing ? (
              <div className="ai-finding-edit">
                <label htmlFor={`ai-finding-body-${finding.id}`}>
                  Review comment
                </label>
                <textarea
                  id={`ai-finding-body-${finding.id}`}
                  value={body}
                  maxLength={12000}
                  rows={5}
                  disabled={adding}
                  onChange={(event) => setBody(event.target.value)}
                  autoFocus
                />
                <div className="ai-finding-actions">
                  <button
                    className="btn"
                    disabled={
                      adding ||
                      !body.trim() ||
                      new TextEncoder().encode(body.trim()).length > 12000
                    }
                    onClick={() =>
                      void apply([{ ...finding, body: body.trim() }])
                    }
                  >
                    Add edited comment
                  </button>
                  <button
                    className="btn btn-ghost"
                    disabled={adding}
                    onClick={() => setEditing(undefined)}
                  >
                    Cancel edit
                  </button>
                </div>
              </div>
            ) : (
              <div className="ai-finding-actions">
                <button
                  className="btn btn-ghost"
                  disabled={run.headSha !== pr.headSha}
                  title={
                    run.headSha !== pr.headSha
                      ? "This finding belongs to an earlier commit"
                      : undefined
                  }
                  onClick={() => onInspect(finding)}
                >
                  View code
                </button>
                {isAdded ? (
                  <button
                    className="btn btn-ghost"
                    onClick={() =>
                      onOpenComment(commentID(finding), finding.path)
                    }
                  >
                    Go to comment
                  </button>
                ) : (
                  !active && (
                    <>
                      <button
                        className="btn"
                        disabled={adding || !!editing}
                        onClick={() => void apply([finding])}
                      >
                        Add to review
                      </button>
                      <button
                        className="btn btn-ghost"
                        disabled={adding || !!editing}
                        onClick={() => {
                          setEditing(finding.id);
                          setBody(finding.body);
                          setError("");
                        }}
                      >
                        Edit comment
                      </button>
                      <button
                        className="btn btn-ghost"
                        disabled={adding || !!editing}
                        onClick={() => {
                          setSelected((ids) =>
                            ids.filter((id) => id !== finding.id),
                          );
                          onDismiss([...dismissed, finding.id]);
                        }}
                      >
                        Dismiss
                      </button>
                    </>
                  )
                )}
              </div>
            )}
          </article>
        );
      })}
      {!!hidden.length && (
        <details className="ai-dismissed-findings">
          <summary>Dismissed findings ({hidden.length})</summary>
          {hidden.map((finding) => (
            <div key={finding.id}>
              <span title={finding.path}>
                {finding.path.split("/").pop()}:{finding.line}
              </span>
              <button
                className="btn btn-ghost"
                disabled={adding}
                onClick={() =>
                  onDismiss(dismissed.filter((id) => id !== finding.id))
                }
              >
                Restore finding
              </button>
            </div>
          ))}
        </details>
      )}
    </section>
  );
}
