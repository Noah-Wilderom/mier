import type { PendingComment, PullRequest } from "./data";
export type AIProvider = "codex" | "copilot";
export interface AIModel {
  id: string;
  name: string;
  default: boolean;
}
export interface AIProviderStatus {
  provider: AIProvider;
  installed: boolean;
  executable: string;
  authenticated: boolean;
  account: string;
  plan: string;
  models: AIModel[];
  error: string;
}
export interface AIPreferences {
  codexPath: string;
  copilotPath: string;
  provider: AIProvider;
  model: string;
}
export interface AIFinding {
  id: string;
  path: string;
  line: number;
  side: "old" | "new";
  body: string;
  severity: "bug" | "warning" | "suggestion";
}
export interface AIReview {
  id: string;
  key: string;
  provider: AIProvider | "external";
  model: string;
  headSha: string;
  state: "preparing" | "running" | "completed" | "failed" | "cancelled";
  progress: string;
  summary: string;
  error: string;
  findings: AIFinding[];
  version: number;
  createdAt: string;
  finishedAt: string;
  warnings: string[];
}
export interface AIReviewRequest {
  host: string;
  owner: string;
  repository: string;
  pullRequest: number;
  headSha: string;
  baseSha: string;
  provider: AIProvider;
  model: string;
  instructions: string;
}
export interface AIConnection {
  run: AIReview;
  url: string;
  token: string;
  tools: string[];
}
export interface AILogin {
  loginId: string;
  url: string;
  code: string;
}
export interface AILoginStatus {
  state: "waiting" | "completed" | "failed" | "cancelled";
  error: string;
}
export const aiProviderName = (provider: AIReview["provider"]) =>
  provider === "codex"
    ? "Codex"
    : provider === "copilot"
      ? "GitHub Copilot"
      : "External agent";
export const aiReviewActive = (run?: AIReview) =>
  !!run && ["preparing", "running"].includes(run.state);
export function aiPendingComment(
  run: AIReview,
  finding: AIFinding,
  pr: PullRequest,
): PendingComment {
  const valid = pr.files.some(
    (file) =>
      file.path === finding.path &&
      file.lines.some((line) =>
        finding.side === "old"
          ? line.oldLine === finding.line
          : line.newLine === finding.line,
      ),
  );
  return {
    id: `ai:${run.id}:${finding.id}`,
    path: finding.path,
    line: finding.line,
    side: finding.side,
    body: `**AI review · ${aiProviderName(run.provider)} · ${finding.severity}**\n\n${finding.body}`,
    headSha: run.headSha,
    outdated: run.headSha !== pr.headSha || !valid,
    ai: {
      runId: run.id,
      provider: run.provider,
      model: run.model,
      severity: finding.severity,
    },
  };
}
// Apply the user's comment edits as a delta so an async reviewer cannot lose a
// human draft, and a human edit cannot drop new AI suggestions received later.
export function mergePendingComments(
  previous: PendingComment[],
  next: PendingComment[],
  current: PendingComment[],
): PendingComment[] {
  const prior = new Map(previous.map((comment) => [comment.id, comment]));
  const updated = new Map(next.map((comment) => [comment.id, comment]));
  const removed = new Set(
    previous
      .filter((comment) => !updated.has(comment.id))
      .map((comment) => comment.id),
  );
  const result = current.filter((comment) => !removed.has(comment.id));
  for (const comment of next) {
    if (
      !prior.has(comment.id) ||
      JSON.stringify(prior.get(comment.id)) !== JSON.stringify(comment)
    ) {
      const index = result.findIndex((value) => value.id === comment.id);
      if (index >= 0) result[index] = comment;
      else result.push(comment);
    }
  }
  return result;
}
