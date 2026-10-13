import { useEffect, useRef, useState } from 'react'
import { Icon } from './Icon'

export interface RepositoryCard {
  path: string
  name: string
  branch?: string
  host?: string
  owner?: string
  repo?: string
  openPullRequests?: number
  loadingPullRequests?: boolean
}

export function repositoryName(path: string): string {
  return path.split(/[\\/]/).filter(Boolean).pop() || path
}

export default function RepositorySelector({items, selectedPath, loading, busy, onSelect, onOpen}: {
  items: RepositoryCard[]
  selectedPath: string
  loading: boolean
  busy: boolean
  onSelect: (path: string) => void
  onOpen?: () => void
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [index, setIndex] = useState(0)
  const anchor = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const selected = items.find(item => item.path === selectedPath)
  const filtered = items.filter(item => `${item.name} ${item.owner ?? ''} ${item.host ?? ''} ${item.path}`.toLowerCase().includes(query.toLowerCase()))
  const close = (restore = true) => {setOpen(false); if (restore) trigger.current?.focus()}
  const choose = (path: string) => {close(); onSelect(path)}
  useEffect(() => {
    if (!open) return
    input.current?.focus()
    const outside = (event: PointerEvent) => {if (!anchor.current?.contains(event.target as Node)) setOpen(false)}
    window.addEventListener('pointerdown', outside)
    return () => window.removeEventListener('pointerdown', outside)
  }, [open])
  useEffect(() => {if (open) anchor.current?.querySelector(`[data-repository-index="${index}"]`)?.scrollIntoView({block:'nearest'})}, [index, open])
  const count = (item: RepositoryCard) => {
    const pending = !!item.loadingPullRequests || (loading && item.path === selectedPath)
    return <span className="repository-pr-count" title="Open pull requests" aria-label={pending ? 'Loading open pull request count' : item.openPullRequests !== undefined ? `${item.openPullRequests} open pull requests` : 'Pull request count has not loaded'}><Icon name="pull-request" size={14}/>{pending ? <span className="skeleton skeleton-count" aria-hidden="true"/> : item.openPullRequests ?? '—'}</span>
  }
  return <div className="repository-switcher" ref={anchor}>
    <button className="repository-switcher-trigger" ref={trigger} aria-label={`Select repository, current ${selected?.name ?? 'none'}`} aria-haspopup="dialog" aria-expanded={open} onClick={() => {setOpen(value => !value); setQuery(''); setIndex(0)}}>
      <Icon name={selected?.host ? 'github' : 'folder'} size={20}/>
      <span className="repository-trigger-text"><strong title={selected?.name}>{selected?.name ?? 'Choose a repository'}</strong><span><Icon name="branch" size={13}/>{selected?.branch ?? 'Open your workspace'}{selected?.owner && <><span className="repository-meta-dot">·</span>{selected.owner}</>}</span></span>
      {selected && count(selected)}<Icon name="chevron-down" size={14}/>
    </button>
    {open && <div className="repository-switcher-menu" role="dialog" aria-label="Switch repository" onKeyDown={event => {
      if(event.key === 'Escape'){event.preventDefault();event.stopPropagation();close()}
      if(['ArrowDown','ArrowUp','Home','End'].includes(event.key)){event.preventDefault();setIndex(value => event.key === 'Home' ? 0 : event.key === 'End' ? Math.max(0, filtered.length - 1) : Math.max(0,Math.min(filtered.length-1,value+(event.key==='ArrowDown'?1:-1))))}
      if(event.key === 'Enter' && document.activeElement === input.current && filtered[index]){event.preventDefault();choose(filtered[index].path)}
    }}>
      <label className="repository-switcher-search"><Icon name="search" size={17}/><input ref={input} role="combobox" aria-label="Search repositories" aria-autocomplete="list" aria-expanded="true" aria-controls="repository-options" aria-activedescendant={filtered[index] ? `repository-option-${index}` : undefined} value={query} onChange={event => {setQuery(event.target.value);setIndex(0)}} placeholder="Search repositories…"/><kbd>Esc</kbd></label>
      <div className="repository-switcher-list" id="repository-options" role="listbox" aria-label="Repositories">{filtered.map((item, row) => <button key={item.path} id={`repository-option-${row}`} role="option" aria-selected={item.path===selectedPath} className={`repository-option ${row===index?'highlighted':''}`} data-repository-index={row} disabled={busy} onMouseMove={() => setIndex(row)} onClick={() => choose(item.path)}>
        <Icon name={item.host ? 'github':'folder'} size={20}/><span className="repository-option-text"><strong title={item.name}>{item.name}</strong><span title={item.path}>{item.owner && `${item.owner}/`}{item.repo ?? item.path}</span><span className="repository-option-branch"><Icon name="branch" size={13}/>{item.branch ?? 'Branch not loaded'}{item.host && <><span className="repository-meta-dot">·</span>{item.host}</>}</span></span>{count(item)}{item.path===selectedPath&&<Icon name="check" className="text-green" size={15}/>}</button>)}</div>
      {!filtered.length && <p className="repository-switcher-empty">No repositories match “{query}”.</p>}
      {onOpen && <button className="repository-switcher-open" disabled={busy} onClick={() => {close();onOpen()}}><Icon name="plus" size={17}/>Open repository…<kbd>⌘ / Ctrl O</kbd></button>}
    </div>}
  </div>
}
