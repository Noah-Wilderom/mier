import { useCallback, useEffect, useRef, useState, type SetStateAction } from 'react'

// The owning view is keyed by repository. Drafts survive navigation and restart,
// and storage failures remain visible while the in-memory draft is usable.
export function usePersistentDraft<T>(key: string, read: (raw: string | null) => T) {
  const [initial] = useState(() => {
    try { return { value: read(localStorage.getItem(key)), error: '' } }
    catch { return { value: read(null), error: 'Saved drafts could not be read. Keep this window open while you work.' } }
  })
  const [value, updateValue] = useState(initial.value)
  const current = useRef(initial.value)
  const [error, setError] = useState(initial.error)
  const persist = useCallback((next: T) => {
    try { localStorage.setItem(key, JSON.stringify(next)); setError('') }
    catch { setError('This draft could not be saved on this device. Keep this window open until you finish.') }
  }, [key])
  const setValue = useCallback((next: SetStateAction<T>) => {
    const resolved = typeof next === 'function' ? (next as (previous: T) => T)(current.current) : next
    current.current = resolved
    // A successful write may finish after navigation unmounts its composer.
    // Persist directly so clearing that completed draft still reaches storage.
    persist(resolved)
    updateValue(resolved)
  }, [persist])
  useEffect(() => persist(current.current), [persist])
  return [value, setValue, error] as const
}
