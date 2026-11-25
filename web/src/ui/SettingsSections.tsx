import { useState } from 'react'
import { Check, ExternalLink, KeyRound, Monitor, Moon, Pencil, Sun, Trash2, TriangleAlert } from 'lucide-react'
import { checkToken, clearPassword, saveSettings, saveToken, setPassword } from '../data/api'
import { remember, set, toast, useStore, type Motion, type Settings, type Theme } from '../data/store'
import { Button, Field, PasswordInput, Segmented, Switch, TagInput, TextInput, useDebouncedEffect, useShake } from './kit'

export const SETTINGS_TABS = [
  { id: 'general', label: 'General' },
  { id: 'appearance', label: 'Appearance' },
  { id: 'runners', label: 'Runners' },
  { id: 'security', label: 'Security' },
  { id: 'info', label: 'Info' },
] as const

export type SettingsTab = (typeof SETTINGS_TABS)[number]['id']

export function settingsTab(raw: string | undefined): SettingsTab {
  return SETTINGS_TABS.some((t) => t.id === raw) ? (raw as SettingsTab) : 'general'
}

export function SettingsSection({ id }: { id: SettingsTab }) {
  switch (id) {
    case 'appearance':
      return <Appearance />
    case 'runners':
      return <Runners />
    case 'security':
      return <Security />
    case 'info':
      return <Info />
    default:
      return <General />
  }
}

const failed = (e: unknown) => toast((e as Error).message, 'fail')

async function save(patch: Partial<Settings>, done: string) {
  try {
    await saveSettings(patch)
    toast(done)
  } catch (e) {
    failed(e)
  }
}

function General() {
  const { settings, repos } = useStore()
  const [replacing, setReplacing] = useState(false)
  const [next, setNext] = useState('')
  const [bad, setBad] = useState('')
  const [saving, setSaving] = useState(false)
  const [owner, setOwner] = useState(settings.owner)
  const [test, setTest] = useState<'idle' | 'busy' | 'done'>('idle')
  const [reachable, setReachable] = useState(0)
  const [testBtn, shake] = useShake<HTMLButtonElement>()
  useDebouncedEffect(owner, (v) => save({ owner: v.trim() }, v.trim() ? `Default owner is now ${v.trim()}.` : 'Default owner removed.'))

  const cancel = () => (setReplacing(false), setNext(''), setBad(''))
  const store = async () => {
    setSaving(true)
    try {
      await saveToken(next.trim())
      cancel()
      setTest('idle')
      toast('Token saved. Repositories that refused the old one try again now.')
    } catch (e) {
      setBad((e as Error).message)
    } finally {
      setSaving(false)
    }
  }
  const check = async () => {
    setTest('busy')
    try {
      const results = await checkToken()
      const ok = results.filter((r) => r.ok).length
      setReachable(ok)
      setTest('done')
      if (ok < results.length) shake()
    } catch (e) {
      setTest('idle')
      failed(e)
    }
  }

  const verdict = test !== 'done' ? null : reachable === repos.length ? 'all' : 'some'

  return (
    <>
      <Field label="Access token" hint="Fine-grained with Administration: Read and write on each repository, or classic with the repo scope." error={bad || undefined} wide={replacing}>
        {(id) =>
          replacing ? (
            <div className="inline">
              <PasswordInput id={id} autoFocus placeholder="github_pat_" value={next} invalid={!!bad} onChange={(e) => (setNext(e.target.value), setBad(''))} />
              <Button kind="quiet" onClick={cancel}>
                Cancel
              </Button>
              <Button kind="primary" disabled={!next.trim()} busy={saving} onClick={store}>
                Save token
              </Button>
            </div>
          ) : (
            <div className="inline">
              <code className="token" id={id}>
                {settings.token.set ? `${'•'.repeat(8)}${settings.token.last4 ?? ''}` : 'Not set'}
              </code>
              <Button icon={<Pencil size={15} />} onClick={() => setReplacing(true)}>
                {settings.token.set ? 'Replace' : 'Add token'}
              </Button>
            </div>
          )
        }
      </Field>
      <Field label="Default owner" hint="Used when you add a repository by its name alone.">
        {(id) => <TextInput id={id} value={owner} placeholder="you" onChange={(e) => setOwner(e.target.value)} />}
      </Field>
      <Field label="Token check" hint="Asks GitHub whether the token may manage runners in every repository.">
        {() => (
          <Button
            ref={testBtn}
            className={verdict ? `verdict-${verdict}` : undefined}
            icon={verdict === 'all' ? <Check size={15} /> : verdict === 'some' ? <TriangleAlert size={15} /> : <KeyRound size={15} />}
            busy={test === 'busy'}
            disabled={!settings.token.set || !repos.length}
            onClick={check}
          >
            {verdict === 'all' ? 'Works everywhere' : verdict === 'some' ? `Works for ${reachable} of ${repos.length}` : 'Check token'}
          </Button>
        )}
      </Field>
    </>
  )
}

