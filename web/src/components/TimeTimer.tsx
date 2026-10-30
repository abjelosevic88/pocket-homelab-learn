// The shared timer in Pocket Time: Learn's study timer is the person's one
// timer there. This polls it (every 20 s while the page is visible, and on
// focus) and shows a timer running there, with Stop.
import { useCallback, useEffect, useRef, useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { TimeRunning, TimeTimer } from '../lib/types'
import { Icon } from './icons'
import { useToast } from './ui'

const POLL_MS = 20000

/** Polls Pocket Time's timer while visible; session = the study run to keep in step. */
export function useTimeTimer(enabled: boolean, session = 0) {
  const [data, setData] = useState<TimeTimer | null>(null)
  const busy = useRef(false)
  const load = useCallback(async () => {
    if (!enabled || busy.current || document.visibilityState === 'hidden') return
    busy.current = true
    try { setData(await api.get<TimeTimer>(`${V1}/time/timer${session ? `?session=${session}` : ''}`)) } catch { /* the next poll tries again */ } finally { busy.current = false }
  }, [enabled, session])
  useEffect(() => {
    if (!enabled) { setData(null); return }
    load()
    const t = setInterval(load, POLL_MS)
    const onFocus = () => load()
    window.addEventListener('focus', onFocus)
    document.addEventListener('visibilitychange', onFocus)
    window.addEventListener('pln:timer', onFocus)
    return () => { clearInterval(t); window.removeEventListener('focus', onFocus); document.removeEventListener('visibilitychange', onFocus); window.removeEventListener('pln:timer', onFocus) }
  }, [enabled, load])
  return { data, reload: load, setData }
}

/** Tell every timer view to look again (after a start or a stop here). */
export const timerChanged = () => window.dispatchEvent(new CustomEvent('pln:timer'))

/** Whether this person's study timer is Pocket Time's. */
export function useTimeConnected() {
  const { settings } = useApp()
  return !!settings && settings.time_mode !== 'off' && settings.time_via !== '' && !(settings.time_mode === 'core' && !settings.core_configured)
}

/** Seconds since a moment, refreshed every `every` ms. */
export function useElapsed(since: string | undefined, every = 1000) {
  const [, tick] = useState(0)
  useEffect(() => { if (!since) return; const t = setInterval(() => tick(x => x + 1), every); return () => clearInterval(t) }, [since, every])
  return since ? Math.max(0, Math.floor((Date.now() - new Date(since).getTime()) / 1000)) : 0
}

export const clockText = (s: number) => {
  const h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), sec = s % 60
  return h ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : `${m}:${String(sec).padStart(2, '0')}`
}

export const minutesText = (s: number) => s < 60 ? 'under a minute' : s < 3600 ? `${Math.round(s / 60)} min` : `${Math.floor(s / 3600)} h ${Math.round(s % 3600 / 60)} min`

/** Stops the timer in Pocket Time (only if it is still that one). */
export async function stopTimeTimer(id: number, toast: (m: string, k?: 'ok' | 'err') => void): Promise<boolean> {
  try {
    await api.post(`${V1}/time/timer/stop`, { id })
    timerChanged()
    return true
  } catch (e) { toast((e as Error).message, 'err'); timerChanged(); return false }
}

/** The top bar: "Timer running in Pocket Time: X — Stop", wherever it was started. */
export function TimerPill() {
  const connected = useTimeConnected()
  const { data, setData } = useTimeTimer(connected)
  const { toast } = useToast()
  const run = data?.running || null
  const secs = useElapsed(run?.started_at, 30000)
  if (!run) return null
  const stop = async () => { if (await stopTimeTimer(run.id, toast)) { setData(d => d ? { ...d, running: null } : d); toast(`Stopped: ${run.name}`) } }
  return <RunningTimer run={run} secs={secs} onStop={stop} openURL={data?.open_url} />
}

function RunningTimer({ run, secs, onStop, openURL }: { run: TimeRunning; secs: number; onStop: () => void; openURL?: string }) {
  const where = run.started_in === 'Pocket Learn' ? 'started here' : `started in ${run.started_in}`
  return (
    <span className="time-pill" title={`Timer running in Pocket Time: ${run.name} (${where}) · ${minutesText(secs)}`}>
      <Icon name="timer" size={15} className="time-pill-ico" />
      {openURL ? <a className="time-pill-name ellipsis" href={openURL} target="_blank" rel="noreferrer">{run.name}</a> : <span className="time-pill-name ellipsis">{run.name}</span>}
      <span className="time-pill-t tnum">{minutesText(secs).replace('under a minute', '<1 min')}</span>
      <button type="button" className="time-pill-stop" onClick={onStop} aria-label={`Stop the timer: ${run.name}`} title="Stop it in Pocket Time"><Icon name="stop" size={12} /></button>
    </span>
  )
}
