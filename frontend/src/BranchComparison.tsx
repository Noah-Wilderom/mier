import { useEffect, useMemo, useState, useSyncExternalStore } from 'react'
import type { ChangedFile } from './data'
import { services, type RepositorySnapshot } from './services'
import { mapRepositoryDiff } from './liveData'
import { comparisonBase } from './gitWorkspace'
import { Icon } from './Icon'
import FileChangeIcon from './FileChangeIcon'
import { codeReviewStyleVariables, DEFAULT_CODE_REVIEW_PREFERENCES, effectiveCodeTheme, getCodeAppearance, getCodeReviewPreferences, subscribeCodeAppearance, subscribeCodeReviewPreferences } from './codeReviewPreferences'
import { highlightDiff, syntaxLineKey, syntaxTokenStyle, type HighlightedDiff } from './syntaxHighlighter'
import './RepositoryWorkspace.css'

export function useBranchComparison(repository: RepositorySnapshot | null, base: string, head: string) {
  const [result, setResult] = useState<{ key: string; files: ChangedFile[] } | null>(null)
  const [failure, setFailure] = useState<{ key: string; message: string } | null>(null)
  const [loading, setLoading] = useState(false)
  const [retry, setRetry] = useState(0)
  const revisions = (repository?.branches ?? []).filter(branch => branch.name === base || branch.name === head).map(branch => [branch.name, branch.commitSha])
  const key = JSON.stringify([repository?.root, base, head, revisions])
  const valid = !!repository?.root && !!base && !!head && base !== head
  useEffect(() => {
    let canceled = false
    if (!valid || !repository) { setResult(null); setFailure(null); setLoading(false); return }
    setLoading(true); setFailure(null)
    setResult(previous => previous?.key === key ? previous : null)
    void services.repository.compare(repository.root, base, head).then(files => {
      if (!canceled) setResult({ key, files: files.map(mapRepositoryDiff) })
    }).catch(issue => { if (!canceled) setFailure({ key, message: issue instanceof Error ? issue.message : String(issue) }) })
      .finally(() => { if (!canceled) setLoading(false) })
    return () => { canceled = true }
  }, [key, retry, valid])
  return { files: result?.key === key ? result.files : null, error: failure?.key === key ? failure.message : '', loading, retry: () => setRetry(value => value + 1) }
}

export default function BranchComparison({ repository }: { repository: RepositorySnapshot }) {
  const [base, setBase] = useState(() => comparisonBase(repository))
  const [head, setHead] = useState(repository.currentBranch || 'HEAD')
  const comparison = useBranchComparison(repository, base, head)
  const branches = [...new Set(['HEAD', ...(repository.branches ?? []).map(branch => branch.name)])]
  return <div className="branch-comparison">
    <div className="comparison-selectors"><label>Base revision<select value={base} onChange={event => setBase(event.target.value)}><option value="">Choose a base</option>{branches.map(name => <option key={name}>{name}</option>)}</select></label><Icon name="arrow-right"/><label>Compare revision<select value={head} onChange={event => setHead(event.target.value)}>{branches.map(name => <option key={name}>{name}</option>)}</select></label></div>
    <p className="comparison-note">Committed changes since the common ancestor. Your index and working files are preserved.</p>
    <ComparisonPreview {...comparison} valid={!!base && !!head && base !== head}/>
  </div>
}

export function ComparisonPreview({ files, error, loading, retry, valid }: {
  files: ChangedFile[] | null; error: string; loading: boolean; retry: () => void; valid: boolean
}) {
  const [selected, setSelected] = useState('')
  const file = files?.find(file => file.path === selected) ?? files?.[0]
  const totals = files?.reduce((sum, file) => ({ additions: sum.additions + file.additions, deletions: sum.deletions + file.deletions }), { additions: 0, deletions: 0 })
  return <div className="comparison-preview" aria-busy={loading}>
    {error && <div className="comparison-error" role="alert"><Icon name="alert-circle"/><span>{error}</span><button type="button" className="btn" disabled={loading} onClick={retry}>Retry comparison</button></div>}
    {!valid ? <p className="comparison-empty">Choose two different revisions to preview their changes.</p> : !files && loading ? <p role="status" className="comparison-empty">Loading committed changes…</p> : files?.length ? <>
      <div className="comparison-summary"><strong>{files.length} {files.length === 1 ? 'file' : 'files'} changed</strong><span className="text-green">+{totals?.additions}</span><span className="text-red">−{totals?.deletions}</span>{loading && <span role="status">Refreshing…</span>}</div>
      <label className="comparison-file">{file && <FileChangeIcon file={file}/>}<span className="sr-only">Compared file</span><select aria-label="Compared file" value={file?.path ?? ''} onChange={event => setSelected(event.target.value)}>{files.map(file => <option key={file.path} value={file.path}>{file.path}{file.previousPath ? ` ← ${file.previousPath}` : ''} · +{file.additions} −{file.deletions}</option>)}</select></label>
      {file && <DiffPreview file={file}/>}</> : files ? <p className="comparison-empty">No file changes between these revisions.</p> : null}
  </div>
}

export function DiffPreview({ file }: { file: ChangedFile }) {
  const preferences = useSyncExternalStore(subscribeCodeReviewPreferences, getCodeReviewPreferences, () => DEFAULT_CODE_REVIEW_PREFERENCES)
  const appearance = useSyncExternalStore(subscribeCodeAppearance, getCodeAppearance, () => 'light' as const)
  const theme = effectiveCodeTheme(appearance, preferences)
  const source = useMemo(() => JSON.stringify([file.path, file.previousPath, file.lines]), [file])
  const [highlighted, setHighlighted] = useState<{ source: string; theme: string; syntax: HighlightedDiff } | null>(null)
  useEffect(() => {
    let canceled = false
    void highlightDiff(file, theme).then(syntax => { if (!canceled) setHighlighted({ source, theme, syntax }) })
    return () => { canceled = true }
  }, [source, theme])
  const syntax = highlighted?.source === source && highlighted.theme === theme ? highlighted.syntax : undefined
  if (file.binary) return <p className="comparison-empty">Binary file changed. A text preview is unavailable.</p>
  if (file.patchUnavailable) return <p className="comparison-empty">GitHub did not supply a text patch for this file. It may be binary or too large to preview.</p>
  return <div className="comparison-code" style={codeReviewStyleVariables(preferences)} aria-label={`Diff for ${file.path}`}>
    <div className="comparison-code-lines">{file.lines.map((line, index) => {
      const side = line.kind === 'remove' ? 'old' : 'new'
      const tokens = syntax?.[side].get(syntaxLineKey(line, side))
      return <div key={index} className={`comparison-code-line kind-${line.kind}`}><span className="comparison-line-number" aria-hidden="true">{line.oldLine ?? ''}</span><span className="comparison-line-number" aria-hidden="true">{line.newLine ?? ''}</span><span className="comparison-line-marker" aria-hidden="true">{line.kind === 'add' ? '+' : line.kind === 'remove' ? '−' : ' '}</span><code>{tokens ? tokens.map((token, part) => <span key={part} style={syntaxTokenStyle(token)}>{token.content}</span>) : line.text || '\u00a0'}</code></div>
    })}</div>
    {!file.lines.length && <p className="comparison-empty">No text changes in this file.</p>}
    {file.truncated && <p className="comparison-note" role="status">This file is too large to preview in full.</p>}
  </div>
}
