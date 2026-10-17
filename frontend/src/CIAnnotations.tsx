import { lazy, Suspense, useState, useSyncExternalStore } from "react";
import { services, type RepositoryIdentity } from "./services";
import { useActionsResource } from "./useActionsResource";
import type { CheckAnnotation } from "./actionsTypes";
import { ActionsError } from "./Actions";
import Modal from "./Modal";
import {
  getCodeAppearance,
  subscribeCodeAppearance,
} from "./codeReviewPreferences";
const CodeEditor = lazy(() => import("./CodeEditor"));
export default function CIAnnotations({
  repository,
  id,
  head,
  onOpen,
}: {
  repository: RepositoryIdentity;
  id: number;
  head: string;
  onOpen?: (annotation: CheckAnnotation) => boolean | void;
}) {
  const [source, setSource] = useState<{
      path: string;
      revision: string;
      text: string;
      line: number;
    }>(),
    [sourceBusy, setSourceBusy] = useState(false),
    [sourceError, setSourceError] = useState("");
  const appearance = useSyncExternalStore(
    subscribeCodeAppearance,
    getCodeAppearance,
    () => "light" as const,
  );
  const [page, setPage] = useState(1);
  const data = useActionsResource(
    `annotations:${id}:${head}:${page}`,
    (signal) =>
      services.actions.annotations(repository, id, head, page, signal),
  );
  return (
    <section className="ci-annotations" aria-label="Check annotations">
      <h3>Code annotations</h3>
      {sourceError && (
        <p role="alert" className="text-red">
          {sourceError}
        </p>
      )}
      {source && (
        <Modal
          title={`CI source · ${source.path}:${source.line}`}
          onClose={() => setSource(undefined)}
        >
          <p>Read only · commit {source.revision.slice(0, 8)}</p>
          <Suspense fallback={<p role="status">Loading source viewer…</p>}>
            <CodeEditor
              value={source.text}
              path={source.path}
              label="CI annotation source"
              disabled
              dark={appearance === "dark"}
              revealLine={source.line}
              revealKey={String(source.line)}
              onChange={() => {}}
            />
          </Suspense>
        </Modal>
      )}
      <ActionsError
        error={data.error}
        retry={data.refresh}
        loading={data.loading}
      />
      {data.loading && !data.data && <p role="status">Loading annotations…</p>}
      {(data.data?.items ?? []).map((item, index) => (
        <article
          key={index}
          className={`ci-annotation is-${item.annotationLevel}`}
        >
          <button
            className="btn-ghost"
            disabled={sourceBusy}
            onClick={() => {
              if (onOpen && onOpen(item) !== false) return;
              setSourceBusy(true);
              setSourceError("");
              void services.actions
                .annotationSource(
                  repository,
                  id,
                  head,
                  item.path,
                  item.startLine,
                  page,
                )
                .then(setSource)
                .catch((issue) => setSourceError(String(issue)))
                .finally(() => setSourceBusy(false));
            }}
          >
            {item.path}:{item.startLine}
            {item.endLine !== item.startLine ? `–${item.endLine}` : ""}
          </button>
          <strong>
            {item.annotationLevel} {item.title}
          </strong>
          <p>{item.message}</p>
          {item.rawDetails && (
            <details>
              <summary>Details</summary>
              <pre>{item.rawDetails}</pre>
            </details>
          )}
        </article>
      ))}
      {data.data && !data.data.items.length && (
        <p>No source annotations on this page.</p>
      )}
      <div className="ci-pagination">
        {page > 1 && (
          <button
            className="btn"
            disabled={data.loading}
            onClick={() => setPage((value) => value - 1)}
          >
            Previous annotations
          </button>
        )}
        {data.data?.hasMore && (
          <button
            className="btn"
            disabled={data.loading}
            onClick={() => setPage((value) => value + 1)}
          >
            Next annotations
          </button>
        )}
      </div>
    </section>
  );
}
