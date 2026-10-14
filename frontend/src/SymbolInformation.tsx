import { Children, isValidElement, useEffect, useMemo, useState, type CSSProperties } from 'react'
import type { Components } from 'react-markdown'
import { Markdown } from './Markdown'
import type { CodeThemeId } from './codeReviewPreferences'
import { highlightCodeLines, syntaxTokenStyle, type HighlightedCode } from './syntaxHighlighter'

const extensions: Record<string, string> = {
  php: 'php', typescript: 'ts', ts: 'ts', javascript: 'js', js: 'js', typescriptreact: 'tsx', tsx: 'tsx', jsx: 'jsx', vue: 'vue', json: 'json', html: 'html', css: 'css', shell: 'sh', bash: 'sh', go: 'go', python: 'py', rust: 'rs', java: 'java',
}

function SignatureCode({ source, language, theme }: { source: string; language: string; theme: CodeThemeId }) {
  const lines = useMemo(() => source.split('\n'), [source])
  const [code, setCode] = useState<{ source: string; theme: CodeThemeId; value: HighlightedCode }>()
  const highlight = code?.source === source && code.theme === theme ? code.value : undefined
  useEffect(() => {
    let stale = false
    void highlightCodeLines(`symbol.${extensions[language.toLowerCase()] || 'txt'}`, lines, theme).then(value => {
      if (!stale) setCode({ source, theme, value })
    })
    return () => { stale = true }
  }, [source, lines, language, theme])
  return <pre className="code-hover-signature" tabIndex={0} aria-label="Symbol signature" style={{ '--signature-foreground': highlight?.foreground, '--signature-background': highlight?.background } as CSSProperties}><code>{lines.map((line, index) => <span className="code-hover-signature-line" key={index}>{highlight?.lines[index]?.map((token, part) => <span key={part} style={syntaxTokenStyle(token)}>{token.content}</span>) ?? line}{index < lines.length - 1 ? '\n' : ''}</span>)}</code></pre>
}

export function SymbolInformation({ contents, kind, language, theme }: { contents: string; kind: string; language: string; theme: CodeThemeId }) {
  const codeBlock = useMemo<Components['pre']>(() => function SymbolCodeBlock({ children }) {
    const element = Children.toArray(children).find(isValidElement)
    if (!element || !isValidElement<{ className?: string; children?: string }>(element)) return <pre>{children}</pre>
    const supplied = element.props.className?.match(/\blanguage-([^\s]+)/)?.[1]
    const source = String(element.props.children ?? '').replace(/\n$/, '')
    return <SignatureCode source={source} language={supplied || language} theme={theme} />
  }, [language, theme])
  return kind === 'plaintext'
    ? <div className="code-hover-plaintext">{contents}</div>
    : <Markdown className="code-hover-documentation" codeBlock={codeBlock}>{contents}</Markdown>
}
