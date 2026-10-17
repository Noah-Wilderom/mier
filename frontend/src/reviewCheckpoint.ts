import { currentUser, latestReview, type PullRequest } from "./data";
import { loadWorkspaceState, saveWorkspaceState } from "./workspaceState";
export interface ReviewCheckpoint {
  head: string;
  at: string;
  files: Record<string, string>;
  viewed: string[];
  openThreads: string[];
}
export { fileSignature } from "./reviewFileSignature";
import { fileSignature } from "./reviewFileSignature";
export const checkpointKey = (pr: PullRequest) =>
  `checkpoint:${JSON.stringify([currentUser, pr.provider?.host ?? "demo", pr.provider?.owner ?? "", pr.provider?.repo ?? pr.repository, pr.number])}`;
export async function readCheckpoint(
  pr: PullRequest,
): Promise<ReviewCheckpoint | undefined> {
  const value = await loadWorkspaceState<ReviewCheckpoint>(checkpointKey(pr));
  if (!value) {
    const review = latestReview(pr, currentUser);
    return review && /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/i.test(review.headSha)
      ? {
          head: review.headSha,
          at: review.createdAt,
          files: {},
          viewed: [],
          openThreads: [],
        }
      : undefined;
  }
  if (
    typeof value.head !== "string" ||
    typeof value.at !== "string" ||
    !value.files ||
    typeof value.files !== "object" ||
    !Array.isArray(value.viewed) ||
    !value.viewed.every((path) => typeof path === "string") ||
    !Array.isArray(value.openThreads) ||
    !value.openThreads.every((id) => typeof id === "string")
  )
    throw new Error(
      `Saved review checkpoint is invalid (${Object.keys(value).join(", ")})`,
    );
  return value;
}
export function saveCheckpoint(pr: PullRequest) {
  const value: ReviewCheckpoint = {
    head: pr.headSha,
    at: new Date().toISOString(),
    files: Object.fromEntries(
      pr.files.map((file) => [file.path, fileSignature(file)]),
    ),
    viewed: pr.viewedFiles,
    openThreads: pr.threads
      .filter((thread) => !thread.resolved && !thread.outdated)
      .map((thread) => thread.id),
  };
  saveWorkspaceState(checkpointKey(pr), value);
  window.dispatchEvent(
    new CustomEvent("mier:checkpoint", { detail: checkpointKey(pr) }),
  );
}
