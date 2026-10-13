import { useEffect, useId, useRef, type ReactNode } from 'react'
import { Icon } from './Icon'
import './RepositoryWorkspace.css'

export default function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const heading = useId()
  const close = useRef(onClose)
  close.current = onClose
  useEffect(() => {
    const target = dialog.current
    const previous = document.activeElement as HTMLElement | null
    target?.showModal()
    target?.querySelector<HTMLElement>('input:not([disabled]), select:not([disabled]), textarea:not([disabled])')?.focus()
    return () => {
      target?.close()
      if (previous?.isConnected) previous.focus()
    }
  }, [])
  return <dialog ref={dialog} className="repository-modal" aria-labelledby={heading}
    onCancel={event => { event.preventDefault(); close.current() }}
    onClick={event => { if (event.target === event.currentTarget) close.current() }}
    onKeyDown={event => event.stopPropagation()}>
    <header className="repository-modal-heading"><h2 id={heading}>{title}</h2><button type="button" className="icon-button" aria-label={`Close ${title}`} onClick={onClose}><Icon name="close"/></button></header>
    {children}
  </dialog>
}
