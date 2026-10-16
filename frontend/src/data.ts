export type UserId = string;
export let currentUser: UserId = "noah";
export interface Person {name:string; initials:string; color:string; avatarUrl?:string; bot?:boolean}
export const people: Record<UserId, Person> = {
  noah: { name: "Noah", initials: "N", color: "#5f7dbd" },
  elena: { name: "Elena Chen", initials: "EC", color: "#b27653" },
  marcus: { name: "Marcus Lee", initials: "ML", color: "#568b7d" },
  ravi: { name: "Ravi Shah", initials: "RS", color: "#8b6daf" },
};

export function registerPerson(id: string, profile: Person) {
  people[id] = {...people[id], ...profile};
}
export function configureCurrentUser(id: string, profile?: Person) {
  currentUser = id;
  if (profile) registerPerson(id, profile);
}

export type ReviewDecision = "approved" | "changes_requested" | "commented";
export type PRStatus = "open" | "draft" | "closed" | "merged";
export type ReviewTab = "conversation" | "changes" | "commits" | "checks";
export type MergeMethod = "merge" | "squash" | "rebase";

export interface Comment {
  id: string;
  author: UserId;
  body: string;
  createdAt: string;
}
export interface Thread {
  canResolve?: boolean;
  canUnresolve?: boolean;
  headSha?: string;
  outdated?: boolean;
  id: string;
  path: string;
  line: number;
  side: "old" | "new";
  resolved: boolean;
  comments: Comment[];
}
export interface Review {
  id: string;
  author: UserId;
  decision: ReviewDecision;
  body: string;
  headSha: string;
  createdAt: string;
}
export interface Check {
  id: string;
  name: string;
  status: "passed" | "failed" | "running";
  required: boolean;
  duration: string;
  output: string[];
  detailsUrl?: string;
  checkRunId?: number;
  appSlug?: string;
  rawStatus?: string;
  conclusion?: string;
}
export interface DiffLine {
  hunk?: number;
  kind: "context" | "add" | "remove";
  oldLine?: number;
  newLine?: number;
  text: string;
}
export interface ChangedFile {
  binary?: boolean;
  truncated?: boolean;
  patchUnavailable?: boolean;
  rawPatch?: string;
  path: string;
  status: "added" | "modified" | "deleted" | "renamed";
  previousPath?: string;
  additions: number;
  deletions: number;
  lines: DiffLine[];
}
export interface Commit {
  parents?: string[];
  refs?: string[];
  sha: string;
  subject: string;
  author: UserId;
  createdAt: string;
}
export interface PendingComment {
  ai?: {runId:string;provider:'codex'|'copilot'|'external';model:string;severity:'bug'|'warning'|'suggestion'};
  headSha?: string;
  outdated?: boolean;
  id: string;
  path: string;
  line: number;
  side: "old" | "new";
  body: string;
}
export interface PullRequest {
  checkHeadSha?: string;
  provider?: {
    kind?:import('./services').ReviewProviderKind; capabilities?:import('./services').ReviewCapabilities;
    host: string; owner: string; repo: string; number: number;
    nodeId?: string; url?: string; mergeStateStatus?: string; reviewDecision?: string;
    hasNewCommits?: boolean; detailLoaded?:boolean; countsKnown?:boolean; viewerCanUpdate?:boolean; requiredStatusCheckContexts?:string[]; counts?:{additions:number;deletions:number;changedFiles:number;commits:number;comments:number;reviewComments:number;threads?:number}; warnings?: string[]; allowedMergeMethods?: MergeMethod[];
  };
  number: number;
  title: string;
  body: string;
  repository: string;
  author: UserId;
  status: PRStatus;
  base: string;
  baseSha?: string;
  head: string;
  headSha: string;
  reviewers: UserId[];
  assignees: UserId[];
  labels: string[];
  milestone?: string;
  requiredApprovals: number;
  hasConflicts: boolean;
  files: ChangedFile[];
  commits: Commit[];
  checks: Check[];
  comments: Comment[];
  threads: Thread[];
  reviews: Review[];
  viewedFiles: string[];
  pendingComments: PendingComment[];
  createdAt: string;
  updatedAt: string;
  merge?: { method: MergeMethod; sha: string };
}

// Checks describe the current head. Publishing another head must replace them.
// A comment-only review does not revoke an earlier approval or change request.
export function latestReview(
  pr: PullRequest,
  user: UserId,
): Review | undefined {
  const reviews = pr.reviews
    .filter((review) => review.author === user && review.headSha === pr.headSha)
    .map((review, index) => ({ review, index }))
    .sort(
      (a, b) =>
        a.review.createdAt.localeCompare(b.review.createdAt) ||
        a.index - b.index,
    );
  const decisive = reviews.filter(
    ({ review }) => review.decision !== "commented",
  );
  return (
    decisive[decisive.length - 1]?.review ?? reviews[reviews.length - 1]?.review
  );
}

