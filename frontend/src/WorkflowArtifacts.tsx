import { useState } from "react";
import { services, type RepositoryIdentity } from "./services";
import type { WorkflowRun } from "./actionsTypes";
import { useActionsResource } from "./useActionsResource";
import { ActionsError } from "./Actions";
type Props = { repository: RepositoryIdentity; run: WorkflowRun };
export default function WorkflowArtifacts(props: Props) {
  const [opened, setOpened] = useState(false);
  return (
    <details
      className="workflow-artifacts"
      onToggle={(event) => {
        if (event.currentTarget.open) setOpened(true);
      }}
    >
      <summary>Artifacts</summary>
      {opened && <ArtifactContents {...props} />}
    </details>
  );
}
function ArtifactContents({
  repository,
  run,
}: {
  repository: RepositoryIdentity;
  run: WorkflowRun;
}) {
  const [page, setPage] = useState(1),
    [pending, setPending] = useState<number>(),
    [message, setMessage] = useState(""),
    [error, setError] = useState("");
  const data = useActionsResource(
    `artifacts:${run.id}:${page}:${run.status}`,
    (signal) => services.actions.artifacts(repository, run.id, page, signal),
  );
  return (
    <div>
      <ActionsError
        error={data.error || error}
        retry={() => {
          setError("");
          data.refresh();
        }}
        loading={data.loading}
      />
      {data.loading && !data.data && <p role="status">Loading artifacts…</p>}
      {message && <p role="status">{message}</p>}
      {data.data?.items.map((item) => (
        <div className="workflow-artifact" key={item.id}>
          <strong>{item.name}</strong>
          <span>{(item.sizeInBytes / 1024 / 1024).toFixed(1)} MiB</span>
          <span>
            {item.expired
              ? "Expired"
              : `Expires ${new Date(item.expiresAt).toLocaleDateString()}`}
          </span>
          <button
            className="btn"
            disabled={!!pending || item.expired}
            onClick={() => {
              setPending(item.id);
              setError("");
              setMessage("");
              void services.actions
                .downloadArtifact(repository, {
                  runId: run.id,
                  artifactId: item.id,
                  headSha: run.headSha,
                })
                .then((path) => {
                  if (path) setMessage(`Saved ${item.name} to ${path}`);
                })
                .catch((issue) => setError(String(issue)))
                .finally(() => setPending(undefined));
            }}
          >
            {pending === item.id ? "Downloading…" : "Download ZIP"}
          </button>
        </div>
      ))}
      {data.data && !data.data.items.length && (
        <p>
          No artifacts available. Artifacts belong to the run and may include
          multiple attempts.
        </p>
      )}
      <div className="ci-pagination">
        {page > 1 && (
          <button className="btn" onClick={() => setPage((value) => value - 1)}>
            Previous artifacts
          </button>
        )}
        {data.data?.hasMore && (
          <button className="btn" onClick={() => setPage((value) => value + 1)}>
            Next artifacts
          </button>
        )}
      </div>
    </div>
  );
}
