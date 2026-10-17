import { hasNativeBackend, services } from "./services";

const pending = new Map<string, { data: string }>();
export async function loadWorkspaceState<T>(
  scope: string,
): Promise<T | undefined> {
  const data =
    pending.get(scope)?.data ??
    (hasNativeBackend()
      ? await services.workspace.loadState(scope)
      : localStorage.getItem(`mier.workspace.${scope}`));
  return data ? (JSON.parse(data) as T) : undefined;
}
// Keep writes ordered: a slow earlier save cannot overwrite the latest draft.
export function saveWorkspaceState(scope: string, value: unknown) {
  const data = value === undefined ? "" : JSON.stringify(value);
  const existing = pending.get(scope);
  if (existing) {
    existing.data = data;
    return;
  }
  const entry = { data };
  pending.set(scope, entry);
  void (async () => {
    try {
      for (;;) {
        const next = entry.data;
        if (hasNativeBackend()) await services.workspace.saveState(scope, next);
        else if (next) localStorage.setItem(`mier.workspace.${scope}`, next);
        else localStorage.removeItem(`mier.workspace.${scope}`);
        if (entry.data === next) break;
      }
    } catch (error) {
      window.dispatchEvent(
        new CustomEvent("mier:state-error", {
          detail: `Could not save workspace: ${error instanceof Error ? error.message : String(error)}`,
        }),
      );
    } finally {
      pending.delete(scope);
    }
  })();
}
