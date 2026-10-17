import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useSyncExternalStore,
  type Dispatch,
  type MutableRefObject,
  type ReactNode,
  type SetStateAction,
} from "react";

// Persist user-authored view state, never execution flags, errors or capabilities.
export function createReviewSession(
  initial: Record<string, unknown> = {},
  changed: () => void = () => {},
) {
  const allowed = (key: string) =>
    /^(workspace\.(activePath|query|diffMode|ignoreWhitespace|wrapLines|filePaneWidth|commentAt|commentBody|composerHeadSha|composerPath|scroll)|footer\.(expanded|decision|body)|header\.(editing|title|body)|conversation\.(comment|replyId|reply|editingMetadata|reviewers|assignees|labels)|thread\..+\.(reply|replyOpen|expanded)|ai\.(open|instructions))$/.test(
      key,
    );
  const values = new Map<string, unknown>(
    Object.entries(
      initial && typeof initial === "object" && !Array.isArray(initial)
        ? initial
        : {},
    )
      .filter(([key]) => allowed(key))
      .map(([key, value]) => [
        key,
        key === "workspace.scroll" && Array.isArray(value)
          ? {
              current: new Map(
                (value as unknown[])
                  .filter(
                    (entry): entry is [string, { top: number; left: number }] =>
                      Array.isArray(entry) &&
                      typeof entry[0] === "string" &&
                      !!entry[1] &&
                      Number.isFinite(entry[1].top) &&
                      Number.isFinite(entry[1].left),
                  )
                  .slice(-100),
              ),
            }
          : value,
      ]),
  );
  const unchecked = new Set(values.keys());
  const listeners = new Set<() => void>();
  return {
    get<T>(key: string, initial: () => T): T {
      if (unchecked.delete(key)) {
        const fallback = initial(),
          value = values.get(key);
        const valid =
          fallback === null
            ? value === null || key === "workspace.commentAt"
              ? value === null ||
                (typeof value === "object" &&
                  Number.isSafeInteger((value as { line: number }).line) &&
                  ["old", "new"].includes((value as { side: string }).side))
              : typeof value === "string"
            : typeof fallback === typeof value &&
              (!Array.isArray(fallback) || Array.isArray(value));
        if (
          !valid ||
          (key === "workspace.scroll" &&
            !((value as { current?: unknown })?.current instanceof Map)) ||
          (key === "footer.decision" &&
            !["commented", "approved", "changes_requested"].includes(
              String(value),
            )) ||
          (key === "workspace.diffMode" &&
            !["split", "unified"].includes(String(value)))
        )
          values.set(key, fallback);
      }
      if (!values.has(key)) values.set(key, initial());
      return values.get(key) as T;
    },
    set<T>(key: string, value: T) {
      if (Object.is(values.get(key), value)) return;
      values.set(key, value);
      for (const listener of [...listeners]) listener();
      if (allowed(key)) changed();
    },
    touch: changed,
    snapshot() {
      return Object.fromEntries(
        [...values]
          .filter(([key]) => allowed(key))
          .map(([key, value]) => [
            key,
            key === "workspace.scroll"
              ? [...(value as { current: Map<string, unknown> }).current].slice(
                  -100,
                )
              : value,
          ]),
      );
    },
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
}
export type ReviewSession = ReturnType<typeof createReviewSession>;
const SessionContext = createContext<ReviewSession | undefined>(undefined);
export function useReviewSessionTouch() {
  return useContext(SessionContext)?.touch ?? (() => {});
}
export function ReviewSessionProvider({
  session,
  children,
}: {
  session: ReviewSession;
  children: ReactNode;
}) {
  return (
    <SessionContext.Provider value={session}>
      {children}
    </SessionContext.Provider>
  );
}
export function useReviewSessionState<T>(
  key: string,
  initial: T | (() => T),
): [T, Dispatch<SetStateAction<T>>] {
  const shared = useContext(SessionContext);
  const local = useMemo(createReviewSession, []);
  const store = shared ?? local;
  const initialRef = useRef(initial);
  const read = useCallback(
    () =>
      store.get(key, () =>
        typeof initialRef.current === "function"
          ? (initialRef.current as () => T)()
          : initialRef.current,
      ),
    [store, key],
  );
  const value = useSyncExternalStore(store.subscribe, read, read);
  const setValue = useCallback(
    (next: SetStateAction<T>) => {
      store.set(
        key,
        typeof next === "function"
          ? (next as (previous: T) => T)(read())
          : next,
      );
    },
    [store, key, read],
  );
  return [value, setValue];
}
export function useReviewSessionRef<T>(
  key: string,
  initial: T,
): MutableRefObject<T> {
  const store = useContext(SessionContext);
  const local = useRef(initial);
  return store ? store.get(key, () => ({ current: initial })) : local;
}
