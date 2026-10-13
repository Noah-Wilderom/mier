import { useEffect, useRef, useState } from 'react'
import { Icon } from './Icon'
import { hasNativeBackend } from './services'
import './NotificationToasts.css'

export interface PullRequestNotice {
  id: string
  key: string
  host: string
  owner: string
  repo: string
  number: number
  type: 'opened' | 'closed' | 'merged' | 'reviewRequested'
  title: string
  url: string
  actor: string
  toast: boolean
  desktop: boolean
  createdAt: string
}

const labels = {opened:'Pull request opened',closed:'Pull request closed',merged:'Pull request merged',reviewRequested:'Your review was requested'}
const icons = {opened:'pull-request',closed:'close',merged:'git-merge',reviewRequested:'user'}

export function NotificationToasts({onOpen}: {onOpen: (notice: PullRequestNotice) => Promise<void>}) {
  const [notices, setNotices] = useState<PullRequestNotice[]>([])
  const [opening, setOpening] = useState('')
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>())
  const seen = useRef(new Set<string>())
  const dismiss = (id: string) => setNotices(items => items.filter(item => item.id !== id))
  useEffect(() => {
    let closed = false
    let connecting = false
    let unsubscribe: (()=>void) | undefined
    const connect = () => {
      if(closed || connecting || !hasNativeBackend()) return
      connecting = true
      void import('@wailsio/runtime').then(({Events}) => {
        if(closed) return
        unsubscribe = Events.On('notifications:pr', event => {
          const notice = event.data as PullRequestNotice
          if(!notice?.toast || !notice.id || !(notice.type in labels) || seen.current.has(notice.id)) return
          seen.current.add(notice.id)
          if(seen.current.size > 1000) seen.current.delete(seen.current.values().next().value!)
          setNotices(items => [...items, notice])
        })
      }).catch(() => {connecting = false})
    }
    connect()
    window.addEventListener('wails:runtime-config-ready', connect)
    return () => {closed = true; unsubscribe?.();window.removeEventListener('wails:runtime-config-ready', connect)}
  }, [])
  const visible = notices.slice(0,3)
  useEffect(() => {
    for(const notice of visible) if(!timers.current.has(notice.id)) timers.current.set(notice.id,setTimeout(() => dismiss(notice.id),8000))
    for(const [id,timer] of timers.current) if(!notices.some(notice => notice.id===id)){clearTimeout(timer);timers.current.delete(id)}
  }, [notices])
  useEffect(() => () => {for(const timer of timers.current.values())clearTimeout(timer)}, [])
  if(!visible.length) return null
  return <aside className="pr-notification-toasts" aria-label="Pull request notifications" aria-live="polite" aria-relevant="additions">
    {visible.map(notice => <article className="pr-notification-toast" key={notice.id}>
      <Icon name={icons[notice.type]} className={notice.type==='merged'?'text-purple':notice.type==='closed'?'text-red':'text-green'} size={19}/>
      <button className="pr-notification-open" disabled={opening===notice.id} onClick={() => {setOpening(notice.id);void onOpen(notice).then(()=>dismiss(notice.id)).catch(()=>{}).finally(()=>setOpening(''))}}>
        <strong>{labels[notice.type]}</strong><span>#{notice.number} {notice.title}</span><small>{notice.owner}/{notice.repo}</small>
      </button><button className="icon-button" aria-label={`Dismiss ${labels[notice.type].toLowerCase()} for pull request ${notice.number}`} onClick={() => dismiss(notice.id)}><Icon name="close" size={14}/></button>
    </article>)}
  </aside>
}
