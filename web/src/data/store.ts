import { useSyncExternalStore } from 'react'

export type RunnerState = 'starting' | 'idle' | 'busy' | 'offline' | 'error'
export type Result = 'passed' | 'failed' | 'cancelled'
export type Theme = 'system' | 'light' | 'dark'
export type Motion = 'system' | 'off'

export interface Job {
  id: number
  repo: string
  name: string
  workflow?: string
  branch?: string
  url?: string
  started: number
  ended?: number
  result?: Result
  expected?: number
}

export interface LogLine {
  id: number
  at: number
  text: string
  tone?: 'ok' | 'fail'
}

export interface Repo {
  id: string
  line: number
  state: RunnerState
  since: number
  added: number
  error?: string
  retryAt?: number
  backAt?: number
  job?: Job
  ownLabels: string[] | null
}

export interface Settings {
  owner: string
  name: string
  labels: string[]
  noDefaultLabels: boolean
  runAsRoot: boolean
  deregisterOnStop: boolean
  retrySeconds: number
  token: { set: boolean; last4?: string }
  password: boolean
}

export interface Toast {
  id: number
  text: string
  tone: 'ok' | 'fail'
  undo?: () => void
  until: number
}

export interface State {
  phase: 'loading' | 'locked' | 'ready'
  connected: boolean
  now: number
  repos: Repo[]
  jobs: Job[]
  logs: Record<string, LogLine[]>
  settings: Settings
  version: string
  runnerVersion: string
  theme: Theme
  motion: Motion
  toasts: Toast[]
  route: string
}

let seq = 1
export const nextId = () => seq++

function remembered<T extends string>(key: string, allowed: T[], fallback: T): T {
  try {
    const v = localStorage.getItem(key) as T | null
    return v && allowed.includes(v) ? v : fallback
  } catch {
    return fallback
  }
}

export function remember(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // A private window may refuse storage, the choice then lasts until reload.
  }
}

let state: State = {
  phase: 'loading',
  connected: true,
  now: Date.now(),
  repos: [],
  jobs: [],
  logs: {},
  settings: {
    owner: '',
    name: 'GitHub-Runner',
    labels: [],
    noDefaultLabels: false,
    runAsRoot: true,
    deregisterOnStop: true,
    retrySeconds: 300,
    token: { set: false },
    password: false,
  },
  version: '',
  runnerVersion: '',
  theme: remembered<Theme>('theme', ['system', 'light', 'dark'], 'system'),
  motion: remembered<Motion>('motion', ['system', 'off'], 'system'),
  toasts: [],
  route: location.hash.replace(/^#/, '') || '/',
}

const listeners = new Set<() => void>()

export const store = {
  get: () => state,
  subscribe(fn: () => void) {
    listeners.add(fn)
    return () => listeners.delete(fn)
  },
}

export function set(patch: Partial<State> | ((s: State) => Partial<State>)) {
  state = { ...state, ...(typeof patch === 'function' ? patch(state) : patch) }
  listeners.forEach((fn) => fn())
}

export function useStore() {
  return useSyncExternalStore(store.subscribe, store.get)
}

export function toast(text: string, tone: Toast['tone'] = 'ok', undo?: () => void) {
  const t: Toast = { id: nextId(), text, tone, undo, until: Date.now() + (undo ? 8000 : 4000) }
  set((s) => ({ toasts: [...s.toasts, t].slice(-3) }))
}

export function dismissToast(id: number) {
  set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }))
}

export function go(route: string) {
  if (location.hash !== '#' + route) history.pushState(null, '', '#' + route)
  set({ route })
}

window.addEventListener('popstate', () => set({ route: location.hash.replace(/^#/, '') || '/' }))

// Times from the server are turned into this browser's clock, so durations
// stay right when the two clocks disagree.
let skew = 0
export const serverTime = (iso?: string) => (iso ? Date.parse(iso) - skew : undefined)
export const setSkew = (serverNow: string) => {
  skew = Date.parse(serverNow) - Date.now()
}

setInterval(() => {
  const now = Date.now()
  const s = store.get()
  set({ now, toasts: s.toasts.some((t) => t.until <= now) ? s.toasts.filter((t) => t.until > now) : s.toasts })
}, 1000)
