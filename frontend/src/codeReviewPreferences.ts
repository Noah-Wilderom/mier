export const LIGHT_CODE_THEMES = [
  { id: 'github-light', label: 'GitHub Light' },
  { id: 'light-plus', label: 'Light Plus' },
  { id: 'one-light', label: 'One Light' },
  { id: 'catppuccin-latte', label: 'Catppuccin Latte' },
] as const
export const DARK_CODE_THEMES = [
  { id: 'github-dark', label: 'GitHub Dark' },
  { id: 'dark-plus', label: 'Dark Plus' },
  { id: 'one-dark-pro', label: 'One Dark Pro' },
  { id: 'nord', label: 'Nord' },
] as const
export type LightCodeThemeId = typeof LIGHT_CODE_THEMES[number]['id']
export type DarkCodeThemeId = typeof DARK_CODE_THEMES[number]['id']
export type CodeThemeId = LightCodeThemeId | DarkCodeThemeId
export type CodeAppearance = 'light' | 'dark'
export interface CodeReviewPreferences {
  fontSize: number
  lightTheme: LightCodeThemeId
  darkTheme: DarkCodeThemeId
}
export const CODE_REVIEW_PREFERENCES_KEY = 'mier.code-review.preferences.v1'
export const DEFAULT_CODE_REVIEW_PREFERENCES: Readonly<CodeReviewPreferences> = Object.freeze({ fontSize: 13, lightTheme: 'github-light', darkTheme: 'github-dark' })

export function normalizeCodeReviewPreferences(value: unknown): CodeReviewPreferences {
  const input = value && typeof value === 'object' ? value as Partial<CodeReviewPreferences> : {}
  return {
    fontSize: typeof input.fontSize === 'number' && Number.isFinite(input.fontSize) ? Math.min(20, Math.max(12, Math.round(input.fontSize))) : DEFAULT_CODE_REVIEW_PREFERENCES.fontSize,
    lightTheme: LIGHT_CODE_THEMES.some(theme => theme.id === input.lightTheme) ? input.lightTheme! : DEFAULT_CODE_REVIEW_PREFERENCES.lightTheme,
    darkTheme: DARK_CODE_THEMES.some(theme => theme.id === input.darkTheme) ? input.darkTheme! : DEFAULT_CODE_REVIEW_PREFERENCES.darkTheme,
  }
}

function readStoredPreferences(): CodeReviewPreferences {
  try {
    return normalizeCodeReviewPreferences(typeof window === 'undefined' ? undefined : JSON.parse(window.localStorage.getItem(CODE_REVIEW_PREFERENCES_KEY) || 'null'))
  } catch { return { ...DEFAULT_CODE_REVIEW_PREFERENCES } }
}

let preferences = readStoredPreferences()
const listeners = new Set<() => void>()
export function getCodeReviewPreferences(): CodeReviewPreferences { return preferences }
export function effectiveCodeTheme(appearance: CodeAppearance, input = preferences): CodeThemeId { return appearance === 'dark' ? input.darkTheme : input.lightTheme }
export function codeReviewStyleVariables(input = preferences): Record<string, string> {
  return { '--review-code-font-size': `${input.fontSize}px`, '--review-code-line-height': `${Math.ceil(input.fontSize * 1.75)}px` }
}

function publish(next: CodeReviewPreferences) {
  if (next.fontSize === preferences.fontSize && next.lightTheme === preferences.lightTheme && next.darkTheme === preferences.darkTheme) return
  preferences = next
  for (const listener of listeners) listener()
}
export function updateCodeReviewPreferences(patch: Partial<CodeReviewPreferences>): CodeReviewPreferences {
  const next = normalizeCodeReviewPreferences({ ...preferences, ...patch })
  try { if (typeof window !== 'undefined') window.localStorage.setItem(CODE_REVIEW_PREFERENCES_KEY, JSON.stringify(next)) } catch { /* A restricted storage environment still supports settings for this session. */ }
  publish(next)
  return preferences
}
function storageChanged(event: StorageEvent) {
  if (event.key === CODE_REVIEW_PREFERENCES_KEY || event.key === null) publish(readStoredPreferences())
}
export function subscribeCodeReviewPreferences(listener: () => void): () => void {
  if (!listeners.size && typeof window !== 'undefined') window.addEventListener('storage', storageChanged)
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
    if (!listeners.size && typeof window !== 'undefined') window.removeEventListener('storage', storageChanged)
  }
}

export function getCodeAppearance(): CodeAppearance {
  if (typeof document !== 'undefined' && document.documentElement.dataset.theme) return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'
  return typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}
export function subscribeCodeAppearance(listener: () => void): () => void {
  if (typeof window === 'undefined') return () => {}
  const media = window.matchMedia('(prefers-color-scheme: dark)')
  media.addEventListener('change', listener)
  const observer = new MutationObserver(listener)
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
  return () => { media.removeEventListener('change', listener); observer.disconnect() }
}
