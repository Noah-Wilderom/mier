import { useEffect, useState, type ReactNode } from "react";
import type { ChangedFile, PullRequest } from "./data";
import {
  readCheckpoint,
  saveCheckpoint,
  fileSignature,
  checkpointKey,
  type ReviewCheckpoint,
} from "./reviewCheckpoint";
import { services } from "./services";
import { mapRepositoryDiff } from "./liveData";
import { ComparisonPreview } from "./BranchComparison";

export default function IncrementalReview({
  pr,
  root,
  children,
}: {
  pr: PullRequest;
  root: string;
  children: ReactNode;
}) {
  const [checkpoint, setCheckpoint] = useState<ReviewCheckpoint>();
  const [incremental, setIncremental] = useState(false);
  const [files, setFiles] = useState<ChangedFile[] | null>(null),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(false),
    [retry, setRetry] = useState(0);
  const key = checkpointKey(pr);
  useEffect(() => {
    let canceled = false;
    const load = () =>
      void readCheckpoint(pr)
        .then((value) => {
          if (!canceled) setCheckpoint(value);
        })
        .catch((issue) => {
          if (!canceled) setError(String(issue));
        });
    load();
    window.addEventListener("mier:checkpoint", load);
    return () => {
      canceled = true;
      window.removeEventListener("mier:checkpoint", load);
    };
  }, [key, pr.reviews.length]);
  useEffect(() => {
    if (!incremental || !checkpoint || checkpoint.head === pr.headSha) return;
    let canceled = false;
    setFiles(null);
    setError("");
    setLoading(true);
    void services.repository
      .compareReviewHeads({
        root,
        host: pr.provider?.host ?? "",
        owner: pr.provider?.owner ?? "",
        repo: pr.provider?.repo ?? "",
        number: pr.number,
        previousHead: checkpoint.head,
        head: pr.headSha,
      })
      .then((value) => {
        if (!canceled) setFiles(value.map(mapRepositoryDiff));
      })
      .catch((issue) => {
        if (!canceled) setError(String(issue));
      })
      .finally(() => {
        if (!canceled) setLoading(false);
      });
    return () => {
      canceled = true;
    };
  }, [incremental, checkpoint?.head, pr.headSha, root, retry]);
  const unchanged = checkpoint
    ? pr.files.filter(
        (file) =>
          checkpoint.viewed.includes(file.path) &&
          checkpoint.files[file.path] === fileSignature(file),
      ).length
    : 0;
  const resolved = pr.threads.filter(
    (thread) => thread.resolved && checkpoint?.openThreads.includes(thread.id),
  );
  const addressed = resolved.length;
  return (
    <div className="incremental-review">
      <div className="incremental-toolbar">
        <label>
          Review changes
          <select
            aria-label="Review comparison"
            value={incremental ? "incremental" : "full"}
            onChange={(event) =>
              setIncremental(event.target.value === "incremental")
            }
          >
            <option value="full">All PR changes</option>
            <option value="incremental" disabled={!checkpoint || !root}>
              Since my last review
              {checkpoint ? ` (${checkpoint.head.slice(0, 7)})` : ""}
            </option>
          </select>
        </label>
        {checkpoint && (
          <span className="muted">
            {unchanged} viewed files unchanged · {addressed} conversations
            resolved since review
          </span>
        )}
        <button
          className="btn btn-ghost"
          disabled={!pr.provider?.detailLoaded}
          onClick={() => saveCheckpoint(pr)}
        >
          Save review checkpoint
        </button>
      </div>
      {!!resolved.length && (
        <details className="incremental-feedback">
          <summary>Resolved review feedback ({resolved.length})</summary>
          <ul>
            {resolved.map((thread) => (
              <li key={thread.id}>
                <code>
                  {thread.path}:{thread.line}
                </code>
                <p>{thread.comments[0]?.body}</p>
              </li>
            ))}
          </ul>
        </details>
      )}
      {incremental && checkpoint ? (
        <>
          <p className="comparison-note">
            {checkpoint.head.slice(0, 7)} → {pr.headSha.slice(0, 7)} · Exact
            changes since {new Date(checkpoint.at).toLocaleString()}. Return to
            all PR changes to comment.
          </p>
          {checkpoint.head === pr.headSha ? (
            <p className="comparison-empty">
              No new commits since your last review.
            </p>
          ) : (
            <ComparisonPreview
              files={files}
              error={error}
              loading={loading}
              retry={() => setRetry((value) => value + 1)}
              valid
            />
          )}
        </>
      ) : (
        children
      )}
    </div>
  );
}