function Appearance() {
  const { theme, motion } = useStore()
  return (
    <>
      <Field label="Theme" hint="System follows your device, day and night.">
        {() => (
          <Segmented<Theme>
            label="Theme"
            value={theme}
            onChange={(v) => (set({ theme: v }), remember('theme', v))}
            options={[
              { id: 'system', label: 'System', icon: <Monitor size={15} /> },
              { id: 'light', label: 'Light', icon: <Sun size={15} /> },
              { id: 'dark', label: 'Dark', icon: <Moon size={15} /> },
            ]}
          />
        )}
      </Field>
      <Field label="Motion" hint="Reduced motion in your system settings always wins.">
        {() => (
          <Segmented<Motion>
            label="Motion"
            value={motion}
            onChange={(v) => (set({ motion: v }), remember('motion', v))}
            options={[
              { id: 'system', label: 'Follow system' },
              { id: 'off', label: 'Off' },
            ]}
          />
        )}
      </Field>
    </>
  )
}

function Runners() {
  const { settings } = useStore()
  const [name, setName] = useState(settings.name)
  const [retry, setRetry] = useState(String(settings.retrySeconds))
  useDebouncedEffect(name, (v) => {
    if (v.trim()) save({ name: v.trim() }, 'Runner name saved. The runners register again under it.')
  })
  useDebouncedEffect(retry, (v) => {
    const n = Number(v)
    if (Number.isInteger(n) && n >= 10) save({ retrySeconds: n }, `A refused repository is tried again every ${n} seconds.`)
  })
  const retryBad = !/^\d+$/.test(retry) || Number(retry) < 10
  const flip = (key: 'noDefaultLabels' | 'runAsRoot' | 'deregisterOnStop', words: [string, string]) => (on: boolean) => save({ [key]: on }, on ? words[0] : words[1])
  return (
    <>
      <Field label="Runner name" hint="How the runner shows up under Settings, Actions, Runners on GitHub." error={name.trim() ? undefined : 'The runners need a name.'}>
        {(id) => <TextInput id={id} value={name} invalid={!name.trim()} onChange={(e) => setName(e.target.value)} />}
      </Field>
      <Field label="Labels" hint="Extra labels a workflow can ask for with runs-on. Press Enter after each one." wide>
        {(id) => <TagInput id={id} value={settings.labels} placeholder="Add a label" onChange={(labels) => save({ labels }, 'Labels saved. The runners register again with them.')} />}
      </Field>
      <Field label="Leave out the default labels" hint="Drops self-hosted, Linux and X64, which GitHub adds on its own.">
        {(id) => (
          <Switch
            id={id}
            label="Leave out the default labels"
            checked={settings.noDefaultLabels}
            onChange={flip('noDefaultLabels', ['Default labels left out. The runners register again.', 'Default labels are back. The runners register again.'])}
          />
        )}
      </Field>
      <Field label="Run jobs as root" hint="Off runs them as the unprivileged runner user.">
        {(id) => <Switch id={id} label="Run jobs as root" checked={settings.runAsRoot} onChange={flip('runAsRoot', ['Jobs run as root.', 'Jobs run as the runner user.'])} />}
      </Field>
      <Field label="Remove runners when the container stops" hint="Keeps your repositories free of offline leftovers. Starting the container registers them again.">
        {(id) => (
          <Switch
            id={id}
            label="Remove runners when the container stops"
            checked={settings.deregisterOnStop}
            onChange={flip('deregisterOnStop', ['Runners are removed on stop.', 'Runners stay registered on stop.'])}
          />
        )}
      </Field>
      <Field label="Try a refused repository again after" hint="The others keep running meanwhile." error={retryBad ? 'At least 10 seconds.' : undefined}>
        {(id) => (
          <div className="inline">
            <TextInput id={id} className="num" inputMode="numeric" value={retry} invalid={retryBad} onChange={(e) => setRetry(e.target.value)} />
            <span className="unit">seconds</span>
          </div>
        )}
      </Field>
    </>
  )
}

