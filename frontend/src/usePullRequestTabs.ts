import {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";
import type { ReviewTab } from "./data";
import type { ReviewLocation } from "./ReviewWorkspace";
import {
  emptyPullRequestTabs,
  pullRequestTabsReducer,
  type PullRequestTabsState,
} from "./pullRequestTabState";
import { createReviewSession, type ReviewSession } from "./ReviewSession";
import { loadWorkspaceState, saveWorkspaceState } from "./workspaceState";

export function usePullRequestTabs(scope: string) {
  const [saved, dispatch] = useReducer(
    pullRequestTabsReducer,
    scope,
    emptyPullRequestTabs,
  );
  const state = saved.scope === scope ? saved : emptyPullRequestTabs(scope);
  const [readyScope, setReadyScope] = useState(""),
    ready = readyScope === scope;
  const currentScope = useRef(scope);
  currentScope.current = scope;
  const stateRef = useRef(state);
  stateRef.current = state;
  const intent = useRef(0);
  const cache = useMemo(
    () => ({
      sessions: new Map<number, ReviewSession>(),
      loading: new Map<number, Promise<ReviewSession>>(),
    }),
    [scope],
  );
  const loadSession = useCallback(
    (number: number): Promise<ReviewSession> => {
      const existing = cache.sessions.get(number);
      if (existing) return Promise.resolve(existing);
      const pending = cache.loading.get(number);
      if (pending) return pending;
      const key = `review-session:${scope}:${number}`;
      const loading = loadWorkspaceState<Record<string, unknown>>(key)
        .then((initial) => {
          const session = createReviewSession(initial, () =>
            saveWorkspaceState(key, session.snapshot()),
          );
          cache.sessions.set(number, session);
          // Closed tabs stay on disk. Bound memory independently of unfinished drafts.
          for (const id of cache.sessions.keys()) {
            if (cache.sessions.size <= 100) break;
            if (
              id !== number &&
              !stateRef.current.items.some((item) => item.number === id)
            )
              cache.sessions.delete(id);
          }
          return session;
        })
        .finally(() => cache.loading.delete(number));
      cache.loading.set(number, loading);
      return loading;
    },
    [scope, cache],
  );
  useEffect(() => {
    let canceled = false;
    const opening = intent.current;
    dispatch({ scope, type: "scope" });
    void loadWorkspaceState<{ version: number; state: PullRequestTabsState }>(
      `tabs:${scope}`,
    )
      .then(async (value) => {
        if (value?.version !== 2 || !Array.isArray(value.state?.items)) return;
        const unique = new Set<number>();
        const items = value.state.items
          .filter((item) => {
            if (
              !Number.isSafeInteger(item?.number) ||
              item.number <= 0 ||
              unique.has(item.number) ||
              !["changes", "conversation", "commits", "checks"].includes(
                item.section,
              )
            )
              return false;
            unique.add(item.number);
            return true;
          })
          .slice(-50)
          .map((item) => ({
            ...item,
            location:
              item.location && typeof item.location.path === "string"
                ? item.location
                : undefined,
          }));
        await Promise.all(items.map((item) => loadSession(item.number)));
        if (canceled || intent.current !== opening) return;
        dispatch({
          scope,
          type: "restore",
          state: {
            ...emptyPullRequestTabs(scope),
            items,
            active: items.some((item) => item.number === value.state.active)
              ? value.state.active
              : (items[0]?.number ?? 0),
            initialized: !!value.state.initialized,
          },
        });
      })
      .catch((error) =>
        window.dispatchEvent(
          new CustomEvent("mier:state-error", {
            detail: `Could not restore workspace: ${String(error)}`,
          }),
        ),
      )
      .finally(() => {
        if (!canceled) setReadyScope(scope);
      });
    return () => {
      canceled = true;
    };
  }, [scope, loadSession]);
  useEffect(() => {
    if (ready && state.scope === scope)
      saveWorkspaceState(`tabs:${scope}`, { version: 2, state });
  }, [state, ready, scope]);
  const fallback = useMemo(createReviewSession, [scope]);
  const session = cache.sessions.get(state.active) ?? fallback;
  const open = useCallback(
    async (number: number, section?: ReviewTab) => {
      if (!Number.isSafeInteger(number) || number < 1) return false;
      if (
        stateRef.current.items.length >= 50 &&
        !stateRef.current.items.some((item) => item.number === number)
      ) {
        window.dispatchEvent(
          new CustomEvent("mier:state-error", {
            detail:
              "Close a PR tab before opening another. Up to 50 tabs can stay open.",
          }),
        );
        return false;
      }
      const opening = ++intent.current;
      try {
        await loadSession(number);
        if (currentScope.current !== scope || opening !== intent.current)
          return false;
        dispatch({ scope, type: "open", number, section });
        return true;
      } catch (error) {
        window.dispatchEvent(
          new CustomEvent("mier:state-error", {
            detail: `Could not restore PR draft: ${String(error)}`,
          }),
        );
        return false;
      }
    },
    [scope, loadSession],
  );
  const close = useCallback(
    (number: number) => dispatch({ scope, type: "close", number }),
    [scope],
  );
  const setSection = useCallback(
    (section: ReviewTab) =>
      dispatch({ scope, type: "section", number: state.active, section }),
    [scope, state.active],
  );
  const setLocation = useCallback(
    (location?: ReviewLocation) =>
      dispatch({ scope, type: "location", number: state.active, location }),
    [scope, state.active],
  );
  const reset = useCallback(() => {
    for (const number of cache.sessions.keys())
      saveWorkspaceState(`review-session:${scope}:${number}`, undefined);
    cache.sessions.clear();
    dispatch({ scope, type: "reset" });
  }, [scope, cache]);
  return {
    ...state,
    ready,
    selected: state.items.find((item) => item.number === state.active),
    session,
    open,
    close,
    setSection,
    setLocation,
    reset,
  };
}
