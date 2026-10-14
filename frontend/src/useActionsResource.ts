import { useCallback, useEffect, useRef, useState } from "react";

// One visible request at a time. Cancellation reaches Go; key tags also discard
// late results when the repository, account, filters or selected attempt change.
export function useActionsResource<T>(
  key: string,
  load: (signal: AbortSignal) => Promise<T>,
  interval: (data: T | undefined) => number = () => 0,
) {
  const loader = useRef(load),
    delay = useRef(interval);
  loader.current = load;
  delay.current = interval;
  const [version, setVersion] = useState(0);
  const [state, setState] = useState<{
    key: string;
    data?: T;
    error: string;
    loading: boolean;
  }>({ key, error: "", loading: true });
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined,
      busy = false,
      data: T | undefined;
    const fetch = async () => {
      if (busy || controller.signal.aborted) return;
      if (timer) clearTimeout(timer);
      busy = true;
      setState((previous) => ({
        key,
        data: previous.key === key ? previous.data : undefined,
        error: previous.key === key ? previous.error : "",
        loading: true,
      }));
      try {
        const result = await loader.current(controller.signal);
        if (!controller.signal.aborted) {
          data = result;
          setState({ key, data: result, error: "", loading: false });
        }
      } catch (error) {
        if (!controller.signal.aborted)
          setState((previous) => ({
            key,
            data: previous.key === key ? previous.data : undefined,
            loading: false,
            error: error instanceof Error ? error.message : String(error),
          }));
      } finally {
        busy = false;
        if (!controller.signal.aborted) {
          const ms = delay.current(data);
          if (ms > 0)
            timer = setTimeout(() => {
              if (document.visibilityState !== "hidden") void fetch();
            }, ms);
        }
      }
    };
    const visible = () => {
      if (document.visibilityState === "visible" && delay.current(data) > 0)
        void fetch();
    };
    document.addEventListener("visibilitychange", visible);
    void fetch();
    return () => {
      controller.abort();
      if (timer) clearTimeout(timer);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [key, version]);
  const refresh = useCallback(() => setVersion((value) => value + 1), []);
  return {
    ...(state.key === key
      ? state
      : { key, data: undefined, error: "", loading: true }),
    refresh,
  };
}
