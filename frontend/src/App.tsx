import { useCallback, useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { Icon } from "./Icon";
import { Markdown } from "./Markdown";
import ReviewWorkspace, { ReviewFooter } from "./ReviewWorkspace";
import {
  currentUser,
  latestReview,
  mergeBlockers,
  mergePullRequest,
  people,
  seedHistory,
  seedLocalChanges,
  seedPullRequests,
} from "./data";
import type {
  Commit,
  MergeMethod,
  PullRequest,
  ReviewTab,
  UserId,
} from "./data";
import { subscribeNativeCommands, toggleFullscreen } from "./native";
import type { ThemePreference } from "./native";
import "./app.css";
import { useLiveWorkspace } from "./useLiveWorkspace";
import { AccountSettings, RepositoryWelcome, LiveCreatePullRequest } from "./LiveSetup";
import LocalChangesView, {type LocalFile} from "./LocalChangesView";
import RepositorySelector, {repositoryName} from "./RepositorySelector";
import {InboxSkeleton, RepositorySkeleton, ReviewSkeleton} from "./Loading";
import HistoryGraph from "./HistoryGraph";
import {JiraIntegration} from "./JiraIntegration";
import NotificationSettings from "./NotificationSettings";
import EditorActions from "./EditorActions";
import {Avatar, ActorName, personLabel} from "./Avatar";
import CodeIntelligence from "./CodeIntelligence";
import {CodeReviewSettings} from "./CodeReviewSettings";
import LanguageServerSettings from "./LanguageServerSettings";
import {NotificationToasts, type PullRequestNotice} from "./NotificationToasts";
import RepositoryToolbar from "./RepositoryToolbar";
import CommitChanges, {type LoadCommitDiff} from "./CommitChanges";
import Modal from "./Modal";
import { services } from "./services";
import { mapChangedFile } from "./liveData";

type Destination =
  | "inbox"
  | "mine"
  | "requested"
  | "assigned"
  | "drafts"
  | "history"
  | "local-changes"
  | "settings";
type Repository = string;
const storageKey = "mag-ik-een-review.prototype.v1";
function restore() {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) || "null");
    if (
      saved?.version === 1 &&
      Array.isArray(saved.prs) &&
      Array.isArray(saved.history) &&
      Array.isArray(saved.changes) &&
      typeof saved.branch === "string" &&
      saved.prs.every(
        (p: PullRequest) =>
          Array.isArray(p.pendingComments) &&
          Array.isArray(p.files) &&
          Array.isArray(p.reviews) &&
          Array.isArray(p.threads),
      )
    )
      return saved as {
        prs: PullRequest[];
        history: Commit[];
        changes: typeof seedLocalChanges;
        branch: string;
      };
  } catch {
    /* An unavailable or damaged local store falls back to the sample. */
  }
  return {
    prs: seedPullRequests(),
    history: seedHistory,
    changes: seedLocalChanges.map((f) => ({ ...f })),
    branch: "main",
  };
}
function StatusBadge({ status }: { status: PullRequest["status"] }) {
  return (
    <span className={`badge status-${status}`}>
      <Icon
        name={
          status === "merged"
            ? "git-merge"
            : status === "draft"
              ? "clock"
              : status === "closed"
                ? "close"
                : "pull-request"
        }
        size={13}
      />
      {status === "draft" ? "Draft" : status[0].toUpperCase() + status.slice(1)}
    </span>
  );
}
function Count({ children }: { children: React.ReactNode }) {
  return <span className="count">{children}</span>;
}
function isEditingTarget(target: EventTarget | null) {
  return (
    target instanceof HTMLElement &&
    (["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName) ||
      target.isContentEditable)
  );
}
function when() {
  return new Date().toISOString();
}
function relativeTime(value: string) {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return value;
  const minutes = Math.max(0, Math.floor((Date.now() - timestamp) / 60000));
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  if (minutes < 1440) return `${Math.floor(minutes / 60)}h ago`;
  if (minutes < 10080) return `${Math.floor(minutes / 1440)}d ago`;
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
  }).format(timestamp);
}
function newId() {
  return crypto.randomUUID();
}

