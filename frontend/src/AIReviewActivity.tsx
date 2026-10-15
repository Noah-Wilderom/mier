import type { AIReview } from "./aiTypes";

const number = (value: number | undefined) =>
  value == null
    ? "Not reported"
    : value > 0 && value < 0.000001
      ? value.toPrecision(3)
      : value.toLocaleString("en-US", { maximumFractionDigits: 6 });
const duration = (value: number) =>
  value < 1000 ? `${Math.max(0, value)} ms` : `${(value / 1000).toFixed(1)} s`;
const toolLabels: Record<string, string> = {
  get_review_context: "Read PR context",
  read_review_file: "Read file",
  read_review_ticket: "Read linked ticket",
  add_review_comment: "Propose finding",
  finish_review: "Write summary",
};
export function AIReviewActivity({ run }: { run: AIReview }) {
  const totals = run.activityTotals;
  const usage = run.usage;
  const elapsed =
    (run.finishedAt ? Date.parse(run.finishedAt) : Date.now()) -
    Date.parse(run.createdAt);
  const metrics: [string, string][] = [
    ["Input tokens", number(usage?.inputTokens)],
    ["Output tokens", number(usage?.outputTokens)],
    ["Dollar cost", "Not reported"],
  ];
  if (usage?.totalTokens != null)
    metrics.push(["Total tokens", number(usage.totalTokens)]);
  if (usage?.cachedInputTokens != null)
    metrics.push(["Cached input tokens", number(usage.cachedInputTokens)]);
  if (usage?.cacheWriteTokens != null)
    metrics.push(["Cache write tokens", number(usage.cacheWriteTokens)]);
  if (usage?.reasoningTokens != null)
    metrics.push(["Reasoning tokens", number(usage.reasoningTokens)]);
  if (usage?.contextTokens != null)
    metrics.push([
      "Context tokens",
      `${number(usage.contextTokens)}${usage.contextWindow != null ? ` / ${number(usage.contextWindow)}` : ""}`,
    ]);
  if (usage?.modelCalls != null)
    metrics.push(["Model calls", number(usage.modelCalls)]);
  if (usage?.apiDurationMs != null)
    metrics.push(["Model API time", duration(usage.apiDurationMs)]);
  if (usage?.premiumRequests != null)
    metrics.push(["Premium requests", number(usage.premiumRequests)]);
  if (usage?.nanoAiu != null && usage.billingStatus !== "unavailable")
    metrics.push([
      usage.billingStatus === "partial"
        ? "Nano-AI units (subtotal)"
        : "Nano-AI units",
      number(usage.nanoAiu),
    ]);
  if (Number.isFinite(elapsed))
    metrics.push(["Review time", duration(elapsed)]);
  return (
    <details className="ai-activity" aria-label="Review activity and usage">
      <summary>
        Activity and usage
        {totals && (
          <span>
            {totals.toolCalls} tools · {totals.fileReads} file{" "}
            {totals.fileReads === 1 ? "read" : "reads"}
            {!!totals.ticketReads &&
              ` · ${totals.ticketReads} ticket ${totals.ticketReads === 1 ? "read" : "reads"}`}
          </span>
        )}
      </summary>
      <dl className="ai-usage-metrics">
        {metrics.map(([label, value]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
      <p className="muted ai-usage-note">
        {usage?.source
          ? `Provider-reported ${usage.partial ? "subtotals" : "usage"}. `
          : "This provider has not reported usage. "}
        This review connection does not supply a dollar charge.
        {usage?.premiumRequests != null || usage?.nanoAiu != null
          ? " Premium requests and nano-AI units are provider billing units, not dollars."
          : ""}
      </p>
      {totals ? (
        <p className="ai-activity-totals muted">
          {totals.filesRead} unique {totals.filesRead === 1 ? "file" : "files"}{" "}
          · {totals.linesRead} source lines · {number(totals.bytesRead)} source
          bytes · {totals.failedCalls} failed or cancelled{" "}
          {totals.failedCalls === 1 ? "call" : "calls"}
          {!!totals.ticketsRead &&
            ` · ${totals.ticketsRead} unique ${totals.ticketsRead === 1 ? "ticket" : "tickets"} · ${number(totals.ticketBytesRead)} ticket bytes`}
        </p>
      ) : (
        <p className="muted">Tool activity is unavailable for this session.</p>
      )}
      {!!totals?.omitted && (
        <p className="muted">
          Showing the latest {run.activity?.length ?? 0} calls; {totals.omitted}{" "}
          earlier {totals.omitted === 1 ? "call" : "calls"} omitted. Totals
          include them.
        </p>
      )}
      {!!run.activity?.length && (
        <ol className="ai-tool-activity" aria-label="Executed review tools">
          {run.activity.map((call) => (
            <li key={call.id} data-state={call.state}>
              <div>
                <strong>{toolLabels[call.tool] || "Review tool"}</strong>
                <span>
                  {call.state} ·{" "}
                  {call.state === "running"
                    ? "in progress"
                    : duration(call.durationMs)}
                </span>
              </div>
              {call.path && (
                <code>
                  {call.path}
                  {call.startLine
                    ? `:${call.startLine}${call.endLine && call.endLine !== call.startLine ? `–${call.endLine}` : ""}`
                    : ""}
                  {call.side
                    ? ` · ${call.side === "old" ? "Original" : "Changed"}`
                    : ""}
                </code>
              )}
              {call.ticketKey && (
                <code>
                  {call.ticketProvider === "jira" ? "Jira" : "Linear"} ·{" "}
                  {call.ticketKey}
                </code>
              )}
              {call.tool === "read_review_file" &&
                call.state === "completed" && (
                  <span className="muted">
                    {call.lines} lines returned · {number(call.bytes)} bytes
                  </span>
                )}
              {call.tool === "read_review_ticket" &&
                call.state === "completed" && (
                  <span className="muted">
                    {number(call.bytes)} ticket bytes returned · read only
                  </span>
                )}
            </li>
          ))}
        </ol>
      )}
    </details>
  );
}
