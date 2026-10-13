import { useEffect, useState, useSyncExternalStore } from 'react'
import { CodeAppearance, CodeThemeId, DARK_CODE_THEMES, DEFAULT_CODE_REVIEW_PREFERENCES, LIGHT_CODE_THEMES, codeReviewStyleVariables, effectiveCodeTheme, getCodeAppearance, getCodeReviewPreferences, subscribeCodeAppearance, subscribeCodeReviewPreferences, updateCodeReviewPreferences } from './codeReviewPreferences'
import { highlightCodeLines, syntaxTokenStyle, type HighlightedCode } from './syntaxHighlighter'
import './CodeReviewSettings.css'

const PREVIEW_LINES = ['/** @return list<string> */', 'public function pages(): array', '{', "    return ['home', 'about'];", '}']

function CodePreview({ theme, fontSize }: { theme: CodeThemeId; fontSize: number }) {
  const [highlighted, setHighlighted] = useState<HighlightedCode | null>(null)
  useEffect(() => {
    let canceled = false
    void highlightCodeLines('preview.php', PREVIEW_LINES, theme).then(result => { if (!canceled) setHighlighted(result) })
    return () => { canceled = true }
  }, [theme])
  return <pre className="code-theme-preview" aria-hidden="true" style={{ color: highlighted?.foreground, background: highlighted?.background, fontSize: `${fontSize}px` }}><code>{PREVIEW_LINES.map((line, index) => <span className="code-theme-preview-line" key={index}>{highlighted?.lines[index]?.map((token, part) => <span style={syntaxTokenStyle(token)} key={part}>{token.content}</span>) ?? line}{'\n'}</span>)}</code></pre>
}

export function CodeReviewSettings() {
  const preferences = useSyncExternalStore(subscribeCodeReviewPreferences, getCodeReviewPreferences, () => DEFAULT_CODE_REVIEW_PREFERENCES)
  const appearance = useSyncExternalStore(subscribeCodeAppearance, getCodeAppearance, () => 'light' as CodeAppearance)
  return <div className="code-review-settings" style={codeReviewStyleVariables(preferences)}>
    <h2>Code review</h2>
    <p className="code-review-settings-intro">Choose a comfortable code size and colors for each appearance.</p>
    <div className="code-font-settings"><div><label htmlFor="code-review-font-size">Font size</label><output htmlFor="code-review-font-size">{preferences.fontSize} px</output></div><input id="code-review-font-size" type="range" min={12} max={20} step={1} value={preferences.fontSize} onChange={event => updateCodeReviewPreferences({ fontSize: Number(event.target.value) })} /><div className="code-font-limits"><span>12 px</span><span>20 px</span></div></div>
    <CodePreview theme={effectiveCodeTheme(appearance, preferences)} fontSize={preferences.fontSize} />
    {(['light', 'dark'] as const).map(mode => <fieldset className="code-theme-group" key={mode}><legend>{mode === 'light' ? 'Light themes' : 'Dark themes'}</legend><div className="code-theme-options">{(mode === 'light' ? LIGHT_CODE_THEMES : DARK_CODE_THEMES).map(theme => {
      const selected = (mode === 'light' ? preferences.lightTheme : preferences.darkTheme) === theme.id
      return <label className={`code-theme-card${selected ? ' is-selected' : ''}`} key={theme.id}><input type="radio" name={`code-${mode}-theme`} className="sr-only" value={theme.id} checked={selected} onChange={() => updateCodeReviewPreferences(mode === 'light' ? { lightTheme: theme.id as typeof preferences.lightTheme } : { darkTheme: theme.id as typeof preferences.darkTheme })} /><CodePreview theme={theme.id} fontSize={12} /><span className="code-theme-caption"><span>{theme.label}</span><span className="code-theme-choice" aria-hidden="true" /></span></label>
    })}</div></fieldset>)}
  </div>
}
