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

// Only the active PR mounts its editor and integrations. Lightweight, in-memory
// view state survives tab changes without hidden editors or duplicate DOM IDs.
export function createReviewSession() {
  const values = new Map<string, unknown>();
  const listeners = new Set<() => void>();
  return {
    get<T>(key: string, initial: () => T): T {
      if (!values.has(key)) values.set(key, initial());
      return values.get(key) as T;
    },
    set<T>(key: string, value: T) {
      if (Object.is(values.get(key), value)) return;
      values.set(key, value);
      for (const listener of [...listeners]) listener();
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
