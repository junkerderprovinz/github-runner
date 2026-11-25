import { useState } from 'react'
import { Info } from 'lucide-react'
import { requestReset, resetPassword, signIn, start } from './data/api'
import { Button, Logo, PasswordInput, TextInput, useShake } from './ui/kit'

const codeShape = /^[2-9A-HJ-NP-Z]{12}$/

function Password() {
  const [pw, setPw] = useState('')
  const [wrong, setWrong] = useState('')
  const [busy, setBusy] = useState(false)
  const [forgetting, setForgetting] = useState(false)
  const [reset, setReset] = useState<'form' | 'done' | null>(null)
  const [button, shake] = useShake<HTMLButtonElement>()

  if (reset === 'form') return <Reset onBack={() => setReset(null)} onDone={() => setReset('done')} />
  if (reset === 'done') return <ResetDone />

  return (
    <form
      className="signin-box"
      onSubmit={async (e) => {
        e.preventDefault()
        setBusy(true)
        try {
          await signIn(pw)
        } catch (err) {
          setWrong((err as Error).message)
          shake()
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
      <Button kind="primary" type="submit" disabled={!pw} busy={busy} ref={button}>
        Sign in
      </Button>
      <button
        type="button"
        className="signin-forgot"
        disabled={forgetting}
        onClick={async () => {
          setForgetting(true)
          try {
            await requestReset()
            setReset('form')
          } catch (err) {
            setWrong((err as Error).message)
          } finally {
            setForgetting(false)
          }
        }}
      >
        Forgot password?
      </button>
    </form>
  )
}

function Reset({ onBack, onDone }: { onBack: () => void; onDone: () => void }) {
  const [code, setCode] = useState('')
  const [pw, setPw] = useState('')
  const [again, setAgain] = useState('')
  const [why, setWhy] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [button, shake] = useShake<HTMLButtonElement>()
  const typed = code.toUpperCase().replace(/[\s-]/g, '')
  const badCode = code !== '' && typed.length >= 12 && !codeShape.test(typed) ? 'A code has 12 letters and digits, such as 7KQM-3XHT-9WPC.' : ''
  const mismatch = again && again !== pw ? 'The two passwords differ.' : ''

  return (
    <form
      className="signin-box"
      onSubmit={async (e) => {
        e.preventDefault()
        if (!codeShape.test(typed)) return setError('A code has 12 letters and digits, such as 7KQM-3XHT-9WPC.')
        setBusy(true)
        try {
          await resetPassword(typed, pw)
          onDone()
        } catch (err) {
          setError((err as Error).message)
          shake()
          requestReset().catch(() => {})
        } finally {
          setBusy(false)
        }
      }}
    >
      <Logo size={56} />
      <h1>GitHub-Runner</h1>
      <div className="signin-label">
        <label htmlFor="reset-code">Code from the container log</label>
        <button type="button" className="signin-info" aria-label="Where the code is" aria-expanded={why} onClick={() => setWhy(!why)}>
          <Info size={16} aria-hidden />
        </button>
      </div>
      {why && (
        <p className="field-hint">
          GitHub-Runner has written a code to its container log. On Unraid: Docker tab, the GitHub-Runner icon, Logs. Elsewhere: docker logs GitHub-Runner. A code is valid for 10 minutes. Anyone who can read this log can reset the password, including through a log viewer such as Dozzle. The reset also ends every other session.
        </p>
      )}
      <TextInput
        id="reset-code"
        className="signin-code"
        autoFocus
        autoComplete="one-time-code"
        placeholder="XXXX-XXXX-XXXX"
        value={code}
        invalid={!!(badCode || error)}
        onChange={(e) => (setCode(e.target.value), setError(''))}
      />
      {(badCode || error) && <p className="field-error">{badCode || error}</p>}
      <label htmlFor="reset-pw">New password</label>
      <PasswordInput id="reset-pw" value={pw} onChange={(e) => setPw(e.target.value)} />
      <label htmlFor="reset-again">Confirm password</label>
      <PasswordInput id="reset-again" value={again} invalid={!!mismatch} onChange={(e) => setAgain(e.target.value)} />
      {mismatch && <p className="field-error">{mismatch}</p>}
      <Button kind="primary" type="submit" disabled={!typed || !pw || again !== pw || !!badCode} busy={busy} ref={button}>
        Set password
      </Button>
      <Button kind="quiet" onClick={onBack}>
        Back
      </Button>
    </form>
  )
}

function ResetDone() {
  return (
    <div className="signin-box">
      <Logo size={56} />
      <h1>Your new password is set</h1>
      <p>The reset also removed every other way in:</p>
      <ul className="signin-list">
        <li>Every other session has ended.</li>
      </ul>
      <Button kind="primary" autoFocus onClick={() => start()}>
        Done
      </Button>
    </div>
  )
}

export function SignIn() {
  return (
    <main className="signin">
      <Password />
    </main>
  )
}
