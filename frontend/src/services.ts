/** DTOs mirror the registered Go services. Tokens never enter browser storage. */
export interface GitHubUser {
  login: string;
  name: string;
  avatarUrl: string;
  url: string;
}

export interface RepositoryIdentity {
  host: string;
  owner: string;
  repo: string;
}

export interface NotificationPreferences {
  toastsEnabled: boolean;
  desktopEnabled: boolean;
  opened: boolean;
  closed: boolean;
  merged: boolean;
  reviewRequested: boolean;
}

export interface PRNotificationEvent extends RepositoryIdentity {
  id: string;
  key: string;
  number: number;
  type: "opened" | "closed" | "merged" | "reviewRequested";
  title: string;
  url: string;
  actor: string;
  toast: boolean;
  desktop: boolean;
  createdAt: string;
}

export interface LSPPosition { line: number; character: number }
export interface LSPRange { start: LSPPosition; end: LSPPosition }
export interface CodePositionRequest {
  root: string;
  revision: string;
  path: string;
  host?: string;
  owner?: string;
  repo?: string;
  pullRequest?: number;
  line: number;
  character: number;
  lineText?: string;
  comparisonHead?: string;
}
export interface LSPHover {
  contents: string;
  kind: string;
  range?: LSPRange;
  revision: string;
  language: string;
}
export interface LSPDefinitionTarget {
  id: string;
  uri: string;
  path: string;
  range: LSPRange;
  revision: string;
  language: string;
  external: boolean;
}
export interface LSPDefinitionDocument {
  uri?: string;
  text: string;
  path: string;
  language: string;
  revision: string;
  range: LSPRange;
  external: boolean;
}
export interface LSPServerConfiguration {
  command: string;
  args: string[];
  typescriptSdkPath?: string;
  vuePluginPath?: string;
}
export interface LSPServerStatus {
  language: string;
  name: string;
  available: boolean;
  command: string;
  args: string[];
  source: string;
  setupUrl: string;
  error: string;
  configuration: LSPServerConfiguration;
  managed: LSPToolStatus;
}
export interface LSPToolStatus {
  installedVersion: string;
  latestVersion: string;
  ready: boolean;
  updateAvailable: boolean;
  dependencyUpdate: boolean;
  state: "idle" | "installing" | "updating" | "cancelling" | "failed" | "cancelled";
  progress: string;
  installError: string;
  checkError: string;
  prerequisiteError: string;
  checkedAt: string;
}
export const LSP_CONFIGURATION_CHANGED_EVENT = "mier:lsp-configuration-changed";

export interface AccountStatus {
  host: string;
  authenticated: boolean;
  login: string;
  name: string;
  avatarUrl: string;
  source: "saved" | "gh" | "";
  cliInstalled: boolean;
  error?: string;
}

export interface Account {
  host: string;
  login: string;
  source: string;
}

export interface GitHubBranch {
  ref: string;
  sha: string;
  label: string;
  repository: string;
}

export interface PullRequestSummary {
  number: number;
  nodeId: string;
  title: string;
  body: string;
  url: string;
  status: "open" | "draft" | "closed" | "merged";
  author: GitHubUser;
  createdAt: string;
  updatedAt: string;
  head: GitHubBranch;
  base: GitHubBranch;
  reviewers: GitHubUser[] | null;
  assignees: GitHubUser[] | null;
  labels: string[] | null;
  draft: boolean;
  merged: boolean;
  mergeable: boolean | null;
  mergeableState: string;
  additions: number;
  deletions: number;
  changedFiles: number;
  commitsCount: number;
  commentsCount: number;
  reviewCommentsCount: number;
  threadsCount: number;
  countsKnown: boolean;
  warnings: string[] | null;
}

export interface PullRequestPageQuery {
  state: "open" | "all" | "merged" | "closed";
  view: "inbox" | "mine" | "review-requested" | "assigned" | "drafts";
  query: string;
}

export interface PullRequestPageRequest extends PullRequestPageQuery {
  cursor: string;
  pageSize: number;
}

export interface PullRequestPage {
  items: PullRequestSummary[];
  nextCursor: string;
  hasNextPage: boolean;
  totalCount: number;
  openCount: number;
  resultLimit?: number;
}