export function mergeBlockers(pr: PullRequest): string[] {
  if (pr.status === "merged")
    return ["This pull request has already been merged."];
  if (pr.status === "closed")
    return ["Reopen this pull request before merging."];

  const blockers: string[] = [];
  if (pr.provider?.capabilities?.merge === false) blockers.push("Complete the merge in your provider.");
  if (pr.provider?.hasNewCommits) blockers.push("New commits are available. Refresh the review before merging.");
  if (pr.provider?.reviewDecision === "REVIEW_REQUIRED") blockers.push("Additional review approval is required.");
  if (pr.provider?.mergeStateStatus === "BLOCKED") blockers.push("Branch protection requirements are not yet satisfied.");
  if (pr.provider?.mergeStateStatus === "BEHIND") blockers.push("Update this branch with the latest base branch changes.");
  if (pr.provider?.mergeStateStatus === "UNKNOWN") blockers.push("The provider is still calculating merge readiness.");
  if (pr.status === "draft")
    blockers.push("Mark this pull request ready for review.");
  if (pr.hasConflicts)
    blockers.push("Resolve merge conflicts with " + pr.base + ".");
  const failed = pr.checks.filter(
    (check) => check.required && check.status === "failed",
  );
  const running = pr.checks.filter(
    (check) => check.required && check.status === "running",
  );
  if (failed.length)
    blockers.push(
      "Required checks failed: " +
        failed.map((check) => check.name).join(", ") +
        ".",
    );
  if (running.length)
    blockers.push(
      "Required checks are running: " +
        running.map((check) => check.name).join(", ") +
        ".",
    );

  const reviewers = [
    ...new Set(pr.reviews.map((review) => review.author)),
  ].filter((user) => user !== pr.author);
  const reviews = reviewers
    .map((user) => latestReview(pr, user))
    .filter((review): review is Review => Boolean(review));
  const requested = reviews.filter(
    (review) => review.decision === "changes_requested",
  );
  if (requested.length)
    blockers.push(
      "Changes requested by " +
        requested.map((review) => people[review.author].name).join(", ") +
        ".",
    );
  const approvals = reviews.filter(
    (review) => review.decision === "approved",
  ).length;
  const missing = Math.max(0, pr.requiredApprovals - approvals);
  if (missing)
    blockers.push(
      missing +
        (missing === 1
          ? " more approval required."
          : " more approvals required."),
    );
  const unresolved = pr.threads.filter((thread) => !thread.resolved).length;
  if (unresolved)
    blockers.push(
      unresolved +
        (unresolved === 1
          ? " conversation must be resolved."
          : " conversations must be resolved."),
    );
  return blockers;
}

function nextId(prefix: string, ids: string[]): string {
  const existing = new Set(ids);
  let suffix = 1;
  while (existing.has(prefix + "-" + suffix)) suffix += 1;
  return prefix + "-" + suffix;
}

export function submitReview(
  pr: PullRequest,
  decision: ReviewDecision,
  body: string,
): PullRequest {
  if (!["approved", "changes_requested", "commented"].includes(decision))
    throw new Error("Choose a review decision.");
  if (pr.status === "closed" || pr.status === "merged")
    throw new Error(
      "Reviews cannot be submitted to a closed or merged pull request.",
    );
  if (pr.status === "draft" && decision !== "commented")
    throw new Error(
      "Mark this pull request ready before submitting a formal review.",
    );
  if (pr.author === currentUser && decision === "approved")
    throw new Error("You cannot approve your own pull request.");
  const trimmedBody = body.trim();
  if (decision === "changes_requested" && !trimmedBody)
    throw new Error("Explain the changes you are requesting.");
  if (decision === "commented" && !trimmedBody && !pr.pendingComments.length)
    throw new Error("Add a review comment or an inline comment.");

  for (const pending of pr.pendingComments) {
    if (pending.outdated || (pending.headSha && pending.headSha !== pr.headSha)) throw new Error("Some draft comments refer to earlier commits. Reposition or remove them before submitting.");
    if (!pending.body.trim())
      throw new Error("Inline comments cannot be empty.");
    const file = pr.files.find((candidate) => candidate.path === pending.path);
    const exists = file?.lines.some((line) =>
      pending.side === "new"
        ? line.newLine === pending.line
        : line.oldLine === pending.line,
    );
    if (!exists)
      throw new Error("An inline comment no longer matches this diff.");
  }

  const next = structuredClone(pr);
  const createdAt = new Date().toISOString();
  for (const pending of next.pendingComments) {
    const threadId = nextId(
      "thread-" + pr.number,
      next.threads.map((thread) => thread.id),
    );
    next.threads.push({
      id: threadId,
      path: pending.path,
      line: pending.line,
      side: pending.side,
      resolved: false,
      comments: [
        {
          id: threadId + "-comment-1",
          author: currentUser,
          body: pending.body.trim(),
          createdAt,
        },
      ],
    });
  }
  next.reviews.push({
    id: nextId(
      "review-" + pr.number,
      next.reviews.map((review) => review.id),
    ),
    author: currentUser,
    decision,
    body: trimmedBody,
    headSha: next.headSha,
    createdAt,
  });
  next.pendingComments = [];
  next.updatedAt = createdAt;
  return next;
}

function sampleMergeSha(pr: PullRequest, method: MergeMethod): string {
  let hash = 2166136261;
  for (const character of pr.number + ":" + pr.headSha + ":" + method) {
    hash = Math.imul(hash ^ character.charCodeAt(0), 16777619);
  }
  return (hash >>> 0).toString(16).padStart(8, "0").slice(0, 7);
}

export function mergePullRequest(
  pr: PullRequest,
  method: MergeMethod,
): PullRequest {
  if (!["merge", "squash", "rebase"].includes(method))
    throw new Error("Choose a merge method.");
  const blockers = mergeBlockers(pr);
  if (blockers.length) throw new Error(blockers.join(" "));
  const next = structuredClone(pr);
  next.status = "merged";
  next.merge = { method, sha: sampleMergeSha(pr, method) };
  next.updatedAt = new Date().toISOString();
  return next;
}

// Fixture patches have a prefix on every line, just like a unified diff.
// Counts and line anchors are derived from the patch, never duplicated by hand.
function file(
  path: string,
  status: ChangedFile["status"],
  patch: string,
  previousPath?: string,
): ChangedFile {
  let oldLine = 1;
  let newLine = 1;
  const lines: DiffLine[] = patch
    .replace(/^\n|\n$/g, "")
    .split("\n")
    .map((raw) => {
      const text = raw.slice(1);
      if (raw[0] === "+") return { kind: "add", newLine: newLine++, text };
      if (raw[0] === "-") return { kind: "remove", oldLine: oldLine++, text };
      if (raw[0] !== " ")
        throw new Error("Invalid fixture diff prefix in " + path);
      return { kind: "context", oldLine: oldLine++, newLine: newLine++, text };
    });
  return {
    path,
    status,
    ...(previousPath ? { previousPath } : {}),
    lines,
    additions: lines.filter((line) => line.kind === "add").length,
    deletions: lines.filter((line) => line.kind === "remove").length,
  };
}

