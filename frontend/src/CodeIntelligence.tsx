import { useEffect, useMemo, useRef, useState, useSyncExternalStore, type CSSProperties, type ReactNode } from 'react'
import { Icon } from './Icon'
import { Markdown } from './Markdown'
import { hasNativeBackend, LSP_CONFIGURATION_CHANGED_EVENT, services, type CodePositionRequest, type LSPDefinitionDocument, type LSPDefinitionTarget, type LSPHover, type LSPServerStatus } from './services'
import { codeReviewStyleVariables, effectiveCodeTheme, getCodeAppearance, getCodeReviewPreferences, subscribeCodeAppearance, subscribeCodeReviewPreferences } from './codeReviewPreferences'
import { highlightCodeLines, syntaxTokenStyle, type HighlightedCode } from './syntaxHighlighter'
import './CodeIntelligence.css'

export interface CodeIntelligenceProps {
  root: string
  host?: string
  owner?: string
  repo?: string
  pullRequest?: number
  headSha: string
  baseSha?: string
  headRevision?: string
  baseRevision?: string
  revisionKey?: string
  enabled?: boolean
  onOpenSettings?: () => void
  children: ReactNode
}

export interface DOMCodeLocation {
  element: HTMLElement
  path: string
  line: number
  side: 'old' | 'new'
  character: number
  lineText: string
  revision?: string
}

type CaretDocument = Document & {
  caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null
  caretRangeFromPoint?: (x: number, y: number) => Range | null
}

/** DOM Range measures UTF-16 code units across nested syntax spans. */
export function codeCharacterOffset(code: HTMLElement, node: Node, offset: number): number | undefined {
  if (node !== code && !code.contains(node)) return undefined
  try {
    const range = code.ownerDocument.createRange()
    range.selectNodeContents(code)
    range.setEnd(node, offset)
    return range.toString().length
  } catch { return undefined }
}

function codeLocation(code: HTMLElement, node: Node, offset: number): DOMCodeLocation | undefined {
  const character = codeCharacterOffset(code, node, offset)
  const line = Number(code.dataset.codeLine)
  const side = code.dataset.codeSide
  if (character === undefined || !code.dataset.codePath || !Number.isInteger(line) || line < 1 || (side !== 'old' && side !== 'new')) return undefined
  return { element: code, path: code.dataset.codePath, line: line - 1, side, character, lineText: code.textContent ?? '', revision: code.dataset.codeRevision }
}

export function codeLocationAtPoint(container: HTMLElement, target: EventTarget | null, x: number, y: number): DOMCodeLocation | undefined {
  const element = target instanceof Element ? target : target instanceof Node ? target.parentElement : null
  const code = element?.closest<HTMLElement>('[data-code-path][data-code-line][data-code-side]')
  if (!code || !container.contains(code)) return undefined
  const document = code.ownerDocument as CaretDocument
  const position = document.caretPositionFromPoint?.(x, y)
  if (position) return codeLocation(code, position.offsetNode, position.offset)
  const range = document.caretRangeFromPoint?.(x, y)
  return range ? codeLocation(code, range.startContainer, range.startOffset) : undefined
}

export function codeLocationFromSelection(container: HTMLElement): DOMCodeLocation | undefined {
  const selection = container.ownerDocument.getSelection()
  const node = selection?.anchorNode
  const element = node instanceof Element ? node : node?.parentElement
  const code = element?.closest<HTMLElement>('[data-code-path][data-code-line][data-code-side]')
  return code && node && selection && container.contains(code) && !code.closest('[hidden]') ? codeLocation(code, node, selection.anchorOffset) : undefined
}

type HoverState = { location: DOMCodeLocation; x: number; y: number; loading: boolean; result?: LSPHover; error?: string }
type ReaderEntry = { target: LSPDefinitionTarget; document: LSPDefinitionDocument }
const completeRevision = /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/i

function languageFor(position: DOMCodeLocation): string | undefined {
  const supplied = position.element.dataset.codeLanguage
  if (supplied) return ['javascript', 'typescriptreact', 'javascriptreact'].includes(supplied) ? 'typescript' : supplied
  const extension = position.path.split('.').pop()?.toLowerCase()
  if (extension && ['php', 'phtml'].includes(extension)) return 'php'
  if (extension && ['js', 'jsx', 'mjs', 'cjs', 'ts', 'tsx', 'mts', 'cts'].includes(extension)) return 'typescript'
  return extension === 'vue' ? 'vue' : undefined
}

