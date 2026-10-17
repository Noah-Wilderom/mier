import { useEffect, useState } from "react";
import type { PullRequest } from "./data";
import { services } from "./services";
import type { CheckAnnotation } from "./actionsTypes";
export function useCIAnnotations(pr: PullRequest) {
  const [state, setState] = useState<{
      key: string;
      items: CheckAnnotation[];
      error: string;
    }>({ key: "", items: [], error: "" }),
    [retry, setRetry] = useState(0);
  const checks = pr.checks
    .filter((check) => check.checkRunId && check.status === "failed")
    .slice(0, 8);
  const key = JSON.stringify([
    pr.provider?.host,
    pr.provider?.owner,
    pr.provider?.repo,
    pr.headSha,
    pr.checkHeadSha,
    checks.map((check) => check.checkRunId),
  ]);
  useEffect(() => {
    if (
      !pr.provider ||
      (pr.provider.kind && pr.provider.kind !== "github") ||
      pr.provider.capabilities?.actions === false ||
      (pr.checkHeadSha && pr.checkHeadSha !== pr.headSha) ||
      !checks.length
    )
      return;
    const controller = new AbortController();
    let index = 0;
    const items: CheckAnnotation[] = [];
    const errors: string[] = [];
    void Promise.all(
      Array.from({ length: Math.min(2, checks.length) }, async () => {
        for (;;) {
          const check = checks[index++];
          if (!check || controller.signal.aborted) return;
          try {
            const page = await services.actions.annotations(
              pr.provider!,
              check.checkRunId!,
              pr.headSha,
              1,
              controller.signal,
            );
            items.push(...page.items);
            if (page.hasMore)
              errors.push("More annotations are available in Checks.");
          } catch (issue) {
            errors.push(String(issue));
          }
        }
      }),
    ).then(() => {
      if (!controller.signal.aborted)
        setState({ key, items, error: errors.join(" ") });
    });
    return () => controller.abort();
  }, [key, retry]);
  return {
    items: state.key === key ? state.items : [],
    error: state.key === key ? state.error : "",
    retry: () => setRetry((value) => value + 1),
  };
}
