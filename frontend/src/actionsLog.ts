// Job logs are text, never HTML. Remove terminal controls before displaying them.
export function cleanJobLog(input: string): string {
  return input
    .replace(/\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g, "")
    .replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "")
    .replace(/\x1b[@-_]/g, "")
    .replace(/\r\n/g, "\n")
    .replace(/\r/g, "\n")
    .replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g, "");
}
export interface LogSection {
  title: string;
  text: string;
  error: boolean;
}
export function jobLogSections(input: string): LogSection[] {
  const sections: LogSection[] = [];
  let current: LogSection = { title: "Job output", text: "", error: false };
  const flush = () => {
    if (current.text.trim()) sections.push(current);
  };
  for (const line of cleanJobLog(input).split("\n")) {
    const group = line.match(/(?:##\[group\]|::group::)(.*)$/);
    if (group) {
      flush();
      current = {
        title: group[1].trim() || "Step output",
        text: "",
        error: false,
      };
      continue;
    }
    if (/##\[endgroup\]|::endgroup::/.test(line)) {
      flush();
      current = { title: "Job output", text: "", error: false };
      continue;
    }
    current.text += line + "\n";
    if (/##\[error\]|::error(?: |::)/.test(line)) current.error = true;
  }
  flush();
  return sections;
}
export function actionStatus(
  status: string,
  conclusion?: string,
): { label: string; color: string; icon: string } {
  const value = status === "completed" ? conclusion || "completed" : status;
  const label =
    (
      {
        in_progress: "Running",
        action_required: "Action required",
        timed_out: "Timed out",
        startup_failure: "Startup failed",
        success: "Passed",
        failure: "Failed",
        error: "Failed",
        stale: "Stale",
        cancelled: "Cancelled",
        skipped: "Skipped",
        neutral: "Neutral",
        queued: "Queued",
        pending: "Pending",
        requested: "Requested",
        waiting: "Waiting",
        completed: "Completed",
      } as Record<string, string>
    )[value] || value.replace(/_/g, " ");
  return {
    label,
    color:
      value === "success"
        ? "text-green"
        : [
              "failure",
              "error",
              "timed_out",
              "action_required",
              "startup_failure",
            ].includes(value)
          ? "text-red"
          : [
                "queued",
                "in_progress",
                "waiting",
                "pending",
                "requested",
              ].includes(value)
            ? "text-amber"
            : "text-muted",
    icon:
      value === "success"
        ? "check-circle"
        : [
              "failure",
              "error",
              "timed_out",
              "action_required",
              "startup_failure",
            ].includes(value)
          ? "alert-circle"
          : ["cancelled", "skipped"].includes(value)
            ? "close"
            : "clock",
  };
}
