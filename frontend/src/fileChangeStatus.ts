export type FileChangeKind = 'modified' | 'added' | 'deleted' | 'renamed' | 'copied' | 'untracked' | 'conflict' | 'typechanged' | 'unknown'
export interface FileChangeState { status: string; indexStatus?: string; worktreeStatus?: string; conflict?: boolean; untracked?: boolean }

const statuses: Record<FileChangeKind, { label: string; icon: string }> = {
  modified: { label: 'Modified', icon: 'dot' },
  added: { label: 'Added', icon: 'plus' },
  deleted: { label: 'Deleted', icon: 'minus' },
  renamed: { label: 'Renamed', icon: 'arrow-right' },
  copied: { label: 'Copied', icon: 'copy' },
  untracked: { label: 'Untracked', icon: 'plus' },
  conflict: { label: 'Merge conflict', icon: 'alert-circle' },
  typechanged: { label: 'File type changed', icon: 'refresh' },
  unknown: { label: 'Changed', icon: 'dot' },
}
const codes: Record<string, FileChangeKind> = { M: 'modified', A: 'added', D: 'deleted', R: 'renamed', C: 'copied', T: 'typechanged', U: 'conflict', '?': 'untracked' }

// Git's XY status describes the index and working tree independently. A newly
// added staged file can have further modifications in the working tree.
export function fileChangeStatus(file: FileChangeState, layer?: 'index' | 'working') {
  let kind: FileChangeKind
  if (file.conflict || file.indexStatus === 'U' || file.worktreeStatus === 'U' || file.status === 'conflicted') kind = 'conflict'
  else {
    const code = layer === 'index' ? file.indexStatus : layer === 'working' ? file.worktreeStatus : undefined
    if (code && Object.prototype.hasOwnProperty.call(codes, code)) kind = codes[code]
    else if (layer !== 'index' && file.untracked) kind = 'untracked'
    else if (Object.prototype.hasOwnProperty.call(codes, file.status)) kind = codes[file.status]
    else if (Object.prototype.hasOwnProperty.call(statuses, file.status)) kind = file.status as FileChangeKind
    else kind = file.status === 'removed' ? 'deleted' : 'unknown'
  }
  return { kind, ...statuses[kind] }
}