const keyboardFiles: ChangedFile[] = [
  file(
    "frontend/src/components/FileTree.tsx",
    "modified",
    `
 import { useEffect, useMemo, useRef, useState } from 'react';
 import { ChevronRight, FileCode, Folder } from '../icons';
 import { flattenVisibleNodes } from '../lib/file-tree';
+import { useTreeKeyboard } from '../hooks/useTreeKeyboard';
 import type { TreeNode } from '../lib/file-tree';
 
 interface FileTreeProps {
   nodes: TreeNode[];
   selectedPath: string | null;
   onSelect: (path: string) => void;
 }
 
 export function FileTree({ nodes, selectedPath, onSelect }: FileTreeProps) {
   const rootRef = useRef<HTMLDivElement>(null);
   const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
-  const [focusedIndex, setFocusedIndex] = useState(0);
+  const [focusedPath, setFocusedPath] = useState<string | null>(selectedPath);
   const visibleNodes = useMemo(
     () => flattenVisibleNodes(nodes, expanded),
     [nodes, expanded],
   );
 
   useEffect(() => {
-    setFocusedIndex(0);
+    if (visibleNodes.some((node) => node.path === focusedPath)) return;
+    setFocusedPath(visibleNodes[0]?.path ?? null);
-  }, [visibleNodes]);
+  }, [focusedPath, visibleNodes]);
 
   function toggleFolder(path: string) {
     setExpanded((previous) => {
       const next = new Set(previous);
       if (next.has(path)) next.delete(path);
       else next.add(path);
       return next;
     });
   }
 
-  function handleKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
-    if (event.key !== 'ArrowDown') return;
-    event.preventDefault();
-    const index = Math.min(focusedIndex + 1, visibleNodes.length - 1);
-    setFocusedIndex(index);
-    onSelect(visibleNodes[index].path);
+  function focusPath(path: string) {
+    setFocusedPath(path);
+    const selector = '[data-path="' + CSS.escape(path) + '"]';
+    rootRef.current?.querySelector<HTMLElement>(selector)?.focus();
   }
 
+  useEffect(() => {
+    const root = rootRef.current;
+    if (!root?.contains(document.activeElement) || !focusedPath) return;
+    const selector = '[data-path="' + CSS.escape(focusedPath) + '"]';
+    root.querySelector<HTMLElement>(selector)?.focus();
+  }, [focusedPath, visibleNodes]);
+
+  const handleKeyDown = useTreeKeyboard({
+    entries: visibleNodes,
+    focusedPath,
+    onFocus: focusPath,
+    onSelect,
+    onToggle: toggleFolder,
+  });
+
   return (
-    <div ref={rootRef} className="file-tree" onKeyDown={handleKeyDown}>
+    <div ref={rootRef} className="file-tree" role="tree" aria-label="Changed files" onKeyDown={handleKeyDown}>
       {visibleNodes.map((node) => (
         <button
           key={node.path}
           type="button"
           className="file-tree__row"
+          role="treeitem"
+          data-path={node.path}
+          tabIndex={node.path === focusedPath ? 0 : -1}
+          aria-selected={node.path === selectedPath}
+          aria-expanded={node.isFolder ? expanded.has(node.path) : undefined}
+          onFocus={() => setFocusedPath(node.path)}
           style={{ paddingLeft: 12 + node.depth * 16 }}
           onClick={() => node.isFolder ? toggleFolder(node.path) : onSelect(node.path)}
         >
           {node.isFolder ? <Folder /> : <FileCode />}
           <span>{node.name}</span>
           {node.isFolder && <ChevronRight className={expanded.has(node.path) ? 'expanded' : ''} />}
         </button>
       ))}
     </div>
   );
 }
`,
  ),
  file(
    "frontend/src/hooks/useTreeKeyboard.ts",
    "added",
    `
+import { useCallback } from 'react';
+import type { KeyboardEvent } from 'react';
+
+export interface TreeEntry {
+  path: string;
+  parentPath?: string;
+  isFolder: boolean;
+  expanded?: boolean;
+}
+
+interface Options {
+  entries: readonly TreeEntry[];
+  focusedPath: string | null;
+  onFocus: (path: string) => void;
+  onSelect: (path: string) => void;
+  onToggle: (path: string) => void;
+}
+
+export function useTreeKeyboard(options: Options) {
+  const { entries, focusedPath, onFocus, onSelect, onToggle } = options;
+  return useCallback((event: KeyboardEvent<HTMLDivElement>) => {
+    if (event.altKey || event.ctrlKey || event.metaKey || !entries.length) return;
+    const index = Math.max(0, entries.findIndex((entry) => entry.path === focusedPath));
+    const current = entries[index];
+    let destination: TreeEntry | undefined;
+    switch (event.key) {
+      case 'ArrowDown':
+        destination = entries[Math.min(index + 1, entries.length - 1)];
+        break;
+      case 'ArrowUp':
+        destination = entries[Math.max(index - 1, 0)];
+        break;
+      case 'Home': destination = entries[0]; break;
+      case 'End': destination = entries[entries.length - 1]; break;
+      case 'ArrowRight':
+        if (current.isFolder && !current.expanded) onToggle(current.path);
+        else if (current.isFolder) destination = entries[index + 1];
+        break;
+      case 'ArrowLeft':
+        if (current.isFolder && current.expanded) onToggle(current.path);
+        else destination = entries.find((entry) => entry.path === current.parentPath);
+        break;
+      case 'Enter':
+      case ' ': current.isFolder ? onToggle(current.path) : onSelect(current.path); break;
+      default: return;
+    }
+    event.preventDefault();
+    if (destination) onFocus(destination.path);
+  }, [entries, focusedPath, onFocus, onSelect, onToggle]);
+}
`,
  ),
  file(
    "frontend/src/components/DiffView.tsx",
    "modified",
    `
 import { useEffect, useRef } from 'react';
 import type { ChangedFile, DiffLine } from '../types';
 import { DiffToolbar } from './DiffToolbar';
 import { DiffRow } from './DiffRow';
 
 interface DiffViewProps {
   file: ChangedFile;
   mode: 'split' | 'unified';
   onModeChange: (mode: 'split' | 'unified') => void;
   onComment: (line: DiffLine) => void;
+  returnFocusToTree: () => void;
 }
 
 export function DiffView(props: DiffViewProps) {
   const { file, mode, onModeChange, onComment } = props;
   const viewport = useRef<HTMLDivElement>(null);
 
   useEffect(() => {
     viewport.current?.scrollTo({ top: 0 });
   }, [file.path]);
 
+  function handleKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
+    if (event.key !== 'Escape') return;
+    event.preventDefault();
+    props.returnFocusToTree();
+  }
+
   return (
-    <section className="diff-view">
+    <section className="diff-view" aria-label={'Changes in ' + file.path}>
       <DiffToolbar
         path={file.path}
         additions={file.additions}
         deletions={file.deletions}
         mode={mode}
         onModeChange={onModeChange}
       />
       <div
         ref={viewport}
         className="diff-view__viewport"
-        tabIndex={-1}
+        tabIndex={0}
+        onKeyDown={handleKeyDown}
         role="region"
-        aria-label="Code"
+        aria-label={'Code diff for ' + file.path}
       >
         <table className={'diff-table diff-table--' + mode}>
           <tbody>
             {file.lines.map((line, index) => (
               <DiffRow
                 key={file.path + ':' + index}
                 line={line}
                 mode={mode}
                 onComment={() => onComment(line)}
               />
             ))}
           </tbody>
         </table>
       </div>
     </section>
   );
 }
`,
  ),
  file(
    "frontend/src/styles/file-tree.css",
    "modified",
    `
 .file-tree {
   min-width: 180px;
   overflow: auto;
   padding: 4px 0;
 }
 
 .file-tree__row {
   display: flex;
   align-items: center;
   gap: 6px;
   width: 100%;
   min-height: 26px;
   padding: 4px 12px;
   border: 0;
   border-radius: 0;
   background: transparent;
   color: var(--text-primary);
   text-align: left;
   font: inherit;
-  outline: none;
+  outline-offset: -2px;
 }
 
 .file-tree__row:hover {
   background: var(--surface-hover);
 }
 
-.file-tree__row.selected {
+.file-tree__row[aria-selected='true'] {
   background: var(--selection-background);
   color: var(--selection-text);
 }
 
+.file-tree__row:focus-visible {
+  outline: 2px solid var(--focus-ring);
+}
+
+.file-tree__row[aria-expanded='true'] .expanded {
+  transform: rotate(90deg);
+}
+
+.file-tree__row > span {
+  min-width: 0;
+  overflow: hidden;
+  text-overflow: ellipsis;
+  white-space: nowrap;
+}
+
 .file-tree__row svg {
   flex-shrink: 0;
   width: 14px;
   height: 14px;
 }
 
 .file-tree__row .expanded {
   margin-left: auto;
 }
`,
  ),
  file(
    "frontend/src/hooks/useTreeKeyboard.test.ts",
    "added",
    `
+import { act, renderHook } from '@testing-library/react';
+import { describe, expect, it, vi } from 'vitest';
+import { useTreeKeyboard } from './useTreeKeyboard';
+import type { TreeEntry } from './useTreeKeyboard';
+
+const entries: TreeEntry[] = [
+  { path: 'src', isFolder: true, expanded: true },
+  { path: 'src/App.tsx', parentPath: 'src', isFolder: false },
+  { path: 'src/main.tsx', parentPath: 'src', isFolder: false },
+];
+
+function setup(focusedPath = entries[0].path) {
+  const onFocus = vi.fn();
+  const onSelect = vi.fn();
+  const onToggle = vi.fn();
+  const { result } = renderHook(() => useTreeKeyboard({
+    entries, focusedPath, onFocus, onSelect, onToggle,
+  }));
+  function press(key: string) {
+    const event = { key, preventDefault: vi.fn() };
+    act(() => result.current(event as never));
+    return event;
+  }
+  return { press, onFocus, onSelect, onToggle };
+}
+
+describe('tree keyboard navigation', () => {
+  it('moves focus without changing the selected file', () => {
+    const tree = setup();
+    expect(tree.press('ArrowDown').preventDefault).toHaveBeenCalled();
+    expect(tree.onFocus).toHaveBeenCalledWith('src/App.tsx');
+    expect(tree.onSelect).not.toHaveBeenCalled();
+  });
+
+  it.each([['Home', 'src'], ['End', 'src/main.tsx']])(
+    'handles %s from the middle of the tree', (key, path) => {
+      const tree = setup('src/App.tsx');
+      tree.press(key);
+      expect(tree.onFocus).toHaveBeenCalledWith(path);
+    },
+  );
+
+  it('selects a focused file on Enter', () => {
+    const tree = setup('src/App.tsx');
+    tree.press('Enter');
+    expect(tree.onSelect).toHaveBeenCalledWith('src/App.tsx');
+  });
+
+  it('moves to the parent folder on ArrowLeft', () => {
+    const tree = setup('src/App.tsx');
+    tree.press('ArrowLeft');
+    expect(tree.onFocus).toHaveBeenCalledWith('src');
+  });
+});
`,
  ),
  file(
    "frontend/src/components/FileTree.test.tsx",
    "modified",
    `
 import { fireEvent, render, screen } from '@testing-library/react';
 import { describe, expect, it, vi } from 'vitest';
 import { FileTree } from './FileTree';
 import type { TreeNode } from '../lib/file-tree';
 
 const nodes: TreeNode[] = [
   { path: 'src', name: 'src', depth: 0, isFolder: true, children: [
     { path: 'src/App.tsx', name: 'App.tsx', depth: 1, isFolder: false },
     { path: 'src/main.tsx', name: 'main.tsx', depth: 1, isFolder: false },
   ] },
 ];
 
 function renderTree(selectedPath = 'src/App.tsx') {
   return render(<FileTree nodes={nodes} selectedPath={selectedPath} onSelect={vi.fn()} />);
 }
 
 describe('FileTree', () => {
   it('opens a folder when clicked', () => {
     renderTree();
     fireEvent.click(screen.getByText('src'));
     expect(screen.getByText('App.tsx')).toBeVisible();
   });
 
+  it('exposes the selected file to assistive technology', () => {
+    renderTree();
+    fireEvent.click(screen.getByText('src'));
+    expect(screen.getByRole('treeitem', { name: 'App.tsx' }))
+      .toHaveAttribute('aria-selected', 'true');
+  });
+
+  it('retains focus when refreshed nodes have the same paths', () => {
+    const view = renderTree();
+    fireEvent.click(screen.getByText('src'));
+    const selected = screen.getByRole('treeitem', { name: 'App.tsx' });
+    selected.focus();
+    const refreshed = structuredClone(nodes);
+    view.rerender(<FileTree nodes={refreshed} selectedPath="src/App.tsx" onSelect={vi.fn()} />);
+    expect(screen.getByRole('treeitem', { name: 'App.tsx' })).toHaveFocus();
+  });
+
+  it('keeps one visible item in the tab order', () => {
+    renderTree();
+    fireEvent.click(screen.getByText('src'));
+    const tabStops = screen.getAllByRole('treeitem').filter((item) => item.tabIndex === 0);
+    expect(tabStops).toHaveLength(1);
+  });
+
+  it('returns focus to a parent when a folder collapses', () => {
+    renderTree();
+    fireEvent.click(screen.getByText('src'));
+    const child = screen.getByRole('treeitem', { name: 'App.tsx' });
+    child.focus();
+    fireEvent.keyDown(child, { key: 'ArrowLeft' });
+    expect(screen.getByRole('treeitem', { name: 'src' })).toHaveFocus();
+  });
+
   it('does not select a folder as a file', () => {
     const onSelect = vi.fn();
     render(<FileTree nodes={nodes} selectedPath={null} onSelect={onSelect} />);
     fireEvent.click(screen.getByText('src'));
     expect(onSelect).not.toHaveBeenCalled();
   });
 });
`,
  ),
];
const otherFiles: Record<string, ChangedFile[]> = {
  restore: [
    file(
      "frontend/src/lib/repository-session.ts",
      "modified",
      `
 import type { Repository } from '../types';
 import { settings } from './settings';
 
 export function restoreRepository(repositories: Repository[]) {
-  return repositories[0] ?? null;
+  const savedPath = settings.get<string>('session.repository');
+  return repositories.find((repository) => repository.path === savedPath)
+    ?? repositories[0]
+    ?? null;
 }
 
 export function saveRepository(repository: Repository) {
+  settings.set('session.repository', repository.path);
   settings.set('session.lastOpenedAt', new Date().toISOString());
 }
`,
    ),
  ],
  theme: [
    file(
      "frontend/src/hooks/useSystemTheme.ts",
      "added",
      `
+import { useEffect, useState } from 'react';
+
+export function useSystemTheme() {
+  const [dark, setDark] = useState(
+    () => window.matchMedia('(prefers-color-scheme: dark)').matches,
+  );
+  useEffect(() => {
+    const query = window.matchMedia('(prefers-color-scheme: dark)');
+    const update = (event: MediaQueryListEvent) => setDark(event.matches);
+    query.addEventListener('change', update);
+    return () => query.removeEventListener('change', update);
+  }, []);
+  return dark ? 'dark' : 'light';
+}
`,
    ),
  ],
  panels: [
    file(
      "frontend/src/lib/panel-layout.ts",
      "modified",
      `
 import { settings } from './settings';
 
 export const defaultLayout = { sidebar: 240, inspector: 280 };
 
 export function loadLayout() {
-  return defaultLayout;
+  const stored = settings.get<Partial<typeof defaultLayout>>('panels');
+  return {
+    sidebar: Math.max(180, Math.min(stored?.sidebar ?? 240, 420)),
+    inspector: Math.max(220, Math.min(stored?.inspector ?? 280, 440)),
+  };
 }
 
+export function saveLayout(layout: typeof defaultLayout) {
+  settings.set('panels', layout);
+}
`,
    ),
  ],
  retry: [
    file(
      "internal/git/remote.go",
      "modified",
      `
 package git
 
 import (
     "context"
     "errors"
+    "time"
 )
 
 func (r *Repository) Fetch(ctx context.Context, remote string) error {
-    return r.run(ctx, "fetch", "--prune", remote)
+    for attempt := 0; attempt < 3; attempt++ {
+        err := r.run(ctx, "fetch", "--prune", remote)
+        if err == nil || !errors.Is(err, ErrInterrupted) {
+            return err
+        }
+        time.Sleep(time.Duration(attempt+1) * time.Second)
+    }
+    return ErrInterrupted
 }
`,
    ),
  ],
  checkbox: [
    file(
      "src/components/Checkbox.tsx",
      "modified",
      `
 import { useEffect, useRef } from 'react';
 import type { InputHTMLAttributes } from 'react';
 
 interface CheckboxProps extends InputHTMLAttributes<HTMLInputElement> {
   indeterminate?: boolean;
 }
 
 export function Checkbox({ indeterminate = false, ...props }: CheckboxProps) {
   const input = useRef<HTMLInputElement>(null);
+  useEffect(() => {
+    if (input.current) input.current.indeterminate = indeterminate;
+  }, [indeterminate]);
   return (
     <input
       ref={input}
       type="checkbox"
-      aria-checked={props.checked}
+      aria-checked={indeterminate ? 'mixed' : props.checked}
       {...props}
     />
   );
 }
`,
    ),
  ],
  badges: [
    file(
      "frontend/src/components/BranchBadge.tsx",
      "added",
      `
+interface BranchBadgeProps {
+  ahead: number;
+  behind: number;
+}
+
+export function BranchBadge({ ahead, behind }: BranchBadgeProps) {
+  if (!ahead && !behind) return null;
+  return (
+    <span className="branch-badge" aria-label={ahead + ' ahead, ' + behind + ' behind'}>
+      {ahead > 0 && <span>↑ {ahead}</span>}
+      {behind > 0 && <span>↓ {behind}</span>}
+    </span>
+  );
+}
`,
    ),
  ],
  rename: [
    file(
      "frontend/src/components/RenamedFileDiff.tsx",
      "renamed",
      `
 import type { ChangedFile } from '../types';
 import { DiffView } from './DiffView';
 
-export function FileDiff({ file }: { file: ChangedFile }) {
+export function RenamedFileDiff({ file }: { file: ChangedFile }) {
   return (
     <section>
-      <h2>{file.path}</h2>
+      <h2>
+        {file.previousPath && <span className="muted">{file.previousPath} → </span>}
+        {file.path}
+      </h2>
       <DiffView file={file} />
     </section>
   );
 }
`,
      "frontend/src/components/FileDiff.tsx",
    ),
  ],
};
function checks(state: "passed" | "failed" | "running" = "passed"): Check[] {
  return [
    {
      id: "typecheck",
      name: "Typecheck",
      status: "passed",
      required: true,
      duration: "27s",
      output: [
        "Checkout completed.",
        "npm ci",
        "npx tsc --noEmit",
        "No TypeScript errors found.",
      ],
    },
    {
      id: "tests",
      name: "Frontend tests",
      status: state === "failed" ? "failed" : "passed",
      required: true,
      duration: state === "failed" ? "1m 09s" : "1m 14s",
      output:
        state === "failed"
          ? [
              "RUN remote.test.ts",
              "PASS retries an interrupted operation",
              "FAIL cancels retry backoff on abort",
              "Expected the operation to stop within 50ms; received 1038ms.",
              "1 failed · 48 passed",
            ]
          : [
              "RUN tree-keyboard.test.ts",
              "PASS preserves focus on refresh",
              "PASS handles Arrow keys, Home and End",
              "PASS returns focus to parent folders",
              "49 passed · 0 failed",
            ],
    },
    {
      id: "desktop",
      name: "Desktop build matrix",
      status: state === "running" ? "running" : "passed",
      required: true,
      duration: state === "running" ? "2m 10s" : "3m 05s",
      output:
        state === "running"
          ? [
              "Linux amd64: passed (2m 43s)",
              "macOS arm64: passed (2m 58s)",
              "Windows amd64: compiling application resources…",
            ]
          : [
              "Linux amd64: passed (2m 43s)",
              "macOS arm64: passed (2m 58s)",
              "Windows amd64: passed (3m 05s)",
              "Build artifacts retained for 7 days.",
            ],
    },
    {
      id: "format",
      name: "Formatting",
      status: "passed",
      required: false,
      duration: "8s",
      output: [
        "Checked source formatting.",
        "All files match the repository style.",
      ],
    },
  ];
}