function Security() {
  const { settings } = useStore()
  const [editing, setEditing] = useState(false)
  const [pw, setPw] = useState('')
  const [again, setAgain] = useState('')
  const [saving, setSaving] = useState(false)
  const done = () => (setEditing(false), setPw(''), setAgain(''))
  const mismatch = again && again !== pw ? 'The two passwords differ.' : undefined
  const store = async () => {
    setSaving(true)
    try {
      await setPassword(pw)
      toast(settings.password ? 'Password changed.' : 'Password set. This page now asks for it.')
      done()
    } catch (e) {
      failed(e)
    } finally {
      setSaving(false)
    }
  }
  const remove = async () => {
    try {
      await clearPassword()
      toast('Password removed. The page opens without one.')
    } catch (e) {
      failed(e)
    }
  }
  return (
    <>
      <Field
        label="Password"
        hint={settings.password ? 'This page asks for it before it shows anything.' : 'Nothing is locked until you set a password. Anyone who can reach this page can use it.'}
        wide={editing}
      >
        {(id) =>
          editing ? (
            <div className="stack-sm">
              <PasswordInput id={id} autoFocus placeholder="New password" value={pw} onChange={(e) => setPw(e.target.value)} />
              {pw && <PasswordInput aria-label="The same password again" placeholder="The same again" value={again} invalid={!!mismatch} onChange={(e) => setAgain(e.target.value)} />}
              {mismatch ? <p className="field-error">{mismatch}</p> : pw && pw.length < 12 && <p className="field-note">That works. A longer one is harder to guess.</p>}
              <div className="inline">
                <Button kind="quiet" onClick={done}>
                  Cancel
                </Button>
                <Button kind="primary" disabled={!pw || pw !== again} busy={saving} onClick={store}>
                  {settings.password ? 'Save password' : 'Set password'}
                </Button>
              </div>
            </div>
          ) : settings.password ? (
            <div className="inline">
              <span className="state-word" id={id}>
                Set
              </span>
              <Button icon={<Pencil size={15} />} onClick={() => setEditing(true)}>
                Change
              </Button>
              <Button icon={<Trash2 size={15} />} onClick={remove}>
                Remove
              </Button>
            </div>
          ) : (
            <div className="inline">
              <span className="state-word" id={id}>
                Not set
              </span>
              <Button icon={<KeyRound size={15} />} onClick={() => setEditing(true)}>
                Set a password
              </Button>
            </div>
          )
        }
      </Field>
    </>
  )
}

function Info() {
  const { version, runnerVersion } = useStore()
  return (
    <>
      <dl className="facts">
        <div>
          <dt>GitHub-Runner</dt>
          <dd>{version}</dd>
        </div>
        {runnerVersion && (
          <div>
            <dt>Actions runner</dt>
            <dd>{runnerVersion}</dd>
          </div>
        )}
      </dl>
      <div className="prose">
        <p>Problems, wishes or suggestions? You're welcome to open an issue on GitHub.</p>
        <p>
          <a className="link" href="https://github.com/junkerderprovinz/github-runner/issues" target="_blank" rel="noreferrer">
            Open an issue <ExternalLink size={14} aria-hidden />
          </a>
        </p>
        <p>
          A one-knight crusade: good-looking open-source tools that did not exist in the shape I wanted, built until nothing is missing. It is free, with no accounts, no telemetry, no ads and no
          paid tier. Forged on evenings and weekends, with heart and stubbornness.
        </p>
        <p>
          <a className="link" href="https://buymeacoffee.com/junkerderprovinz" target="_blank" rel="noreferrer">
            Toss a coin to your knight <ExternalLink size={14} aria-hidden />
          </a>
        </p>
      </div>
    </>
  )
}
