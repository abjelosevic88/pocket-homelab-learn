// Statistics: how much you study, how much you remember, what is coming and what is hard.
import { useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Stats as StatsT } from '../lib/types'
import { plainText, sides } from '../lib/render'
import { addDays, fmtDate, fmtTime } from '../lib/format'
import { minutesText } from '../components/TimeTimer'
import { Card, Empty, Loading, Segmented, useAsync } from '../components/ui'
import { Select } from '../components/Select'
import { Icon } from '../components/icons'
import { Bars, Heatmap, LineChart, Stacked } from '../components/Chart'
import { useDecks, useOnChange } from '../components/Layout'

const pct = (v: number | null) => v === null || v === undefined ? '—' : `${Math.round(v * 100)}%`

export default function Stats() {
  const { settings } = useApp()
  const { decks } = useDecks()
  const [range, setRange] = useState('m')
  const [deck, setDeck] = useState('')
  const { data, reload } = useAsync(() => api.get<StatsT>(`${V1}/stats?range=${range}${deck ? `&deck=${deck}` : ''}`), [range, deck])
  useOnChange(reload)
  if (!data) return <Loading />
  const t = data.totals
  const heat: { date: string; minutes: number }[] = []
  for (let x = data.heat_from; x <= data.to; x = addDays(x, 1)) heat.push({ date: x, minutes: data.heat[x] || 0 })
  const c = data.counts
  const ret = data.days.map(d => ({ x: d.day, y: d.review ? Math.round((d.review - d.again) / d.review * 100) : null }))
  const fc = data.forecast.map((n, i) => ({ x: addDays(data.to, i), y: n }))
  const best = data.hours.filter(h => h.total >= 10).sort((a, b) => b.ok / b.total - a.ok / a.total)[0]
  const desired = Math.round((data.desired_retention || 0.9) * 100)
  return (
    <div className="stack loose">
      <div className="page-head">
        <h1 className="grow">Statistics</h1>
        <Select ariaLabel="Deck" value={deck} onChange={setDeck} options={[{ value: '', label: 'All decks' }, ...(decks || []).map(d => ({ value: String(d.id), label: d.name }))]} />
        <div className="scroll-x"><Segmented size="sm" value={range} onChange={setRange} options={[{ value: 'w', label: 'Week' }, { value: 'm', label: 'Month' }, { value: '3m', label: '3 months' }, { value: 'y', label: 'Year' }, { value: 'all', label: 'All' }]} /></div>
      </div>
      {data.lifetime.reviews === 0 ? <Empty title="Nothing studied yet" icon="chart">Your numbers appear here after the first cards.</Empty> : <>
        <div className="stats-row">
          <div className="stat"><div className="stat-l">Studied</div><div className="stat-v tnum">{t.reviews.toLocaleString()}</div><div className="stat-s small">{t.per_day} a day · {t.days_studied} of {t.days} days</div></div>
          <div className="stat"><div className="stat-l">Time</div><div className="stat-v tnum">{t.minutes >= 120 ? `${Math.round(t.minutes / 6) / 10} h` : `${t.minutes} min`}</div><div className="stat-s small">{t.seconds_per_card}s a card</div></div>
          <div className="stat"><div className="stat-l">Remembered</div><div className="stat-v tnum">{pct(data.retention)}</div><div className="stat-s small">of reviews · aim {desired}%</div></div>
          <div className="stat"><div className="stat-l">Streak</div><div className="stat-v tnum row" style={{ gap: 6 }}><Icon name="flame" size={17} className={data.streak ? 'flame-on' : 'muted'} />{data.streak}</div><div className="stat-s small">longest {data.longest_streak}</div></div>
        </div>
        <Card title="Cards studied each day" sub={`${t.new} new cards learned`}>
          <LineChart points={data.days.map(d => ({ x: d.day, y: d.reviews }))} kind="bar" zero height={170} goal={settings?.daily_goal || undefined} goalLabel="goal" />
        </Card>
        {data.sessions.length > 0 && <Card title="Study sessions" sub={`${data.session_totals.count} ${data.session_totals.count === 1 ? 'session' : 'sessions'} in this period, ${minutesText(data.session_totals.seconds)} in all`} flush>
          <ul className="list session-list">{data.sessions.map(x => (
            <li key={x.id}><div className="list-row">
              <span className="ses-when muted small tnum">{fmtDate(x.date, false)} · {fmtTime(new Date(x.started_at))}</span>
              <span className="grow ellipsis">{x.deck_name || 'All decks'}{x.cards > 0 && <span className="muted small"> · {x.cards} {x.cards === 1 ? 'card' : 'cards'}</span>}</span>
              {x.kept_by_time && <span className="badge" title={x.source === 'time' ? 'Timed in Pocket Time, which wrote the time here' : 'Timed in Pocket Time; the time arrives here once it is written back'}>Pocket Time</span>}
              <span className="tnum small">{minutesText(x.duration_s)}</span>
            </div></li>
          ))}</ul>
        </Card>}
        <div className="home-grid">
          <Card title="How much you remember" sub={`Reviews remembered: young cards ${pct(data.retention_young)}, well-known cards ${pct(data.retention_mature)}.`}>
            <LineChart points={ret} height={150} format={v => `${Math.round(v)}%`} goal={desired} goalLabel={`aim ${desired}%`} />
            <p className="muted small mt">{data.retention === null ? 'No reviews in this period yet.' : (data.retention * 100) < desired - 5 ? 'Lower than you aim for: more cards are forgotten than planned. Hard cards may need rewriting (see below).' : (data.retention * 100) > desired + 5 ? 'Higher than you aim for: you could study fewer reviews by lowering “how sure to be” in Settings.' : 'Close to what you aim for: the schedule fits you.'}</p>
          </Card>
          <Card title="Coming up" sub={`${data.forecast.slice(1, 8).reduce((a, b) => a + b, 0)} reviews in the next 7 days`}>
            <LineChart points={fc} kind="bar" zero height={150} color="var(--dc-review)" />
          </Card>
        </div>
        <Card title="The last year">
          <div className="scroll-x"><Heatmap days={heat} weekStart={settings?.week_start || 1} fmt={m => `${m} ${m === 1 ? 'card' : 'cards'}`} none="nothing studied" label="Cards studied in the last year" /></div>
        </Card>
        <div className="home-grid">
          <Card title="Your cards" sub={`${c.total.toLocaleString()} in all`}>
            <Stacked parts={[{ value: c.new, color: 'var(--dc-new)', label: 'New' }, { value: c.learning, color: 'var(--dc-learn)', label: 'Learning' }, { value: c.young, color: 'var(--dc-young)', label: 'Young' }, { value: c.mature, color: 'var(--dc-review)', label: 'Well known' }, { value: c.suspended, color: 'var(--faint)', label: 'Set aside' }]} height={12} />
            <div className="legend small muted mt">
              <span><i style={{ background: 'var(--dc-new)' }} />New {c.new}</span><span><i style={{ background: 'var(--dc-learn)' }} />Learning {c.learning}</span><span><i style={{ background: 'var(--dc-young)' }} />Young {c.young}</span><span><i style={{ background: 'var(--dc-review)' }} />Well known {c.mature}</span>{c.suspended > 0 && <span><i style={{ background: 'var(--faint)' }} />Set aside {c.suspended}</span>}
            </div>
            <h4 className="mt">Time until the next review</h4>
            <Bars items={data.intervals.map(b => ({ label: b.label.replace(' days', 'd').replace(' weeks', 'w').replace(' months', 'mo'), value: b.count, hint: b.label }))} height={110} />
          </Card>
          <Card title="Time of day" sub={best ? `You remember best around ${best.hour}:00 (${Math.round(best.ok / best.total * 100)}%).` : 'When you study.'}>
            <Bars items={data.hours.filter((h, i) => h.reviews > 0 || i % 3 === 0).map(h => ({ label: h.hour % 3 === 0 ? `${h.hour}` : '', value: h.reviews, hint: `${h.hour}:00 · ${h.reviews} cards${h.total ? `, ${Math.round(h.ok / h.total * 100)}% remembered` : ''}` }))} height={130} />
          </Card>
        </div>
        {data.hard.length > 0 && <Card title="Forgotten most often" sub="Rewrite these: shorter, one fact, a picture or a memory aid. Or set them aside." flush>
          <ul className="list">{data.hard.map(h => { const s = sides(h); return (
            <li key={h.id}><div className="list-row"><span className="grow" style={{ minWidth: 0 }}><b className="ellipsis">{plainText(s.q, { cloze: s.cloze })}</b><span className="muted small ellipsis">{plainText(s.a, { cloze: s.cloze, reveal: true })} · {h.deck}</span></span><span className="badge warn">{h.lapses}×</span></div></li>
          ) })}</ul>
        </Card>}
        <p className="muted small">All time: {data.lifetime.reviews.toLocaleString()} answers on {data.lifetime.days} days since {fmtDate(data.lifetime.first_day)}, {Math.round(data.lifetime.duration_ms / 3600000 * 10) / 10} hours, {data.lifetime.notes.toLocaleString()} notes.</p>
      </>}
    </div>
  )
}
