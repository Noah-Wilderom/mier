import { useEffect, useRef, useState } from 'react'
import Modal from './Modal'
import { services } from './services'
import type { GitMutation } from './gitOperations'
import type { DiscardBackup, DiscardPreview, Stash } from './localGit'
import './LocalGitTools.css'

type Operate = (mutation: GitMutation) => Promise<unknown>
const messageOf = (issue: unknown) => issue instanceof Error ? issue.message : String(issue)

export function PatchPreview({ patch }: { patch: string }) {
  return <pre className="git-patch-preview" tabIndex={0}>{patch ? patch.split('\n').map((line, i) => <span key={i} className={line.startsWith('+') ? 'patch-added' : line.startsWith('-') ? 'patch-removed' : line.startsWith('@@') ? 'patch-section' : ''}>{line}{'\n'}</span>) : 'No text patch. This may be an empty or binary file.'}</pre>
}

export default function LocalGitTools({ root, headSha, busy, disabled, onOperate }: { root: string; headSha: string; busy: boolean; disabled: boolean; onOperate: Operate }) {
  const [panel, setPanel] = useState<'stashes' | 'recovery'>()
  return <div className="local-git-tools">
    <button className="btn" disabled={busy} onClick={() => setPanel('stashes')}>Stashes…</button>
    <button className="btn" disabled={busy} onClick={() => setPanel('recovery')}>Recovery…</button>
    {panel === 'stashes' && <StashDialog root={root} headSha={headSha} busy={busy} disabled={disabled} onOperate={onOperate} onClose={() => setPanel(undefined)} />}
    {panel === 'recovery' && <RecoveryDialog root={root} busy={busy} onOperate={onOperate} onClose={() => setPanel(undefined)} />}
  </div>
}

function StashDialog({ root, headSha, busy, disabled, onOperate, onClose }: { root: string; headSha: string; busy: boolean; disabled: boolean; onOperate: Operate; onClose: () => void }) {
  const [items, setItems] = useState<Stash[]>([])
  const [selected, setSelected] = useState('')
  const [patch, setPatch] = useState<{ sha: string; text: string }>()
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState(false)
  const [reload, setReload] = useState(0)
  const [message, setMessage] = useState('')
  const [mode, setMode] = useState<'all' | 'keep-index' | 'staged'>('all')
  const [untracked, setUntracked] = useState(true)
  const [restoreIndex, setRestoreIndex] = useState(true)
  const [confirmDrop, setConfirmDrop] = useState('')
  const [expectedHead] = useState(headSha)
  const guard = useRef(false)
  const locked = busy || pending
  const changed = headSha !== expectedHead
  useEffect(() => {
    let canceled = false
    setLoading(true)
    void services.repository.stashes(root).then(list => {
      if (canceled) return
      setItems(list ?? []); setSelected(previous => list?.some(item => item.sha === previous) ? previous : list?.[0]?.sha ?? '')
    }).catch(issue => { if (!canceled) setError(messageOf(issue)) }).finally(() => { if (!canceled) setLoading(false) })
    return () => { canceled = true }
  }, [root, reload])
  useEffect(() => {
    if (!selected) return
    let canceled = false
    setPatch(undefined)
    void services.repository.stashDiff(root, selected).then(text => { if (!canceled) setPatch({ sha: selected, text }) }).catch(issue => { if (!canceled) setError(messageOf(issue)) })
    return () => { canceled = true }
  }, [root, selected, reload])
  const run = async (mutation: GitMutation) => {
    if (locked || guard.current) return
    guard.current = true; setPending(true); setError('')
    try { await onOperate(mutation); setConfirmDrop(''); setReload(n => n + 1) }
    catch (issue) { setError(messageOf(issue)) }
    finally { guard.current = false; setPending(false) }
  }
  return <Modal title="Stashes" onClose={() => { if (!locked) onClose() }}><div className="local-git-dialog">
    <form onSubmit={e => { e.preventDefault(); void run({ kind: 'stash-save', request: { message, mode, includeUntracked: mode !== 'staged' && untracked, expectedHead: headSha } }) }}>
      <h3>Save local changes</h3>
      <label>Stash message<input value={message} onChange={e => setMessage(e.target.value)} disabled={locked} placeholder="Work in progress" /></label>
      <label>Changes to save<select value={mode} onChange={e => setMode(e.target.value as typeof mode)} disabled={locked}><option value="all">All tracked changes</option><option value="keep-index">Keep staged changes in place</option><option value="staged">Staged changes only</option></select></label>
      <label className="git-check"><input type="checkbox" checked={mode !== 'staged' && untracked} disabled={locked || mode === 'staged'} onChange={e => setUntracked(e.target.checked)} />Include untracked files</label>
      <button className="btn btn-primary" disabled={locked || disabled || changed || !headSha}>Save stash</button>
    </form>
    <hr />
    <div className="git-tool-heading"><h3>Saved stashes</h3><button className="btn btn-ghost" disabled={locked || loading} onClick={() => setReload(n => n + 1)}>Refresh</button></div>
    {loading ? <p role="status">Loading stashes…</p> : !items.length ? <p className="muted">No saved stashes.</p> : <>
      <label>Stash<select aria-label="Saved stash" value={selected} onChange={e => { setSelected(e.target.value); setConfirmDrop('') }} disabled={locked}>{items.map(item => <option key={item.sha + item.ref} value={item.sha}>{item.ref} · {item.message} · {new Date(item.createdAt).toLocaleString()}</option>)}</select></label>
      {patch?.sha === selected ? <PatchPreview patch={patch.text} /> : <p role="status">Loading stash preview…</p>}
      <label className="git-check"><input type="checkbox" checked={restoreIndex} disabled={locked} onChange={e => setRestoreIndex(e.target.checked)} />Restore staged changes to the index</label>
      <div className="git-tool-actions">{(['apply', 'pop'] as const).map(action => <button key={action} className="btn" disabled={locked || disabled || changed || patch?.sha !== selected} onClick={() => void run({ kind: 'stash', request: { sha: selected, action, restoreIndex, expectedHead } })}>{action === 'apply' ? 'Apply and keep stash' : 'Pop stash'}</button>)}<button className="btn" disabled={locked || changed} onClick={() => setConfirmDrop(selected)}>Drop…</button></div>
      {confirmDrop === selected && <div className="git-confirm"><p>Remove this stash from the list? Its saved changes will no longer be available here.</p><button className="btn" disabled={locked} onClick={() => setConfirmDrop('')}>Keep stash</button><button className="btn btn-danger" disabled={locked} onClick={() => void run({ kind: 'stash', request: { sha: selected, action: 'drop', restoreIndex: false, expectedHead: headSha } })}>Drop stash</button></div>}
    </>}
    {changed && <p className="git-operation-error" role="alert">HEAD changed. Close this dialog and reopen it before changing stashes.</p>}
    {error && <p role="alert" className="git-operation-error">{error}</p>}
  </div></Modal>
}