function review(
  author: UserId,
  decision: ReviewDecision,
  headSha: string,
  body: string,
  createdAt: string,
): Review {
  return {
    id: "review-" + headSha + "-" + author,
    author,
    decision,
    body,
    headSha,
    createdAt,
  };
}

function pullRequest(
  number: number,
  title: string,
  author: UserId,
  head: string,
  overrides: Partial<PullRequest>,
): PullRequest {
  return {
    number,
    title,
    author,
    head,
    body: "Ready for review. See the changed files for implementation details.",
    repository: "mag-ik-een-review",
    status: "open",
    base: "main",
    headSha: "0000000",
    reviewers: ["noah", "elena"].filter(
      (user): user is UserId => user !== author,
    ),
    assignees: [author],
    labels: ["enhancement"],
    milestone: "0.2",
    requiredApprovals: 2,
    hasConflicts: false,
    files: [],
    commits: [],
    checks: checks(),
    comments: [],
    threads: [],
    reviews: [],
    viewedFiles: [],
    pendingComments: [],
    createdAt: "2026-10-12T10:00:00Z",
    updatedAt: "2026-10-13T08:00:00Z",
    ...overrides,
  };
}

const fixtures: PullRequest[] = [
  pullRequest(
    142,
    "Add keyboard navigation to file tree",
    "elena",
    "feature/keyboard-navigation",
    {
      headSha: "7bd06f2",
      body: "Navigate changed files with Arrow keys, Home and End. Enter opens a file; Escape returns focus from the diff to the file tree.\n\nFocus stays attached to the file path when the list refreshes. Includes keyboard and refresh coverage, plus visible focus states in both themes.\n\nTested on Linux, macOS and Windows.",
      reviewers: ["noah", "marcus"],
      labels: ["enhancement", "accessibility"],
      createdAt: "2026-10-12T10:08:00Z",
      updatedAt: "2026-10-13T09:17:00Z",
      files: keyboardFiles,
      commits: [
        {
          sha: "7bd06f2",
          subject: "Cover keyboard movement and focus after refresh",
          author: "elena",
          createdAt: "2026-10-13T08:03:00Z",
        },
        {
          sha: "c702ad3",
          subject: "Support Home, End and folder collapse",
          author: "elena",
          createdAt: "2026-10-12T16:24:00Z",
        },
        {
          sha: "d54fa91",
          subject: "Add roving focus to the changed file tree",
          author: "elena",
          createdAt: "2026-10-12T14:52:00Z",
        },
        {
          sha: "a8c9b2f",
          subject: "Preserve file selection by path",
          author: "elena",
          createdAt: "2026-10-12T10:01:00Z",
        },
      ],
      comments: [
        {
          id: "comment-142-1",
          author: "ravi",
          body: "Checked Arrow keys and Home / End on Linux. Navigation feels consistent with the repository tree.",
          createdAt: "2026-10-12T15:12:00Z",
        },
        {
          id: "comment-142-2",
          author: "elena",
          body: "Ready for another pass. The refresh case now preserves the focused file instead of resetting to the first row.",
          createdAt: "2026-10-13T08:06:00Z",
        },
      ],
      threads: [
        {
          id: "thread-142-1",
          path: keyboardFiles[0].path,
          line: 63,
          side: "new",
          resolved: false,
          comments: [
            {
              id: "comment-142-focus-1",
              author: "marcus",
              body: "Could we preserve focus when this list refreshes?",
              createdAt: "2026-10-12T15:40:00Z",
            },
            {
              id: "comment-142-focus-2",
              author: "elena",
              body: "Refreshes now reconcile nodes by path, so the focused item stays stable. I’ve added coverage for it.",
              createdAt: "2026-10-13T08:02:00Z",
            },
          ],
        },
        {
          id: "thread-142-2",
          path: keyboardFiles[1].path,
          line: 34,
          side: "new",
          resolved: true,
          comments: [
            {
              id: "comment-142-home-1",
              author: "ravi",
              body: "Can we include Home and End? They help with long file lists.",
              createdAt: "2026-10-12T14:59:00Z",
            },
            {
              id: "comment-142-home-2",
              author: "elena",
              body: "Added both, with a test that starts in the middle of the tree.",
              createdAt: "2026-10-12T16:26:00Z",
            },
          ],
        },
      ],
      reviews: [
        review(
          "marcus",
          "approved",
          "7bd06f2",
          "Keyboard behavior looks good. Leaving the refresh conversation open for Noah to confirm.",
          "2026-10-13T08:45:00Z",
        ),
      ],
    },
  ),
  pullRequest(
    141,
    "Restore repository selection on launch",
    "marcus",
    "fix/restore-repository",
    {
      headSha: "94f3a1c",
      files: otherFiles.restore,
      checks: checks("running"),
      body: "Restore the last opened repository when the app starts. Fall back to the first available repository if the saved path has been removed.\n\nDesktop builds are still running.",
      reviewers: ["noah", "ravi"],
      labels: ["bug", "desktop"],
      commits: [
        {
          sha: "94f3a1c",
          subject: "Restore the last active repository from settings",
          author: "marcus",
          createdAt: "2026-10-13T08:52:00Z",
        },
      ],
      reviews: [
        review(
          "ravi",
          "approved",
          "94f3a1c",
          "The removed-repository fallback works as expected.",
          "2026-10-13T09:01:00Z",
        ),
      ],
      createdAt: "2026-10-13T08:55:00Z",
      updatedAt: "2026-10-13T09:12:00Z",
    },
  ),
  pullRequest(
    139,
    "Follow system appearance changes",
    "noah",
    "feature/system-theme",
    {
      headSha: "ab48d32",
      status: "draft",
      files: otherFiles.theme,
      body: "Update the application theme when the operating system appearance changes.\n\nThe subscription and cleanup are implemented. I’m checking the first-render behavior before marking this ready.",
      reviewers: ["elena", "marcus"],
      labels: ["desktop", "appearance"],
      commits: [
        {
          sha: "ab48d32",
          subject: "Subscribe to system appearance changes",
          author: "noah",
          createdAt: "2026-10-12T13:15:00Z",
        },
      ],
      createdAt: "2026-10-12T13:20:00Z",
      updatedAt: "2026-10-13T08:41:00Z",
      comments: [
        {
          id: "comment-139-1",
          author: "elena",
          body: "Happy to review when the initial render check is done.",
          createdAt: "2026-10-12T14:10:00Z",
        },
      ],
    },
  ),
  pullRequest(
    138,
    "Persist repository panel widths",
    "ravi",
    "feature/panel-layout",
    {
      headSha: "23f45b1",
      status: "merged",
      files: otherFiles.panels,
      body: "Remember sidebar and inspector widths per workspace, with bounds that keep every pane usable on smaller windows.",
      reviewers: ["noah", "elena"],
      labels: ["desktop"],
      commits: [
        {
          sha: "23f45b1",
          subject: "Persist panel layout and clamp restored widths",
          author: "ravi",
          createdAt: "2026-10-12T09:28:00Z",
        },
      ],
      reviews: [
        review(
          "noah",
          "approved",
          "23f45b1",
          "Checked the minimum widths on a small laptop.",
          "2026-10-12T11:42:00Z",
        ),
        review(
          "elena",
          "approved",
          "23f45b1",
          "Looks good in both themes.",
          "2026-10-12T12:11:00Z",
        ),
      ],
      merge: { method: "squash", sha: "bfe51a3" },
      createdAt: "2026-10-12T09:31:00Z",
      updatedAt: "2026-10-13T07:52:00Z",
    },
  ),
  pullRequest(
    137,
    "Retry interrupted remote operations",
    "noah",
    "fix/remote-retry",
    {
      headSha: "52c9fe1",
      files: otherFiles.retry,
      checks: checks("failed"),
      body: "Retry fetch operations after a transient interruption, while keeping the retry count bounded.\n\nThe cancellation test is failing. The backoff needs to stop immediately when the user cancels.",
      reviewers: ["elena", "ravi"],
      labels: ["bug", "git"],
      commits: [
        {
          sha: "52c9fe1",
          subject: "Add bounded retry backoff for interrupted fetches",
          author: "noah",
          createdAt: "2026-10-11T15:14:00Z",
        },
      ],
      reviews: [
        review(
          "elena",
          "changes_requested",
          "52c9fe1",
          "Please cover cancellation during backoff. A canceled fetch should return immediately rather than waiting for the timer.",
          "2026-10-12T09:42:00Z",
        ),
      ],
      threads: [
        {
          id: "thread-137-1",
          path: otherFiles.retry[0].path,
          line: 15,
          side: "new",
          resolved: false,
          comments: [
            {
              id: "comment-137-1",
              author: "elena",
              body: "Can we make this delay observe context cancellation?",
              createdAt: "2026-10-12T09:38:00Z",
            },
          ],
        },
      ],
      createdAt: "2026-10-11T15:20:00Z",
      updatedAt: "2026-10-12T09:42:00Z",
    },
  ),
  pullRequest(
    135,
    "Expose mixed state in checkbox controls",
    "marcus",
    "fix/checkbox-mixed-state",
    {
      repository: "design-system",
      headSha: "83ac419",
      files: otherFiles.checkbox,
      checks: checks().map((check) => {
        if (check.id === "tests")
          return {
            ...check,
            name: "Component tests",
            duration: "42s",
            output: [
              "RUN checkbox.test.tsx",
              "PASS announces the mixed state",
              "PASS synchronizes parent selection changes",
              "32 passed · 0 failed",
            ],
          };
        if (check.id === "desktop")
          return {
            ...check,
            name: "Package build",
            duration: "35s",
            output: [
              "npm run build",
              "ES module bundle generated.",
              "Type declarations generated.",
              "Package size: 18.4 kB gzip.",
            ],
          };
        return check;
      }),
      body: "Expose the indeterminate checkbox state to assistive technology and keep it synchronized when parent selections change. Used by repository and changed-file selection.",
      reviewers: ["elena", "ravi"],
      labels: ["accessibility", "bug"],
      milestone: "1.4",
      commits: [
        {
          sha: "83ac419",
          subject: "Expose mixed checkbox state and sync the native property",
          author: "marcus",
          createdAt: "2026-10-10T13:02:00Z",
        },
      ],
      reviews: [
        review(
          "elena",
          "approved",
          "83ac419",
          "Verified the mixed state with keyboard navigation.",
          "2026-10-11T08:30:00Z",
        ),
        review(
          "ravi",
          "approved",
          "83ac419",
          "Parent selection updates now stay in sync.",
          "2026-10-11T09:12:00Z",
        ),
      ],
      createdAt: "2026-10-10T13:08:00Z",
      updatedAt: "2026-10-11T09:12:00Z",
    },
  ),
  pullRequest(
    134,
    "Show repository ahead/behind badges",
    "elena",
    "feature/branch-badges",
    {
      headSha: "45b9c20",
      status: "closed",
      files: otherFiles.badges,
      body: "Show ahead and behind counts beside the current branch.\n\nClosed while we settle the placement with the repository toolbar. The branch is retained so this can be reopened.",
      reviewers: ["noah", "marcus"],
      labels: ["desktop", "needs-design"],
      commits: [
        {
          sha: "45b9c20",
          subject: "Show ahead and behind counts beside the branch",
          author: "elena",
          createdAt: "2026-10-09T10:25:00Z",
        },
      ],
      comments: [
        {
          id: "comment-134-1",
          author: "marcus",
          body: "Let’s revisit the placement after the toolbar update. Keeping the branch makes it easy to pick this back up.",
          createdAt: "2026-10-10T08:32:00Z",
        },
      ],
      createdAt: "2026-10-09T10:30:00Z",
      updatedAt: "2026-10-10T08:37:00Z",
    },
  ),
  pullRequest(
    131,
    "Handle renamed files in split diffs",
    "ravi",
    "fix/renamed-file-diffs",
    {
      headSha: "12da8e4",
      files: otherFiles.rename,
      hasConflicts: true,
      body: "Show both paths when reviewing a renamed file, rather than presenting the change as a deletion and an addition.\n\nThis branch needs to pick up the recent DiffView props change from main.",
      reviewers: ["noah", "elena"],
      labels: ["bug", "diff"],
      commits: [
        {
          sha: "12da8e4",
          subject: "Preserve both file paths in renamed-file diffs",
          author: "ravi",
          createdAt: "2026-10-08T15:10:00Z",
        },
      ],
      requiredApprovals: 1,
      createdAt: "2026-10-08T15:14:00Z",
      updatedAt: "2026-10-09T08:21:00Z",
    },
  ),
];

