import { useEffect, useId, useRef, useState, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type Ref } from 'react'
import { flushSync } from 'react-dom'
import { Eye, EyeOff, LoaderCircle, X } from 'lucide-react'
import mark from '../mark.svg'
import { dismissToast, useStore } from '../data/store'

export function motionOn() {
  return document.documentElement.dataset.motion === 'full'
}

// Route and layout changes morph through the View Transitions API where it exists and motion is allowed.
export function morph(fn: () => void) {
  const doc = document as Document & { startViewTransition?: (cb: () => void) => unknown }
  if (!motionOn() || !doc.startViewTransition) return fn()
  doc.startViewTransition(() => flushSync(fn))
}

export function Logo({ size = 28 }: { size?: number }) {
  return <img src={mark} alt="" width={size} height={size} className="logo" />
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { kind?: 'primary' | 'plain' | 'quiet'; icon?: ReactNode; busy?: boolean; ref?: Ref<HTMLButtonElement> }

export function Button({ kind = 'plain', icon, busy, children, className, disabled, ...rest }: ButtonProps) {
  return (
    <button type="button" className={`btn btn-${kind} ${className ?? ''}`} disabled={disabled || busy} aria-busy={busy || undefined} {...rest}>
      {busy ? <LoaderCircle className="spin" size={16} aria-hidden /> : icon}
      {children && <span>{children}</span>}
    </button>
  )
}

export function Switch({ checked, onChange, label, id }: { checked: boolean; onChange: (on: boolean) => void; label: string; id?: string }) {
  return (
    <button type="button" id={id} role="switch" aria-checked={checked} aria-label={label} className="switch" onClick={() => onChange(!checked)}>
      <span className="switch-knob" />
    </button>
  )
}

export function Field({ label, hint, error, note, children, wide }: { label: string; hint?: string; error?: string; note?: string; children: (id: string) => ReactNode; wide?: boolean }) {
  const id = useId()
  return (
    <div className={wide ? 'field field-wide' : 'field'}>
      <div className="field-text">
        <label htmlFor={id}>{label}</label>
        {hint && <p className="field-hint">{hint}</p>}
      </div>
      <div className="field-control">
        {children(id)}
        {error ? (
          <p className="field-error" role="alert">
            {error}
          </p>
        ) : (
          note && <p className="field-note">{note}</p>
        )}
      </div>
    </div>
  )
}

export function TextInput({ invalid, mono, className, ...rest }: InputHTMLAttributes<HTMLInputElement> & { invalid?: boolean; mono?: boolean }) {
  return <input className={`input ${mono ? 'mono' : ''} ${className ?? ''}`} aria-invalid={invalid || undefined} spellCheck={false} autoComplete="off" {...rest} />
}

export function PasswordInput({ invalid, ...rest }: InputHTMLAttributes<HTMLInputElement> & { invalid?: boolean }) {
  const [shown, setShown] = useState(false)
  return (
    <span className="secret">
      <input className="input" type={shown ? 'text' : 'password'} aria-invalid={invalid || undefined} autoComplete="new-password" {...rest} />
      <button type="button" className="secret-eye" aria-label={shown ? 'Hide' : 'Show'} onClick={() => setShown(!shown)}>
        {shown ? <EyeOff size={16} /> : <Eye size={16} />}
      </button>
    </span>
  )
}

export function Segmented<T extends string>({ value, onChange, options, label }: { value: T; onChange: (v: T) => void; options: { id: T; label: string; icon?: ReactNode }[]; label: string }) {
  return (
    <div className="seg" role="radiogroup" aria-label={label}>
      {options.map((o) => (
        <button key={o.id} type="button" role="radio" aria-checked={o.id === value} className="seg-item" onClick={() => onChange(o.id)}>
          {o.icon}
          <span>{o.label}</span>
        </button>
      ))}
    </div>
  )
}

export function TagInput({ value, onChange, id, placeholder }: { value: string[]; onChange: (v: string[]) => void; id?: string; placeholder?: string }) {
  const [draft, setDraft] = useState('')
  const commit = () => {
    const tag = draft.trim().replace(/,$/, '')
    if (tag && !value.includes(tag)) onChange([...value, tag])
    setDraft('')
  }
  return (
    <div className="tags">
      {value.map((t) => (
        <span key={t} className="tag">
          {t}
          <button type="button" aria-label={`Remove label ${t}`} onClick={() => onChange(value.filter((x) => x !== t))}>
            <X size={12} />
          </button>
        </span>
      ))}
      <input
        id={id}
        className="tags-input"
        value={draft}
        placeholder={placeholder}
        onChange={(e) => (e.target.value.endsWith(',') ? (setDraft(e.target.value), setTimeout(commit)) : setDraft(e.target.value))}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            commit()
          } else if (e.key === 'Backspace' && !draft && value.length) onChange(value.slice(0, -1))
        }}
      />
    </div>
  )
}

export function Toasts() {
  const { toasts } = useStore()
  return (
    <div className="toasts" role="status" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`toast toast-${t.tone}`}>
          <span>{t.text}</span>
          {t.undo && (
            <button
              type="button"
              className="toast-action"
              onClick={() => {
                t.undo!()
                dismissToast(t.id)
              }}
            >
              Undo
            </button>
          )}
          <button type="button" className="toast-close" aria-label="Dismiss" onClick={() => dismissToast(t.id)}>
            <X size={14} />
          </button>
        </div>
      ))}
    </div>
  )
}

// Replays a CSS refusal animation on an element by toggling its class across a frame.
export function useShake<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  const shake = () => {
    const el = ref.current
    if (!el) return
    el.classList.remove('refused')
    requestAnimationFrame(() => requestAnimationFrame(() => el.classList.add('refused')))
  }
  return [ref, shake] as const
}

export function useDebouncedEffect(value: string, save: (v: string) => void, delay = 800) {
  const first = useRef(true)
  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    const t = setTimeout(() => save(value), delay)
    return () => clearTimeout(t)
  }, [value])
}
