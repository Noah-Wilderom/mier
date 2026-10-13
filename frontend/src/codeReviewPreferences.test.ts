// @ts-nocheck -- These pure preference and storage tests run in Node.
import test from 'node:test'
import assert from 'node:assert/strict'
import { DEFAULT_CODE_REVIEW_PREFERENCES, codeReviewStyleVariables, effectiveCodeTheme, getCodeReviewPreferences, normalizeCodeReviewPreferences, subscribeCodeReviewPreferences, updateCodeReviewPreferences } from './codeReviewPreferences.ts'

test('code font and theme preferences normalize untrusted stored values and preserve separate appearances', () => {
  assert.deepEqual(normalizeCodeReviewPreferences(null), DEFAULT_CODE_REVIEW_PREFERENCES)
  assert.equal(normalizeCodeReviewPreferences({ fontSize: 8 }).fontSize, 12)
  assert.equal(normalizeCodeReviewPreferences({ fontSize: 99 }).fontSize, 20)
  assert.equal(normalizeCodeReviewPreferences({ fontSize: 14.6 }).fontSize, 15)
  assert.equal(normalizeCodeReviewPreferences({ fontSize: Infinity }).fontSize, 13)
  assert.deepEqual(normalizeCodeReviewPreferences({ lightTheme: 'nord', darkTheme: 'one-light' }), DEFAULT_CODE_REVIEW_PREFERENCES)
  const preferences = normalizeCodeReviewPreferences({ fontSize: 18, lightTheme: 'catppuccin-latte', darkTheme: 'nord' })
  assert.equal(effectiveCodeTheme('light', preferences), 'catppuccin-latte')
  assert.equal(effectiveCodeTheme('dark', preferences), 'nord')
  assert.deepEqual(codeReviewStyleVariables(preferences), { '--review-code-font-size': '18px', '--review-code-line-height': '32px' })
})

test('subscriber snapshots update only on changed settings and remain usable without browser storage', () => {
  const initial = { ...getCodeReviewPreferences() }
  let updates = 0
  const unsubscribe = subscribeCodeReviewPreferences(() => updates++)
  const next = updateCodeReviewPreferences({ fontSize: initial.fontSize === 20 ? 19 : 20 })
  assert.equal(updates, 1)
  assert.equal(getCodeReviewPreferences(), next)
  updateCodeReviewPreferences({ fontSize: next.fontSize })
  assert.equal(updates, 1)
  unsubscribe()
  updateCodeReviewPreferences(initial)
  assert.equal(updates, 1)
})

test('device storage survives module reload, synchronizes other windows, and recovers malformed JSON', async () => {
  const originalWindow = globalThis.window
  const values = new Map()
  const events = new Map()
  globalThis.window = {
    localStorage: { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value) },
    addEventListener: (name, callback) => events.set(name, callback),
    removeEventListener: name => events.delete(name),
  }
  try {
    const first = await import('./codeReviewPreferences.ts?storage-first')
    first.updateCodeReviewPreferences({ fontSize: 18, lightTheme: 'one-light', darkTheme: 'nord' })
    const reloaded = await import('./codeReviewPreferences.ts?storage-reloaded')
    assert.deepEqual(reloaded.getCodeReviewPreferences(), { fontSize: 18, lightTheme: 'one-light', darkTheme: 'nord' })
    let updates = 0
    const unsubscribe = reloaded.subscribeCodeReviewPreferences(() => updates++)
    values.set(first.CODE_REVIEW_PREFERENCES_KEY, JSON.stringify({ fontSize: 16, lightTheme: 'light-plus', darkTheme: 'one-dark-pro' }))
    events.get('storage')({ key: first.CODE_REVIEW_PREFERENCES_KEY })
    assert.equal(reloaded.getCodeReviewPreferences().fontSize, 16)
    assert.equal(updates, 1)
    values.set(first.CODE_REVIEW_PREFERENCES_KEY, '{invalid')
    events.get('storage')({ key: first.CODE_REVIEW_PREFERENCES_KEY })
    assert.deepEqual(reloaded.getCodeReviewPreferences(), DEFAULT_CODE_REVIEW_PREFERENCES)
    unsubscribe()
    assert.equal(events.has('storage'), false)
  } finally {
    if (originalWindow === undefined) delete globalThis.window
    else globalThis.window = originalWindow
  }
})
