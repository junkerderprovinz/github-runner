import { useEffect, useState } from 'react'
import { useStore } from './data/store'
import { LineMap } from './LineMap'
import { SignIn } from './SignIn'
import { Toasts } from './ui/kit'

function useMedia(query: string) {
  const [match, setMatch] = useState(() => matchMedia(query).matches)
  useEffect(() => {
    const m = matchMedia(query)
    const on = () => setMatch(m.matches)
    m.addEventListener('change', on)
    return () => m.removeEventListener('change', on)
  }, [query])
  return match
}

function useEnvironment() {
  const { theme, motion } = useStore()
  const dark = useMedia('(prefers-color-scheme: dark)')
  const reduce = useMedia('(prefers-reduced-motion: reduce)')
  useEffect(() => {
    const root = document.documentElement
    root.dataset.theme = theme === 'system' ? (dark ? 'dark' : 'light') : theme
    root.dataset.motion = reduce || motion === 'off' ? 'reduce' : 'full'
  }, [theme, motion, dark, reduce])
}

export function App() {
  const { phase } = useStore()
  useEnvironment()
  return (
    <div className="world">
      {phase === 'locked' ? <SignIn /> : phase === 'ready' ? <LineMap /> : null}
      <Toasts />
    </div>
  )
}
