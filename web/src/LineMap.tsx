import { useEffect, useState, type CSSProperties, type MouseEvent } from 'react'
import { ArrowLeft, Plus, RotateCcw, Trash2, X } from 'lucide-react'
import { loadLog, removeRepo, restartRunner, setRepoLabels, signOut } from './data/api'
import { ago, clock, current, detail, duration, jobsOf, labelsOf, progress, repoName, summary, title, RESULT_WORD, STATE_WORD } from './data/format'
import { go, toast, useStore, type Job, type Repo } from './data/store'
import { AddForm } from './ui/AddForm'
import { Button, Logo, TagInput, morph } from './ui/kit'
import { SETTINGS_TABS, SettingsSection, settingsTab } from './ui/SettingsSections'

const nav = (route: string) => (e?: MouseEvent) => {
  e?.preventDefault()
  morph(() => go(route))
}

const slug = (id: string) => id.replace(/[^a-z0-9]/gi, '-')
const lineVar = (r: Repo) => ({ '--line': `var(--l${r.line % 7})` }) as CSSProperties
const failed = (e: unknown) => toast((e as Error).message, 'fail')

export function LineMap() {
  const { route, repos } = useStore()
  const [section, arg] = route.replace(/^\//, '').split('/')
  const id = section === 'repo' ? decodeURIComponent(arg ?? '') : ''
  const picked = repos.find((r) => r.id === id)
  return (
    <div className="a-app">
      <Band section={section === 'settings' ? 'settings' : 'lines'} />
      {section === 'settings' ? <SettingsPage tab={settingsTab(arg)} /> : picked ? <Detail repo={picked} key={picked.id} /> : <Overview />}
    </div>
  )
}

function Band({ section }: { section: 'lines' | 'settings' }) {
  const { now, repos, settings, connected } = useStore()
  const [adding, setAdding] = useState(false)
  const sum = summary(repos, settings, connected)
  return (
    <header className="a-band-wrap">
      <div className="a-band">
        <a href="#/" className="a-brand" onClick={nav('/')}>
          <Logo size={30} />
          <span>GitHub-Runner</span>
        </a>
        <nav className="a-nav" aria-label="Main">
          <a href="#/" aria-current={section === 'lines' ? 'page' : undefined} onClick={nav('/')}>
            Lines
          </a>
          <a href="#/settings/general" aria-current={section === 'settings' ? 'page' : undefined} onClick={nav('/settings/general')}>
            Settings
          </a>
        </nav>
        <p className={sum.alert ? 'a-headline a-headline-alert' : 'a-headline'}>{sum.head}</p>
        <time className="a-clock">{clock(now, false)}</time>
        {settings.password && (
          <button type="button" className="a-band-link" onClick={() => signOut().catch(failed)}>
            Sign out
          </button>
        )}
        {repos.length > 0 && (
          <Button kind="primary" icon={adding ? <X size={16} /> : <Plus size={16} />} aria-expanded={adding} aria-label={adding ? 'Close' : 'Add repository'} onClick={() => setAdding(!adding)}>
            {adding ? 'Close' : 'Add repository'}
          </Button>
        )}
      </div>
      {adding && repos.length > 0 && (
        <div className="a-addbar">
          <AddForm
            onDone={() => {
              setAdding(false)
              morph(() => go('/'))
            }}
          />
        </div>
      )}
    </header>
  )
}

function Bullet({ repo, size = 'md' }: { repo: Repo; size?: 'md' | 'lg' }) {
  return (
    <span className={`a-bullet a-bullet-${size} a-is-${repo.state}`} style={{ ...lineVar(repo), viewTransitionName: size === 'lg' ? `bullet-${slug(repo.id)}` : undefined }} aria-hidden>
      <span key={repo.state} className="a-bullet-ring" />
      {repoName(repo.id).slice(0, 1).toUpperCase()}
    </span>
  )
}

function StateMark({ repo }: { repo: Repo }) {
  return (
    <span className={`a-state a-state-${repo.state}`}>
      <span className="a-state-icon" aria-hidden />
      {STATE_WORD[repo.state]}
    </span>
  )
}

function Overview() {
  const { repos } = useStore()
  if (!repos.length) return <Empty />
  return (
    <main className="a-main">
      <section className="a-strip" aria-label="Repositories">
        {repos.map((r) => (
          <Sign key={r.id} repo={r} />
        ))}
      </section>
      <Timeline />
    </main>
  )
}

function Track({ job, large }: { job: Job; large?: boolean }) {
  const { now } = useStore()
  const share = progress(job, now)
  if (share === undefined) return null
  return (
    <div className={large ? 'a-track a-track-lg' : 'a-track'} role="progressbar" aria-label={`${job.name} progress`} aria-valuenow={Math.round(share * 100)}>
      <span className="a-track-fill" style={{ width: `${share * 100}%` }} />
    </div>
  )
}

function Sign({ repo }: { repo: Repo }) {
  const { now, jobs } = useStore()
  const job = current(repo)
  return (
    <a href={`#/repo/${encodeURIComponent(repo.id)}`} className={`a-sign a-sign-${repo.state}`} style={{ ...lineVar(repo), viewTransitionName: `sign-${slug(repo.id)}` }} onClick={nav(`/repo/${encodeURIComponent(repo.id)}`)}>
      <div className="a-sign-head">
        <Bullet repo={repo} size="lg" />
        <div className="a-sign-name">
          <span className="a-owner">{repo.id.split('/')[0]}/</span>
          <strong>{repoName(repo.id)}</strong>
        </div>
      </div>
      <StateMark repo={repo} />
      {job ? (
        <div className="a-train">
          <div className="a-train-text">
            <span>{title(job)}</span>
            <span className="num">{duration(now - job.started)}</span>
          </div>
          <Track job={job} />
        </div>
      ) : (
        <p className="a-sign-detail num">{repo.state === 'error' ? 'Token refused. ' + detail(repo, jobs, now) : detail(repo, jobs, now)}</p>
      )}
    </a>
  )
}

const SPAN = 2 * 3_600_000

function Timeline() {
  const { repos, jobs, now } = useStore()
  const from = now - SPAN
  const x = (t: number) => `${Math.max(0, ((t - from) / SPAN) * 100)}%`
  const ticks: number[] = []
  for (let t = Math.ceil(from / 1_800_000) * 1_800_000; t <= now; t += 1_800_000) ticks.push(t)
  return (
    <section className="a-panel a-timeline" aria-label="Jobs in the last two hours">
      <header className="a-panel-head">
        <h2>Last two hours</h2>
        <ul className="a-legend">
          <li><span className="a-key a-key-ok" />Passed</li>
          <li><span className="a-key a-key-fail" />Failed</li>
          <li><span className="a-key a-key-cancelled" />Cancelled</li>
          <li><span className="a-key a-key-run" />Running, ring shows the usual end</li>
        </ul>
      </header>
      <div className="a-rows">
        {repos.map((r) => {
          const job = current(r)
          return (
            <div className="a-row" key={r.id} style={lineVar(r)}>
              <a href={`#/repo/${encodeURIComponent(r.id)}`} className="a-row-label" onClick={nav(`/repo/${encodeURIComponent(r.id)}`)}>
                <Bullet repo={r} />
                <span>{repoName(r.id)}</span>
              </a>
              <div className={`a-lane a-lane-${r.state}`}>
                <span className="a-rail" style={{ left: x(Math.max(from, r.added)) }} />
                {jobs
                  .filter((j) => j.repo === r.id && j.ended && j.ended > from)
                  .map((j) => (
                    <Stop key={j.id} job={j} left={x(j.ended!)} />
                  ))}
                {job && (
                  <>
                    <span className="a-train-run" style={{ left: x(job.started), width: `${((now - Math.max(job.started, from)) / SPAN) * 100}%` }} />
                    {job.expected && <span className="a-ghost" style={{ left: `min(${x(job.started + job.expected)}, 100%)` }} title={`Usually ends around ${clock(job.started + job.expected, false)}`} />}
                  </>
                )}
                {r.state === 'error' && <span className="a-alert" style={{ left: '100%' }} title={r.error} />}
              </div>
            </div>
          )
        })}
        <div className="a-axis" aria-hidden>
          {ticks.map((t) => (
            <span key={t} style={{ left: x(t) }}>
              {clock(t, false)}
            </span>
          ))}
          <span className="a-now" style={{ left: '100%' }}>
            now
          </span>
        </div>
      </div>
    </section>
  )
}

function Stop({ job, left }: { job: Job; left: string }) {
  const result = job.result ?? 'cancelled'
  return (
    <span className={`a-stop a-stop-${result}`} style={{ left }} tabIndex={0} aria-label={`${title(job)}, ${RESULT_WORD[result]}, ${duration(job.ended! - job.started)}`}>
      <span className="a-tip" role="tooltip">
        <strong>{title(job)}</strong>
        <span>
          {RESULT_WORD[result]} in {duration(job.ended! - job.started)}, {clock(job.ended!, false)}
        </span>
      </span>
    </span>
  )
}

function Empty() {
  const { settings } = useStore()
  return (
    <main className="a-main">
      <section className="a-empty">
        <div className="a-empty-map" aria-hidden>
          <span className="a-empty-line" />
          <span className="a-empty-stop" />
          <span className="a-empty-stop" />
          <span className="a-empty-stop" />
        </div>
        <h1>No lines yet</h1>
        <p>Every repository you add gets its own runner and its own line here. Its jobs show up as stops along it.</p>
        {!settings.token.set && (
          <p>
            First paste your access token under{' '}
            <a href="#/settings/general" onClick={nav('/settings/general')}>
              Settings
            </a>
            .
          </p>
        )}
        <AddForm submitLabel="Add repository" />
      </section>
    </main>
  )
}

function Detail({ repo }: { repo: Repo }) {
  const { now, settings, jobs } = useStore()
  const job = current(repo)
  const own = jobsOf(jobs, repo.id)
  const restart = () => restartRunner(repo.id).catch(failed)
  const labels = (next: string[] | null, done: string) =>
    setRepoLabels(repo.id, next)
      .then(() => toast(done))
      .catch(failed)
  return (
    <main className="a-main a-detail" style={lineVar(repo)}>
      <a href="#/" className="a-back" onClick={nav('/')}>
        <ArrowLeft size={16} /> All lines
      </a>
      <header className="a-detail-head">
        <Bullet repo={repo} size="lg" />
        <div className="a-detail-title">
          <h1>{repo.id}</h1>
          <div className="a-detail-state">
            <StateMark repo={repo} />
            <span className="num">{detail(repo, jobs, now)}</span>
          </div>
        </div>
        <div className="a-actions">
          <Button icon={<RotateCcw size={16} />} onClick={restart}>
            {repo.state === 'error' ? 'Try again now' : 'Restart runner'}
          </Button>
          <Button
            icon={<Trash2 size={16} />}
            onClick={() =>
              morph(() => {
                removeRepo(repo.id)
                go('/')
              })
            }
          >
            Remove
          </Button>
        </div>
      </header>
      {repo.state === 'error' && (
        <p className="a-notice" role="alert">
          {repo.error}{' '}
          <a href="#/settings/general" onClick={nav('/settings/general')}>
            Replace the token
          </a>
        </p>
      )}
      {job && (
        <div className="a-detail-train">
          <span>
            {title(job)}
            {job.branch && ` on ${job.branch}`}
          </span>
          <div className="a-track-slot">
            <Track job={job} large />
          </div>
          <span className="num">{job.expected ? `${duration(now - job.started)} of about ${duration(job.expected)}` : duration(now - job.started)}</span>
        </div>
      )}
      <div className="a-columns">
        <section className="a-panel a-log-panel" aria-label="Live log">
          <header className="a-panel-head">
            <h2>Live log</h2>
          </header>
          <Log repo={repo} />
        </section>
        <section className="a-panel" aria-label="Jobs">
          <header className="a-panel-head">
            <h2>Stops</h2>
            <span className="a-muted">{own.length === 1 ? '1 job' : `${own.length} jobs`}</span>
          </header>
          <ol className="a-route">
            {own.slice(0, 9).map((j) => (
              <li key={j.id} className={`a-route-stop a-route-${j.ended ? (j.result ?? 'cancelled') : 'run'}`}>
                <span className="a-route-dot" aria-hidden />
                <div>
                  <strong>
                    {j.url ? (
                      <a href={j.url} target="_blank" rel="noreferrer">
                        {title(j)}
                      </a>
                    ) : (
                      title(j)
                    )}
                  </strong>
                  <span className="a-muted">{j.branch ? `${j.branch}, ${ago(j.started, now)}` : ago(j.started, now)}</span>
                </div>
                <span className="a-route-end num">{j.ended ? `${RESULT_WORD[j.result ?? 'cancelled']}, ${duration(j.ended - j.started)}` : `Running, ${duration(now - j.started)}`}</span>
              </li>
            ))}
            {!own.length && <li className="a-muted a-route-none">No jobs yet. The first stop appears once a workflow runs here.</li>}
          </ol>
          <div className="a-labels">
            <h3>Labels</h3>
            {repo.ownLabels ? (
              <>
                <TagInput value={repo.ownLabels} placeholder="Add a label" onChange={(next) => labels(next, `Labels of ${repo.id} saved. Its runner registers again with them.`)} />
                <button type="button" className="a-textbtn" onClick={() => labels(null, `${repo.id} uses the default labels again.`)}>
                  Use the default labels
                </button>
              </>
            ) : (
              <>
                <p className="a-chips">
                  {labelsOf(repo, settings).map((l) => (
                    <span key={l}>{l}</span>
                  ))}
                </p>
                <button type="button" className="a-textbtn" onClick={() => labels([...settings.labels], `${repo.id} has its own labels now.`)}>
                  Give this repository its own labels
                </button>
              </>
            )}
          </div>
        </section>
      </div>
    </main>
  )
}

function Log({ repo }: { repo: Repo }) {
  const { logs } = useStore()
  const lines = logs[repo.id]
  useEffect(() => {
    loadLog(repo.id).catch(failed)
  }, [repo.id])
  return (
    <div
      className="a-log"
      role="log"
      ref={(el) => {
        if (el && el.scrollHeight - el.scrollTop - el.clientHeight < 120) el.scrollTop = el.scrollHeight
      }}
    >
      {lines?.slice(-200).map((l) => (
        <div key={l.id} className={`a-log-line ${l.tone ? 'a-log-' + l.tone : ''}`}>
          <time>{clock(l.at)}</time>
          <span>{l.text}</span>
        </div>
      ))}
      {lines && !lines.length && <p className="a-muted">Nothing yet. The runner's output appears here as it comes.</p>}
    </div>
  )
}

function SettingsPage({ tab }: { tab: ReturnType<typeof settingsTab> }) {
  return (
    <main className="a-main a-settings">
      <nav className="a-tabs" aria-label="Settings">
        {SETTINGS_TABS.map((t) => (
          <a key={t.id} href={`#/settings/${t.id}`} aria-current={t.id === tab ? 'page' : undefined} onClick={nav(`/settings/${t.id}`)}>
            {t.label}
          </a>
        ))}
      </nav>
      <section className="a-panel a-form" key={tab}>
        <SettingsSection id={tab} />
      </section>
    </main>
  )
}
