import { configureCurrentUser, registerPerson, people } from "./data";
import type { ChangedFile, Check, Comment, Commit, DiffLine, MergeMethod, PullRequest, Review, ReviewDecision, Thread } from "./data";
import { services } from "./services";
import {allowsReview} from './reviewCapabilities';
import type { AccountStatus, GitHubChangedFile, GitHubComment, GitHubThread, GitHubUser, PullRequestDetail, PullRequestSummary, RepositoryDiff, RepositoryIdentity, RepositorySnapshot, UpdatePullRequestRequest } from "./services";

export interface ProviderCounts {
  additions: number;
  deletions: number;
  changedFiles: number;
  commits: number;
  comments: number;
  reviewComments: number;
  threads: number;
}

export interface LiveProvider extends NonNullable<PullRequest["provider"]> {
  counts: ProviderCounts;
  detailLoaded: boolean;
  viewerCanUpdate?: boolean;
  countsKnown: boolean;
}

export interface LiveThread extends Thread {
  viewerCanResolve: boolean;
  viewerCanUnresolve: boolean;
}

export interface LivePullRequest extends PullRequest {
  provider: LiveProvider;
  threads: LiveThread[];
}

export interface LocalChange extends ChangedFile {
  staged: boolean;
  unstaged: boolean;
  conflict: boolean;
  untracked: boolean;
  indexStatus: string;
  worktreeStatus: string;
  submodule?: string;
}

function initialsFor(name: string) {
  const words = name.trim().split(/\s+/).filter(Boolean);
  return (words.length > 1 ? words.slice(0, 2).map(word => Array.from(word)[0]).join("") : Array.from(words[0] ?? "?").slice(0, 2).join("")).toUpperCase();
}

function colorFor(id: string) {
  let hash = 0;
  for (const character of id) hash = ((hash << 5) - hash + character.charCodeAt(0)) | 0;
  const hue = ((hash % 360) + 360) % 360;
  return `oklch(0.58 0.105 ${hue})`;
}

export function registerGitHubUser(user: GitHubUser): string {
  const id = user.login || "unknown-github-user";
  const name = user.name || people[id]?.name || user.login || "Unknown author";
  registerPerson(id, { name, initials: initialsFor(name.replace(/\[bot\]/gi,"")), color: colorFor(id), ...(user.avatarUrl?{avatarUrl:user.avatarUrl}:{}), bot:/\[bot\]/i.test(user.login)||/\[bot\]/i.test(name) });
  return id;
}

export function configureGitHubAccount(account: AccountStatus): void {
  if (!account.authenticated || !account.login) throw new Error("This account is not authenticated.");
  const name = account.name || account.login;
  configureCurrentUser(account.login, { name, initials: initialsFor(name), color: colorFor(account.login), ...(account.avatarUrl?{avatarUrl:account.avatarUrl}:{}) });
}

function registerGitAuthor(name: string, email = "") {
  const id = email ? `git:${email.toLowerCase()}` : `git-name:${name || "unknown"}`;
  const label = name || email || "Unknown author";
  registerPerson(id, { name: label, initials: initialsFor(label), color: colorFor(id) });
  return id;
}

