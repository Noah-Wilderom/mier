import { useEffect, useState } from 'react'
import type { ChangedFile } from './data'
import { ComparisonPreview } from './BranchComparison'
import FileHistory from './FileHistory'

export interface CommitDiffResult { files: ChangedFile[]; warnings?: string[] }
export type LoadCommitDiff = (sha: string) => Promise<CommitDiffResult>

export default function CommitChanges({ requestKey, sha, loadDiff, root }: { requestKey: string; sha: string; loadDiff: LoadCommitDiff; root?:string }) {
  const [result, setResult] = useState<{ key: string; value: CommitDiffResult } | null>(null)
  const [failure, setFailure] = useState<{ key: string; message: string } | null>(null)
  const [loading, setLoading] = useState(false)
  const [retry, setRetry] = useState(0)
  const [historyFile,setHistoryFile]=useState('')
  useEffect(() => {
    let canceled = false
    setLoading(true); setFailure(null)
    setResult(previous => previous?.key === requestKey ? previous : null)
    void loadDiff(sha).then(value => { if (!canceled) setResult({ key: requestKey, value }) })
      .catch(issue => { if (!canceled) setFailure({ key: requestKey, message: issue instanceof Error ? issue.message : String(issue) }) })
      .finally(() => { if (!canceled) setLoading(false) })
    return () => { canceled = true }
  }, [requestKey, sha, loadDiff, retry])
  const value = result?.key === requestKey ? result.value : undefined
  return <div className="commit-changes">
    <p className="comparison-note">Changes introduced by this commit relative to its first parent. This view is read-only.</p>
    {value?.warnings?.map(message => <p className="comparison-note" role="status" key={message}>{message}</p>)}
    {root&&value?.files.length&&<div className="history-pagination"><span>File history and blame</span>{value.files.filter(file=>file.status!=='deleted').map(file=><button className="btn btn-ghost" key={file.path} onClick={()=>setHistoryFile(file.path)} title={file.path}>{file.path.split('/').pop()}…</button>)}</div>}
    {root&&historyFile&&<FileHistory key={root+historyFile+sha} root={root} path={historyFile} revision={sha} onClose={()=>setHistoryFile('')}/>}
    <ComparisonPreview files={value?.files ?? null} error={failure?.key === requestKey ? failure.message : ''} loading={loading} retry={() => setRetry(value => value + 1)} valid/>
  </div>
}
