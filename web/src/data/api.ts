import { nextId, serverTime, set, setSkew, toast, type Job, type LogLine, type Repo, type Settings } from './store'

interface WireJob {
  id: number
  repo: string
  name: string
  workflow?: string
  branch?: string
  url?: string
  started: string
  ended?: string
  result?: Job['result']
  expected?: number
}

interface WireRepo {
  repo: string
  line: number
  labels: string[] | null
  added: string
  status: Repo['state']
  since: string
  error?: string
  retryAt?: string
  backAt?: string
  job?: WireJob
}

interface WireLine {
  at: string
  text: string
  tone?: LogLine['tone']
}

const toJob = (j: WireJob): Job => ({ ...j, started: serverTime(j.started)!, ended: serverTime(j.ended) })

const toRepo = (r: WireRepo): Repo => ({
  id: r.repo,
  line: r.line,
  state: r.status,
  since: serverTime(r.since)!,
  added: serverTime(r.added)!,
  error: r.error,
  retryAt: serverTime(r.retryAt),
  backAt: serverTime(r.backAt),
  job: r.job && toJob(r.job),
  ownLabels: r.labels,
})

const toLine = (l: WireLine): LogLine => ({ id: nextId(), at: serverTime(l.at)!, text: l.text, tone: l.tone })

export class ApiError extends Error {}

async function call<T = void>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch('api/' + path, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  }).catch(() => {
    throw new ApiError('GitHub-Runner cannot be reached. Is the container running?')
  })
  if (res.status === 401 && path !== 'login') {
    lock()
    throw new ApiError('Sign in first.')
  }
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `The request failed (${res.status}).` }))
    throw new ApiError(err.error)
  }
  return res.status === 204 || res.status === 202 ? (undefined as T) : res.json()
}

const repoPath = (id: string) => 'repos/' + id.split('/').map(encodeURIComponent).join('/')

let events: EventSource | undefined

function lock() {
  events?.close()
  events = undefined
  set({ phase: 'locked', repos: [], jobs: [], logs: {} })
}

async function load() {
  const s = await call<{ version: string; runnerVersion: string; now: string; settings: Settings; repos: WireRepo[]; jobs: WireJob[] }>('GET', 'state')
  setSkew(s.now)
  set({ phase: 'ready', version: s.version, runnerVersion: s.runnerVersion, settings: s.settings, repos: s.repos.map(toRepo), jobs: s.jobs.map(toJob), logs: {} })
}

function upsertJob(job: Job) {
  set((s) => {
    const i = s.jobs.findIndex((j) => j.id === job.id)
    return { jobs: i < 0 ? [...s.jobs, job] : s.jobs.map((j) => (j.id === job.id ? job : j)) }
  })
}

function listen() {
  events?.close()
  events = new EventSource('api/events')
  // Every (re)connect loads the whole state, so nothing missed while away is lost.
  events.onopen = () => {
    set({ connected: true })
    load().catch(() => {})
  }
  events.onerror = () => {
    set({ connected: false })
    if (events?.readyState === EventSource.CLOSED) {
      setTimeout(start, 3000)
    }
  }
  events.addEventListener('repo', (e) => {
    const repo = toRepo(JSON.parse(e.data))
    set((s) => ({ repos: s.repos.some((r) => r.id === repo.id) ? s.repos.map((r) => (r.id === repo.id ? repo : r)) : [...s.repos, repo] }))
  })
  events.addEventListener('removed', (e) => {
    const { repo } = JSON.parse(e.data)
    set((s) => ({ repos: s.repos.filter((r) => r.id !== repo) }))
  })
  events.addEventListener('job', (e) => upsertJob(toJob(JSON.parse(e.data))))
  events.addEventListener('log', (e) => {
    const { repo, ...line } = JSON.parse(e.data)
    set((s) => (s.logs[repo] ? { logs: { ...s.logs, [repo]: [...s.logs[repo], toLine(line)].slice(-500) } } : {}))
  })
  events.addEventListener('settings', (e) => set({ settings: JSON.parse(e.data) }))
}

export async function start() {
  try {
    const session = await call<{ locked: boolean; signedIn: boolean; homeOnly: boolean }>('GET', 'session')
    if (session.homeOnly) return set({ phase: 'homeOnly' })
    if (!session.signedIn) return lock()
    await load()
    listen()
  } catch {
    set({ connected: false })
    setTimeout(start, 3000)
  }
}

export async function signIn(password: string) {
  await call('POST', 'login', { password })
  await start()
}

export async function signOut() {
  await call('POST', 'logout')
  lock()
}

export async function loadLog(id: string) {
  const lines = await call<WireLine[]>('GET', repoPath(id) + '/log')
  set((s) => ({ logs: { ...s.logs, [id]: lines.map(toLine) } }))
}

export async function addRepo(input: string) {
  const { repo } = await call<{ repo: string }>('POST', 'repos', { repo: input })
  return repo
}

// Removing is undone in one step, so it asks nothing and offers Undo instead.
export async function removeRepo(id: string) {
  set((s) => ({ repos: s.repos.filter((r) => r.id !== id) }))
  try {
    await call('DELETE', repoPath(id))
  } catch (e) {
    toast((e as Error).message, 'fail')
    return load()
  }
  toast(`${id} removed, its runner is gone from GitHub.`, 'ok', () => {
    addRepo(id).catch((e) => toast((e as Error).message, 'fail'))
  })
}

export const restartRunner = (id: string) => call('POST', repoPath(id) + '/restart')

export const setRepoLabels = (id: string, labels: string[] | null) => call('PUT', repoPath(id) + '/labels', { labels })

export async function saveSettings(patch: Partial<Omit<Settings, 'token' | 'password'>>) {
  set({ settings: await call<Settings>('PATCH', 'settings', patch) })
}

export async function saveToken(token: string) {
  set({ settings: await call<Settings>('PUT', 'token', { token }) })
}

export const checkToken = () => call<{ repo: string; ok: boolean; error?: string }[]>('POST', 'token/check')

export async function setPassword(password: string) {
  await call('PUT', 'password', { password })
  set((s) => ({ settings: { ...s.settings, password: true } }))
}

export async function clearPassword() {
  await call('DELETE', 'password')
  set((s) => ({ settings: { ...s.settings, password: false } }))
}