export interface GitHubComment {
  id: number;
  nodeId: string;
  body: string;
  user: GitHubUser;
  createdAt: string;
  updatedAt: string;
  url: string;
  path: string;
  line: number | null;
  originalLine: number | null;
  side: string;
  commitId: string;
  inReplyToId: number;
}

export interface GitHubReview {
  id: number;
  nodeId: string;
  body: string;
  user: GitHubUser;
  state: string;
  commitId: string;
  submittedAt: string;
  url: string;
}

export interface GitHubThread {
  id: string;
  path: string;
  line: number | null;
  originalLine: number | null;
  diffSide: string;
  resolved: boolean;
  outdated: boolean;
  viewerCanResolve: boolean;
  viewerCanUnresolve: boolean;
  comments: GitHubComment[] | null;
}

export interface GitHubChangedFile {
  path: string;
  previousPath: string;
  status: string;
  additions: number;
  deletions: number;
  patch: string | null;
  binary?: boolean;
  truncated?: boolean;
}

export interface GitHubCommit {
  sha: string;
  subject: string;
  body: string;
  author: GitHubUser;
  authorName: string;
  createdAt: string;
  url: string;
}

export interface GitHubCheckRun {
  id: number;
  name: string;
  status: string;
  conclusion: string | null;
  startedAt: string | null;
  completedAt: string | null;
  detailsUrl: string;
  output: { title: string; summary: string; text: string };
  required?: boolean;
}

export interface GitHubCommitStatus {
  id: number;
  context: string;
  state: string;
  description: string;
  targetUrl: string;
  createdAt: string;
  updatedAt?: string;
  required?: boolean;
}

export interface PullRequestDetail {
  pullRequest: PullRequestSummary;
  files: GitHubChangedFile[] | null;
  commits: GitHubCommit[] | null;
  comments: GitHubComment[] | null;
  reviews: GitHubReview[] | null;
  checks: GitHubCheckRun[] | null;
  statuses: GitHubCommitStatus[] | null;
  threads: GitHubThread[] | null;
  mergeStateStatus: string;
  reviewDecision: string;
  viewerCanUpdate: boolean;
  allowedMergeMethods: string[] | null;
  warnings: string[] | null;
  requiredApprovals?: number | null;
  requiredStatusCheckContexts?: string[] | null;
  milestone?: string;
}

export interface ReviewCommentRequest {
  body: string;
  path: string;
  line: number;
  side: "LEFT" | "RIGHT";
}

export interface SubmitReviewRequest {
  event: "APPROVE" | "REQUEST_CHANGES" | "COMMENT";
  body: string;
  commitId: string;
  comments: ReviewCommentRequest[];
}

export interface CreatePullRequestRequest {
  title: string;
  body: string;
  base: string;
  head: string;
  draft: boolean;
}

export interface UpdatePullRequestRequest {
  title?: string;
  body?: string;
  state?: "open" | "closed";
}

export interface MergeResult {
  sha: string;
  merged: boolean;
  message: string;
}

export interface RepositoryRemote {
  name: string;
  fetchUrl: string;
  pushUrl: string;
  host: string;
  owner: string;
  repository: string;
  provider: string;
}

export interface RepositoryBranch {
  name: string;
  current: boolean;
  remote: boolean;
  upstream: string;
  ahead: number;
  behind: number;
  commitSha: string;
}

export interface RepositoryCommit {
  sha: string;
  parents: string[] | null;
  refs?: string[] | null;
  subject: string;
  body: string;
  authorName: string;
  authorEmail: string;
  createdAt: string;
}

export interface RepositoryFileStatus {
  path: string;
  originalPath?: string;
  indexStatus: string;
  worktreeStatus: string;
  status: string;
  staged: boolean;
  unstaged: boolean;
  conflict: boolean;
  untracked: boolean;
  submodule?: string;
}

export interface RepositorySnapshot {
  root: string;
  name: string;
  currentBranch: string;
  detached: boolean;
  hasHead: boolean;
  headSha?: string;
  upstream: string;
  ahead: number;
  behind: number;
  remotes: RepositoryRemote[] | null;
  branches: RepositoryBranch[] | null;
  history: RepositoryCommit[] | null;
  changes: RepositoryFileStatus[] | null;
}

export interface RepositoryDiffLine {
  hunk?: number;
  kind: string;
  oldLine?: number;
  newLine?: number;
  text: string;
}