function providerFor(summary: PullRequestSummary, identity?: RepositoryIdentity): LiveProvider {
  let origin = identity;
  if (!origin) {
    const url = new URL(summary.url);
    const segments = url.pathname.split("/").filter(Boolean).map(decodeURIComponent);
    if(summary.provider==='gitlab') {
      const separator=segments.lastIndexOf('-');
      if(separator<2||segments[separator+1]!=='merge_requests')throw new Error('The merge request URL does not identify a repository.');
      origin={host:url.host,owner:segments.slice(0,separator-1).join('/'),repo:segments[separator-1],kind:'gitlab'};
    } else if(summary.provider==='azure') {
      const separator=segments.indexOf('_git');
      if(separator!==2||!segments[separator+1])throw new Error('The Azure pull request URL does not identify a repository.');
      origin={host:url.host,owner:segments.slice(0,separator).join('/'),repo:segments[separator+1],kind:'azure'};
    } else {
      const pullSegment = segments.lastIndexOf(summary.provider==='bitbucket'?'pull-requests':'pull');
      if (pullSegment < 2) throw new Error("The pull request URL does not identify a repository.");
      origin = { host: url.host, owner: segments[pullSegment - 2], repo: segments[pullSegment - 1] };
    }
  }
  if (!origin.host || !origin.owner || !origin.repo) throw new Error("The pull request repository is incomplete.");
  return {
    ...origin,
    ...(summary.provider?{kind:summary.provider}:{}),
    number: summary.number,
    nodeId: summary.nodeId,
    url: summary.url,
    counts: {
      additions: summary.additions,
      deletions: summary.deletions,
      changedFiles: summary.changedFiles,
      commits: summary.commitsCount,
      comments: summary.commentsCount,
      reviewComments: summary.reviewCommentsCount,
      threads: summary.threadsCount,
    },
    detailLoaded: false,
    countsKnown: summary.countsKnown === true,
    warnings: summary.warnings ?? [],
  };
}

export function mapPullRequestSummary(raw: PullRequestSummary, identity?: RepositoryIdentity): LivePullRequest {
  const provider = providerFor(raw, identity);
  return {
    provider,
    number: raw.number,
    title: raw.title,
    body: raw.body ?? "",
    repository: provider.repo,
    author: registerGitHubUser(raw.author),
    status: raw.merged || raw.status === "merged" ? "merged" : raw.status === "closed" ? "closed" : raw.draft || raw.status === "draft" ? "draft" : "open",
    base: raw.base.ref,
    baseSha: raw.base.sha,
    head: raw.head.ref,
    headSha: raw.head.sha,
    reviewers: (raw.reviewers ?? []).map(registerGitHubUser),
    assignees: (raw.assignees ?? []).map(registerGitHubUser),
    labels: raw.labels ?? [],
    requiredApprovals: 0,
    hasConflicts: raw.mergeable === false || raw.mergeableState === "dirty",
    files: [],
    commits: [],
    checks: [],
    comments: [],
    threads: [],
    reviews: [],
    viewedFiles: [],
    pendingComments: [],
    createdAt: raw.createdAt,
    updatedAt: raw.updatedAt,
  };
}

/** Preserve hunk offsets, including zero-length ranges and gaps between hunks. */
function parsePatch(patch: string): { lines: DiffLine[]; incomplete: boolean } {
  if (/^(?:Binary files .+ differ|GIT binary patch)/m.test(patch)) return { lines: [], incomplete: false };
  const lines: DiffLine[] = [];
  let incomplete = false;
  let oldLine = 0;
  let newLine = 0;
  let inHunk = false;
  let hunkNumber = -1;
  let oldRemaining = 0;
  let newRemaining = 0;
  for (const raw of patch.split("\n")) {
    const hunk = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/.exec(raw);
    if (hunk) {
      if (inHunk && (oldRemaining > 0 || newRemaining > 0)) incomplete = true;
      oldLine = Number(hunk[1]);
      newLine = Number(hunk[3]);
      oldRemaining = hunk[2] === undefined ? 1 : Number(hunk[2]);
      newRemaining = hunk[4] === undefined ? 1 : Number(hunk[4]);
      inHunk = true;
      hunkNumber += 1;
      continue;
    }
    if (!inHunk || raw.startsWith("\\ No newline at end of file")) continue;
    if (oldRemaining === 0 && newRemaining === 0) {
      inHunk = false;
      continue;
    }
    if (raw.startsWith("+") && newRemaining > 0) {
      lines.push({ kind: "add", newLine: newLine++, text: raw.slice(1), hunk: hunkNumber });
      newRemaining -= 1;
    } else if (raw.startsWith("-") && oldRemaining > 0) {
      lines.push({ kind: "remove", oldLine: oldLine++, text: raw.slice(1), hunk: hunkNumber });
      oldRemaining -= 1;
    } else if (raw.startsWith(" ") && oldRemaining > 0 && newRemaining > 0) {
      lines.push({ kind: "context", oldLine: oldLine++, newLine: newLine++, text: raw.slice(1), hunk: hunkNumber });
      oldRemaining -= 1;
      newRemaining -= 1;
    }
  }
  return { lines, incomplete: incomplete || oldRemaining > 0 || newRemaining > 0 };
}

