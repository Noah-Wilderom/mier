import { useEffect, useRef, useState } from "react";
import type { RepositoryCard } from "./RepositorySelector";
import {
  services,
  type PullRequestSummary,
  type PullRequestDetail,
} from "./services";
import { loadWorkspaceState, saveWorkspaceState } from "./workspaceState";
import "./UnifiedInbox.css";

type Attention = "requested" | "blocked" | "failed" | "unread" | "all";
type InboxRepo = RepositoryCard & { host: string; owner: string; repo: string };
interface Page {
  items: PullRequestSummary[];
  details: Record<number, PullRequestDetail>;
  cursor: string;
  more: boolean;
  error: string;
  loading: boolean;
  login: string;
}
interface Saved {
  filters: {
    name: string;
    attention: Attention;
    query: string;
    group: string;
  }[];
  groups: { name: string; paths: string[] }[];
  seen: Record<string, string>;
}
const emptySaved = (): Saved => ({ filters: [], groups: [], seen: {} });
const itemKey = (repo: InboxRepo, pr: PullRequestSummary, login: string) =>
  JSON.stringify([repo.host, repo.owner, repo.repo, login, pr.number]);
const failed = (detail?: PullRequestDetail) =>
  !!detail?.checks?.some((check) =>
    [
      "failure",
      "timed_out",
      "cancelled",
      "action_required",
      "startup_failure",
    ].includes(check.conclusion ?? ""),
  ) ||
  !!detail?.statuses?.some((status) =>
    ["failure", "error"].includes(status.state),
  );