export interface RepositoryDiff {
  path: string;
  status: string;
  previousPath?: string;
  additions: number;
  deletions: number;
  lines: RepositoryDiffLine[] | null;
  rawPatch: string;
  binary: boolean;
  truncated: boolean;
}

export interface WorkspaceWatchRequest extends RepositoryIdentity {
  path: string;
}

export interface WorkspaceUpdate {
  path: string;
  host?: string;
  owner?: string;
  repo?: string;
  repository?: RepositorySnapshot;
  pullRequests?: PullRequestSummary[];
  openPullRequestCount?: number;
  pullRequest?: PullRequestDetail;
  error?: string;
  updatedAt: string;
}

type NativeBridge = Window & { _wails?: { environment?: { OS?: string } } };

export function hasNativeBackend(): boolean {
  if (typeof window === "undefined") return false;
  const os = (window as NativeBridge)._wails?.environment?.OS;
  return os === "darwin" || os === "windows" || os === "linux";
}

async function invoke<T>(method: string, ...args: unknown[]): Promise<T> {
  if (!hasNativeBackend()) {
    throw new Error("Open M.I.E.R. in its desktop window to connect to local Git and GitHub.");
  }
  const { Call } = await import("@wailsio/runtime");
  // Wails bindings.go derives this name from reflect.Type.PkgPath(), not go.mod.
  return (await Call.ByName(method, ...args)) as T;
}

async function invokeLSP<T>(method: string, request: unknown, signal?: AbortSignal): Promise<T> {
  if (!hasNativeBackend()) throw new Error("Open M.I.E.R. in its desktop window to use language intelligence.");
  if (signal?.aborted) throw signal.reason ?? new DOMException("The request was cancelled.", "AbortError");
  const { Call } = await import("@wailsio/runtime");
  if (signal?.aborted) throw signal.reason ?? new DOMException("The request was cancelled.", "AbortError");
  // Installed Wails runtime returns CancellablePromise; cancel reaches the Go context.
  const promise = Call.ByName(method, request);
  const abort = () => { void promise.cancel(signal?.reason).catch(() => undefined); };
  signal?.addEventListener("abort", abort, { once: true });
  try { return (await promise) as T; }
  finally { signal?.removeEventListener("abort", abort); }
}