export function seedPullRequests(): PullRequest[] {
  return structuredClone(fixtures);
}

export const seedHistory: Commit[] = [
  {
    sha: "bfe51a3",
    subject: "Persist repository panel widths (#138)",
    author: "ravi",
    createdAt: "2026-10-13T07:52:00Z",
  },
  {
    sha: "38b1cf0",
    subject: "Keep selected repository visible in tabs",
    author: "marcus",
    createdAt: "2026-10-12T15:31:00Z",
  },
  {
    sha: "a2de48c",
    subject: "Add checks to the pull request inspector",
    author: "noah",
    createdAt: "2026-10-12T11:18:00Z",
  },
  {
    sha: "681fa20",
    subject: "Use platform shortcuts in the command menu",
    author: "elena",
    createdAt: "2026-10-11T14:06:00Z",
  },
  {
    sha: "4bc80ea",
    subject: "Support repository groups in the sidebar",
    author: "ravi",
    createdAt: "2026-10-10T10:47:00Z",
  },
  {
    sha: "d2a778e",
    subject: "Show branch references beside commit subjects",
    author: "marcus",
    createdAt: "2026-10-09T13:22:00Z",
  },
  {
    sha: "316cc4b",
    subject: "Add split and unified diff modes",
    author: "noah",
    createdAt: "2026-10-08T16:35:00Z",
  },
  {
    sha: "85a91e2",
    subject: "Create the Wails application shell",
    author: "elena",
    createdAt: "2026-10-07T09:14:00Z",
  },
];