export function parseUnifiedPatch(patch: string): DiffLine[] {
  return parsePatch(patch).lines;
}

function fileStatus(status: string): ChangedFile["status"] {
  if (["added", "A", "copied", "untracked", "?"].includes(status)) return "added";
  if (["deleted", "removed", "D"].includes(status)) return "deleted";
  if (["renamed", "R"].includes(status)) return "renamed";
  return "modified";
}

export function mapChangedFile(raw: GitHubChangedFile): ChangedFile {
  const patch = raw.patch ?? "";
  const binary = raw.binary === true || /^(?:Binary files .+ differ|GIT binary patch)/m.test(patch);
  const parsed = parsePatch(patch);
  return {
    path: raw.path,
    status: fileStatus(raw.status),
    ...(raw.previousPath ? { previousPath: raw.previousPath } : {}),
    additions: raw.additions,
    deletions: raw.deletions,
    lines: binary ? [] : parsed.lines,
    rawPatch: patch,
    binary,
    truncated: raw.truncated === true || parsed.incomplete,
    patchUnavailable: !patch && !binary,
  };
}

function commentId(comment: GitHubComment): string {
  if (comment.id > 0) return String(comment.id);
  if (comment.nodeId) return comment.nodeId;
  throw new Error("GitHub returned a comment without an identifier.");
}

function mapComment(raw: GitHubComment): Comment {
  return { id: commentId(raw), author: registerGitHubUser(raw.user), body: raw.body ?? "", createdAt: raw.createdAt };
}

function mapThread(raw: GitHubThread): LiveThread {
  return {
    id: raw.id,
    path: raw.path,
    line: raw.line ?? raw.originalLine ?? 0,
    side: raw.diffSide === "LEFT" ? "old" : "new",
    resolved: raw.resolved,
    outdated: raw.outdated || raw.line === null,
    headSha: raw.comments?.[0]?.commitId,
    comments: (raw.comments ?? []).map(mapComment),
    viewerCanResolve: raw.viewerCanResolve,
    viewerCanUnresolve: raw.viewerCanUnresolve,
    canResolve: raw.viewerCanResolve,
    canUnresolve: raw.viewerCanUnresolve,
  };
}

function reviewDecision(state: string): ReviewDecision | undefined {
  switch (state.toUpperCase()) {
    case "APPROVED": return "approved";
    case "CHANGES_REQUESTED": return "changes_requested";
    case "COMMENTED": return "commented";
    default: return undefined;
  }
}