export const services = {
  editor: {
    preferredEditor: () => invoke<string>("main.EditorService.PreferredEditor"),
    openRepository: (root:string, editor:string) => invoke<void>("main.EditorService.OpenRepository",root,editor),
  },
  lsp: {
    servers: async (root: string) => (await invoke<LSPServerStatus[] | null>("main.LSPService.Servers", root)) ?? [],
    warmup: (request: CodePositionRequest, signal?: AbortSignal) => invokeLSP<void>("main.LSPService.Warmup", request, signal),
    hover: (request: CodePositionRequest, signal?: AbortSignal) => invokeLSP<LSPHover>("main.LSPService.Hover", request, signal),
    definition: async (request: CodePositionRequest, signal?: AbortSignal) => (await invokeLSP<LSPDefinitionTarget[] | null>("main.LSPService.Definition", request, signal)) ?? [],
    readDefinition: (request: {root:string; revision:string; target:string}, signal?: AbortSignal) => invokeLSP<LSPDefinitionDocument>("main.LSPService.ReadDefinition", request, signal),
    configureServer: (language: string, configuration: LSPServerConfiguration) => invoke<void>("main.LSPService.ConfigureServer", language, configuration),
    checkServerUpdates: async (root: string, force = false) => (await invoke<LSPServerStatus[] | null>("main.LSPService.CheckServerUpdates", root, force)) ?? [],
    installServer: (language: string) => invoke<void>("main.LSPService.InstallServer", language),
    cancelServerInstall: (language: string) => invoke<void>("main.LSPService.CancelServerInstall", language),
    onChanged: (callback: () => void) => {
      let closed = false, unsubscribe: (() => void) | undefined;
      if (hasNativeBackend()) {
        void import("@wailsio/runtime").then(({ Events }) => {
          if (!closed) unsubscribe = Events.On("lsp:changed", callback);
        }).catch(issue => { if (!closed) console.error("Unable to subscribe to language tool updates", issue); });
      }
      return () => { closed = true; unsubscribe?.(); };
    },
  },
  notifications: {
    getPreferences: (key: string) => invoke<NotificationPreferences>("main.PRNotificationService.GetPreferences", key),
    savePreferences: (key: string, preferences: NotificationPreferences) => invoke<void>("main.PRNotificationService.SavePreferences", key, preferences),
    requestDesktopPermission: () => invoke<void>("main.PRNotificationService.RequestDesktopPermission"),
  },
  integrations: {
    installJira: () => invoke<void>("main.IntegrationService.InstallJira"),
  },
  workspace: {
    chooseRepository: () => invoke<string>("main.WorkspaceService.ChooseRepository"),
    recentRepositories: async () => (await invoke<string[] | null>("main.WorkspaceService.RecentRepositories")) ?? [],
    rememberRepository: (path: string) => invoke<void>("main.WorkspaceService.RememberRepository", path),
  },
  sync: {
    startWatch: (request: WorkspaceWatchRequest) => invoke<void>("main.WorkspaceSyncService.StartWatch", request),
    stopWatch: (path: string) => invoke<void>("main.WorkspaceSyncService.StopWatch", path),
    watchPullRequest: (path: string, number: number) => invoke<void>("main.WorkspaceSyncService.WatchPullRequest", path, number),
    refresh: (path: string) => invoke<void>("main.WorkspaceSyncService.Refresh", path),
  },
  credentials: {
    accountStatus: (host: string) => invoke<AccountStatus>("main.CredentialService.AccountStatus", host),
    saveToken: (host: string, token: string) => invoke<AccountStatus>("main.CredentialService.SaveToken", host, token),
    deleteToken: (host: string) => invoke<void>("main.CredentialService.DeleteToken", host),
    listAccounts: async () => (await invoke<Account[] | null>("main.CredentialService.ListAccounts")) ?? [],
  },
  github: {
    getCommitDiff: (host: string, owner: string, repo: string, sha: string) => invoke<{sha:string; files:GitHubChangedFile[]; warnings:string[]}>("main.GitHubService.GetCommitDiff", host, owner, repo, sha),
    listPullRequests: async (host: string, owner: string, repo: string, state: "open" | "closed" | "all" = "open") => (await invoke<PullRequestSummary[] | null>("main.GitHubService.ListPullRequests", host, owner, repo, state)) ?? [],
    listPullRequestsPage: (host: string, owner: string, repo: string, request: PullRequestPageRequest) => invoke<PullRequestPage>("main.GitHubService.ListPullRequestsPage", host, owner, repo, request),
    getPullRequest: (host: string, owner: string, repo: string, number: number) => invoke<PullRequestDetail>("main.GitHubService.GetPullRequest", host, owner, repo, number),
    createPullRequest: (host: string, owner: string, repo: string, request: CreatePullRequestRequest) => invoke<PullRequestSummary>("main.GitHubService.CreatePullRequest", host, owner, repo, request),
    updatePullRequest: (host: string, owner: string, repo: string, number: number, request: UpdatePullRequestRequest) => invoke<PullRequestSummary>("main.GitHubService.UpdatePullRequest", host, owner, repo, number, request),
    setDraft: (host: string, owner: string, repo: string, number: number, draft: boolean) => invoke<void>("main.GitHubService.SetDraft", host, owner, repo, number, draft),
    addIssueComment: (host: string, owner: string, repo: string, number: number, body: string) => invoke<GitHubComment>("main.GitHubService.AddIssueComment", host, owner, repo, number, body),
    addReviewComment: (host: string, owner: string, repo: string, number: number, request: ReviewCommentRequest & { commitId: string }) => invoke<GitHubComment>("main.GitHubService.AddReviewComment", host, owner, repo, number, request),
    submitReview: (host: string, owner: string, repo: string, number: number, request: SubmitReviewRequest) => invoke<GitHubReview>("main.GitHubService.SubmitReview", host, owner, repo, number, request),
    replyReviewComment: (host: string, owner: string, repo: string, number: number, commentId: number, body: string) => invoke<GitHubComment>("main.GitHubService.ReplyReviewComment", host, owner, repo, number, commentId, body),
    resolveThread: (host: string, threadId: string, resolved: boolean) => invoke<void>("main.GitHubService.ResolveThread", host, threadId, resolved),
    mergePullRequest: (host: string, owner: string, repo: string, number: number, method: "squash" | "merge" | "rebase", expectedHeadSHA: string) => invoke<MergeResult>("main.GitHubService.MergePullRequest", host, owner, repo, number, method, expectedHeadSHA),
    requestReviewers: (host: string, owner: string, repo: string, number: number, reviewers: string[]) => invoke<PullRequestSummary>("main.GitHubService.RequestReviewers", host, owner, repo, number, reviewers),
    removeReviewers: (host: string, owner: string, repo: string, number: number, reviewers: string[]) => invoke<void>("main.GitHubService.RemoveReviewers", host, owner, repo, number, reviewers),
    setAssignees: (host: string, owner: string, repo: string, number: number, assignees: string[]) => invoke<void>("main.GitHubService.SetAssignees", host, owner, repo, number, assignees),
    setLabels: async (host: string, owner: string, repo: string, number: number, labels: string[]) => (await invoke<string[] | null>("main.GitHubService.SetLabels", host, owner, repo, number, labels)) ?? [],
  },
  repository: {
    commitDiff: async (root: string, revision: string) => (await invoke<RepositoryDiff[] | null>("main.RepositoryService.CommitDiff", root, revision)) ?? [],
    open: (path: string) => invoke<RepositorySnapshot>("main.RepositoryService.Open", path),
    snapshot: (path: string) => invoke<RepositorySnapshot>("main.RepositoryService.Snapshot", path),
    diff: (path: string, file: string, staged: boolean) => invoke<RepositoryDiff>("main.RepositoryService.Diff", path, file, staged),
    compare: async (path: string, base: string, head: string) => (await invoke<RepositoryDiff[] | null>("main.RepositoryService.Compare", path, base, head)) ?? [],
    stage: (path: string, files: string[]) => invoke<void>("main.RepositoryService.Stage", path, files),
    unstage: (path: string, files: string[]) => invoke<void>("main.RepositoryService.Unstage", path, files),
    commit: (path: string, message: string) => invoke<string>("main.RepositoryService.Commit", path, message),
    checkout: (path: string, branch: string) => invoke<void>("main.RepositoryService.Checkout", path, branch),
    checkoutPullRequest: (path: string, number: number, remote: string) => invoke<void>("main.RepositoryService.CheckoutPullRequest", path, number, remote),
    createBranch: (path: string, branch: string) => invoke<void>("main.RepositoryService.CreateBranch", path, branch),
    createAndCheckoutBranch: (path: string, branch: string) => invoke<void>("main.RepositoryService.CreateAndCheckoutBranch", path, branch),
    deleteBranch: (path: string, branch: string) => invoke<void>("main.RepositoryService.DeleteBranch", path, branch),
    renameBranch: (path: string, branch: string, name: string) => invoke<void>("main.RepositoryService.RenameBranch", path, branch, name),
    fetch: (path: string, remote: string) => invoke<void>("main.RepositoryService.Fetch", path, remote),
    pull: (path: string, remote: string) => invoke<void>("main.RepositoryService.Pull", path, remote),
    push: (path: string, remote: string) => invoke<void>("main.RepositoryService.Push", path, remote),
    clone: (url: string, directory: string) => invoke<RepositorySnapshot>("main.RepositoryService.Clone", url, directory),
  },
};

/** The callback is silent in a regular browser; live calls still reject there. */
export function subscribeWorkspaceUpdates(callback: (update: WorkspaceUpdate) => void, onError?: (error: Error) => void): () => void {
  let closed = false;
  let connecting = false;
  let unsubscribe: (() => void) | undefined;
  const connect = () => {
    if (closed || connecting || !hasNativeBackend()) return;
    connecting = true;
    void import("@wailsio/runtime").then(({ Events }) => {
      if (closed) return;
      unsubscribe = Events.On("workspace:updated", event => callback(event.data as WorkspaceUpdate));
    }).catch(issue => {
      connecting = false;
      const error = issue instanceof Error ? issue : new Error(String(issue));
      if (!closed && onError) onError(error);
      else if (!closed) console.error("Unable to subscribe to workspace updates", error);
    });
  };
  if (typeof window !== "undefined") window.addEventListener("wails:runtime-config-ready", connect);
  connect();
  return () => {
    closed = true;
    if (typeof window !== "undefined") window.removeEventListener("wails:runtime-config-ready", connect);
    unsubscribe?.();
  };
}