export default function App() {
  const [initial] = useState(restore);
  const [prs, setPrs] = useState<PullRequest[]>(initial.prs);
  const [history, setHistory] = useState<Commit[]>(initial.history);
  const [localChanges, setLocalChanges] = useState<LocalFile[]>(initial.changes);
  const [branch, setBranch] = useState(initial.branch);
  const [destination, setDestination] = useState<Destination>("inbox");
  const [repository, setRepository] = useState<Repository>("all");
  const [selected, setSelected] = useState(142);
  const [tab, setTab] = useState<ReviewTab>("changes");
  const [reviewLocation, setReviewLocation] = useState<
    { path: string; threadId: string } | undefined
  >();
  const [filter, setFilter] = useState<"open" | "all" | "closed" | "merged">(
    "open",
  );
  const [query, setQuery] = useState("");
  const [focusMode, setFocusMode] = useState(false);
  const [creating, setCreating] = useState(false);
  const [inspectedCommit, setInspectedCommit] = useState<Commit | null>(null);
  const [palette, setPalette] = useState(false);
  const [commandQuery, setCommandQuery] = useState("");
  const [commandIndex, setCommandIndex] = useState(0);
  const paletteReturnFocus = useRef<HTMLElement | null>(null);
  const openPalette = useCallback(() => {
    paletteReturnFocus.current = document.activeElement as HTMLElement;
    setCommandQuery("");
    setCommandIndex(0);
    setPalette(true);
  }, []);
  const closePalette = useCallback(() => {
    setPalette(false);
    queueMicrotask(() => {
      if (paletteReturnFocus.current?.isConnected)
        paletteReturnFocus.current.focus();
    });
  }, []);
  const [theme, setTheme] = useState<ThemePreference>(() => {
    try {
      const saved=localStorage.getItem("mag-ik-een-review.appearance");
      return saved==='dark'||saved==='light'?saved:'system';
    } catch {
      return "system";
    }
  });
  const [toast, setToast] = useState("");
  const toastTimer = useRef<ReturnType<typeof setTimeout>>();
  const searchRef = useRef<HTMLInputElement>(null);
  const notify = useCallback((message: string) => {
    setToast(message);
    clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToast(""), 4200);
  }, []);
  const live = useLiveWorkspace(notify);
  const previousMode = useRef(live.enabled);
  useEffect(() => {
    if (live.enabled) {
      setPrs(live.prs); setHistory(live.history); setLocalChanges(live.changes); setBranch(live.branch || "No branch");
    } else if (previousMode.current) {
      setPrs(seedPullRequests()); setHistory(seedHistory); setLocalChanges(seedLocalChanges.map(f=>({...f}))); setBranch("main"); setSelected(142);
    }
    previousMode.current = live.enabled;
  }, [live.enabled, live.prs, live.repository, live.history, live.changes, live.branch]);
  useEffect(() => {if(live.enabled){setRepository("all");setSelected(0);setCreating(false);setFocusMode(false)}}, [live.enabled,live.repository?.root]);
  useEffect(() => { if (live.enabled && live.repository && selected > 0 && live.prs.some(pr=>pr.number===selected)) live.selectPullRequest(selected) }, [live.enabled, live.repository?.root, selected,live.prs.length]);
  const reviewNotify = (message:string) => { if (!live.enabled || /pending|draft|copied|clipboard|unavailable/i.test(message)) notify(message) };
  const pr = prs.find((p) => p.number === selected);
  useEffect(()=>setInspectedCommit(null),[live.enabled,live.repository?.root,selected,destination]);
  const provider=pr?.provider;
  const loadPRCommit:LoadCommitDiff=useCallback(async sha=>{
    if(!provider)throw new Error('This pull request is not connected to GitHub.');
    const result=await services.github.getCommitDiff(provider.host,provider.owner,provider.repo,sha);
    return {files:(result.files??[]).map(mapChangedFile),warnings:result.warnings??[]};
  },[provider?.host,provider?.owner,provider?.repo]);
  const isPRView = ["inbox", "mine", "requested", "assigned", "drafts"].includes(
    destination,
  );
  useEffect(() => {
    if (!isPRView) return;
    live.setPullRequestQuery({ state: filter, view: destination === "requested" ? "review-requested" : destination as "inbox" | "mine" | "assigned" | "drafts", query });
  }, [isPRView, destination, filter, query, live.setPullRequestQuery]);
  const openPRs = prs.filter((p) => ["open", "draft"].includes(p.status));
  const reviewRequested = openPRs.filter(
    (p) =>
      p.reviewers.includes(currentUser) &&
      !latestReview(p, currentUser) &&
      p.author !== currentUser,
  );
  const scopedPRs = prs.filter((p) => {
    if (repository !== "all" && p.repository !== repository) return false;
    if (destination === "mine" && p.author !== currentUser) return false;
    if (
      destination === "requested" &&
      !reviewRequested.some((r) => r.number === p.number)
    )
      return false;
    if (destination === "assigned" && !p.assignees.includes(currentUser)) return false;
    if (destination === "drafts" && p.status !== "draft") return false;
    return true;
  });
  const visiblePRs = live.enabled ? live.inboxPullRequests.filter(p => {
    if (filter === "open" && !["open", "draft"].includes(p.status)) return false;
    if (filter === "closed" && p.status !== "closed") return false;
    if (filter === "merged" && p.status !== "merged") return false;
    if (destination === "mine" && p.author !== currentUser) return false;
    if (destination === "requested" && !p.reviewers.includes(currentUser)) return false;
    if (destination === "assigned" && !p.assignees.includes(currentUser)) return false;
    if (destination === "drafts" && p.status !== "draft") return false;
    return true;
  }) : scopedPRs.filter((p) => {
    if (filter === "open" && !["open", "draft"].includes(p.status))
      return false;
    if (filter === "closed" && p.status !== "closed") return false;
    if (filter === "merged" && p.status !== "merged") return false;
    return `${p.title} ${p.number} ${people[p.author].name} ${p.repository}`
      .toLowerCase()
      .includes(query.toLowerCase());
  });
  const openFilterCount = live.enabled
    ? destination === "inbox" && !query.trim() ? live.openPullRequestCount : filter === "open" ? live.pullRequestTotal : null
    : scopedPRs.filter(p=>["open","draft"].includes(p.status)).length;
  const updatePR = useCallback(
    (next: PullRequest) => {
      const previous = prs.find((item) => item.number === next.number);
      if (live.enabled && previous) {
        return live.updatePullRequest(previous, next).then(() => {
          if (previous.status !== next.status) { setFilter(next.status === "merged" ? "merged" : next.status === "closed" ? "closed" : "open"); if (destination === "drafts" && next.status !== "draft") setDestination("inbox"); }
        });
      }
      if (next.number === selected && previous?.status !== next.status) {
        setFilter(
          next.status === "merged"
            ? "merged"
            : next.status === "closed"
              ? "closed"
              : "open",
        );
        if (destination === "drafts" && next.status !== "draft")
          setDestination("inbox");
        setQuery("");
      }
      setPrs((items) =>
        items.map((item) => (item.number === next.number ? next : item)),
      );
    },
    [prs, selected, destination, live.enabled, live.updatePullRequest],
  );
  const navigate = useCallback((next: Destination) => {
    setDestination(next);
    setFocusMode(false);
    setCreating(false);
    setQuery("");
    setFilter("open");
  }, []);
  const toggleFocus = useCallback(() => {
    setTab("changes");
    setFocusMode((value) => !value);
  }, []);

  useEffect(() => {
    if (live.enabled) return;
    try {
      localStorage.setItem(
        storageKey,
        JSON.stringify({
          version: 1,
          prs,
          history,
          changes: localChanges,
          branch,
        }),
      );
    } catch {
      /* Session state remains usable without local storage. */
    }
  }, [prs, history, localChanges, branch, live.enabled]);
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      document.documentElement.dataset.theme =
        theme === "system" ? (media.matches ? "dark" : "light") : theme;
    };
    apply();
    media.addEventListener("change", apply);
    try {
      localStorage.setItem("mag-ik-een-review.appearance", theme);
    } catch {
      /* Appearance still works for this session. */
    }
    return () => media.removeEventListener("change", apply);
  }, [theme]);
  useEffect(() => {
    if (live.enabled && live.prs.some(pr => pr.number === selected && pr.provider?.detailLoaded)) return;
    if (
      !visiblePRs.some((p) => p.number === selected) &&
      visiblePRs.length &&
      !creating
    )
      setSelected(visiblePRs[0].number);
  }, [visiblePRs.map((p) => p.number).join(","), selected, creating, live.enabled]);
  useEffect(() => {
    const command = (action: string, value?: string) => {
      if (action === "navigate")
        navigate(
          value === "history"
            ? "history"
            : value === "changes"
              ? "local-changes"
              : "inbox",
        );
      if (action === "open-repository") void live.openRepository().catch(()=>{});
      if (action === "create-pr") {
        navigate("inbox");
        setCreating(true);
      }
      if (action === "command-palette") openPalette();
      if (
        action === "theme" &&
        ["system", "light", "dark"].includes(value || "")
      )
        setTheme(value as ThemePreference);
      if (action === "toggle-fullscreen")
        void toggleFullscreen().catch(() =>
          notify("Full screen is unavailable in this window."),
        );
    };
    const stop = subscribeNativeCommands((event) =>
      command(event.action, event.value),
    );
    const keyboard = (event: KeyboardEvent) => {
      if (document.querySelector('dialog[open]')) return;
      const mod = event.metaKey || event.ctrlKey;
      if (mod && event.key.toLowerCase() === "k") {
        event.preventDefault();
        if (palette) closePalette();
        else openPalette();
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        if (palette) {
          closePalette();
          return;
        }
        if (creating) {
          setCreating(false);
          return;
        }
        setFocusMode(false);
        return;
      }
      if (mod && event.key.toLowerCase() === "o") {event.preventDefault();void live.openRepository().catch(()=>{});return;}
      if (mod && ["1", "2", "3"].includes(event.key)) {
        event.preventDefault();
        navigate(
          event.key === "1"
            ? "history"
            : event.key === "2"
              ? "local-changes"
              : "inbox",
        );
        return;
      }
      if (mod && event.shiftKey && event.key.toLowerCase() === "n") {
        event.preventDefault();
        navigate("inbox");
        setCreating(true);
        return;
      }
      if (isEditingTarget(event.target) || palette) return;
      if (event.key.toLowerCase() === "f" && isPRView && pr && !creating) {
        event.preventDefault();
        if (event.shiftKey) command("toggle-fullscreen");
        else toggleFocus();
      }
      if (event.key === "/" && !focusMode) {
        event.preventDefault();
        searchRef.current?.focus();
      }
      if (
        ["j", "k"].includes(event.key) &&
        isPRView &&
        !focusMode &&
        visiblePRs.length
      ) {
        event.preventDefault();
        const index = visiblePRs.findIndex((p) => p.number === selected);
        setSelected(
          visiblePRs[
            Math.max(
              0,
              Math.min(
                visiblePRs.length - 1,
                index + (event.key === "j" ? 1 : -1),
              ),
            )
          ].number,
        );
      }
    };
    window.addEventListener("keydown", keyboard);
    return () => {
      stop();
      window.removeEventListener("keydown", keyboard);
    };
  }, [
    navigate,
    notify,
    openPalette,
    closePalette,
    toggleFocus,
    isPRView,
    pr,
    creating,
    focusMode,
    palette,
    visiblePRs,
    selected,
  ]);
  useEffect(() => () => clearTimeout(toastTimer.current), []);

  const reset = () => {
    if(live.enabled)live.useDemo();
    setPrs(seedPullRequests());
    setHistory(seedHistory);
    setLocalChanges(seedLocalChanges.map((f) => ({ ...f })));
    setBranch("main");
    setSelected(142);
    navigate("inbox");
    setTab("changes");
    notify("Sample workspace restored.");
  };
  const checkout = () => {
    if (live.enabled && pr) return live.git("checkoutPullRequest",String(pr.number));
    if (pr) {
      setBranch(pr.head);
      notify(
        `Checked out ${pr.head} in the sample workspace. Local changes are preserved.`,
      );
    }
  };
  const selectPR = (number: number) => {
    setReviewLocation(undefined);
    setSelected(number);
    setCreating(false);
  };
  const openNotice=async(notice:PullRequestNotice)=>{
    const target=Object.values(live.repositoryCards).find(repo=>repo.host?.toLowerCase()===notice.host.toLowerCase()&&repo.owner?.toLowerCase()===notice.owner.toLowerCase()&&repo.repo?.toLowerCase()===notice.repo.toLowerCase());
    if(!target){notify(`Open ${notice.owner}/${notice.repo} to view pull request #${notice.number}.`);return}
    if(target.path!==live.repository?.root||!live.enabled)await live.openRepository(target.path);
    navigate('inbox');setFilter(notice.type==='merged'?'merged':notice.type==='closed'?'closed':'open');setSelected(notice.number);setTab('conversation');
  };
  const navTitles: Record<Destination, string> = {
    inbox: "Review inbox",
    mine: "My pull requests",
    requested: "Review requested",
    assigned: "Assigned to you",
    drafts: "Draft pull requests",
    history: "History",
    "local-changes": "Local changes",
    settings: "Settings",
  };
  const actions = [
    {
      label: "Review inbox",
      detail: "Cmd/Ctrl 3",
      icon: "inbox",
      run: () => navigate("inbox"),
    },
    {
      label: "Pull requests assigned to you",
      detail: "",
      icon: "user",
      run: () => navigate("assigned"),
    },
    {
      label: "Repository history",
      detail: "Cmd/Ctrl 1",
      icon: "history",
      run: () => navigate("history"),
    },
    {
      label: "Local changes",
      detail: "Cmd/Ctrl 2",
      icon: "files",
      run: () => navigate("local-changes"),
    },
    {
      label: "Create pull request",
      detail: "Cmd/Ctrl Shift N",
      icon: "plus",
      run: () => {
        navigate("inbox");
        setCreating(true);
      },
    },
    {
      label: focusMode ? "Exit expanded review" : "Expand code review",
      detail: "F",
      icon: "expand",
      run: () => {
        if (!isPRView) navigate("inbox");
        if (isPRView && !visiblePRs.length) {
          notify("Select a pull request to expand its review.");
          return;
        }
        setCreating(false);
        setTab("changes");
        setFocusMode(!focusMode);
      },
    },
    {
      label: "Toggle window full screen",
      detail: "Shift F",
      icon: "expand",
      run: () =>
        void toggleFullscreen().catch(() =>
          notify("Full screen is unavailable in this window."),
        ),
    },
    {
      label: "Use system appearance",
      detail: "",
      icon: "monitor",
      run: () => setTheme("system"),
    },
    {
      label: "Use light appearance",
      detail: "",
      icon: "sun",
      run: () => setTheme("light"),
    },
    {
      label: "Use dark appearance",
      detail: "",
      icon: "moon",
      run: () => setTheme("dark"),
    },
  ];

  const commands = actions.filter((action) =>
    action.label.toLowerCase().includes(commandQuery.toLowerCase()),
  );
  return (
    <div className={`app-shell ${focusMode ? "review-focused" : ""} ${live.enabled && live.repository && !focusMode && destination !== 'settings' ? 'has-repository-toolbar' : ''}`}>
      <aside className="sidebar" aria-label="Workspace navigation">
        <div className="brand">
          <span className="brand-mark">
            <Icon name="branch" size={29} />
          </span>
          <div>
            <strong>M.I.E.R.</strong>
            <span>Mag ik een review</span>
          </div>
        </div>
        <nav className="primary-nav">
          {(
            [
              ["inbox", "inbox", "Inbox", undefined],
              ["mine", "pull-request", "My pull requests", undefined],
              [
                "requested",
                "check-circle",
                "Review requested",
                live.enabled ? undefined : reviewRequested.length,
              ],
              ["assigned", "user", "Assigned to you", live.enabled ? undefined : openPRs.filter(pr=>pr.assignees.includes(currentUser)).length],
              [
                "drafts",
                "file",
                "Drafts",
                live.enabled ? undefined : openPRs.filter((p) => p.status === "draft").length,
              ],
            ] as const
          ).map(([id, icon, label, count]) => (
            <button
              key={id}
              className={`nav-item ${destination === id ? "selected" : ""}`}
              onClick={() => navigate(id)}
            >
              <Icon name={icon} />
              <span>{label}</span>
              {count !== undefined && <Count>{live.enabled&&live.loadingPullRequests&&!prs.length?<span className="skeleton skeleton-count" aria-label="Loading count"/>:count}</Count>}
            </button>
          ))}
        </nav>
        <div className="nav-section-heading">
          <span>Repositories</span>
          <button
            className="icon-button"
            title="Open repository"
            aria-label="Open repository"
            onClick={() => {
              if(live.available)void live.openRepository().catch(()=>{});
              else notify("Open the desktop app to connect a repository.");
            }}
          >
            <Icon name="plus" size={15} />
          </button>
        </div>
        <div className="repo-nav">
          {(live.enabled ? [...new Set([...(live.repository?[live.repository.root]:[]),...live.recent])] : ["mag-ik-een-review", "design-system"]).map(path => {
            const name=live.enabled?live.repositoryCards[path]?.name??repositoryName(path):path;
            const active=live.enabled?live.repository?.root===path:repository===path;
            return <button key={path} className={`nav-item ${active?"repository-selected":""}`} title={live.enabled?`${name} — ${path}`:name} disabled={live.busy} onClick={()=>{
              if(live.enabled){if(!active)void live.openRepository(path).catch(()=>{});setRepository("all")}
              else setRepository(active?"all":path);
              navigate("inbox");
            }}><Icon name={live.enabled&&live.repositoryCards[path]?.host?'github':'folder'}/><span className="sidebar-repository-name">{name}</span>{active&&<Icon name="check" size={14}/>}</button>
          })}
        </div>
        <div className="nav-section-heading workspace-heading">
          <span>Local workspace</span>
        </div>
        <button
          className={`nav-item ${destination === "history" ? "selected" : ""}`}
          onClick={() => navigate("history")}
        >
          <Icon name="history" />
          <span>History</span>
          <kbd>1</kbd>
        </button>
        <button
          className={`nav-item ${destination === "local-changes" ? "selected" : ""}`}
          onClick={() => navigate("local-changes")}
        >
          <Icon name="files" />
          <span>Changes</span>
          <Count>{localChanges.length}</Count>
        </button>
        <div className="sidebar-bottom">
          <div className="branch-indicator">
            <Icon name="branch" size={14} />
            <span title={branch}>{branch}</span>
            <span className="small-dot" />
          </div>
          <button
            className={`nav-item ${destination === "settings" ? "selected" : ""}`}
            onClick={() => navigate("settings")}
          >
            <Icon name="settings" />
            <span>Settings</span>
          </button>
          <div className="sidebar-user">
            {live.enabled&&!live.account?.authenticated?<span className="avatar anonymous-avatar"><Icon name="user" size={18}/></span>:<Avatar user={currentUser}/>}
            <div>
              <strong>{live.enabled&&!live.account?.authenticated?"Your workspace":personLabel(currentUser)}</strong>
              <span>{live.enabled ? live.account?.authenticated ? live.account.host : 'Connect an account' : 'Demo workspace'}</span>
            </div>
            <div className="appearance-switch">
              {(["system", "light", "dark"] as const).map((t) => (
                <button
                  key={t}
                  className={`icon-button ${theme === t ? "active" : ""}`}
                  title={`${t[0].toUpperCase() + t.slice(1)} appearance`}
                  aria-label={`${t} appearance`}
                  aria-pressed={theme === t}
                  onClick={() => setTheme(t)}
                >
                  <Icon
                    name={
                      t === "system"
                        ? "monitor"
                        : t === "light"
                          ? "sun"
                          : "moon"
                    }
                    size={14}
                  />
                </button>
              ))}
            </div>
          </div>
        </div>
      </aside>

      <header className="app-topbar">
        {focusMode ? (
          <button
            className="btn btn-ghost back-inbox"
            onClick={() => setFocusMode(false)}
          >
            <Icon name="arrow-left" />
            Back to inbox
          </button>
        ) : (
          <RepositorySelector
            items={live.enabled ? [...new Set([...(live.repository?[live.repository.root]:[]),...live.recent])].map(path=>live.repositoryCards[path]??{path,name:repositoryName(path)}) : [{path:'all',name:'All repositories',branch,host:'github.com',openPullRequests:openPRs.length},...['mag-ik-een-review','design-system'].map(name=>({path:name,name,branch:'main',host:'github.com',openPullRequests:openPRs.filter(pr=>pr.repository===name).length}))]}
            selectedPath={live.enabled?live.repository?.root??'':repository}
            loading={live.enabled&&live.loadingPullRequests}
            busy={live.busy}
            onSelect={path=>{if(live.enabled){if(path!==live.repository?.root)void live.openRepository(path).catch(()=>{});setRepository('all')}else setRepository(path);navigate('inbox')}}
            onOpen={live.available?()=>void live.openRepository().catch(()=>{}):undefined}
          />
        )}
        {focusMode && pr && (
          <span className="focus-context">
            <Icon name="pull-request" size={15} />
            {pr.repository}
            <span>/</span>#{pr.number}
            <span className="focus-mode-label">Expanded review</span>
          </span>
        )}
        <div className="topbar-spacer" />
        <EditorActions root={live.enabled?live.repository?.root:undefined} githubURL={live.enabled?isPRView&&pr?.provider?.url?pr.provider.url:live.identity?`https://${live.identity.host}/${encodeURIComponent(live.identity.owner)}/${encodeURIComponent(live.identity.repo)}`:undefined:undefined} notify={notify}/>
        <button className="global-search" onClick={openPalette}>
          <Icon name="search" size={16} />
          <span>Search or jump to…</span>
          <kbd>⌘ / Ctrl K</kbd>
        </button>
        {focusMode && (
          <button
            className="icon-button"
            title="Toggle window full screen (Shift F)"
            aria-label="Toggle window full screen"
            onClick={() =>
              void toggleFullscreen().catch(() =>
                notify("Full screen is unavailable in this window."),
              )
            }
          >
            <Icon name="expand" />
          </button>
        )}
      </header>

      {live.enabled && live.repository && !focusMode && destination !== 'settings' && <RepositoryToolbar key={live.repository.root} repository={live.repository} busy={live.busy} onGit={live.git} onRefresh={live.refresh}/>}

      <main
        aria-busy={!!live.openingRepository||(isPRView&&live.loadingPullRequests)}
        className={`main-workspace ${isPRView ? "pr-layout" : "single-layout"}`}
      >
        {live.enabled&&live.openingRepository&&<RepositorySkeleton name={live.openingRepository}/>}
        {isPRView && (!live.enabled || !!live.repository) && (
          <section className="inbox-pane" aria-label="Pull request inbox">
            <div className="inbox-heading">
              <h1>{navTitles[destination]}</h1>
              <button
                className="icon-button"
                title="New pull request"
                aria-label="New pull request"
                onClick={() => setCreating(true)}
              >
                <Icon name="plus" size={18} />
              </button>
            </div>
            <div className="inbox-filters">
              {(["open", "all", "merged", "closed"] as const).map((f) => (
                <button
                  key={f}
                  className={filter === f ? "active" : ""}
                  onClick={() => setFilter(f)}
                >
                  {f[0].toUpperCase() + f.slice(1)}
                  {f === "open" && (openFilterCount !== null || (live.enabled && live.loadingPullRequests)) && (
                    <Count>
                      {
                        openFilterCount === null ? <span className="skeleton skeleton-count" aria-label="Loading open pull request count"/> : openFilterCount
                      }
                    </Count>
                  )}
                </button>
              ))}
            </div>
            <label className="inbox-search">
              <Icon name="search" size={15} />
              <input
                ref={searchRef}
                aria-label="Search pull requests"
                placeholder="Filter pull requests…"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
              <kbd>/</kbd>
            </label>
            <div className="pr-list">
              <div className="list-section-label">
                {destination === "assigned" ? "Assigned to you" : destination === "mine"
                  ? "Created by you"
                  : destination === "drafts"
                    ? "Work in progress"
                    : "Pull requests"}
                <span>{live.enabled&&live.loadingPullRequests&&!visiblePRs.length?<span className="skeleton skeleton-count" aria-label="Loading pull request count"/>:live.enabled&&live.pullRequestTotal!==null&&live.pullRequestTotal>visiblePRs.length?`${visiblePRs.length} of ${live.pullRequestTotal}`:visiblePRs.length}</span>
              </div>
              {live.enabled&&live.loadingPullRequests&&!visiblePRs.length ? <InboxSkeleton/> : visiblePRs.length ? (
                visiblePRs.map((item) => (
                  <button
                    key={item.number}
                    className={`pr-list-item ${selected === item.number && !creating ? "selected" : ""}`}
                    aria-pressed={selected === item.number && !creating}
                    onClick={() => selectPR(item.number)}
                  >
                    <Icon
                      name={
                        item.status === "merged"
                          ? "git-merge"
                          : item.status === "draft"
                            ? "clock"
                            : item.status === "closed"
                              ? "close"
                              : "pull-request"
                      }
                      className={`pr-state-icon state-${item.status}`}
                      size={18}
                    />
                    <div className="pr-list-content">
                      <strong>{item.title}</strong>
                      <span className="pr-list-meta">
                        #{item.number}
                        <span>by {personLabel(item.author).split(" ")[0]}</span>
                        <span className="pr-age">
                          {relativeTime(item.createdAt)}
                        </span>
                      </span>
                      <span className="pr-list-bottom">
                        <span>{item.repository}</span>
                        <span>
                          <Icon name="comment" size={12} />
                          {item.provider?.detailLoaded===false ? item.provider.countsKnown?((item.provider.counts?.comments??0)+(item.provider.counts?.threads??0)):"…" : item.comments.length + item.threads.length}
                          {item.provider?.detailLoaded===false ? null : item.checks.some((c) => c.status === "failed") ? (
                            <Icon
                              name="alert-circle"
                              className="text-red"
                              size={13}
                            />
                          ) : item.status === "draft" ? (
                            <span className="draft-word">Draft</span>
                          ) : (
                            <Icon
                              name={
                                item.checks.every((c) => c.status === "passed")
                                  ? "check-circle"
                                  : "clock"
                              }
                              className={
                                item.checks.every((c) => c.status === "passed")
                                  ? "text-green"
                                  : "muted"
                              }
                              size={13}
                            />
                          )}
                        </span>
                      </span>
                    </div>
                  </button>
                ))
              ) : live.enabled && live.pullRequestPageError ? null : (
                <div className="empty-state">
                  <Icon name="search" size={24} />
                  <strong>No pull requests found</strong>
                  <span>Try another filter or repository.</span>
                  <button
                    className="btn"
                    onClick={() => {
                      setQuery("");
                      setFilter("all");
                    }}
                  >
                    Clear filters
                  </button>
                </div>
              )}
              {live.enabled && live.pullRequestPageError && (
                <div className="inbox-page-error" role="alert">
                  <span>{live.pullRequestPageError}</span>
                  <button className="btn" aria-label={visiblePRs.length ? "Retry loading more pull requests" : "Retry loading pull requests"} onClick={() => void live.retryPullRequests()}>Retry</button>
                </div>
              )}
              {live.enabled && live.hasMorePullRequests && !live.pullRequestPageError && (
                <div className="inbox-pagination">
                  <button className="btn" aria-label="Load more pull requests" disabled={live.loadingMorePullRequests} onClick={() => void live.loadMorePullRequests()}>{live.loadingMorePullRequests ? "Loading…" : "Load more"}</button>
                </div>
              )}
              {live.enabled && live.pullRequestResultLimit > 0 && (
                <p className="inbox-result-limit">GitHub returns up to {live.pullRequestResultLimit.toLocaleString()} matching results. Narrow your search to find older pull requests.</p>
              )}
            </div>
            <div className="inbox-tip">
              <Icon name="keyboard" size={14} />
              <span>
                <kbd>J</kbd> <kbd>K</kbd> to move through the inbox
              </span>
            </div>
          </section>
        )}
        {live.enabled && !live.repository && destination !== "settings" && <RepositoryWelcome live={live} onOpen={()=>void live.openRepository().catch(()=>{})} onDemo={live.useDemo}/>}
        {isPRView && (!live.enabled || !!live.repository) &&
          (creating ? (live.enabled ? <LiveCreatePullRequest key={JSON.stringify([live.repository?.root,live.identity])} live={live} onCancel={()=>setCreating(false)} onCreated={number=>{setSelected(number);setCreating(false);setTab('conversation');setFilter('open');setQuery('');setDestination('inbox')}}/> :
            <CreatePullRequest
              prs={prs}
              onCancel={() => setCreating(false)}
              onCreate={(next) => {
                setPrs((items) => [next, ...items]);
                setRepository("all");
                setFilter("open");
                setDestination("inbox");
                setSelected(next.number);
                setTab("conversation");
                setCreating(false);
                notify(`Created sample pull request #${next.number}.`);
              }}
            />
          ) : pr && (live.enabled || visiblePRs.length) ? (
            <section
              className="detail-pane"
              aria-label={`Pull request ${pr.number}`}
            >
              <PullRequestHeader
                pr={pr}
                tab={tab}
                setTab={setTab}
                onUpdate={updatePR}
                onCheckout={checkout}
                branch={branch}
                focusMode={focusMode}
                onToggleFocus={toggleFocus}
                notify={reviewNotify}
                busy={live.busy}
              />
              {live.enabled && pr.provider?.hasNewCommits && <div className="new-commits-banner" role="status"><Icon name="git-commit"/><div><strong>New commits are available</strong><span>Your current diff and draft comments are preserved.</span></div><button className="btn" disabled={live.busy} onClick={()=>void live.acceptNewCommits().catch(()=>{})}>Refresh changes</button></div>}
              {live.enabled && live.error && <div className="live-error" role="status"><Icon name="alert-circle"/><span>{live.error}</span><button className="btn btn-ghost" onClick={()=>void live.refresh()}>Retry</button></div>}
              <div
                className={`detail-content tab-${tab}`}
                role="tabpanel"
                id="pr-review-panel"
                aria-labelledby={`pr-tab-${tab}`}
              >
                {live.enabled&&pr.provider?.detailLoaded===false ? <ReviewSkeleton/> : <>
                {tab === "changes" && (
                  <CodeIntelligence key={`intelligence-${pr.number}`} root={live.enabled?live.repository?.root??'':''} host={pr.provider?.host} owner={pr.provider?.owner} repo={pr.provider?.repo} pullRequest={pr.number} headSha={pr.headSha} baseSha={pr.baseSha} enabled={live.enabled} onOpenSettings={()=>navigate('settings')}>
                  <ReviewWorkspace
                    key={pr.number}
                    pr={pr}
                    onUpdate={updatePR}
                    onNotify={reviewNotify}
                    focusMode={focusMode}
                    onToggleFocus={toggleFocus}
                    busy={live.busy}
                    requestedLocation={reviewLocation}
                  />
                  </CodeIntelligence>
                )}
                {tab === "conversation" && (
                  <Conversation
                    pr={pr}
                    onUpdate={updatePR}
                    notify={reviewNotify}
                    busy={live.busy}
                    showChanges={(path, threadId) => {
                      setReviewLocation({ path, threadId });
                      setTab("changes");
                    }}
                    onMerge={async(method) => {
                      if(live.enabled)return live.merge(pr,method);
                      const next=mergePullRequest(pr,method);
                      await updatePR(next);
                      setHistory(items=>[{sha:next.merge!.sha,subject:`Merge pull request #${next.number}: ${next.title}`,author:currentUser,createdAt:when()},...items]);
                      notify(`Pull request #${next.number} merged in the sample workspace.`);
                    }}
                  />
                )}
                {tab === "commits" && (
                  <div className="commits-pane">
                    <div className="section-intro">
                      <h2>{pr.commits.length} commits</h2>
                      <span>
                        Changes from <code>{pr.head}</code> into{" "}
                        <code>{pr.base}</code>
                      </span>
                    </div>
                    {pr.commits.map((commit) => (
                      <div className="commit-row" key={commit.sha}>
                        <Icon name="git-commit" size={18} />
                        <div>
                          <strong>{commit.subject}</strong>
                          <span>
                            {people[commit.author].name} committed{" "}
                            {relativeTime(commit.createdAt)}
                          </span>
                        </div>
                        <code>{commit.sha.slice(0, 7)}</code>
                        <button
                          className="icon-button"
                          title={live.enabled?"Inspect this commit's changed files":"Review the sample pull request changes"}
                          aria-label={live.enabled?`Inspect ${commit.subject}`:`Review changes for ${commit.subject}`}
                          onClick={() => live.enabled?setInspectedCommit(commit):setTab("changes")}
                        >
                          <Icon name="arrow-right" />
                        </button>
                      </div>
                    ))}
                  </div>
                )}
                {tab === "checks" && <Checks pr={pr} />}
                </>}
              </div>
              {(!live.enabled||pr.provider?.detailLoaded)&&<ReviewFooter
                key={`review-${pr.number}`}
                pr={pr}
                onUpdate={updatePR}
                onNotify={reviewNotify}
                busy={live.busy}
              />}
            </section>
          ) : live.enabled&&live.loadingPullRequests ? (<section className="detail-pane"><ReviewSkeleton label="Loading your review workspace…"/></section>) : (
            <section className="detail-pane empty-state">
              <Icon name="inbox" size={36} />
              <h2>{live.enabled&&live.error?"Unable to load pull requests":"Your review queue is clear"}</h2>
              {live.enabled&&live.error&&<p className="form-error" role="alert">{live.error}</p>}
              <p>
                {live.enabled?'Choose a different filter to explore your pull requests.':'Choose a different filter to explore the sample pull requests.'}
              </p>
              <button
                className="btn"
                onClick={() => {
                  navigate("inbox");
                  setRepository("all");
                  setFilter("all");
                }}
              >
                Show all pull requests
              </button>
            </section>
          ))}
        {destination === "history" && (!live.enabled || !!live.repository) && (
          <HistoryView
            key={live.enabled ? live.repository?.root : 'demo'}
            history={history}
            branch={branch}
            repositoryName={live.enabled?live.repository?.name??"Repository":"mag-ik-een-review (sample)"}
            onReview={() => navigate("inbox")}
            root={live.repository?.root}
            loadCommitDiff={live.enabled?live.commitDiff:undefined}
          />
        )}
        {destination === "local-changes" && (!live.enabled || !!live.repository) && (
          <LocalChangesView
            key={live.enabled ? live.repository?.root : 'demo'}
            changes={localChanges}
            branch={branch}
            root={live.enabled ? live.repository?.root : undefined}
            headSha={live.repository?.headSha}
            host={live.identity?.host}
            owner={live.identity?.owner}
            repo={live.identity?.repo}
            onOpenSettings={() => navigate("settings")}
            onChange={setLocalChanges}
            onStage={live.enabled?(paths,stage)=>live.git(stage?'stage':'unstage',paths):undefined}
            loadDiff={live.enabled?live.diff:undefined}
            busy={live.busy}
            onCommit={(message) => {
              if(live.enabled)return live.git("commit",message);
              const staged = localChanges.filter((f) => f.staged);
              if (!staged.length || !message.trim()) return;
              setHistory((items) => [
                {
                  sha: newId().replace(/-/g, "").slice(0, 40),
                  subject: message.trim(),
                  author: currentUser,
                  createdAt: when(),
                },
                ...items,
              ]);
              setLocalChanges((items) => items.filter((f) => !f.staged));
              notify(
                `Committed ${staged.length} staged ${staged.length === 1 ? "file" : "files"} in the sample workspace.`,
              );
            }}
          />
        )}
        {destination === "settings" && (
          <Settings theme={theme} setTheme={setTheme} onReset={reset} accountSettings={<AccountSettings live={live}/>} notificationSettings={<NotificationSettings repositoryCards={live.repositoryCards} recent={live.recent} activePath={live.repository?.root} available={live.available}/>} codeReviewSettings={<><CodeReviewSettings/><LanguageServerSettings root={live.repository?.root} available={live.available}/></>} />
        )}
      </main>

      <footer className="app-statusbar">
        <span className="status-prototype">
          <span className="small-dot" />
          {live.enabled ? live.busy ? "Updating…" : live.error ? "Sync needs attention" : "Live workspace" : "Demo workspace"}<span className="status-separator">·</span>{live.enabled ? live.updatedAt ? `Synced ${relativeTime(live.updatedAt)} · every 15s` : 'Open a repository to begin' : 'Sample repositories'}
        </span>
        <span className="status-context">
          <Icon name="github" size={13} />
          {live.account?.authenticated ? live.account.host : "GitHub"}
          <Icon name="branch" size={13} />
          <span title={branch}>{branch}</span>
        </span>
        <span className="status-help">
          {focusMode
            ? "Esc to return · [ ] to switch files"
            : "F to expand code review"}
        </span>
      </footer>
      <NotificationToasts onOpen={openNotice}/>
      {inspectedCommit&&live.enabled&&provider&&<Modal title={inspectedCommit.subject} onClose={()=>setInspectedCommit(null)}><div className="pull-request-commit-preview"><code>{inspectedCommit.sha}</code><CommitChanges requestKey={JSON.stringify([provider.host,provider.owner,provider.repo,inspectedCommit.sha])} sha={inspectedCommit.sha} loadDiff={loadPRCommit}/></div></Modal>}
      {toast && (
        <div className="toast" role="status">
          <Icon name="check-circle" size={18} />
          {toast}
          <button
            className="icon-button"
            aria-label="Dismiss notification"
            onClick={() => setToast("")}
          >
            <Icon name="close" size={14} />
          </button>
        </div>
      )}
      {palette && (
        <div
          className="palette-backdrop"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) closePalette();
          }}
        >
          <section
            className="command-palette"
            role="dialog"
            aria-modal="true"
            aria-label="Command palette"
            onKeyDown={(e) => {
              if (["ArrowDown", "ArrowUp"].includes(e.key)) {
                e.preventDefault();
                setCommandIndex((index) =>
                  Math.max(
                    0,
                    Math.min(
                      commands.length - 1,
                      index + (e.key === "ArrowDown" ? 1 : -1),
                    ),
                  ),
                );
                return;
              }
              if (e.key === "Enter") {
                e.preventDefault();
                commands[commandIndex]?.run();
                closePalette();
                return;
              }
              if (e.key === "Tab") {
                const nodes = Array.from(
                  e.currentTarget.querySelectorAll<HTMLElement>("input,button"),
                );
                const first = nodes[0],
                  last = nodes[nodes.length - 1];
                if (e.shiftKey && document.activeElement === first) {
                  e.preventDefault();
                  last.focus();
                } else if (!e.shiftKey && document.activeElement === last) {
                  e.preventDefault();
                  first.focus();
                }
              }
            }}
          >
            <label>
              <Icon name="search" size={20} />
              <input
                autoFocus
                aria-label="Search commands"
                placeholder="Where would you like to go?"
                value={commandQuery}
                onChange={(e) => {
                  setCommandQuery(e.target.value);
                  setCommandIndex(0);
                }}
              />
              <button
                className="icon-button"
                aria-label="Close command palette"
                onClick={closePalette}
              >
                <kbd>Esc</kbd>
              </button>
            </label>
            <div className="command-results">
              {commands.map((a, index) => (
                <button
                  key={a.label}
                  className={index === commandIndex ? "command-active" : ""}
                  onFocus={() => setCommandIndex(index)}
                  onMouseEnter={() => setCommandIndex(index)}
                  onClick={() => {
                    a.run();
                    closePalette();
                    setCommandQuery("");
                  }}
                >
                  <Icon name={a.icon} />
                  <span>{a.label}</span>
                  {a.detail && <kbd>{a.detail}</kbd>}
                </button>
              ))}
            </div>
            <div className="palette-caption">
              Navigate your workspace without leaving the keyboard.
            </div>
          </section>
        </div>
      )}
    </div>
  );
}