export default function CodeIntelligence({ root, host, owner, repo, pullRequest, headSha, baseSha, headRevision, baseRevision, revisionKey, enabled, onOpenSettings, children }: CodeIntelligenceProps) {
  const [nativeAvailable, setNativeAvailable] = useState(hasNativeBackend)
  const [location, setLocation] = useState<DOMCodeLocation>()
  const [hover, setHover] = useState<HoverState>()
  const [targets, setTargets] = useState<LSPDefinitionTarget[]>([])
  const [readerHistory, setReaderHistory] = useState<ReaderEntry[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [serverStatuses, setServerStatuses] = useState<LSPServerStatus[]>()
  const [availabilityError, setAvailabilityError] = useState('')
  const [highlighted, setHighlighted] = useState<{ document: LSPDefinitionDocument; theme: string; code: HighlightedCode }>()
  const preferences = useSyncExternalStore(subscribeCodeReviewPreferences, getCodeReviewPreferences)
  const appearance = useSyncExternalStore(subscribeCodeAppearance, getCodeAppearance)
  const codeTheme = effectiveCodeTheme(appearance, preferences)
  const container = useRef<HTMLDivElement>(null)
  const hoverPanel = useRef<HTMLDivElement>(null)
  const hoverTimer = useRef<ReturnType<typeof setTimeout>>()
  const hoverRefreshTimer = useRef<ReturnType<typeof setTimeout>>()
  const hideTimer = useRef<ReturnType<typeof setTimeout>>()
  const hoverSequence = useRef(0)
  const navigationSequence = useRef(0)
  const hoverAbort = useRef<AbortController>()
  const navigationAbort = useRef<AbortController>()
  const locationRef = useRef<DOMCodeLocation>()
  const hoverPosition = useRef<DOMCodeLocation>()
  const hoverEligible = useRef(false)
  const mounted = useRef(true)
  const gPressedAt = useRef(0)
  const requestedHead = headRevision ?? headSha
  const requestedBase = baseRevision ?? baseSha
  const active = (enabled ?? nativeAvailable) && !!root && (completeRevision.test(requestedHead) || requestedHead === 'working' || requestedHead === 'index')
  const context = `${root}\u0000${host ?? ''}\u0000${owner ?? ''}\u0000${repo ?? ''}\u0000${pullRequest ?? ''}\u0000${headSha}\u0000${baseSha ?? ''}\u0000${requestedHead}\u0000${requestedBase ?? ''}\u0000${revisionKey ?? ''}`
  const currentContext = useRef(context)
  currentContext.current = context
  const reader = readerHistory[readerHistory.length - 1]
  const documentLines = useMemo(() => reader?.document.text.split(/\r?\n/) ?? [], [reader?.document])
  const documentHighlight = highlighted?.document === reader?.document && highlighted?.theme === codeTheme ? highlighted.code : undefined
  const availableServers = serverStatuses?.filter(server => server.available).length ?? 0

  const clearHover = () => {
    clearTimeout(hoverTimer.current)
    clearTimeout(hoverRefreshTimer.current)
    clearTimeout(hideTimer.current)
    hideTimer.current = undefined
    hoverSequence.current++
    hoverAbort.current?.abort()
    hoverAbort.current = undefined
    hoverPosition.current = undefined
    hoverEligible.current = false
    setHover(undefined)
  }

  useEffect(() => {
    mounted.current = true
    const detect = () => setNativeAvailable(hasNativeBackend())
    window.addEventListener('wails:runtime-config-ready', detect)
    return () => {
      mounted.current = false
      hoverSequence.current++
      navigationSequence.current++
      hoverAbort.current?.abort()
      navigationAbort.current?.abort()
      clearTimeout(hoverTimer.current)
      clearTimeout(hoverRefreshTimer.current)
      clearTimeout(hideTimer.current)
      window.removeEventListener('wails:runtime-config-ready', detect)
    }
  }, [])

  useEffect(() => {
    clearHover()
    navigationSequence.current++
    navigationAbort.current?.abort()
    setReaderHistory([])
    setTargets([])
    setLocation(undefined)
    locationRef.current = undefined
    setError('')
    setNotice('')
    setBusy(false)
  }, [context, active])

  useEffect(() => {
    const element = container.current
    if (!element) return
    const path = () => element.querySelector<HTMLElement>('.code-intelligence-review [data-code-path]')?.dataset.codePath
    let previous = path()
    const observer = new MutationObserver(() => {
      const next = path()
      if (next === previous) return
      previous = next
      clearHover()
      navigationSequence.current++
      navigationAbort.current?.abort()
      setBusy(false)
      setTargets([])
      setLocation(undefined)
      locationRef.current = undefined
    })
    observer.observe(element, { subtree: true, childList: true, attributes: true, attributeFilter: ['data-code-path'] })
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    let generation = 0
    let cancelled = false
    const check = () => {
      clearHover()
      if (!active) {setServerStatuses(undefined);setAvailabilityError('');return}
      const requested = ++generation
      void services.lsp.servers(root).then(statuses => {
        if (!cancelled && requested === generation) {setServerStatuses(statuses);setAvailabilityError('')}
      }).catch(issue => {
        if (!cancelled && requested === generation) {
          setServerStatuses(undefined)
          setAvailabilityError(issue instanceof Error ? issue.message : 'Language server availability could not be checked.')
        }
      })
    }
    setServerStatuses(undefined)
    setAvailabilityError('')
    check()
    window.addEventListener(LSP_CONFIGURATION_CHANGED_EVENT, check)
    return () => {cancelled = true;window.removeEventListener(LSP_CONFIGURATION_CHANGED_EVENT, check)}
  }, [root, active])

  useEffect(() => {
    if (!reader) return
    requestAnimationFrame(() => {
      container.current?.querySelector<HTMLElement>('.code-definition-scroll')?.focus({ preventScroll: true })
      container.current?.querySelector<HTMLElement>(`[data-definition-line="${reader.document.range.start.line + 1}"]`)?.scrollIntoView({ block: 'center' })
    })
  }, [reader])

  useEffect(() => {
    if (targets.length) requestAnimationFrame(() => container.current?.querySelector<HTMLElement>('.code-intelligence-targets li button')?.focus())
  }, [targets])

  useEffect(() => {
    let cancelled = false
    if (reader) void highlightCodeLines(reader.document.path, documentLines, codeTheme).then(code => {
      if (!cancelled) setHighlighted({ document: reader.document, theme: codeTheme, code })
    })
    return () => { cancelled = true }
  }, [reader, documentLines, codeTheme])

  useEffect(() => {
    const selected = () => {
      if (!container.current) return
      const position = codeLocationFromSelection(container.current)
      if (position) {setLocation(position);locationRef.current = position}
    }
    document.addEventListener('selectionchange', selected)
    return () => document.removeEventListener('selectionchange', selected)
  }, [])

  const requestFor = (position: DOMCodeLocation): CodePositionRequest | undefined => {
    const revision = position.revision || (position.side === 'old' ? requestedBase : requestedHead)
    if (!active || !revision || (!completeRevision.test(revision) && revision !== 'working' && revision !== 'index')) return undefined
    const compare = position.side === 'old' && !position.revision && !!pullRequest && !headRevision && !baseRevision
    return { root, revision, path: position.path, host, owner, repo, pullRequest, line: position.line, character: position.character, lineText: position.lineText, ...(compare ? { comparisonHead: headSha } : {}) }
  }

  const openTarget = async (target: LSPDefinitionTarget) => {
    if (!active) return
    const sequence = ++navigationSequence.current
    navigationAbort.current?.abort()
    const controller = new AbortController()
    navigationAbort.current = controller
    const expectedContext = context
    setBusy(true)
    setError('')
    try {
      const document = await services.lsp.readDefinition({ root, revision: target.revision, target: target.uri }, controller.signal)
      if (!mounted.current || sequence !== navigationSequence.current || currentContext.current !== expectedContext) return
      if (document.revision !== target.revision) throw new Error('The definition response belongs to another revision. Try again.')
      setReaderHistory(previous => [...previous, { target, document }])
      setTargets([])
      clearHover()
      setLocation(undefined)
      locationRef.current = undefined
    } catch (issue) {
      if (!controller.signal.aborted && mounted.current && sequence === navigationSequence.current && currentContext.current === expectedContext) setError(issue instanceof Error ? issue.message : 'The definition file could not be opened.')
    } finally {
      if (mounted.current && sequence === navigationSequence.current && currentContext.current === expectedContext) setBusy(false)
    }
  }

  const goToDefinition = async (position?: DOMCodeLocation) => {
    if (!position || busy) return
    const language = languageFor(position)
    if (!language) {setNotice('Language intelligence supports PHP, TypeScript / JavaScript, and Vue files.');return}
    const server = serverStatuses?.find(item => item.language === language)
    if (server && !server.available) {setError(server.error || `${server.name} needs to be configured before code navigation is available.`);return}
    const request = requestFor(position)
    if (!request) {
      setError(position.side === 'old' ? 'The base revision is unavailable. Refresh this pull request before navigating its previous code.' : 'Code navigation is available for committed revisions in the desktop app.')
      return
    }
    const sequence = ++navigationSequence.current
    navigationAbort.current?.abort()
    const controller = new AbortController()
    navigationAbort.current = controller
    const expectedContext = context
    clearHover()
    setBusy(true)
    setError('')
    setNotice('')
    setTargets([])
    try {
      const found = await services.lsp.definition(request, controller.signal)
      if (!mounted.current || sequence !== navigationSequence.current || currentContext.current !== expectedContext) return
      const resolvedSnapshot = !!request.comparisonHead || request.revision === 'working' || request.revision === 'index'
      if (found.some(target => !completeRevision.test(target.revision) || (!resolvedSnapshot && target.revision !== request.revision))) throw new Error('The definition response belongs to another revision. Try again.')
      if (!found.length) setNotice('The language server found no definition at this position.')
      else if (found.length === 1) {
        await openTarget(found[0])
      } else setTargets(found)
    } catch (issue) {
      if (!controller.signal.aborted && mounted.current && sequence === navigationSequence.current && currentContext.current === expectedContext) setError(issue instanceof Error ? issue.message : 'Go to definition failed.')
    } finally {
      if (mounted.current && sequence === navigationSequence.current && currentContext.current === expectedContext) setBusy(false)
    }
  }

  const backToReview = () => {
    navigationSequence.current++
    navigationAbort.current?.abort()
    clearHover()
    setReaderHistory([])
    setTargets([])
    setError('')
    setNotice('')
    setBusy(false)
    setLocation(undefined)
    locationRef.current = undefined
    requestAnimationFrame(() => container.current?.querySelector<HTMLElement>('.code-intelligence-review .review-diff-scroll, .code-intelligence-review [tabindex="0"]')?.focus({ preventScroll: true }))
  }

  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      if (!active || !container.current || event.defaultPrevented || event.isComposing || event.altKey || event.metaKey || event.ctrlKey) return
      const target = event.target as HTMLElement | null
      if (target?.matches('input,textarea,select') || target?.isContentEditable) return
      if (reader && (event.key === '[' || event.key === ']')) {event.stopPropagation();return}
      if (event.key === 'Escape' && (hover || targets.length)) {
        event.preventDefault()
        event.stopPropagation()
        clearHover()
        setTargets([])
        return
      }
      const position = codeLocationFromSelection(container.current) ?? locationRef.current
      if (!position) return
      if (event.key === 'F12' || (event.key.toLowerCase() === 'd' && Date.now() - gPressedAt.current < 800)) {
        event.preventDefault()
        event.stopPropagation()
        gPressedAt.current = 0
        void goToDefinition(position)
      } else gPressedAt.current = event.key.toLowerCase() === 'g' ? Date.now() : 0
    }
    container.current?.addEventListener('keydown', keydown)
    // Selection can remain in code while the native webview has focus on the body.
    const globalKeydown = (event: KeyboardEvent) => {
      if (container.current?.contains(event.target as Node)) return
      if ((event.target as Element | null)?.closest('[role="dialog"],[aria-modal="true"]')) return
      if (container.current && (hover || targets.length || codeLocationFromSelection(container.current))) keydown(event)
    }
    window.addEventListener('keydown', globalKeydown, true)
    return () => {
      container.current?.removeEventListener('keydown', keydown)
      window.removeEventListener('keydown', globalKeydown, true)
    }
  }, [active, busy, hover, targets, context, serverStatuses, reader])

  const pointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    if (!active || !container.current || busy) return
    if (hoverPanel.current?.contains(event.target as Node)) {
      clearTimeout(hideTimer.current)
      hideTimer.current = undefined
      hoverEligible.current = true
      return
    }
    const position = codeLocationAtPoint(container.current, event.target, event.clientX, event.clientY)
    if (!position) {
      hoverEligible.current = false
      clearTimeout(hoverTimer.current)
      if (!hideTimer.current) hideTimer.current = setTimeout(() => {hideTimer.current = undefined;clearHover()}, 180)
      return
    }
    clearTimeout(hideTimer.current)
    hideTimer.current = undefined
    hoverEligible.current = true
    const previous = hoverPosition.current
    locationRef.current = position
    if (previous?.path === position.path && previous.line === position.line && previous.side === position.side && previous.character === position.character) return
    hoverPosition.current = position
    setLocation(position)
    clearTimeout(hoverTimer.current)
    clearTimeout(hoverRefreshTimer.current)
    if (!serverStatuses?.some(server => server.language === languageFor(position) && server.available)) {clearHover();return}
    const sequence = ++hoverSequence.current
    hoverAbort.current?.abort()
    const expectedContext = context
    const x = Math.max(10, Math.min(event.clientX + 12, window.innerWidth - Math.min(470, window.innerWidth - 20) - 10))
    const y = Math.max(10, Math.min(event.clientY + 22, window.innerHeight - 320))
    setHover(undefined)
    hoverTimer.current = setTimeout(async () => {
      const request = requestFor(position)
      if (!request) return
      const controller = new AbortController()
      hoverAbort.current = controller
      setHover({ location: position, x, y, loading: true })
      try {
        const result = await services.lsp.hover(request, controller.signal)
        if (!mounted.current || sequence !== hoverSequence.current || currentContext.current !== expectedContext) return
        const resolvedSnapshot = !!request.comparisonHead || request.revision === 'working' || request.revision === 'index'
        if (!completeRevision.test(result.revision) || (!resolvedSnapshot && result.revision !== request.revision)) throw new Error('The hover response belongs to another revision.')
        setHover(result.contents ? { location: position, x, y, loading: false, result } : undefined)
        // Phpactor may return a short authentic result while its initial index warms.
        // Keep it visible and refresh once while the same symbol remains active.
        if (result.language === 'php') hoverRefreshTimer.current = setTimeout(async () => {
          if (!mounted.current || !hoverEligible.current || sequence !== hoverSequence.current || currentContext.current !== expectedContext) return
          const refreshController = new AbortController()
          hoverAbort.current = refreshController
          try {
            const refreshed = await services.lsp.hover(request, refreshController.signal)
            if (!mounted.current || !hoverEligible.current || sequence !== hoverSequence.current || currentContext.current !== expectedContext || refreshController.signal.aborted) return
            if (!completeRevision.test(refreshed.revision) || (!resolvedSnapshot && refreshed.revision !== request.revision)) return
            if (refreshed.contents) setHover({ location: position, x, y, loading: false, result: refreshed })
          } catch { /* Retain the last valid symbol result if the warmup refresh fails. */ }
        }, 1_000)
      } catch (issue) {
        if (!controller.signal.aborted && mounted.current && sequence === hoverSequence.current && currentContext.current === expectedContext) setHover({ location: position, x, y, loading: false, error: issue instanceof Error ? issue.message : 'Symbol information could not be loaded.' })
      }
    }, 450)
  }

  return <div className="code-intelligence" ref={container} onPointerMove={pointerMove} onPointerLeave={() => {hoverEligible.current = false;clearTimeout(hoverTimer.current);hideTimer.current = setTimeout(() => {hideTimer.current = undefined;clearHover()}, 180)}} onClick={event => {
    if (!container.current) return
    const position = codeLocationAtPoint(container.current, event.target, event.clientX, event.clientY)
    if (position) {
      setLocation(position)
      locationRef.current = position
      if (active && (event.metaKey || event.ctrlKey)) {
        event.preventDefault()
        event.stopPropagation()
        void goToDefinition(position)
      }
    } else if (!(event.target as Element).closest('.code-intelligence-toolbar,.code-intelligence-hover,.code-intelligence-targets')) {
      setLocation(undefined)
      locationRef.current = undefined
    }
  }}>
    <div className="code-intelligence-toolbar">
      <span className="code-intelligence-hint" title={availabilityError || undefined}><Icon name="code" size={14} />{!active ? 'Language intelligence is available in the desktop app' : availabilityError ? 'Language server availability could not be checked' : !serverStatuses ? 'Checking language servers…' : availableServers ? 'Hover symbols · Ctrl/Cmd click or F12 for definition' : 'Language servers need setup'}{active && !availableServers && onOpenSettings && <button type="button" className="btn-ghost" onClick={onOpenSettings}>Set up</button>}</span>
      <button type="button" className="btn-ghost code-definition-button" disabled={!active || !location || busy} onClick={() => void goToDefinition(container.current ? codeLocationFromSelection(container.current) ?? locationRef.current : locationRef.current)}><Icon name="external" size={13} />{busy ? 'Loading definition…' : 'Go to definition'}</button>
    </div>
    {(error || notice) && <div className={`code-intelligence-message ${error ? 'is-error' : ''}`} role={error ? 'alert' : 'status'}><span>{error || notice}</span>{error && onOpenSettings && <button className="btn-ghost" type="button" onClick={onOpenSettings}>Language server settings</button>}<button className="icon-button" type="button" aria-label="Dismiss code navigation message" onClick={() => {setError('');setNotice('')}}><Icon name="close" size={13} /></button></div>}
    <div className="code-intelligence-review" hidden={!!reader}>{children}</div>
    {reader && <section className="code-definition-reader" aria-label={`Read-only definition ${reader.document.path}`} style={{ ...codeReviewStyleVariables(preferences), '--definition-foreground': documentHighlight?.foreground ?? 'var(--text)', '--definition-background': documentHighlight?.background ?? 'var(--surface)' } as CSSProperties}>
      <div className="code-definition-header"><button className="btn" type="button" onClick={backToReview}><Icon name="chevron-left" size={14} />Back to review</button>{readerHistory.length > 1 && <button className="icon-button" type="button" aria-label="Previous definition file" onClick={() => {clearHover();navigationSequence.current++;navigationAbort.current?.abort();setBusy(false);setReaderHistory(history => history.slice(0, -1))}}><Icon name="history" size={15} /></button>}<strong title={reader.document.path}>{reader.document.path}</strong><span className="badge">Read only</span>{reader.document.external && <span className="badge">Dependency file</span>}<code className="muted">{reader.document.revision.slice(0, 8)}</code><button className="icon-button" type="button" aria-label="Close definition file" onClick={backToReview}><Icon name="close" size={15} /></button></div>
      <div className="code-definition-scroll" tabIndex={0} aria-label="Definition source code">
        {documentLines.map((line, index) => <div className={`code-definition-row ${index === reader.document.range.start.line ? 'is-target' : ''}`} data-definition-line={index + 1} key={index}><span className="code-definition-line-number" aria-hidden="true">{index + 1}</span><code data-code-path={reader.document.uri || reader.target.uri} data-code-line={index + 1} data-code-side="new" data-code-revision={reader.document.revision} data-code-language={reader.document.language}>{documentHighlight?.lines[index]?.map((token, tokenIndex) => <span key={tokenIndex} style={syntaxTokenStyle(token)}>{token.content}</span>) ?? line}</code></div>)}
      </div>
    </section>}
    {!!targets.length && <div className="code-intelligence-targets" role="dialog" aria-label="Choose a definition"><div><strong>Definitions</strong><button className="icon-button" type="button" aria-label="Close definition choices" onClick={() => setTargets([])}><Icon name="close" size={14} /></button></div><ul>{targets.map(target => <li key={target.id || `${target.uri}:${target.range.start.line}:${target.range.start.character}`}><button type="button" disabled={busy} onClick={() => void openTarget(target)}><Icon name="file" size={16} /><span><strong>{target.path}:{target.range.start.line + 1}</strong><span>{target.external ? 'Dependency file' : 'Repository file'} · {target.language}</span></span><Icon name="chevron-right" size={14} /></button></li>)}</ul></div>}
    {hover && <div ref={hoverPanel} className="code-intelligence-hover" role="dialog" aria-label="Symbol information" style={{ left: hover.x, top: hover.y }} onKeyDown={event => {if (event.key === 'Escape') {event.preventDefault();event.stopPropagation();clearHover()}}}><div className="code-hover-heading"><span>{hover.result?.language ?? 'Symbol information'}</span><button className="icon-button" type="button" aria-label="Close symbol information" onClick={clearHover}><Icon name="close" size={13} /></button></div>{hover.loading ? <div className="code-hover-loading" role="status"><span className="sr-only">Loading symbol information…</span><span className="skeleton" aria-hidden="true" /><span className="skeleton" aria-hidden="true" /></div> : hover.error ? <p className="code-hover-error" role="alert">{hover.error}{onOpenSettings && <button className="btn-ghost" type="button" onClick={onOpenSettings}>Language server settings</button>}</p> : hover.result && (hover.result.kind === 'plaintext' ? <div className="code-hover-plaintext">{hover.result.contents}</div> : <Markdown>{hover.result.contents}</Markdown>)}<button className="btn-ghost code-hover-definition" type="button" disabled={hover.loading || busy || !!hover.error} onClick={() => void goToDefinition(hover.location)}>Go to definition<kbd>F12</kbd></button></div>}
  </div>
}