export const seedLocalChanges: (ChangedFile & { staged: boolean })[] = [
  {
    ...file(
      "frontend/src/components/ReviewInbox.tsx",
      "modified",
      `
 import { useMemo, useState } from 'react';
 import { PullRequestList } from './PullRequestList';
 import type { PullRequest } from '../types';
 
 export function ReviewInbox({ requests }: { requests: PullRequest[] }) {
   const [query, setQuery] = useState('');
   const visible = useMemo(() => {
-    return requests.filter((request) => request.title.includes(query));
+    const needle = query.trim().toLocaleLowerCase();
+    return requests.filter((request) => {
+      const searchable = request.title + ' ' + request.number + ' ' + request.head;
+      return searchable.toLocaleLowerCase().includes(needle);
+    });
   }, [query, requests]);
 
   return <PullRequestList requests={visible} query={query} onQueryChange={setQuery} />;
 }
`,
    ),
    staged: true,
  },
  {
    ...file(
      "frontend/src/styles/app.css",
      "modified",
      `
 .review-search {
   width: 100%;
   min-height: 28px;
   border: 1px solid var(--border);
   border-radius: 4px;
   padding: 4px 8px;
   background: var(--input-background);
   color: var(--text-primary);
 }
 
+.review-search:focus-visible {
+  outline: 2px solid var(--focus-ring);
+  outline-offset: 1px;
+}
+
 .review-search::placeholder {
   color: var(--text-muted);
 }
`,
    ),
    staged: false,
  },
  {
    ...file(
      "frontend/src/lib/review-shortcuts.ts",
      "added",
      `
+export const reviewShortcuts = {
+  nextFile: ']',
+  previousFile: '[',
+  focusFiles: 'Escape',
+  markViewed: 'v',
+  submitReview: 'Mod+Enter',
+} as const;
+
+export function shortcutLabel(key: string, macOS: boolean): string {
+  return key.replace('Mod', macOS ? '⌘' : 'Ctrl');
+}
`,
    ),
    staged: false,
  },
];
