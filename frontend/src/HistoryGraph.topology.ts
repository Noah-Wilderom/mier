/** Only exact commit IDs establish ancestry. Abbreviated hashes are display text. */
export interface GraphCommit {
  sha: string;
  parents?: readonly string[];
  refs?: readonly string[];
}

export interface GraphRow<T extends GraphCommit = GraphCommit> {
  commit: T;
  index: number;
  lane: number;
  missingParents: string[];
  invalidParents: string[];
  parentMetadataUnavailable: boolean;
  invalidTopology: boolean;
}

export interface GraphEdge {
  childSha: string;
  parentSha: string;
  childIndex: number;
  parentIndex: number;
  childLane: number;
  parentLane: number;
}

export interface HistoryLayout<T extends GraphCommit = GraphCommit> {
  rows: GraphRow<T>[];
  edges: GraphEdge[];
  laneCount: number;
  hasInvalidTopology: boolean;
}

export function commitRefs(commit: GraphCommit): string[] {
  return [...new Set((commit.refs ?? []).filter((ref) => ref.trim().length > 0))];
}

function uniqueParents(commit: GraphCommit): string[] {
  return [...new Set(commit.parents ?? [])];
}

/**
 * Stable child-before-parent order, then sparse lanes reserved until their parent
 * is reached. A reserved lane never passes through an unrelated commit node.
 * Disconnected histories can reuse a free lane but never receive invented edges.
 */
export function buildHistoryGraph<T extends GraphCommit>(
  commits: readonly T[],
): HistoryLayout<T> {
  const bySha = new Map<string, T>();
  for (const commit of commits) {
    if (!bySha.has(commit.sha)) bySha.set(commit.sha, commit);
  }
  const unique = [...bySha.values()];
  const originalIndex = new Map(unique.map((commit, index) => [commit.sha, index]));
  const childCounts = new Map(unique.map((commit) => [commit.sha, 0]));
  const invalid = new Set<string>();
  for (const commit of unique) {
    for (const parent of uniqueParents(commit)) {
      if (parent === commit.sha) {
        invalid.add(commit.sha);
      } else if (bySha.has(parent)) {
        childCounts.set(parent, childCounts.get(parent)! + 1);
      }
    }
  }

  const ready = unique.filter((commit) => childCounts.get(commit.sha) === 0);
  const ordered: T[] = [];
  const emitted = new Set<string>();
  while (ready.length > 0) {
    const commit = ready.shift()!;
    ordered.push(commit);
    emitted.add(commit.sha);
    for (const parent of uniqueParents(commit)) {
      if (parent === commit.sha || !bySha.has(parent)) continue;
      const count = childCounts.get(parent)! - 1;
      childCounts.set(parent, count);
      if (count === 0) {
        ready.push(bySha.get(parent)!);
        ready.sort((a, b) => originalIndex.get(a.sha)! - originalIndex.get(b.sha)!);
      }
    }
  }
  // Git commits cannot be cyclic. Keep malformed records inspectable, without
  // drawing ancestry that could misrepresent them or preventing the UI loading.
  for (const commit of unique) {
    if (!emitted.has(commit.sha)) {
      invalid.add(commit.sha);
      ordered.push(commit);
    }
  }

  const rowIndex = new Map(ordered.map((commit, index) => [commit.sha, index]));
  const lanes: (string | undefined)[] = [];
  const rows: GraphRow<T>[] = [];
  const pendingEdges: Omit<GraphEdge, "parentIndex" | "parentLane">[] = [];
  let laneCount = 0;
  const freeLane = (excluded?: number): number => {
    let lane = 0;
    while (lanes[lane] !== undefined || lane === excluded) lane++;
    return lane;
  };

  ordered.forEach((commit, index) => {
    const reserved = lanes.indexOf(commit.sha);
    const lane = reserved >= 0 ? reserved : freeLane();
    lanes[lane] = undefined;
    laneCount = Math.max(laneCount, lane + 1);
    const row: GraphRow<T> = {
      commit,
      index,
      lane,
      missingParents: [],
      invalidParents: [],
      parentMetadataUnavailable: commit.parents === undefined,
      invalidTopology: invalid.has(commit.sha),
    };
    rows.push(row);

    uniqueParents(commit).forEach((parent, parentNumber) => {
      if (!bySha.has(parent)) {
        row.missingParents.push(parent);
        return;
      }
      if (invalid.has(commit.sha) || invalid.has(parent) || rowIndex.get(parent)! <= index) {
        row.invalidParents.push(parent);
        return;
      }
      let parentLane = lanes.indexOf(parent);
      if (parentLane < 0) {
        // First-parent history continues in place. Other parents diverge even
        // when the first parent falls outside the loaded history.
        parentLane = parentNumber === 0 && lanes[lane] === undefined
          ? lane
          : freeLane(lane);
        lanes[parentLane] = parent;
      }
      laneCount = Math.max(laneCount, parentLane + 1);
      pendingEdges.push({
        childSha: commit.sha,
        parentSha: parent,
        childIndex: index,
        childLane: lane,
      });
    });
  });

  const placed = new Map(rows.map((row) => [row.commit.sha, row]));
  const edges = pendingEdges.map((edge): GraphEdge => {
    const parent = placed.get(edge.parentSha)!;
    return { ...edge, parentIndex: parent.index, parentLane: parent.lane };
  });
  return { rows, edges, laneCount, hasInvalidTopology: invalid.size > 0 };
}

export const HISTORY_ROW_HEIGHT = 42;
export const HISTORY_LANE_WIDTH = 18;
export const historyLaneX = (lane: number): number => 16 + lane * HISTORY_LANE_WIDTH;
export const historyRowY = (index: number): number => (index + 0.5) * HISTORY_ROW_HEIGHT;

export function historyEdgePath(edge: GraphEdge): string {
  const startX = historyLaneX(edge.childLane);
  const endX = historyLaneX(edge.parentLane);
  const startY = historyRowY(edge.childIndex);
  const endY = historyRowY(edge.parentIndex);
  if (startX === endX) return `M${startX} ${startY}V${endY}`;
  return `M${startX} ${startY}C${startX} ${startY + 10} ${endX} ${startY + 10} ${endX} ${startY + 21}V${endY}`;
}
