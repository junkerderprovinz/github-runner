import { useEffect, useState } from 'react'
import { signIn } from './data/api'
import { useStore } from './data/store'
import { LineMap } from './LineMap'
import { Button, Logo, PasswordInput, Toasts } from './ui/kit'

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

function SignIn() {
  const [pw, setPw] = useState('')
  const [wrong, setWrong] = useState('')
  const [busy, setBusy] = useState(false)
  return (
    <main className="signin">
      <form
        className="signin-box"
        onSubmit={async (e) => {
          e.preventDefault()
          setBusy(true)
          try {
            await signIn(pw)
          } catch (err) {
            setWrong((err as Error).message)
          } finally {
            setBusy(false)
          }
        }}
      >
        <Logo size={56} />
        <h1>GitHub-Runner</h1>
        <label htmlFor="signin-pw">Password</label>
        <PasswordInput id="signin-pw" autoComplete="current-password" autoFocus value={pw} invalid={!!wrong} onChange={(e) => (setPw(e.target.value), setWrong(''))} />
        {wrong && <p className="field-error">{wrong}</p>}
        <Button kind="primary" type="submit" disabled={!pw} busy={busy}>
          Sign in
        </Button>
      </form>
    </main>
  )
}

function HomeOnly() {
  return (
    <main className="signin">
      <div className="signin-box">
        <Logo size={56} />
        <h1>GitHub-Runner</h1>
        <p>Without a password, GitHub-Runner only answers on addresses of your own network, so no website can reach it behind your back.</p>
        <p>Open it by its IP address, for example http://192.168.1.10:8080, and set a password under Settings, Security. After that it works under this address too.</p>
      </div>
    </main>
  )
}

export function App() {
  const { phase } = useStore()
  useEnvironment()
  return (
    <div className="world">
      {phase === 'locked' ? <SignIn /> : phase === 'homeOnly' ? <HomeOnly /> : phase === 'ready' ? <LineMap /> : null}
      <Toasts />
    </div>
  )
}