function PullRequestHeader({
  pr,
  tab,
  setTab,
  onUpdate,
  onCheckout,
  branch,
  focusMode,
  onToggleFocus,
  notify,
  busy = false,
}: {
  pr: PullRequest;
  tab: ReviewTab;
  setTab: (tab: ReviewTab) => void;
  onUpdate: (p: PullRequest) => void | Promise<void>;
  onCheckout: () => void | Promise<void>;
  branch: string;
  focusMode: boolean;
  onToggleFocus: () => void;
  notify: (s: string) => void;
  busy?: boolean;
}) {
  const [menu, setMenu] = useState(false),
    [editing, setEditing] = useState(false),
    [title, setTitle] = useState(pr.title),
    [body, setBody] = useState(pr.body);
  const [saving, setSaving] = useState(false), [error, setError] = useState("");
  const loaded = pr.provider?.detailLoaded !== false;
  const canEdit = loaded && pr.provider?.viewerCanUpdate !== false;
  const locked = busy || saving;
  const counts = pr.provider?.counts;
  const contextKey = `${pr.provider?.host ?? "local"}/${pr.provider?.owner ?? ""}/${pr.provider?.repo ?? pr.repository}#${pr.number}`;
  const activeContext = useRef(contextKey); activeContext.current = contextKey;
  const countsKnown = loaded || pr.provider?.countsKnown === true;
  const countFor = (name: "files" | "commits" | "conversation") => loaded ? name === "files" ? pr.files.length : name === "commits" ? pr.commits.length : pr.comments.length + pr.threads.length : !countsKnown ? undefined : name === "files" ? counts?.changedFiles : name === "commits" ? counts?.commits : (counts?.comments ?? 0) + (counts?.threads ?? 0);
  const menuRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    setMenu(false);
    setEditing(false);
    setTitle(pr.title);
    setBody(pr.body);
    setError("");
  }, [pr.number]);
  useEffect(() => {
    if (!menu) return;
    const close = (e: PointerEvent) => {
      if (!menuRef.current?.contains(e.target as Node)) setMenu(false);
    };
    window.addEventListener("pointerdown", close);
    return () => window.removeEventListener("pointerdown", close);
  }, [menu]);
  const mutate = async (operation: () => void | Promise<void>, done?: () => void) => {
    if (locked) return;
    setSaving(true); setError("");
    try { await operation(); if (activeContext.current === contextKey) done?.(); }
    catch (issue) { if (activeContext.current === contextKey) setError(issue instanceof Error ? issue.message : "The pull request could not be updated."); }
    finally { setSaving(false); }
  };
  const lifecycle = (status: PullRequest["status"]) => {
    if (!canEdit) return;
    void mutate(() => onUpdate({ ...pr, status, updatedAt: when() }), () => {
      setMenu(false);
      notify(`Pull request #${pr.number} ${status === "open" ? "opened for review" : status === "closed" ? "closed" : status}.`);
    });
  };
  return (
    <div className={`pr-header ${focusMode ? "compact" : ""}`}>
      <div className="pr-header-top">
        <div className="pr-reference">
          <StatusBadge status={pr.status} />
          <span>#{pr.number}</span>
          <span className="muted">{pr.repository}</span>
        </div>
        <div className="pr-header-actions">
          <button className="btn" disabled={locked} onClick={() => void mutate(onCheckout)}>
            <Icon name="branch" size={15} />
            {branch === pr.head ? "Checked out" : "Checkout branch"}
          </button>
          <button
            className="icon-button header-expand"
            title={
              focusMode
                ? "Exit expanded review (Esc)"
                : "Expand code review (F)"
            }
            aria-label={
              focusMode ? "Exit expanded review" : "Expand code review"
            }
            onClick={onToggleFocus}
          >
            <Icon name={focusMode ? "collapse" : "expand"} />
          </button>
          <div className="popover-anchor" ref={menuRef}>
            <button
              className="icon-button"
              aria-label="Pull request actions"
              aria-expanded={menu}
              onClick={() => setMenu((v) => !v)}
            >
              <Icon name="more" />
            </button>
            {menu && (
              <div className="popover-menu">
                <button
                  disabled={!canEdit || locked}
                  onClick={() => {
                    setTitle(pr.title);
                    setBody(pr.body);
                    setEditing(true);
                    setMenu(false);
                  }}
                >
                  <Icon name="file" />
                  Edit title and description
                </button>
                {pr.status === "draft" && (
                  <button disabled={!canEdit || locked} onClick={() => lifecycle("open")}>
                    <Icon name="check-circle" />
                    Mark ready for review
                  </button>
                )}
                {pr.status === "closed" && (
                  <button disabled={!canEdit || locked} onClick={() => lifecycle("open")}>
                    <Icon name="pull-request" />
                    Reopen pull request
                  </button>
                )}
                {["open", "draft"].includes(pr.status) && (
                  <button disabled={!canEdit || locked} onClick={() => lifecycle("closed")}>
                    <Icon name="close" />
                    Close pull request
                  </button>
                )}
                <button
                  onClick={() => {
                    void mutate(async () => {
                      if (!navigator.clipboard) throw new Error("Clipboard is unavailable.");
                      await navigator.clipboard.writeText(pr.provider?.url || `${pr.repository}#${pr.number}: ${pr.title}`);
                    }, () => { notify("Pull request reference copied."); setMenu(false); });
                  }}
                >
                  <Icon name="copy" />
                  Copy reference
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      {editing ? (
        <form
          className="edit-pr-form"
          onSubmit={(e) => {
            e.preventDefault();
            if (title.trim() && canEdit) void mutate(() => onUpdate({ ...pr, title: title.trim(), body, updatedAt: when() }), () => setEditing(false));
          }}
        >
          <input
            autoFocus
            aria-label="Pull request title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
            disabled={saving}
          />
          <textarea
            aria-label="Pull request description"
            value={body}
            onChange={(e) => setBody(e.target.value)}
            rows={4}
            disabled={saving}
          />
          <div>
            <button className="btn btn-primary" disabled={!title.trim() || !canEdit || locked}>
              {saving ? "Saving…" : "Save changes"}
            </button>
            <button
              type="button"
              className="btn"
              disabled={saving}
              onClick={() => setEditing(false)}
            >
              Cancel
            </button>
          </div>
        </form>
      ) : (
        <>
          <h1>{pr.title}</h1>
          <div className="pr-author-line">
            <Avatar user={pr.author} size={20} />
            <strong><ActorName user={pr.author}/></strong>
            <span>opened {relativeTime(pr.createdAt)}</span>
            {countFor("commits") !== undefined && <><span>·</span><span>{countFor("commits")} commits</span></>}
            {countFor("files") !== undefined && <><span>·</span><span>{countFor("files")} files changed</span></>}
            {!loaded && <span className="skeleton-header-meta" aria-hidden="true"><span className="skeleton"/><span className="skeleton"/></span>}
          </div>
        </>
      )}
      <div className="branch-flow">
        <code>
          <Icon name="branch" size={12} />
          {pr.head}
        </code>
        <Icon name="arrow-right" size={14} />
        <code>
          <Icon name="branch" size={12} />
          {pr.base}
        </code>
        {countsKnown && <span className="diff-summary">
          <span className="text-green">
            +{loaded ? pr.files.reduce((n, f) => n + f.additions, 0) : counts?.additions}
          </span>
          <span className="text-red">
            −{loaded ? pr.files.reduce((n, f) => n + f.deletions, 0) : counts?.deletions}
          </span>
        </span>}
      </div>
      <div
        className="detail-tabs"
        role="tablist"
        aria-label="Pull request sections"
        onKeyDown={(event) => {
          if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
          event.preventDefault();
          const tabs = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]'));
          const index = tabs.indexOf(document.activeElement as HTMLButtonElement);
          const next = event.key === "Home" ? 0 : event.key === "End" ? tabs.length - 1 : (index + (event.key === "ArrowRight" ? 1 : -1) + tabs.length) % tabs.length;
          tabs[next].click();
          tabs[next].focus();
        }}
      >
        {(
          [
            [
              "conversation",
              "comment",
              "Conversation",
              countFor("conversation"),
            ],
            ["changes", "code", "Changes", countFor("files")],
            ["commits", "git-commit", "Commits", countFor("commits")],
            ["checks", "check-circle", "Checks", loaded ? pr.checks.length : undefined],
          ] as const
        ).map(([id, icon, label, count]) => (
          <button
            key={id}
            role="tab"
            id={`pr-tab-${id}`}
            aria-controls="pr-review-panel"
            tabIndex={tab === id ? 0 : -1}
            aria-selected={tab === id}
            className={tab === id ? "active" : ""}
            onClick={() => setTab(id)}
          >
            <Icon name={icon} />
            <span>{label}</span>
            {count !== undefined && <Count>{count}</Count>}
          </button>
        ))}
      </div>
    </div>
  );
}

