import type { WorkflowJob } from "./actionsTypes.ts";

export interface WorkflowJobComparison {
  name: string;
  before: WorkflowJob[];
  after: WorkflowJob[];
  ambiguous: boolean;
  durationChange?: number;
}

function duration(job?: WorkflowJob): number | undefined {
  if (!job?.startedAt || !job.completedAt) return;
  const seconds =
    (Date.parse(job.completedAt) - Date.parse(job.startedAt)) / 1000;
  return Number.isFinite(seconds) && seconds >= 0 ? seconds : undefined;
}

// Display repeated names as groups. Provider job IDs differ between runs, so
// array order cannot reliably identify corresponding jobs in such a group.
export function compareWorkflowJobs(
  before: WorkflowJob[],
  after: WorkflowJob[],
): WorkflowJobComparison[] {
  const groups = new Map<string, WorkflowJobComparison>();
  for (const [side, jobs] of [
    ["before", before],
    ["after", after],
  ] as const) {
    for (const job of jobs) {
      let group = groups.get(job.name);
      if (!group) {
        group = { name: job.name, before: [], after: [], ambiguous: false };
        groups.set(job.name, group);
      }
      group[side].push(job);
    }
  }
  for (const group of groups.values()) {
    group.ambiguous = group.before.length > 1 || group.after.length > 1;
    if (group.ambiguous) continue;
    const previous = duration(group.before[0]),
      current = duration(group.after[0]);
    if (previous !== undefined && current !== undefined)
      group.durationChange = current - previous;
  }
  return [...groups.values()];
}
