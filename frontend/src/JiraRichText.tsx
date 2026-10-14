import { Fragment, type ReactNode } from 'react'
import { markdownURL } from './Markdown'
import { openWebURL } from './native'

type Node = { type?: string; text?: string; content?: unknown[]; attrs?: Record<string, unknown>; marks?: { type: string; attrs?: Record<string, unknown> }[] }
export function JiraRichText({ value }: { value: unknown }) {
  if (typeof value === 'string') return <div className="jira-plain-text">{value}</div>
  if (!value || typeof value !== 'object') return null
  let count = 0
  const children = (node: Node, depth: number) => (Array.isArray(node.content) ? node.content : []).map((child, index) => <Fragment key={index}>{render(child, depth + 1)}</Fragment>)
  const link = (address: unknown, content: ReactNode): ReactNode => {
    const href = typeof address === 'string' ? markdownURL(address) : undefined
    return href && !href.startsWith('#') ? <a href={href} target="_blank" rel="noopener noreferrer" onClick={event => { event.preventDefault(); void openWebURL(href).catch(() => undefined) }}>{content}</a> : content
  }
  const render = (input: unknown, depth: number): ReactNode => {
    if (!input || typeof input !== 'object' || depth > 24 || ++count > 4000) return null
    const node = input as Node, attrs = node.attrs ?? {}
    if (node.type === 'text') {
      let text: ReactNode = typeof node.text === 'string' ? node.text : ''
      for (const mark of Array.isArray(node.marks) ? node.marks : []) {
        if (!mark || typeof mark !== 'object') continue
        if (mark.type === 'strong') text = <strong>{text}</strong>
        else if (mark.type === 'em') text = <em>{text}</em>
        else if (mark.type === 'code') text = <code>{text}</code>
        else if (mark.type === 'strike') text = <s>{text}</s>
        else if (mark.type === 'underline') text = <u>{text}</u>
        else if (mark.type === 'link') text = link(mark.attrs?.href, text)
      }
      return text
    }
    const content = () => children(node, depth)
    switch (node.type) {
      case 'doc': return content()
      case 'paragraph': return <p>{content()}</p>
      case 'heading': { const level = Number(attrs.level); return level === 1 ? <h2>{content()}</h2> : level === 2 ? <h3>{content()}</h3> : <h4>{content()}</h4> }
      case 'bulletList': return <ul>{content()}</ul>
      case 'orderedList': return <ol start={Number(attrs.order) || 1}>{content()}</ol>
      case 'listItem': return <li>{content()}</li>
      case 'blockquote': return <blockquote>{content()}</blockquote>
      case 'codeBlock': return <pre tabIndex={0} aria-label="Jira code block"><code>{(Array.isArray(node.content) ? node.content : []).map(item => typeof item === 'object' && item && 'text' in item ? String(item.text) : '').join('')}</code></pre>
      case 'hardBreak': return <br/>
      case 'rule': return <hr/>
      case 'panel': return <div className="jira-adf-panel">{content()}</div>
      case 'table': return <div className="markdown-table-scroll"><table><tbody>{content()}</tbody></table></div>
      case 'tableRow': return <tr>{content()}</tr>
      case 'tableHeader': return <th>{content()}</th>
      case 'tableCell': return <td>{content()}</td>
      case 'taskList': return <ul className="jira-task-list">{content()}</ul>
      case 'taskItem': return <li><span aria-label={attrs.state === 'DONE' ? 'Completed' : 'Incomplete'}>{attrs.state === 'DONE' ? '☑' : '☐'}</span> {content()}</li>
      case 'mention': return <span className="jira-mention">{String(attrs.text || 'Mention')}</span>
      case 'emoji': return String(attrs.text || attrs.shortName || '')
      case 'date': { const date = new Date(Number(attrs.timestamp)); return Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString() }
      case 'status': return <span className="badge">{String(attrs.text || '')}</span>
      case 'inlineCard': case 'blockCard': return link(attrs.url, String(attrs.url || 'Jira link'))
      case 'media': case 'mediaSingle': case 'mediaGroup': return <p className="muted">Attachment · open the ticket in Jira to view it.</p>
      default: return content()
    }
  }
  // Fragments keep valid list/table structure rather than introducing wrappers.
  return <div className="markdown-body jira-rich-text">{render(value, 0)}</div>
}