function RecoveryDialog({ root, busy, onOperate, onClose }: { root: string; busy: boolean; onOperate: Operate; onClose: () => void }) {
  const [items, setItems] = useState<DiscardBackup[]>()
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const [reload, setReload] = useState(0)
  const guard = useRef(false)
  useEffect(() => {
    let canceled = false
    void services.repository.discardBackups(root).then(list => { if (!canceled) setItems((list ?? []).sort((a, b) => b.createdAt.localeCompare(a.createdAt))) }).catch(issue => { if (!canceled) setError(messageOf(issue)) })
    return () => { canceled = true }
  }, [root, reload])
  const restore = async (id: string) => {
    if (guard.current || busy) return
    guard.current = true; setPending(true); setError('')
    try { await onOperate({ kind: 'restore-discard', id }); setReload(n => n + 1) } catch (issue) { setError(messageOf(issue)) } finally { guard.current = false; setPending(false) }
  }
  return <Modal title="Discard recovery" onClose={() => { if (!pending && !busy) onClose() }}><div className="local-git-dialog"><p>Recovery copies stay in this worktree’s Git directory. Restoring preserves the index and refuses to overwrite changes made since the discard.</p>{!items ? <p role="status">Loading recovery copies…</p> : !items.length ? <p className="muted">No discarded changes to recover.</p> : items.map(item => <div className="git-recovery-row" key={item.id}><div><code>{item.path}</code><span className="muted">{new Date(item.createdAt).toLocaleString()}</span></div><button className="btn" disabled={pending || busy || !item.afterToken} onClick={() => void restore(item.id)}>Restore file</button></div>)}{error && <p role="alert" className="git-operation-error">{error}</p>}</div></Modal>
}

export function DiscardDialog({ root, path, hunk, lines, expectedPatch, busy, onOperate, onClose }: { root: string; path: string; hunk?: number; lines?: number[]; expectedPatch?: string; busy: boolean; onOperate: Operate; onClose: () => void }) {
  const [preview, setPreview] = useState<DiscardPreview>()
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const [reload, setReload] = useState(0)
  const guard = useRef(false)
  useEffect(() => {
    let canceled = false
    setPreview(undefined); setError('')
    void services.repository.previewDiscard(root, path).then(value => { if (!canceled) setPreview(value) }).catch(issue => { if (!canceled) setError(messageOf(issue)) })
    return () => { canceled = true }
  }, [root, path, reload])
  const changed = hunk !== undefined && preview?.diff.rawPatch !== expectedPatch
  const discard = async () => {
    if (!preview || changed || pending || busy || guard.current) return
    guard.current = true; setPending(true); setError('')
    try { await onOperate({ kind: 'discard', request: { path, token: preview.token, hunk, lines } }); onClose() } catch (issue) { setError(messageOf(issue)) } finally { guard.current = false; setPending(false) }
  }
  return <Modal title="Discard changes" onClose={() => { if (!pending && !busy) onClose() }}><div className="local-git-dialog"><p>Discard {lines?.length ? `${lines.length} selected lines in section ${(hunk ?? 0) + 1}` : hunk !== undefined ? `section ${hunk + 1}` : 'unstaged changes'} in <code>{path}</code>?</p><p className="muted">A recovery copy is saved first. Staged changes stay in the index.</p>{preview ? <PatchPreview patch={preview.diff.rawPatch} /> : !error && <p role="status">Loading discard preview…</p>}{changed && <p role="alert" className="git-operation-error">This section changed. Close this dialog and select the lines again.</p>}{error && <p role="alert" className="git-operation-error">{error}</p>}<div className="git-tool-actions"><button className="btn" disabled={pending || busy} onClick={onClose}>Keep changes</button>{!preview && error && <button className="btn" onClick={() => setReload(n => n + 1)}>Retry preview</button>}<button className="btn btn-danger" disabled={!preview || changed || pending || busy} onClick={() => void discard()}>{pending ? 'Discarding…' : 'Discard changes'}</button></div></div></Modal>
}
