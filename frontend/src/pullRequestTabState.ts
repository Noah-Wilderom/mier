import type { ReviewTab } from "./data";
import type { ReviewLocation } from "./ReviewWorkspace";

export interface PullRequestTab {
  number: number;
  section: ReviewTab;
  location?: ReviewLocation;
}
export interface PullRequestTabsState {
  scope: string;
  items: PullRequestTab[];
  active: number;
  initialized: boolean;
}
export type PullRequestTabsAction = { scope: string } & (
  | { type: "open"; number: number; section?: ReviewTab }
  | { type: "close"; number: number }
  | { type: "section"; number: number; section: ReviewTab }
  | { type: "location"; number: number; location?: ReviewLocation }
  | { type: "reset" }
  | { type: "scope" }
);
export const emptyPullRequestTabs = (scope: string): PullRequestTabsState => ({
  scope,
  items: [],
  active: 0,
  initialized: false,
});
export function pullRequestTabsReducer(
  previous: PullRequestTabsState,
  action: PullRequestTabsAction,
): PullRequestTabsState {
  if (action.type === "scope")
    return previous.scope === action.scope
      ? previous
      : emptyPullRequestTabs(action.scope);
  if (previous.scope !== action.scope) return previous;
  const state = previous;
  switch (action.type) {
    case "reset":
      return emptyPullRequestTabs(action.scope);
    case "open": {
      if (!Number.isSafeInteger(action.number) || action.number <= 0)
        return state;
      const found = state.items.some((item) => item.number === action.number);
      const items = found
        ? state.items.map((item) =>
            item.number === action.number && action.section
              ? { ...item, section: action.section }
              : item,
          )
        : [
            ...state.items,
            { number: action.number, section: action.section ?? "changes" },
          ];
      return { ...state, items, active: action.number, initialized: true };
    }
    case "close": {
      const index = state.items.findIndex(
        (item) => item.number === action.number,
      );
      if (index < 0) return state;
      const items = state.items.filter((item) => item.number !== action.number);
      const active =
        state.active === action.number
          ? (items[Math.max(0, index - 1)]?.number ?? 0)
          : state.active;
      return { ...state, items, active, initialized: true };
    }
    case "section":
      return {
        ...state,
        items: state.items.map((item) =>
          item.number === action.number
            ? { ...item, section: action.section }
            : item,
        ),
      };
    case "location":
      return {
        ...state,
        items: state.items.map((item) =>
          item.number === action.number
            ? { ...item, location: action.location }
            : item,
        ),
      };
  }
}
