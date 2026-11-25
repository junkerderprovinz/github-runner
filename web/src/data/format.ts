import type { Job, Repo, Result, RunnerState, Settings } from './store'

export function duration(ms: number) {
  const s = Math.max(0, Math.round(ms / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, '0')}s`
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, '0')}m`
}

export function ago(at: number, now: number) {
  const s = Math.round((now - at) / 1000)
  if (s < 45) return 'just now'
  const m = Math.round(s / 60)
  if (m < 60) return `${m} min ago`
  const h = Math.round(m / 60)
  if (h < 24) return `${h} h ago`
  return `${Math.round(h / 24)} d ago`
}

export function clock(at: number, seconds = true) {
  return new Date(at).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', second: seconds ? '2-digit' : undefined })
}

export const STATE_WORD: Record<RunnerState, string> = {
  starting: 'Starting',
  idle: 'Online',
  busy: 'Running',
  offline: 'Offline',
  error: 'Not registered',
}

export const RESULT_WORD: Record<Result, string> = { passed: 'Passed', failed: 'Failed', cancelled: 'Cancelled' }

const DEFAULT_LABELS = ['self-hosted', 'Linux', 'X64']

export function labelsOf(r: Repo, s: Settings) {
  return [...new Set([...(s.noDefaultLabels ? [] : DEFAULT_LABELS), ...(r.ownLabels ?? s.labels)])]
}

export const title = (j: Job) => (j.workflow ? `${j.workflow} / ${j.name}` : j.name)

export function current(r: Repo): Job | undefined {
  return r.state === 'busy' ? r.job : undefined
}

// Newest first.
export function jobsOf(jobs: Job[], id: string) {
  return jobs.filter((j) => j.repo === id).sort((a, b) => b.started - a.started)
}

// progress is undefined while there are no earlier runs to compare with.
export function progress(j: Job, now: number) {
  return j.expected ? Math.min(0.97, (now - j.started) / j.expected) : undefined
}

export function detail(r: Repo, jobs: Job[], now: number) {
  const job = current(r)
  if (job) return `${title(job)}, ${duration(now - job.started)}`
  if (r.state === 'starting') return 'Registering with GitHub'
  if (r.state === 'offline') return r.backAt ? `Back in ${duration(r.backAt - now)}` : 'Starting'
  if (r.state === 'error') return r.retryAt ? `Next try in ${duration(r.retryAt - now)}` : 'Waiting'
  const last = jobsOf(jobs, r.id).find((j) => j.ended)
  return last ? `Last job ${ago(last.ended!, now)}` : 'No jobs yet'
}

export function summary(repos: Repo[], settings: Settings, connected: boolean) {
  const count = (s: RunnerState) => repos.filter((r) => r.state === s).length
  const broken = count('error')
  const busy = count('busy')
  const alert = !connected || !settings.token.set || broken > 0
  const head = !connected
    ? 'Connection lost, trying again'
    : !settings.token.set
      ? 'No access token yet'
      : !repos.length
        ? 'No repositories yet'
        : broken
          ? broken === 1
            ? 'One repository needs you'
            : `${broken} repositories need you`
          : busy
            ? `${busy} ${busy === 1 ? 'job' : 'jobs'} running`
            : 'All runners waiting for jobs'
  return { busy, broken, head, alert }
}

export const repoName = (id: string) => id.split('/')[1] ?? id