export default function UnifiedInbox({
  repositories,
  onOpen,
}: {
  repositories: RepositoryCard[];
  onOpen: (root: string, number: number) => Promise<void>;
}) {
  const [attention, setAttention] = useState<Attention>("requested"),
    [query, setQuery] = useState(""),
    [search, setSearch] = useState(""),
    [group, setGroup] = useState("");
  const [saved, setSaved] = useState<Saved>(emptySaved),
    [ready, setReady] = useState(false),
    [name, setName] = useState(""),
    [paths, setPaths] = useState<string[]>([]),
    [pages, setPages] = useState<Record<string, Page>>({}),
    [refresh, setRefresh] = useState(0),
    [opening, setOpening] = useState(""),
    [error, setError] = useState("");
  const controller = useRef<AbortController>();
  const repos = repositories
    .filter(
      (repo): repo is InboxRepo => !!repo.host && !!repo.owner && !!repo.repo,
    )
    .filter(
      (repo) =>
        !group ||
        saved.groups
          .find((item) => item.name === group)
          ?.paths.includes(repo.path),
    );
  const repoKey = JSON.stringify(
    repos.map((repo) => [repo.path, repo.host, repo.owner, repo.repo]),
  );
  useEffect(() => {
    const timer = setTimeout(() => setSearch(query), 350);
    return () => clearTimeout(timer);
  }, [query]);
  useEffect(() => {
    let canceled = false;
    void loadWorkspaceState<Saved>("unified-inbox")
      .then((value) => {
        if (
          !canceled &&
          value &&
          Array.isArray(value.filters) &&
          Array.isArray(value.groups)
        )
          setSaved({ ...emptySaved(), ...value });
      })
      .catch((issue) => setError(String(issue)))
      .finally(() => {
        if (!canceled) setReady(true);
      });
    return () => {
      canceled = true;
    };
  }, []);
  useEffect(() => {
    if (ready) saveWorkspaceState("unified-inbox", saved);
  }, [saved, ready]);
  const load = async (
    repo: InboxRepo,
    append: boolean,
    signal: AbortSignal,
    previous?: Page,
  ) => {
    setPages((value) => ({
      ...value,
      [repo.path]: {
        ...(value[repo.path] ?? {
          items: [],
          details: {},
          cursor: "",
          more: false,
          login: "",
        }),
        loading: true,
        error: "",
      },
    }));
    try {
      const account = await services.credentials.accountStatus(repo.host);
      const page = await services.github.listPullRequestsPage(
        repo.host,
        repo.owner,
        repo.repo,
        {
          state: "open",
          view:
            attention === "requested"
              ? "review-requested"
              : attention === "blocked"
                ? "mine"
                : "inbox",
          query: search,
          cursor: append ? (previous?.cursor ?? "") : "",
          pageSize: 10,
        },
        signal,
      );
      const details: Record<number, PullRequestDetail> = {};
      if (["blocked", "failed", "unread"].includes(attention)) {
        // Bound provider requests; details are loaded only for filters which need them.
        let index = 0;
        await Promise.all(
          Array.from({ length: Math.min(3, page.items.length) }, async () => {
            for (;;) {
              const item = page.items[index++];
              if (!item || signal.aborted) return;
              details[item.number] = await services.github.getPullRequest(
                repo.host,
                repo.owner,
                repo.repo,
                item.number,
                signal,
              );
            }
          }),
        );
      }
      if (signal.aborted) return;
      setPages((value) => ({
        ...value,
        [repo.path]: {
          items: append
            ? [
                ...(previous?.items ?? []),
                ...page.items.filter(
                  (item) =>
                    !previous?.items.some((old) => old.number === item.number),
                ),
              ]
            : page.items,
          details: { ...(append ? previous?.details : {}), ...details },
          cursor: page.nextCursor,
          more: page.hasNextPage,
          error: "",
          loading: false,
          login: account.login,
        },
      }));
    } catch (issue) {
      if (!signal.aborted)
        setPages((value) => ({
          ...value,
          [repo.path]: {
            ...value[repo.path],
            loading: false,
            error: issue instanceof Error ? issue.message : String(issue),
          },
        }));
    }
  };
  useEffect(() => {
    if (!ready) return;
    controller.current?.abort();
    const pending = new AbortController();
    controller.current = pending;
    setPages({});
    let index = 0;
    void Promise.all(
      Array.from({ length: Math.min(2, repos.length) }, async () => {
        for (;;) {
          const repo = repos[index++];
          if (!repo || pending.signal.aborted) return;
          await load(repo, false, pending.signal);
        }
      }),
    );
    return () => pending.abort();
  }, [repoKey, attention, search, refresh, ready]);
  const open = async (repo: InboxRepo, pr: PullRequestSummary) => {
    const key = itemKey(repo, pr, pages[repo.path]?.login ?? "");
    setOpening(key);
    setError("");
    try {
      await onOpen(repo.path, pr.number);
      const next = {
        ...saved,
        seen: { ...saved.seen, [key]: new Date().toISOString() },
      };
      setSaved(next);
      saveWorkspaceState("unified-inbox", next);
    } catch (issue) {
      setError(String(issue));
    } finally {
      setOpening("");
    }
  };
  const visible = (repo: InboxRepo, pr: PullRequestSummary, page: Page) => {
    const detail = page.details[pr.number];
    if (attention === "failed") return failed(detail);
    if (attention === "blocked")
      return (
        pr.author.login === page.login &&
        (failed(detail) ||
          detail?.reviewDecision === "CHANGES_REQUESTED" ||
          detail?.mergeStateStatus === "BLOCKED" ||
          pr.mergeable === false)
      );
    if (attention === "unread") {
      const seen = saved.seen[itemKey(repo, pr, page.login)] ?? "";
      return [
        ...(detail?.comments ?? []),
        ...(detail?.threads?.flatMap((thread) => thread.comments ?? []) ?? []),
      ].some(
        (comment) =>
          comment.user.login !== page.login &&
          (comment.updatedAt || comment.createdAt) > seen,
      );
    }
    return true;
  };
  return (
    <section className="unified-inbox" aria-label="All repositories inbox">
      <header>
        <div>
          <h1>All repositories</h1>
          <p>Pull requests across your connected repositories.</p>
        </div>
        <button
          className="btn"
          onClick={() => setRefresh((value) => value + 1)}
        >
          Refresh inbox
        </button>
      </header>
      <div className="unified-inbox-filters">
        <label>
          Attention
          <select
            aria-label="Inbox attention"
            value={attention}
            onChange={(e) => setAttention(e.target.value as Attention)}
          >
            <option value="requested">Review requested</option>
            <option value="blocked">My blocked PRs</option>
            <option value="failed">Failed checks</option>
            <option value="unread">Unread conversations</option>
            <option value="all">All open PRs</option>
          </select>
        </label>
        <label>
          Project group
          <select
            aria-label="Inbox project group"
            value={group}
            onChange={(e) => setGroup(e.target.value)}
          >
            <option value="">All repositories</option>
            {saved.groups.map((item) => (
              <option key={item.name}>{item.name}</option>
            ))}
          </select>
        </label>
        <input
          aria-label="Search all repositories"
          placeholder="Filter pull requests…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>
      <details className="unified-inbox-saved">
        <summary>Saved filters and project groups</summary>
        <div className="unified-inbox-filters">
          <input
            aria-label="Filter or group name"
            placeholder="Name…"
            maxLength={60}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <button
            className="btn"
            disabled={!name.trim()}
            onClick={() => {
              setSaved((value) => ({
                ...value,
                filters: [
                  ...value.filters
                    .filter((item) => item.name !== name.trim())
                    .slice(-19),
                  { name: name.trim(), attention, query, group },
                ],
              }));
              setName("");
            }}
          >
            Save filter
          </button>
          <button
            className="btn"
            disabled={!name.trim() || !paths.length}
            onClick={() => {
              setSaved((value) => ({
                ...value,
                groups: [
                  ...value.groups
                    .filter((item) => item.name !== name.trim())
                    .slice(-19),
                  { name: name.trim(), paths },
                ],
              }));
              setGroup(name.trim());
              setName("");
            }}
          >
            Save project group
          </button>
        </div>
        <div className="unified-inbox-filters">
          {repositories.map((repo) => (
            <label key={repo.path}>
              <input
                type="checkbox"
                checked={paths.includes(repo.path)}
                onChange={(e) =>
                  setPaths((value) =>
                    e.target.checked
                      ? [...value, repo.path]
                      : value.filter((path) => path !== repo.path),
                  )
                }
              />
              {repo.name}
            </label>
          ))}
        </div>
        {saved.filters.map((item) => (
          <span className="inbox-saved-item" key={item.name}>
            <button
              className="btn btn-ghost"
              onClick={() => {
                setAttention(item.attention);
                setQuery(item.query);
                setGroup(item.group);
              }}
            >
              {item.name}
            </button>
            <button
              aria-label={`Delete filter ${item.name}`}
              onClick={() =>
                setSaved((value) => ({
                  ...value,
                  filters: value.filters.filter(
                    (filter) => filter.name !== item.name,
                  ),
                }))
              }
            >
              ×
            </button>
          </span>
        ))}
        {saved.groups.map((item) => (
          <span className="inbox-saved-item" key={item.name}>
            {item.name}
            <button
              aria-label={`Delete group ${item.name}`}
              onClick={() => {
                if (group === item.name) setGroup("");
                setSaved((value) => ({
                  ...value,
                  groups: value.groups.filter(
                    (group) => group.name !== item.name,
                  ),
                }));
              }}
            >
              ×
            </button>
          </span>
        ))}
      </details>
      {error && (
        <p role="alert" className="git-operation-error">
          {error}
        </p>
      )}
      {!repos.length && (
        <p className="comparison-empty">
          Open a repository with a connected review provider to add it to this
          inbox.
        </p>
      )}
      <div className="unified-inbox-results">
        {repos.map((repo) => {
          const page = pages[repo.path],
            items = page?.items.filter((pr) => visible(repo, pr, page)) ?? [];
          return (
            <section
              key={repo.path}
              aria-label={`${repo.owner}/${repo.repo} inbox`}
            >
              <h2>
                {repo.owner}/{repo.repo}
                <span className="muted">{repo.host}</span>
              </h2>
              {page?.error && (
                <p role="alert">
                  {page.error}
                  <button
                    className="btn"
                    onClick={() =>
                      void load(repo, false, controller.current!.signal)
                    }
                  >
                    Retry repository
                  </button>
                </p>
              )}
              {items.map((pr) => (
                <button
                  className="unified-pr-row"
                  key={pr.number}
                  disabled={!!opening}
                  onClick={() => void open(repo, pr)}
                >
                  <span className="text-green">#{pr.number}</span>
                  <strong>{pr.title}</strong>
                  <span>{pr.author.login}</span>
                  {failed(page?.details[pr.number]) && (
                    <span className="text-red">Checks failed</span>
                  )}
                  <time>{new Date(pr.updatedAt).toLocaleDateString()}</time>
                </button>
              ))}
              {page?.loading ? (
                <p role="status">Loading pull requests…</p>
              ) : !items.length && !page?.error ? (
                <p className="muted">
                  No matching pull requests on this page
                  {page?.more ? " · Load more to continue searching." : "."}
                </p>
              ) : null}
              {page?.more && (
                <button
                  className="btn"
                  disabled={page.loading}
                  onClick={() =>
                    void load(repo, true, controller.current!.signal, page)
                  }
                >
                  Load more from {repo.repo}
                </button>
              )}
            </section>
          );
        })}
      </div>
    </section>
  );
}
