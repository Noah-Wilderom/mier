import type { GitHubUser } from "./services";

export interface Workflow {
  id: number;
  name: string;
  path: string;
  state: string;
  url: string;
}
export interface WorkflowRun {
  id: number;
  workflowId: number;
  name: string;
  displayTitle: string;
  runNumber: number;
  runAttempt: number;
  headBranch: string;
  headSha: string;
  event: string;
  status: string;
  conclusion: string;
  createdAt: string;
  updatedAt: string;
  runStartedAt: string;
  url: string;
  actor: GitHubUser;
}
export interface WorkflowStep {
  name: string;
  number: number;
  status: string;
  conclusion: string;
  startedAt: string;
  completedAt: string;
}
export interface WorkflowJob {
  id: number;
  runId: number;
  runAttempt: number;
  name: string;
  status: string;
  conclusion: string;
  startedAt: string;
  completedAt: string;
  url: string;
  checkRunUrl: string;
  runnerName: string;
  steps: WorkflowStep[] | null;
}
export interface WorkflowRunsRequest {
  workflowId: number;
  branch: string;
  status: string;
  page: number;
}
export interface WorkflowRunsPage {
  items: WorkflowRun[] | null;
  totalCount: number;
  hasMore: boolean;
}
export interface WorkflowRunDetail {
  run: WorkflowRun;
  jobs: WorkflowJob[] | null;
}
export interface WorkflowJobLog {
  job: WorkflowJob;
  state: "available" | "pending" | "unavailable";
  message: string;
  text: string;
  truncated: boolean;
  tail: boolean;
}
export type WorkflowAction = "rerun" | "rerun-failed" | "rerun-job" | "cancel";
export interface WorkflowMutation {
  runId: number;
  expectedAttempt: number;
  jobId: number;
  action: WorkflowAction;
}
export interface WorkflowInput {
  name: string;
  description: string;
  type: "string" | "boolean" | "number" | "choice" | "environment";
  required: boolean;
  default: string;
  options: string[] | null;
}
export interface WorkflowDispatch {
  workflow: Workflow;
  ref: string;
  enabled: boolean;
  inputs: WorkflowInput[] | null;
}
export interface WorkflowDispatchRequest {
  workflowId: number;
  ref: string;
  inputs: Record<string, string>;
}