function checkDuration(start: string | null, end: string | null) {
  if (!start || !end) return "";
  const seconds = Math.round((new Date(end).getTime() - new Date(start).getTime()) / 1_000);
  if (!Number.isFinite(seconds) || seconds < 0) return "";
  return seconds < 60 ? `${seconds}s` : `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

export function mapPullRequest(raw: PullRequestDetail, identity?: RepositoryIdentity): LivePullRequest {
  const mapped = mapPullRequestSummary(raw.pullRequest, identity);
  const requiredContexts = new Set(raw.requiredStatusCheckContexts ?? []);
  const reviews: Review[] = [];
  for (const review of raw.reviews ?? []) {
    const decision = reviewDecision(review.state);
    if (!decision) continue;
    const id = review.id > 0 ? String(review.id) : review.nodeId;
    if (!id) throw new Error("GitHub returned a review without an identifier.");
    reviews.push({ id, author: registerGitHubUser(review.user), decision, body: review.body ?? "", headSha: review.commitId, createdAt: review.submittedAt });
  }
  const checks: Check[] = (raw.checks ?? []).map(check => ({
    id: `check:${check.id}`,
    name: check.name,
    status: check.status !== "completed" ? "running" : ["success", "neutral", "skipped"].includes(check.conclusion ?? "") ? "passed" : "failed",
    required: check.required === true || requiredContexts.has(check.name),
    duration: checkDuration(check.startedAt, check.completedAt),
    output: [check.output?.title, check.output?.summary, check.output?.text].filter(Boolean),
    detailsUrl: check.detailsUrl,
    checkRunId: check.id,
    appSlug: check.app?.slug,
    rawStatus: check.status,
    conclusion: check.conclusion ?? undefined,
  }));
  for (const status of raw.statuses ?? []) {
    checks.push({ id: `status:${status.id}`, name: status.context, status: status.state === "success" ? "passed" : status.state === "pending" || status.state === "expected" ? "running" : "failed", required: status.required === true || requiredContexts.has(status.context), duration: "", output: status.description ? [status.description] : [], detailsUrl: status.targetUrl, rawStatus: status.state });
  }
  return {
    ...mapped,
    provider: {
      ...mapped.provider,
      detailLoaded: true,
      viewerCanUpdate: raw.viewerCanUpdate,
      capabilities: raw.capabilities,
      requiredStatusCheckContexts: raw.requiredStatusCheckContexts ?? [],
      mergeStateStatus: raw.mergeStateStatus || undefined,
      reviewDecision: raw.reviewDecision || undefined,
      warnings: [...new Set([...(mapped.provider.warnings ?? []), ...(raw.warnings ?? [])])],
      allowedMergeMethods: (raw.allowedMergeMethods ?? []).filter((method): method is MergeMethod => ["merge", "squash", "rebase"].includes(method)),
    },
    files: (raw.files ?? []).map(mapChangedFile),
    commits: (raw.commits ?? []).map(commit => ({ sha: commit.sha, subject: commit.subject, author: commit.author.login ? registerGitHubUser(commit.author) : registerGitAuthor(commit.authorName), createdAt: commit.createdAt })),
    comments: (raw.comments ?? []).map(mapComment),
    threads: (raw.threads ?? []).map(mapThread),
    reviews,
    checks,
    requiredApprovals: raw.requiredApprovals ?? 0,
    ...(raw.milestone ? { milestone: raw.milestone } : {}),
  };
}

export function mapRepositorySnapshot(raw: RepositorySnapshot): {
  root: string;
  name: string;
  branch: string;
  currentBranch: string;
  history: Commit[];
  changes: LocalChange[];
  snapshot: RepositorySnapshot;
} {
  return {
    root: raw.root,
    name: raw.name,
    branch: raw.currentBranch,
    currentBranch: raw.currentBranch,
    snapshot: raw,
    history: (raw.history ?? []).map(commit => ({ sha: commit.sha, subject: commit.subject, author: registerGitAuthor(commit.authorName, commit.authorEmail), createdAt: commit.createdAt, parents: commit.parents ?? [], refs: [...new Set([...(commit.refs ?? []), ...(raw.branches ?? []).filter(branch => branch.commitSha === commit.sha).map(branch => branch.name)])] })),
    changes: (raw.changes ?? []).map(file => ({ path: file.path, status: fileStatus(file.status), ...(file.originalPath ? { previousPath: file.originalPath } : {}), additions: 0, deletions: 0, lines: [], staged: file.staged, unstaged: file.unstaged, conflict: file.conflict, untracked: file.untracked, indexStatus: file.indexStatus, worktreeStatus: file.worktreeStatus, ...(file.submodule ? { submodule: file.submodule } : {}) })),
  };
}

export function mapRepositoryDiff(raw: RepositoryDiff): ChangedFile {
  const parsed = parseUnifiedPatch(raw.rawPatch);
  const hunkForLine = new Map(parsed.map(line => [`${line.kind}:${line.oldLine}:${line.newLine}`, line.hunk]));
  return {
    path: raw.path,
    status: fileStatus(raw.status),
    ...(raw.previousPath ? { previousPath: raw.previousPath } : {}),
    additions: raw.additions,
    deletions: raw.deletions,
    binary: raw.binary,
    truncated: raw.truncated,
    rawPatch: raw.rawPatch,
    lines: raw.binary ? [] : (raw.lines ?? []).map(line => {
      if (!["add", "remove", "context"].includes(line.kind)) throw new Error(`Unknown Git diff line kind: ${line.kind}`);
      const hunk = line.hunk ?? hunkForLine.get(`${line.kind}:${line.oldLine}:${line.newLine}`);
      return { kind: line.kind as DiffLine["kind"], ...(line.oldLine !== undefined ? { oldLine: line.oldLine } : {}), ...(line.newLine !== undefined ? { newLine: line.newLine } : {}), ...(hunk !== undefined ? { hunk } : {}), text: line.text };
    }),
  };
}

function replyCommentId(thread: Thread): number {
  const id = thread.comments[0]?.id;
  const number = id && /^\d+$/.test(id) ? Number(id) : NaN;
  if (!Number.isSafeInteger(number) || number <= 0) throw new Error("This conversation does not have a replyable comment identifier.");
  return number;
}

function samePullRequest(previous: PullRequest, next: PullRequest): RepositoryIdentity & { number: number } {
  const provider = previous.provider;
  if (!provider) throw new Error("This pull request is not connected to a review provider.");
  if (next.number !== previous.number || next.provider?.host !== provider.host || next.provider?.owner !== provider.owner || next.provider?.repo !== provider.repo) throw new Error("The pull request changed before this action could be sent.");
  return { host: provider.host, owner: provider.owner, repo: provider.repo, number: provider.number };
}

function sameMembers(first: string[], second: string[]): boolean {
  const left = new Set(first), right = new Set(second);
  return left.size === right.size && [...left].every(item => right.has(item));
}

export class RemoteActionAppliedError extends Error {
  readonly remoteApplied = true;
  constructor(readonly originalError: unknown) {
    super("The provider accepted the action, but the updated pull request could not be loaded. Refresh before trying the action again.");
    this.name = "RemoteActionAppliedError";
  }
}

/** Translate one local UI mutation into server writes, then fetch canonical state. */
export async function syncPullRequest(previous: PullRequest, next: PullRequest): Promise<PullRequest> {
  const { host, owner, repo, number } = samePullRequest(previous, next);
  if (next.status === "merged" && previous.status !== "merged") throw new Error("Use the merge action to merge this pull request with its expected head commit.");
  let wrote = false;
  const newReviews = next.reviews.filter(review => !previous.reviews.some(item => item.id === review.id));
  const requireCapability=(capability:Parameters<typeof allowsReview>[1])=>{if(!allowsReview(previous,capability))throw new Error('This action is unavailable for this provider or read-only connection.');};
  if(newReviews.length){for(const review of newReviews)requireCapability(review.decision==='approved'?'approve':review.decision==='changes_requested'?'requestChanges':'comments');if(previous.pendingComments.length)requireCapability('inlineComments');}
  if(next.comments.some(comment=>!previous.comments.some(item=>item.id===comment.id)))requireCapability('comments');
  for(const thread of next.threads){const original=previous.threads.find(item=>item.id===thread.id);if(!original)continue;if(thread.resolved!==original.resolved)requireCapability('resolveThreads');if(thread.comments.some(comment=>!original.comments.some(item=>item.id===comment.id)))requireCapability('comments');}
  if(!sameMembers(previous.reviewers,next.reviewers))requireCapability('reviewers');
  if(!sameMembers(previous.assignees,next.assignees))requireCapability('assignees');
  if(!sameMembers(previous.labels,next.labels))requireCapability('labels');
  if(previous.status!==next.status&&(previous.status==='draft'||next.status==='draft'))requireCapability('draft');
  if(previous.status==='closed'&&next.status!=='closed')requireCapability('reopen');
  if((previous.title!==next.title||previous.body!==next.body||previous.status!==next.status)&&previous.provider?.viewerCanUpdate===false)throw new Error('This account cannot edit the pull request.');
  if (newReviews.length > 1) throw new Error("Submit one review at a time.");
  if (!newReviews.length && next.threads.some(thread => !previous.threads.some(item => item.id === thread.id))) throw new Error("New inline comments must be submitted as part of a review.");
  for (const thread of next.threads) {
    const original = previous.threads.find(item => item.id === thread.id);
    if (original && thread.comments.some(comment => !original.comments.some(item => item.id === comment.id))) replyCommentId(original);
  }
  if (newReviews.length) {
    const review = newReviews[0];
    if (previous.pendingComments.some(comment => comment.outdated || (comment.headSha && comment.headSha !== previous.headSha))) throw new Error("A pending comment refers to an older commit. Update or remove it before submitting your review.");
    await services.github.submitReview(host, owner, repo, number, {
      event: review.decision === "approved" ? "APPROVE" : review.decision === "changes_requested" ? "REQUEST_CHANGES" : "COMMENT",
      body: review.body,
      commitId: previous.headSha,
      comments: previous.pendingComments.map(comment => ({ clientId:comment.id, body: comment.body, path: comment.path, line: comment.line, side: comment.side === "old" ? "LEFT" : "RIGHT" })),
    });
    wrote = true;
  }
  for (const comment of next.comments) {
    if (previous.comments.some(item => item.id === comment.id)) continue;
    await services.github.addIssueComment(host, owner, repo, number, comment.body);
    wrote = true;
  }
  for (const thread of next.threads) {
    const original = previous.threads.find(item => item.id === thread.id);
    // New threads created by submitReview are sent atomically with that review.
    if (!original) {
      if (!newReviews.length) throw new Error("New inline comments must be submitted as part of a review.");
      continue;
    }
    for (const comment of thread.comments) {
      if (original.comments.some(item => item.id === comment.id)) continue;
      await services.github.replyReviewComment(host, owner, repo, number, replyCommentId(original), comment.body);
      wrote = true;
    }
    if (thread.resolved !== original.resolved) {
      await services.github.resolveThread(host, original.id, thread.resolved);
      wrote = true;
    }
  }
  const update: UpdatePullRequestRequest = {};
  if (next.title !== previous.title) update.title = next.title;
  if (next.body !== previous.body) update.body = next.body;
  if (next.status === "closed" && previous.status !== "closed") update.state = "closed";
  if (previous.status === "closed" && ["open", "draft"].includes(next.status)) update.state = "open";
  if (Object.keys(update).length) {
    await services.github.updatePullRequest(host, owner, repo, number, update);
    wrote = true;
  }
  if (next.status === "draft" && previous.status !== "draft") {
    await services.github.setDraft(host, owner, repo, number, true);
    wrote = true;
  } else if (previous.status === "draft" && next.status === "open") {
    await services.github.setDraft(host, owner, repo, number, false);
    wrote = true;
  }
  const addedReviewers = next.reviewers.filter(user => !previous.reviewers.includes(user));
  const removedReviewers = previous.reviewers.filter(user => !next.reviewers.includes(user));
  if (removedReviewers.length) {
    await services.github.removeReviewers(host, owner, repo, number, removedReviewers);
    wrote = true;
  }
  if (addedReviewers.length) {
    await services.github.requestReviewers(host, owner, repo, number, addedReviewers);
    wrote = true;
  }
  if (!sameMembers(previous.assignees, next.assignees)) {
    await services.github.setAssignees(host, owner, repo, number, next.assignees);
    wrote = true;
  }
  if (!sameMembers(previous.labels, next.labels)) {
    await services.github.setLabels(host, owner, repo, number, next.labels);
    wrote = true;
  }
  if (!wrote) return next;
  let canonical: LivePullRequest;
  try {
    canonical = mapPullRequest(await services.github.getPullRequest(host, owner, repo, number), { host, owner, repo });
  } catch (issue) {
    throw new RemoteActionAppliedError(issue);
  }
  return {
    ...canonical,
    viewedFiles: next.viewedFiles.filter(path => canonical.files.some(file => file.path === path)),
    pendingComments: next.pendingComments.map(comment => ({ ...comment, ...(canonical.headSha !== previous.headSha ? { outdated: true } : {}) })),
  };
}
