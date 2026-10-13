import { useId, useMemo, useRef } from "react";
import type { CSSProperties, KeyboardEvent } from "react";
import { people } from "./data";
import type { Commit } from "./data";
import {
  buildHistoryGraph,
  commitRefs,
  HISTORY_ROW_HEIGHT,
  historyEdgePath,
  historyLaneX,
  historyRowY,
} from "./HistoryGraph.topology";
import type { GraphRow } from "./HistoryGraph.topology";
import "./HistoryGraph.css";

export interface HistoryGraphProps {
  commits: readonly Commit[];
  selectedSha?: string;
  onSelect: (sha: string) => void;
}

const laneColors = ["var(--accent)", "var(--blue)", "var(--purple)", "var(--amber)", "var(--red)"];
const laneColor = (lane: number): string => laneColors[lane % laneColors.length];

function ancestryDescription(row: GraphRow<Commit>): string {
  if (row.invalidTopology || row.invalidParents.length > 0) {
    return "Parent metadata is inconsistent; unavailable relationships are not drawn.";
  }
  if (row.parentMetadataUnavailable) return "Parent metadata is unavailable.";
  if (row.missingParents.length > 0) {
    return `${row.missingParents.length} parent${row.missingParents.length === 1 ? " is" : "s are"} outside the loaded history.`;
  }
  const count = new Set(row.commit.parents).size;
  return count === 0 ? "Root commit." : `${count} known parent${count === 1 ? "" : "s"}.`;
}

/** A bounded history graph, with no assumed connections beyond loaded parents. */
export default function HistoryGraph({ commits, selectedSha, onSelect }: HistoryGraphProps) {
  const descriptionId = useId();
  const layout = useMemo(() => buildHistoryGraph(commits), [commits]);
  const buttons = useRef(new Map<string, HTMLButtonElement>());
  const selected = layout.rows.some((row) => row.commit.sha === selectedSha)
    ? selectedSha
    : layout.rows[0]?.commit.sha;
  const graphWidth = Math.max(70, historyLaneX(Math.max(0, layout.laneCount - 1)) + 20);
  const boundaryCount = layout.rows.filter((row) =>
    row.parentMetadataUnavailable || row.missingParents.length > 0 || row.invalidTopology || row.invalidParents.length > 0,
  ).length;
  const style = { "--history-graph-width": `${graphWidth}px` } as CSSProperties;

  function navigate(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    let next: number;
    switch (event.key) {
      case "ArrowDown": next = Math.min(layout.rows.length - 1, index + 1); break;
      case "ArrowUp": next = Math.max(0, index - 1); break;
      case "Home": next = 0; break;
      case "End": next = layout.rows.length - 1; break;
      default: return;
    }
    event.preventDefault();
    const sha = layout.rows[next]?.commit.sha;
    if (sha) {
      onSelect(sha);
      buttons.current.get(sha)?.focus();
    }
  }

  return (
    <div className="history-table real-history" style={style}>
      <p className="sr-only" id={descriptionId}>
        Commits are ordered before their parents. Lines connect loaded parent commits.
        Dashed markers indicate unavailable parent history.
        Use arrow keys, Home or End to select a commit.
      </p>
      <div className="real-history-content">
        <div className="history-table-heading real-history-heading" aria-hidden="true">
          <span title="Lines connect known parents; dashed markers indicate unavailable parent history.">Graph</span>
          <span>Commit</span>
          <span>Author</span>
          <span>Hash</span>
        </div>
        {layout.rows.length > 0 && (
          <svg
            className="real-history-graph"
            width={graphWidth}
            height={layout.rows.length * HISTORY_ROW_HEIGHT}
            viewBox={`0 0 ${graphWidth} ${layout.rows.length * HISTORY_ROW_HEIGHT}`}
            aria-hidden="true"
            focusable="false"
          >
            {layout.edges.map((edge) => (
              <path
                key={`${edge.childSha}:${edge.parentSha}`}
                d={historyEdgePath(edge)}
                fill="none"
                stroke={laneColor(edge.parentLane)}
                strokeWidth="1.7"
                strokeLinecap="round"
              />
            ))}
            {layout.rows.map((row) => {
              const x = historyLaneX(row.lane);
              const y = historyRowY(row.index);
              const boundary = row.parentMetadataUnavailable || row.missingParents.length > 0 || row.invalidParents.length > 0 || row.invalidTopology;
              return (
                <g key={row.commit.sha}>
                  {row.missingParents.length > 0 && (
                    <path d={`M${x} ${y + 6}V${y + 16}`} stroke={laneColor(row.lane)} strokeWidth="1.5" strokeDasharray="2 3" />
                  )}
                  {boundary && (
                    <circle cx={x} cy={y} r="7.5" fill="none" stroke={laneColor(row.lane)} strokeWidth="1" strokeDasharray="2 2" />
                  )}
                  <circle
                    cx={x}
                    cy={y}
                    r="4.3"
                    fill={row.commit.sha === selected ? "var(--accent-soft)" : "var(--surface)"}
                    stroke={laneColor(row.lane)}
                    strokeWidth="1.8"
                  />
                </g>
              );
            })}
          </svg>
        )}
        <div role="listbox" aria-label="Repository commits" aria-describedby={descriptionId}>
          {layout.rows.map((row) => {
            const commit = row.commit;
            const refs = commitRefs(commit);
            const author = people[commit.author]?.name ?? commit.author ?? "Unknown author";
            const ancestry = ancestryDescription(row);
            return (
              <button
                className={`history-row real-history-row ${commit.sha === selected ? "selected" : ""}`}
                key={commit.sha}
                ref={(button) => {
                  if (button) buttons.current.set(commit.sha, button);
                  else buttons.current.delete(commit.sha);
                }}
                type="button"
                role="option"
                aria-selected={commit.sha === selected}
                aria-label={`${commit.subject}. ${author}. Commit ${commit.sha.slice(0, 7)}.${refs.length ? ` References: ${refs.join(", ")}.` : ""} ${ancestry}`}
                tabIndex={commit.sha === selected ? 0 : -1}
                onClick={() => onSelect(commit.sha)}
                onKeyDown={(event) => navigate(event, row.index)}
              >
                <span className="real-history-graph-cell" title={ancestry} aria-hidden="true" />
                <span className="real-history-subject" title={commit.subject}>
                  {refs.length > 0 && (
                    <span className="real-history-refs">
                      {refs.slice(0, 3).map((ref) => (
                        <span className="label-chip real-history-ref" key={ref} title={ref} data-kind={ref.startsWith("tag: ") || ref.startsWith("refs/tags/") ? "tag" : "branch"}>
                          {ref}
                        </span>
                      ))}
                      {refs.length > 3 && <span className="real-history-extra-refs" title={refs.slice(3).join("\n")}>+{refs.length - 3}</span>}
                    </span>
                  )}
                  <span className="real-history-message">{commit.subject}</span>
                </span>
                <span className="muted real-history-author" title={author}>{author}</span>
                <code title={commit.sha}>{commit.sha.slice(0, 7)}</code>
              </button>
            );
          })}
        </div>
        {layout.rows.length === 0 && <p className="real-history-empty">No commits in this repository yet.</p>}
        {boundaryCount > 0 && (
          <p className="real-history-boundary-note">
            {layout.hasInvalidTopology
              ? "Some parent metadata is inconsistent. Those relationships are not shown."
              : "Dashed markers indicate parent history that is not loaded."}
          </p>
        )}
      </div>
    </div>
  );
}
