import { useState } from 'react'
import { Plus } from 'lucide-react'
import { addRepo } from '../data/api'
import { toast, useStore } from '../data/store'
import { Button, TextInput, useShake } from './kit'

// The server reads the entry the same way. This only decides when the button
// can be pressed and what to say before asking it.
function parse(raw: string, owner: string) {
  const entry = raw
    .trim()
    .replace(/^https?:\/\/(www\.)?github\.com\//, '')
    .replace(/\/$/, '')
    .replace(/\.git$/, '')
  if (!entry) return { error: '' }
  const parts = entry.split('/')
  if (parts.length > 2 || parts.some((p) => !/^[A-Za-z0-9._-]+$/.test(p))) return { error: 'Write it as owner/name, such as you/website.' }
  if (parts.length === 1) return owner ? { id: `${owner}/${parts[0]}` } : { error: 'Write it as owner/name, or set a default owner in Settings.' }
  return { id: entry }
}

export function AddForm({ onDone, autoFocus = true, submitLabel = 'Add', className, onCancel }: { onDone?: (id: string) => void; onCancel?: () => void; autoFocus?: boolean; submitLabel?: string; className?: string }) {
  const { settings, repos } = useStore()
  const [value, setValue] = useState('')
  const [refused, setRefused] = useState('')
  const [checking, setChecking] = useState(false)
  const [btn, shake] = useShake<HTMLButtonElement>()
  const parsed = parse(value, settings.owner)
  const taken = parsed.id && repos.some((r) => r.id.toLowerCase() === parsed.id!.toLowerCase())
  const error = parsed.error || (taken ? `${parsed.id} already has a runner.` : '') || refused

  const submit = async () => {
    if (!parsed.id || taken || checking) return
    setChecking(true)
    try {
      const id = await addRepo(value)
      toast(`Runner for ${id} is starting.`)
      setValue('')
      onDone?.(id)
    } catch (e) {
      setRefused((e as Error).message)
      shake()
    } finally {
      setChecking(false)
    }
  }

  return (
    <form
      className={`add-form ${className ?? ''}`}
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      <div className="add-row">
        <TextInput
          aria-label="Repository, as owner/name"
          autoFocus={autoFocus}
          placeholder={settings.owner ? `${settings.owner}/website` : 'owner/name'}
          value={value}
          invalid={!!error}
          onChange={(e) => {
            setValue(e.target.value)
            setRefused('')
          }}
        />
        <Button ref={btn} kind="primary" type="submit" icon={<Plus size={16} />} busy={checking} disabled={!parsed.id || !!taken}>
          {checking ? 'Checking token' : submitLabel}
        </Button>
        {onCancel && (
          <Button kind="quiet" onClick={onCancel}>
            Cancel
          </Button>
        )}
      </div>
      {error ? (
        <p className="add-error" role="alert">
          {error}
        </p>
      ) : (
        <p className="add-hint">{settings.owner ? `As owner/name. For ${settings.owner}, the name alone is enough.` : 'As owner/name.'}</p>
      )}
    </form>
  )
}
