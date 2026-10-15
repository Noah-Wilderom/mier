export interface GitOperation {
  kind: 'merge' | 'cherry-pick' | 'rebase' | 'revert' | 'am' | 'bisect'
  token: string
  sourceSha: string
  sourceLabel: string
  currentLabel: string
  conflicts: number
  message: string
  canSkip: boolean
  empty?: boolean
  pausedForEdit?:boolean
}
export interface GitOperationResult { headSha: string; operation?: GitOperation }
export interface ConflictVersion { exists: boolean; sha: string; mode: string; content: string; binary: boolean }
export interface ConflictFile { path: string; token: string; base: ConflictVersion; current: ConflictVersion; incoming: ConflictVersion; working: string; editable: boolean; reason: string; currentLabel: string; incomingLabel: string }
export interface ResolveConflictRequest { path: string; token: string; choice: 'result' | 'current' | 'incoming' | 'delete'; content: string }
export interface CherryPickRequest { sha: string; expectedHead: string; expectedBranch: string; mainline: number }
export interface MergeBranchRequest { branch: string; expectedSource: string; expectedHead: string; expectedBranch: string }
export interface MergePreflight { head:string;source:string;clean:boolean;fastForward:boolean;conflicts:string[] }
export interface StageHunkRequest { path: string; staged: boolean; hunk: number; expectedPatch: string; lines?:number[] }
export type GitMutation =
  | { kind: 'cherry-pick'; request: CherryPickRequest }
  | { kind: 'merge'; request: MergeBranchRequest }
  | { kind: 'resolve'; request: ResolveConflictRequest }
  | { kind: 'hunk'; request: StageHunkRequest }
  | { kind: 'stash-save'; request: import('./localGit').SaveStashRequest }
  | { kind: 'stash'; request: import('./localGit').StashRequest }
  | { kind: 'amend'; request: import('./localGit').AmendRequest }
  | { kind: 'discard'; request: import('./localGit').DiscardRequest }
  | { kind: 'restore-discard'; id:string }
  | { kind: 'ignore'; path:string }
  | { kind: 'tag'; request:import('./repositoryToolTypes').TagRequest }
  | { kind: 'remote'; request:import('./repositoryToolTypes').RemoteRequest }
  | { kind: 'signing'; request:import('./repositoryToolTypes').SigningConfig }
  | { kind: 'reset'; request:import('./repositoryToolTypes').ResetRequest }
  | { kind: 'recover-branch'; request:import('./repositoryToolTypes').RecoverBranchRequest }
  | { kind: 'cherry-pick-many'; request:import('./repositoryToolTypes').CherryPickManyRequest }
  | { kind: 'revert'; request:CherryPickRequest }
  | { kind: 'rebase'; request:import('./repositoryToolTypes').RebaseRequest }
  | { kind: 'worktree'; request:import('./repositoryToolTypes').WorktreeRequest }
  | { kind:'apply-patch';request:import('./repositoryToolTypes').ApplyPatchRequest }
  | { kind:'submodule';request:import('./repositoryToolTypes').SubmoduleRequest }
  | { kind:'lfs';request:import('./repositoryToolTypes').LFSRequest }
  | { kind:'gitflow';request:import('./repositoryToolTypes').FlowRequest }
  | { kind:'bisect';request:import('./repositoryToolTypes').BisectRequest }
  | { kind:'custom-command';id:string }
  | { kind: 'continue' | 'abort' | 'skip'; token: string }
export const operationLabel = (kind: GitOperation['kind']) => ({ merge: 'Merge', 'cherry-pick': 'Cherry-pick', rebase: 'Rebase', revert: 'Revert', am: 'Apply patches',bisect:'Bisect' })[kind]

export interface ConflictChunk { before: string; current: string; incoming: string; base?: string; start: number; end: number }
// Offsets retain CRLF and the last line's newline. Both regular and diff3/zdiff3
// markers, including repository-specific marker sizes, are handled.
export function conflictChunks(source: string): ConflictChunk[] {
  const lines = source.match(/[^\n]*\n|[^\n]+$/g) ?? []
  const offsets = [0]
  for (const line of lines) offsets.push(offsets[offsets.length - 1] + line.length)
  const chunks: ConflictChunk[] = []
  let previous = 0
  for (let i = 0; i < lines.length; i++) {
    const start = /^(<{7,})(?:[ \t].*)?\r?\n?$/.exec(lines[i])
    if (!start) continue
    const size = start[1].length
    const isMarker = (line: string, character: string) => new RegExp(`^${character}{${size}}(?:[ \\t].*)?\\r?\\n?$`).test(line)
    let base = -1, separator = -1, end = -1
    for (let j = i + 1; j < lines.length; j++) {
      if (isMarker(lines[j], '\\|') && separator < 0) base = j
      else if (isMarker(lines[j], '=') && separator < 0) separator = j
      else if (isMarker(lines[j], '>') && separator >= 0) { end = j; break }
      else if (/^<{7,}(?:[ \t]|\r?$)/.test(lines[j])) break
    }
    if (separator < 0 || end < 0) continue
    chunks.push({ before: source.slice(previous, offsets[i]), current: source.slice(offsets[i + 1], offsets[base >= 0 ? base : separator]), incoming: source.slice(offsets[separator + 1], offsets[end]), ...(base >= 0 ? { base: source.slice(offsets[base + 1], offsets[separator]) } : {}), start: offsets[i], end: offsets[end + 1] })
    previous = offsets[end + 1]; i = end
  }
  return chunks
}
export function resolveChunk(source: string, chunk: ConflictChunk, choice: 'current' | 'incoming' | 'both' | 'incoming-first'): string {
  return source.slice(0, chunk.start) + (choice === 'current' ? chunk.current : choice === 'incoming' ? chunk.incoming : choice==='incoming-first'?chunk.incoming+chunk.current:chunk.current + chunk.incoming) + source.slice(chunk.end)
}
export const containsConflictMarkers = (source: string) => /^(?:<{7,}|={7,}|>{7,}|\|{7,})(?:[ \t\r].*)?$/m.test(source)
