import type { AIIntensity, AIProvider, AIReview } from "./aiTypes";

export type AIActionKind =
  | "commit"
  | "pull_request"
  | "explain"
  | "conflict"
  | "code";
export interface AIActionScope {
  root: string;
  kind: AIActionKind;
  base?: string;
  head?: string;
  expectedHead?: string;
  path?: string;
  worktreeId?: string;
  clientId?: string;
  currentResult?: string;
}
export interface AIActionRequest extends AIActionScope {
  provider: AIProvider;
  model: string;
  instructions: string;
  intensity: AIIntensity;
}
export interface AIActionProposal {
  checks?:import("./WorktreeVerification").CheckProposal[];
  text?: string;
  title?: string;
  body?: string;
  content?: string;
}
export interface AIWorktreeInfo {
  id: string;
  repositoryRoot: string;
  path: string;
  branch: string;
  headSha: string;
  createdAt: string;
  lastActionId: string;
  state: string;
}
export interface AIActionSnapshot {
  run: AIReview;
  kind: AIActionKind;
  root: string;
  proposal: AIActionProposal | null;
  worktree?: AIWorktreeInfo;
  writes: number;
}
export function aiActionScopeKey(scope: AIActionScope) {
  return JSON.stringify([
    scope.root,
    scope.kind,
    scope.base,
    scope.head,
    scope.kind === "code" ? undefined : scope.expectedHead,
    scope.path,
    scope.worktreeId,
    scope.clientId,
  ]);
}