function Conversation({
  pr,
  onUpdate,
  notify,
  showChanges,
  onMerge,
  busy = false,
}: {
  pr: PullRequest;
  onUpdate: (p: PullRequest) => void | Promise<void>;
  notify: (s: string) => void;
  showChanges: (path: string, threadId: string) => void;
  onMerge: (method: MergeMethod) => void | Promise<void>;
  busy?: boolean;
}) {
  const [comment, setComment] = useState(""),
    [replyId, setReplyId] = useState<string | null>(null),
    [reply, setReply] = useState(""),
    [confirmMerge, setConfirmMerge] = useState(false),
    [method, setMethod] = useState<MergeMethod>("squash");
  const [saving, setSaving] = useState(false), [error, setError] = useState("");
  const [editingMetadata, setEditingMetadata] = useState(false);
  const [reviewers, setReviewers] = useState(""), [assignees, setAssignees] = useState(""), [labels, setLabels] = useState("");
  const loaded = pr.provider?.detailLoaded !== false;
  const locked = busy || saving;
  const canDiscuss = loaded && (!!pr.provider || ["open", "draft"].includes(pr.status));
  const canEditMetadata = loaded && pr.provider?.viewerCanUpdate !== false;
  const contextKey = `${pr.provider?.host ?? "local"}/${pr.provider?.owner ?? ""}/${pr.provider?.repo ?? pr.repository}#${pr.number}`;
  const activeContext = useRef(contextKey); activeContext.current = contextKey;
  const allowedMethods = pr.provider ? pr.provider.allowedMergeMethods ?? [] : ["squash", "merge", "rebase"];
  useEffect(() => {
    setComment("");
    setReplyId(null);
    setReply("");
    setConfirmMerge(false);
    setEditingMetadata(false);
    setError("");
  }, [pr.number]);
  useEffect(() => {
    if (allowedMethods.length && !allowedMethods.includes(method)) setMethod(allowedMethods[0] as MergeMethod);
  }, [allowedMethods.join(","), method]);
  const blockers = mergeBlockers(pr);
  const apply = async (next: PullRequest, done?: () => void) => {
    if (locked) return;
    setSaving(true); setError("");
    try { await onUpdate(next); if (activeContext.current === contextKey) done?.(); }
    catch (issue) { if (activeContext.current === contextKey) setError(issue instanceof Error ? issue.message : "The action could not be completed. Your draft has been kept."); }
    finally { setSaving(false); }
  };
  const addComment = (e: FormEvent) => {
    e.preventDefault();
    if (!comment.trim() || !canDiscuss) return;
    void apply({
      ...pr,
      comments: [
        ...pr.comments,
        {
          id: newId(),
          author: currentUser,
          body: comment.trim(),
          createdAt: when(),
        },
      ],
    }, () => { setComment(""); notify("Comment added."); });
  };
  if (!loaded) return <div className="conversation-pane" aria-busy="true"><div className="conversation-main"><div className="conversation-skeleton" aria-hidden="true"><span className="skeleton skeleton-avatar"/><div><div className="skeleton skeleton-title"/><div className="skeleton skeleton-line"/><div className="skeleton skeleton-line"/><div className="skeleton skeleton-line short"/><div className="metadata-skeleton"><div className="skeleton skeleton-meta"/><div className="skeleton skeleton-meta"/><div className="skeleton skeleton-meta"/></div><div className="skeleton skeleton-line"/><div className="skeleton skeleton-line short"/></div></div><span className="sr-only">Loading pull request discussion and review requirements.</span></div></div>;
  return (
    <div className="conversation-pane">
      <div className="conversation-main">
        {error && <p className="form-error" role="alert">{error}</p>}
        <article className="description-entry">
          <div className="entry-heading">
            <Avatar user={pr.author} />
            <strong><ActorName user={pr.author}/></strong>
            <span>opened this pull request</span>
            <span className="entry-time">{relativeTime(pr.createdAt)}</span>
          </div>
          <div className="description-body">
            {pr.body ? <Markdown>{pr.body}</Markdown> : <p className="muted">No description provided.</p>}
          </div>
        </article>
        <div className="pr-metadata">
          <div>
            <span>Reviewers</span>
            <div>
              {pr.reviewers.map((user) => (
                <span className="reviewer-person" key={user}>
                  {people[user] && <Avatar user={user} size={22} />}
                  <ActorName user={user} compact/>
                  <span className="sr-only">{latestReview(pr,user)?.decision === "approved" ? "Approved" : latestReview(pr,user)?.decision === "changes_requested" ? "Changes requested" : "Review requested"}</span>
                  <Icon
                    name={
                      latestReview(pr, user)?.decision === "approved"
                        ? "check-circle"
                        : latestReview(pr, user)?.decision ===
                            "changes_requested"
                          ? "alert-circle"
                          : "clock"
                    }
                    className={
                      latestReview(pr, user)?.decision === "approved"
                        ? "text-green"
                        : latestReview(pr, user)?.decision ===
                            "changes_requested"
                          ? "text-red"
                          : "muted"
                    }
                    size={13}
                  />
                </span>
              ))}
              {!pr.reviewers.length && <span className="muted">None requested</span>}
            </div>
          </div>
          <div>
            <span>Labels</span>
            <div>
              {pr.labels.map((label) => (
                <span className="label-chip" key={label}>
                  {label}
                </span>
              ))}
              {!pr.labels.length && <span className="muted">None</span>}
            </div>
          </div>
          <div><span>Assignees</span><div>{pr.assignees.map(user => <span className="reviewer-person" key={user}>{people[user] && <Avatar user={user} size={22}/>} <ActorName user={user}/></span>)}{!pr.assignees.length && <span className="muted">None</span>}</div></div>
        </div>
        <button className="btn btn-ghost metadata-edit-button" disabled={!canEditMetadata || locked} onClick={() => { setReviewers(pr.reviewers.join(", ")); setAssignees(pr.assignees.join(", ")); setLabels(pr.labels.join("\n")); setEditingMetadata(true); setError(""); }}><Icon name="settings" size={13}/>Edit reviewers, assignees and labels</button>
        {editingMetadata && <form className="metadata-editor" onSubmit={event => {
          event.preventDefault();
          if (!canEditMetadata) return;
          const users = (value: string) => [...new Set(value.split(/[,\n]/).map(item => item.trim()).filter(Boolean))];
          void apply({...pr, reviewers:users(reviewers),assignees:users(assignees),labels:[...new Set(labels.split("\n").map(item => item.trim()).filter(Boolean))]}, () => { setEditingMetadata(false); notify("Pull request metadata updated."); });
        }}><label>Reviewers<input aria-label="Requested reviewers" placeholder="GitHub usernames, separated by commas" value={reviewers} disabled={saving} onChange={event=>setReviewers(event.target.value)}/></label><label>Assignees<input aria-label="Pull request assignees" placeholder="GitHub usernames, separated by commas" value={assignees} disabled={saving} onChange={event=>setAssignees(event.target.value)}/></label><label>Labels<textarea aria-label="Pull request labels" placeholder="One label per line" rows={2} value={labels} disabled={saving} onChange={event=>setLabels(event.target.value)}/></label><div className="metadata-editor-actions"><button type="button" className="btn" disabled={saving} onClick={()=>setEditingMetadata(false)}>Cancel</button><button className="btn btn-primary" disabled={locked || !canEditMetadata}>{saving ? "Saving…" : "Save metadata"}</button></div></form>}
        {pr.reviews.map((review) => (
          <div className="timeline-event" key={review.id}>
            <span
              className={`timeline-icon ${review.decision === "approved" ? "text-green" : review.decision === "changes_requested" ? "text-red" : ""}`}
            >
              <Icon
                name={
                  review.decision === "approved"
                    ? "check-circle"
                    : review.decision === "changes_requested"
                      ? "alert-circle"
                      : "comment"
                }
              />
            </span>
            <div>
              <strong><ActorName user={review.author} author={pr.author}/></strong>{" "}
              {review.decision === "approved"
                ? "approved these changes"
                : review.decision === "changes_requested"
                  ? "requested changes"
                  : "reviewed this pull request"}
              <span className="muted"> · {relativeTime(review.createdAt)}</span>
              {review.body && <Markdown>{review.body}</Markdown>}
            </div>
          </div>
        ))}
        {pr.threads.map((thread) => (
          <article
            className={`conversation-thread ${thread.resolved ? "resolved" : ""}`}
            key={thread.id}
          >
            <div className="thread-file">
              <button
                className="btn btn-ghost"
                disabled={thread.outdated || thread.line <= 0 || !pr.files.some(file => file.path === thread.path && file.lines.some(line => thread.side === "old" ? line.oldLine === thread.line : line.newLine === thread.line))}
                onClick={() => showChanges(thread.path, thread.id)}
              >
                <Icon name="code" size={14} />
                <code>
                  {thread.path.split("/").pop()}:{thread.line}
                </code>
              </button>
              <span className={thread.resolved ? "text-green" : "muted"}>
                <Icon
                  name={thread.resolved ? "check-circle" : "comment"}
                  size={13}
                />
                {thread.resolved ? "Resolved" : "Unresolved"}
              </span>
              {thread.outdated && <span className="muted">Outdated{thread.headSha ? ` · ${thread.headSha.slice(0,7)}` : ""}</span>}
            </div>
            {thread.comments.map((c) => (
              <div className="comment-entry" key={c.id}>
                <Avatar user={c.author} size={26} />
                <div>
                  <div className="comment-author">
                    <strong><ActorName user={c.author} author={pr.author}/></strong>
                    <span>{relativeTime(c.createdAt)}</span>
                  </div>
                  <Markdown>{c.body}</Markdown>
                </div>
              </div>
            ))}
            {loaded && (
              <div className="thread-actions">
                <button
                  className="btn btn-ghost"
                  disabled={!canDiscuss || locked}
                  onClick={() => {
                    setReplyId(replyId === thread.id ? null : thread.id);
                    setReply("");
                  }}
                >
                  <Icon name="comment" size={13} />
                  Reply
                </button>
                <button
                  className="btn btn-ghost"
                  disabled={locked || (pr.provider ? thread.resolved ? thread.canUnresolve !== true : thread.canResolve !== true : pr.status !== "open")}
                  onClick={() =>
                    void apply({
                      ...pr,
                      threads: pr.threads.map((t) =>
                        t.id === thread.id
                          ? { ...t, resolved: !t.resolved }
                          : t,
                      ),
                    })
                  }
                >
                  <Icon
                    name={thread.resolved ? "refresh" : "check"}
                    size={13}
                  />
                  {thread.resolved
                    ? "Reopen conversation"
                    : "Resolve conversation"}
                </button>
              </div>
            )}
            {replyId === thread.id && (
              <form
                className="reply-form"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (!reply.trim() || !canDiscuss) return;
                  void apply({
                    ...pr,
                    threads: pr.threads.map((t) =>
                      t.id === thread.id
                        ? {
                            ...t,
                            comments: [
                              ...t.comments,
                              {
                                id: newId(),
                                author: currentUser,
                                body: reply.trim(),
                                createdAt: when(),
                              },
                            ],
                          }
                        : t,
                    ),
                  }, () => { setReplyId(null); setReply(""); notify("Reply added."); });
                }}
              >
                <textarea
                  autoFocus
                  aria-label="Reply to conversation"
                  placeholder="Write a reply…"
                  value={reply}
                  onChange={(e) => setReply(e.target.value)}
                  rows={2}
                  disabled={saving}
                />
                <div>
                  <button className="btn btn-primary" disabled={!reply.trim() || !canDiscuss || locked}>
                    {saving ? "Posting…" : "Reply"}
                  </button>
                  <button
                    type="button"
                    className="btn"
                    disabled={saving}
                    onClick={() => setReplyId(null)}
                  >
                    Cancel
                  </button>
                </div>
              </form>
            )}
          </article>
        ))}
        {pr.comments.map((c) => (
          <div className="comment-entry standalone-comment" key={c.id}>
            <Avatar user={c.author} />
            <div>
              <div className="comment-author">
                <strong><ActorName user={c.author} author={pr.author}/></strong>
                <span>{relativeTime(c.createdAt)}</span>
              </div>
              <Markdown>{c.body}</Markdown>
            </div>
          </div>
        ))}
        {canDiscuss && (
          <form className="conversation-composer" onSubmit={addComment}>
            <Avatar user={currentUser} />
            <div>
              <textarea
                aria-label="Add a conversation comment"
                placeholder="Join the conversation…"
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                rows={3}
                disabled={saving}
              />
              <div className="composer-actions">
                <span className="muted">
                  Keep feedback specific and actionable.
                </span>
                <button className="btn btn-primary" disabled={!comment.trim() || locked}>
                  {saving ? "Posting…" : "Comment"}
                </button>
              </div>
            </div>
          </form>
        )}
        <div
          className={`merge-panel ${pr.status === "merged" ? "merged" : ""}`}
        >
          <Icon
            name={
              pr.status === "merged"
                ? "git-merge"
                : blockers.length
                  ? "shield"
                  : "check-circle"
            }
            size={24}
          />
          <div>
            <strong>
              {pr.status === "merged"
                ? "Pull request merged"
                : pr.status === "closed"
                  ? "Pull request closed"
                  : pr.status === "draft"
                    ? "Draft, not ready for review"
                    : blockers.length
                      ? "A few things before merging"
                      : "Ready to merge"}
            </strong>
            {pr.status === "merged" ? (
              <p>
                Merged into <code>{pr.base}</code>{pr.merge?.method ? ` with ${pr.merge.method}` : ""}.
              </p>
            ) : (
              <p>
                {blockers.length
                  ? blockers.join(" · ")
                  : pr.provider ? "GitHub reports no outstanding merge requirements." : "Required checks passed and conversations are resolved."}
              </p>
            )}
            {confirmMerge && (
              <div className="merge-confirmation">
                <label>
                  Merge method
                  <select
                    aria-label="Merge method"
                    value={method}
                    disabled={locked}
                    onChange={(e) => setMethod(e.target.value as MergeMethod)}
                  >
                    {allowedMethods.includes("squash") && <option value="squash">Squash and merge</option>}
                    {allowedMethods.includes("merge") && <option value="merge">Create a merge commit</option>}
                    {allowedMethods.includes("rebase") && <option value="rebase">Rebase and merge</option>}
                  </select>
                </label>
                <div>
                  <button
                    className="btn btn-primary"
                    disabled={locked || blockers.length > 0 || !allowedMethods.includes(method)}
                    onClick={async () => {
                      if (locked || blockers.length || !allowedMethods.includes(method)) return;
                      setSaving(true); setError("");
                      try {
                        await onMerge(method);
                        if (activeContext.current === contextKey) setConfirmMerge(false);
                      } catch (issue) { if (activeContext.current === contextKey) setError(issue instanceof Error ? issue.message : "This pull request could not be merged."); }
                      finally { setSaving(false); }
                    }}
                  >
                    {saving ? "Merging…" : "Confirm merge"}
                  </button>
                  <button
                    className="btn"
                    disabled={saving}
                    onClick={() => setConfirmMerge(false)}
                  >
                    Cancel
                  </button>
                </div>
              </div>
            )}
          </div>
          {pr.status === "open" && !confirmMerge && (
            <button
              className="btn btn-primary"
              disabled={locked || blockers.length > 0 || !allowedMethods.length}
              onClick={() => setConfirmMerge(true)}
            >
              <Icon name="git-merge" size={15} />
              Merge pull request
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function Checks({ pr }: { pr: PullRequest }) {
  const [expanded, setExpanded] = useState<string | null>(null);
  return (
    <div className="checks-pane">
      <div className="section-intro">
        <h2>
          {pr.checks.every((c) => c.status === "passed")
            ? "All checks passed"
            : pr.checks.some((c) => c.status === "failed")
              ? "Some checks need attention"
              : "Checks are in progress"}
        </h2>
        <span>
          Results for commit <code>{(pr.checkHeadSha || pr.headSha).slice(0, 7)}</code>
        </span>
      </div>
      {pr.checks.map((check) => (
        <div className="check-entry" key={check.id}>
          <button
            className="check-row"
            aria-expanded={expanded === check.id}
            onClick={() => setExpanded(expanded === check.id ? null : check.id)}
          >
            <Icon
              name={
                check.status === "passed"
                  ? "check-circle"
                  : check.status === "failed"
                    ? "alert-circle"
                    : "clock"
              }
              className={
                check.status === "passed"
                  ? "text-green"
                  : check.status === "failed"
                    ? "text-red"
                    : "text-amber"
              }
              size={20}
            />
            <div>
              <strong>{check.name}</strong>
              <span>
                {check.status === "passed"
                  ? "Completed successfully"
                  : check.status === "failed"
                    ? "Failed, changes needed"
                    : "Running"}
                {check.required ? " · Required" : ""}
              </span>
            </div>
            <span>{check.duration}</span>
            <Icon
              name={expanded === check.id ? "chevron-down" : "chevron-right"}
            />
          </button>
          {expanded === check.id && (
            <pre className="check-output">{check.output.join("\n")}</pre>
          )}
        </div>
      ))}
      <div className="checks-note">
        <Icon name="shield" size={16} />
        Sample checks illustrate merge requirements. Their results do not run
        against a live repository.
      </div>
    </div>
  );
}

function CreatePullRequest({
  prs,
  onCancel,
  onCreate,
}: {
  prs: PullRequest[];
  onCancel: () => void;
  onCreate: (pr: PullRequest) => void;
}) {
  const [title, setTitle] = useState(""),
    [body, setBody] = useState(""),
    [head, setHead] = useState("feature/pr-inspector"),
    [base, setBase] = useState("main"),
    [draft, setDraft] = useState(false),
    [reviewer, setReviewer] = useState<UserId>("elena");
  const duplicate = prs.some(
    (p) =>
      ["open", "draft"].includes(p.status) &&
      p.head === head &&
      p.base === base,
  );
  const source = seedPullRequests().find((p) => p.number === 142)!;
  const create = (e: FormEvent) => {
    e.preventDefault();
    if (!title.trim() || head === base || duplicate) return;
    const number = Math.max(...prs.map((p) => p.number)) + 1;
    const headSha = newId().replace(/-/g, "");
    onCreate({
      ...source,
      number,
      title: title.trim(),
      body:
        body.trim() ||
        "Ready for a careful review of the pull request inspector.",
      author: currentUser,
      status: draft ? "draft" : "open",
      repository: "mag-ik-een-review",
      head,
      base,
      headSha,
      requiredApprovals: 1,
      reviewers: [reviewer],
      assignees: [currentUser],
      labels: ["enhancement"],
      comments: [],
      threads: [],
      reviews: [],
      pendingComments: [],
      viewedFiles: [],
      merge: undefined,
      files: source.files.slice(0, 2),
      commits: [
        {
          sha: headSha,
          subject: title.trim(),
          author: currentUser,
          createdAt: when(),
        },
      ],
      checks: source.checks.map((c) => ({
        ...c,
        status: "running",
        duration: "Queued",
        output: ["Waiting for the sample build workflow."],
      })),
      createdAt: when(),
      updatedAt: when(),
    });
  };
  return (
    <section className="create-pr-pane">
      <div className="create-pr-heading">
        <span className="eyebrow">mag-ik-een-review</span>
        <h1>Create a pull request</h1>
        <p>Give reviewers the context they need to understand your changes.</p>
      </div>
      <form onSubmit={create}>
        <div className="create-branches">
          <label>
            Base branch
            <select value={base} onChange={(e) => setBase(e.target.value)}>
              <option>main</option>
              <option>develop</option>
              <option>feature/pr-inspector</option>
            </select>
          </label>
          <Icon name="arrow-left" />
          <label>
            Compare branch
            <select value={head} onChange={(e) => setHead(e.target.value)}>
              <option>feature/pr-inspector</option>
              <option>feature/keyboard-navigation</option>
              <option>main</option>
            </select>
          </label>
        </div>
        {head === base && (
          <div className="form-error" role="alert">
            Choose different base and compare branches.
          </div>
        )}
        {duplicate && (
          <div className="form-error" role="alert">
            An open pull request already exists for these branches.
          </div>
        )}
        <label>
          Title
          <input
            autoFocus
            required
            placeholder="Describe what changed"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
        </label>
        <label>
          Description
          <textarea
            rows={7}
            placeholder="What changed? Why? What should reviewers look for?"
            value={body}
            onChange={(e) => setBody(e.target.value)}
          />
        </label>
        <div className="create-form-meta">
          <label>
            Request review from
            <select
              value={reviewer}
              onChange={(e) => setReviewer(e.target.value as UserId)}
            >
              {(["elena", "marcus", "ravi"] as const).map((user) => (
                <option key={user} value={user}>
                  {people[user].name}
                </option>
              ))}
            </select>
          </label>
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={draft}
              onChange={(e) => setDraft(e.target.checked)}
            />
            Create as draft
          </label>
        </div>
        <div className="branch-preview">
          <Icon name="code" />
          <div>
            <strong>Branch preview</strong>
            <span>2 files changed · 1 commit · sample branch contents</span>
          </div>
          <span className="text-green">
            +{source.files.slice(0, 2).reduce((n, f) => n + f.additions, 0)}
          </span>
        </div>
        <div className="create-form-actions">
          <button type="button" className="btn" onClick={onCancel}>
            Cancel
          </button>
          <button
            className="btn btn-primary"
            disabled={!title.trim() || head === base || duplicate}
          >
            <Icon name="pull-request" />
            {draft ? "Create draft" : "Create pull request"}
          </button>
        </div>
        <p className="prototype-note">
          This creates a pull request in your local sample workspace.
        </p>
      </form>
    </section>
  );
}

function HistoryView({
  history,
  branch,
  repositoryName,
  onReview,
  root,
  loadCommitDiff,
}: {
  history: Commit[];
  branch: string;
  repositoryName: string;
  onReview: () => void;
  root?: string;
  loadCommitDiff?: LoadCommitDiff;
}) {
  const [selected, setSelected] = useState(history[0]?.sha);
  const commit = history.find((c) => c.sha === selected) || history[0];
  return (
    <section className="history-workspace">
      <div className="workspace-toolbar">
        <h1>Repository history</h1>
        <code>
          <Icon name="branch" size={14} />
          {branch}
        </code>
        <button className="btn" onClick={onReview}>
          <Icon name="pull-request" />
          Review pull requests
        </button>
      </div>
      <HistoryGraph commits={history} selectedSha={commit?.sha} onSelect={setSelected}/>
      {commit && (
        <div className="commit-inspector">
          <div className="section-intro">
            <h2>{commit.subject}</h2>
            <span>
              <Avatar user={commit.author} size={22} />
              {people[commit.author].name} · {relativeTime(commit.createdAt)}
            </span>
          </div>
          <dl>
            <dt>Commit</dt>
            <dd>
              <code>{commit.sha}</code>
            </dd>
            <dt>Repository</dt>
            <dd>{repositoryName}</dd>
            <dt>Branch</dt>
            <dd>
              <code>{branch}</code>
            </dd>
          </dl>
          {loadCommitDiff&&<CommitChanges requestKey={JSON.stringify([root,commit.sha])} sha={commit.sha} loadDiff={loadCommitDiff}/>}
        </div>
      )}
    </section>
  );
}
function Settings({
  theme,
  setTheme,
  onReset,
  accountSettings,
  notificationSettings,
  codeReviewSettings,
}: {
  codeReviewSettings?:React.ReactNode;
  notificationSettings?:React.ReactNode;
  accountSettings?: React.ReactNode;
  theme: ThemePreference;
  setTheme: (theme: ThemePreference) => void;
  onReset: () => void;
}) {
  const [confirm, setConfirm] = useState(false);
  return (
    <section className="settings-pane">
      <h1>Settings</h1>
      {accountSettings&&<div className="settings-section">{accountSettings}</div>}
      <div className="settings-section">{codeReviewSettings}</div>
      <div className="settings-section">{notificationSettings}</div>
      <div className="settings-section"><h2>Apps</h2><p>Extend your review workspace.</p><JiraIntegration/></div>
      <div className="settings-section">
        <h2>Appearance</h2>
        <p>Make the workspace comfortable for how you review.</p>
        <div className="theme-options">
          {(["system", "light", "dark"] as const).map((t) => (
            <button
              key={t}
              className={theme === t ? "selected" : ""}
              aria-pressed={theme === t}
              onClick={() => setTheme(t)}
            >
              <Icon
                name={
                  t === "system" ? "monitor" : t === "light" ? "sun" : "moon"
                }
                size={25}
              />
              <strong>{t[0].toUpperCase() + t.slice(1)}</strong>
              {theme === t && <Icon name="check-circle" size={15} />}
            </button>
          ))}
        </div>
      </div>
      <div className="settings-section">
        <h2>Keyboard shortcuts</h2>
        {[
          ["Command palette", "⌘ / Ctrl K"],
          ["History", "⌘ / Ctrl 1"],
          ["Local changes", "⌘ / Ctrl 2"],
          ["Pull requests", "⌘ / Ctrl 3"],
          ["Expand code review", "F"],
          ["Return to inbox", "Esc"],
          ["Previous / next file", "[ / ]"],
          ["Window full screen", "Shift F"],
        ].map(([label, key]) => (
          <div className="shortcut-row" key={label}>
            <span>{label}</span>
            <kbd>{key}</kbd>
          </div>
        ))}
      </div>
      <div className="settings-section">
        <h2>Sample workspace</h2>
        <p>
          Review comments, PR decisions, and commits are stored on this device.
          Restore the original sample to try the workflow again.
        </p>
        {confirm ? (
          <div className="reset-confirm">
            <span>Reset all sample changes and reviews?</span>
            <button className="btn" onClick={() => setConfirm(false)}>
              Cancel
            </button>
            <button className="btn btn-primary" onClick={onReset}>
              Reset sample workspace
            </button>
          </div>
        ) : (
          <button className="btn" onClick={() => setConfirm(true)}>
            <Icon name="refresh" />
            Reset prototype
          </button>
        )}
      </div>
    </section>
  );
}
