import { memo } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import remarkGemoji from 'remark-gemoji'
import { hasNativeBackend } from './services'
import './markdown.css'

/** Provider content may only navigate to a web URL or a local footnote. */
export function markdownURL(value: string | undefined): string | undefined {
  if (!value || /[\u0000-\u0020\u007f]/.test(value)) return undefined
  if (value.startsWith('#')) return value
  try {
    const url = new URL(value)
    if ((url.protocol === 'https:' || url.protocol === 'http:') && !url.username && !url.password) return url.href
  } catch { /* Relative and malformed provider links are not navigable. */ }
  return undefined
}

async function openExternalURL(url: string): Promise<void> {
  try {
    const { Browser } = await import('@wailsio/runtime')
    await Browser.OpenURL(url)
  } catch {
    window.open(url, '_blank', 'noopener,noreferrer')
  }
}

const components: Components = {
  a({ href, children, title, node: _node, ...props }) {
    const url = markdownURL(href)
    if (!url) return <span className="markdown-invalid-link">{children}</span>
    if (url.startsWith('#')) return <a {...props} href={url} title={title}>{children}</a>
    return <a {...props} href={url} title={title} target="_blank" rel="noopener noreferrer" onClick={event => {
      if (hasNativeBackend()) {
        event.preventDefault()
        void openExternalURL(url)
      }
    }}>{children}<span className="markdown-link-note"> (opens in browser)</span></a>
  },
  img({ src, alt, title }) {
    const url = markdownURL(src)
    return url && !url.startsWith('#')
      ? <img src={url} alt={alt || ''} title={title} loading="lazy" decoding="async" referrerPolicy="no-referrer" />
      : <span>{alt}</span>
  },
  pre({ children }) {
    return <pre tabIndex={0} aria-label="Code block">{children}</pre>
  },
  table({ children }) {
    return <div className="markdown-table-scroll" role="region" aria-label="Table" tabIndex={0}><table>{children}</table></div>
  },
}

export const Markdown = memo(function Markdown({ children, className = '', codeBlock, attachmentsAsLinks = false }: { children: string; className?: string; codeBlock?: Components['pre']; attachmentsAsLinks?: boolean }) {
  const rendered: Components = { ...components, ...(codeBlock ? { pre: codeBlock } : {}) }
  if (attachmentsAsLinks) rendered.img = ({ src, alt }) => {
    const url = markdownURL(src)
    return url && !url.startsWith('#') ? <a href={url} target="_blank" rel="noopener noreferrer" onClick={event => { if (hasNativeBackend()) { event.preventDefault(); void openExternalURL(url) } }}>Attachment: {alt || 'Open in browser'}</a> : <span>{alt}</span>
  }
  return <div className={`markdown-body ${className}`.trim()}><ReactMarkdown skipHtml remarkPlugins={[remarkGfm, remarkGemoji]} components={rendered} urlTransform={url => markdownURL(url) || ''}>{children}</ReactMarkdown></div>
})
