import { useCallback, useEffect, useMemo, useReducer } from "react";
import type { ReviewTab } from "./data";
import type { ReviewLocation } from "./ReviewWorkspace";
import {
  emptyPullRequestTabs,
  pullRequestTabsReducer,
} from "./pullRequestTabState";
import { createReviewSession, type ReviewSession } from "./ReviewSession";

export function usePullRequestTabs(scope: string) {
  const [saved, dispatch] = useReducer(
    pullRequestTabsReducer,
    scope,
    emptyPullRequestTabs,
  );
  const state = saved.scope === scope ? saved : emptyPullRequestTabs(scope);
  useEffect(() => {
    dispatch({ scope, type: "scope" });
  }, [scope]);
  const sessions = useMemo(() => new Map<number, ReviewSession>(), [scope]);
  const session = useMemo(() => {
    if (!sessions.has(state.active))
      sessions.set(state.active, createReviewSession());
    return sessions.get(state.active)!;
  }, [sessions, state.active]);
  const open = useCallback(
    (number: number, section?: ReviewTab) =>
      dispatch({ scope, type: "open", number, section }),
    [scope],
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
    sessions.clear();
    dispatch({ scope, type: "reset" });
  }, [scope, sessions]);
  return {
    ...state,
    selected: state.items.find((item) => item.number === state.active),
    session,
    open,
    close,
    setSection,
    setLocation,
    reset,
  };
}
