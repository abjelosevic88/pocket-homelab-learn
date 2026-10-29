// Today: what is due, one button to study it, your decks, the last weeks and the next days.
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Deck, Home } from '../lib/types'
import { addDays, fmtTime, todayISO, weekday } from '../lib/format'
import { Card, Empty, Loading, useAsync } from '../components/ui'
import { Icon } from '../components/icons'
import { Heatmap } from '../components/Chart'
import { openCreate, useOnChange } from '../components/Layout'

export function DueCounts({ d, compact = false }: { d: { due_new: number; due_learn: number; due_review: number }; compact?: boolean }) {
  return (
    <span className={`due-counts ${compact ? 'compact' : ''}`}>
      <span className="dc new" title="New cards to learn">{d.due_new}</span>
      <span className="dc learn" title="In learning, due now">{d.due_learn}</span>
      <span className="dc review" title="Reviews due">{d.due_review}</span>
    </span>
  )
}

export function DeckRow({ d }: { d: Deck }) {
  const navigate = useNavigate()
  const c = d.counts!
  const due = c.due_new + c.due_learn + c.due_review
  return (
    <div className="deck-row" role="button" tabIndex={0} onClick={() => navigate(`/decks/${d.id}`)} onKeyDown={e => { if (e.key === 'Enter') navigate(`/decks/${d.id}`) }}>
      <span className="deck-dot" style={{ background: d.color || 'var(--accent)' }} />
      <div className="grow" style={{ minWidth: 0 }}>
        <div className="deck-name ellipsis">{d.name}</div>
        <div className="muted small">{c.total.toLocaleString()} cards{c.mature ? ` · ${Math.round(c.mature / Math.max(1, c.total) * 100)}% well known` : ''}{c.done_today ? ` · ${c.done_today} today` : ''}</div>
      </div>
      <DueCounts d={c} compact />
      <button type="button" className={`btn sm ${due ? 'primary' : ''}`} disabled={!due} onClick={e => { e.stopPropagation(); navigate(`/study?deck=${d.id}`) }}>{due ? 'Study' : 'Done'}</button>
    </div>
  )
}

export default function Today() {
  const navigate = useNavigate()
  const { user, settings } = useApp()
  const { data, reload } = useAsync(() => api.get<Home>(`${V1}/home`), [])
  useOnChange(reload)
  if (!data) return <Loading />
  const d = data.dashboard
  const name = (user?.name || '').split(' ')[0]
  const h = new Date().getHours()
  const hello = h < 5 ? 'Good night' : h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'
  if (data.decks.length === 0) {
    return (
      <div className="stack loose">
        <h1 className="home-title">{hello}{name ? `, ${name}` : ''}</h1>
        <Card>
          <div className="welcome">
            <img src="/favicon.svg" alt="" width={64} height={64} />
            <div className="grow">
              <h2>Learn anything, and keep it</h2>
              <p className="muted">Pocket Learn shows each card just before you would forget it, so a few minutes a day are enough to remember things for years. Start with a deck of your own, bring your Anki decks along, or let the AI make cards from something you read.</p>
              <div className="row wrap">
                <button type="button" className="btn primary" onClick={() => openCreate('card')}><Icon name="plus" size={15} />Add your first card</button>
                <button type="button" className="btn" onClick={() => navigate('/import')}><Icon name="upload" size={15} />Import from Anki or a file</button>
                <button type="button" className="btn" onClick={() => navigate('/create')}><Icon name="sparkles" size={15} />Make cards with AI</button>
              </div>
            </div>
          </div>
        </Card>
      </div>
    )
  }
  const heat: { date: string; minutes: number }[] = []
  for (let x = data.heat_from; x <= d.today; x = addDays(x, 1)) heat.push({ date: x, minutes: data.heat[x] || 0 })
  const max = Math.max(1, ...data.forecast)
  const goalPct = d.goal > 0 ? Math.min(1, d.done_today / d.goal) : 0
  const comeBack = d.come_back ? fmtTime(new Date(d.come_back)) : ''
  return (
    <div className="stack loose">
      <div className="home-head">
        <div className="grow">
          <h1 className="home-title">{hello}{name ? `, ${name}` : ''}</h1>
          <p className="muted">{weekday(todayISO(), true)}{d.streak > 1 ? ` · day ${d.streak} of your streak` : ''}</p>
        </div>
      </div>
      <Card className="due-hero">
        <div className="due-hero-body">
          <div className="grow">
            {d.due > 0 ? <>
              <div className="due-big"><b className="tnum">{d.due}</b> {d.due === 1 ? 'card' : 'cards'} to study</div>
              <div className="row wrap muted small" style={{ gap: 14 }}>
                <span><i className="dc-dot new" />{d.due_new} new</span><span><i className="dc-dot learn" />{d.due_learn} learning</span><span><i className="dc-dot review" />{d.due_review} to review</span>
                <span>about {Math.max(1, Math.round(d.due * 8 / 60))} min</span>
              </div>
            </> : <>
              <div className="due-big done"><Icon name="check-circle" size={26} />All done for today</div>
              <p className="muted small">{comeBack ? `A card in learning comes back at ${comeBack}. ` : ''}{d.due_tomorrow ? `${d.due_tomorrow} ${d.due_tomorrow === 1 ? 'card is' : 'cards are'} due tomorrow.` : 'Nothing is due tomorrow yet.'}</p>
            </>}
          </div>
          <div className="due-hero-actions">
            <button type="button" className="btn primary lg" disabled={d.due === 0} onClick={() => navigate('/study')}><Icon name="play" size={17} />{d.done_today > 0 && d.due > 0 ? 'Continue' : 'Study now'}</button>
            <button type="button" className="btn lg" onClick={() => openCreate('card')}><Icon name="plus" size={17} />Add a card</button>
          </div>
        </div>
        {(d.done_today > 0 || d.goal > 0) && <div className="due-progress">
          <span className="progress"><span style={{ width: `${d.goal > 0 ? goalPct * 100 : Math.min(100, d.done_today / Math.max(1, d.done_today + d.due) * 100)}%` }} /></span>
          <span className="muted small">{d.done_today} studied today{d.goal > 0 ? ` of your goal of ${d.goal}` : ''} · {d.minutes_today} min</span>
        </div>}
      </Card>
      <div className="home-grid">
        <Card title="Your decks" actions={<Link to="/decks" className="btn ghost sm">All decks</Link>} flush>
          <div className="deck-list">{data.decks.map(x => <DeckRow key={x.id} d={x} />)}</div>
        </Card>
        <div className="stack">
          <Card title="Coming up">
            <div className="forecast">
              {data.forecast.map((n, i) => (
                <div key={i} className="fc-col" title={`${n} ${n === 1 ? 'card' : 'cards'}`}>
                  <span className="fc-n tnum">{n}</span>
                  <span className="fc-bar" style={{ height: `${Math.max(3, n / max * 64)}px` }} />
                  <span className="fc-l">{i === 0 ? 'Today' : i === 1 ? 'Tmrw' : weekday(addDays(d.today, i)).slice(0, 3)}</span>
                </div>
              ))}
            </div>
          </Card>
          <Card title="The last 12 weeks" actions={<Link to="/stats" className="btn ghost sm">Statistics</Link>}>
            <div className="scroll-x"><Heatmap days={heat} weekStart={settings?.week_start || 1} fmt={m => `${m} ${m === 1 ? 'card' : 'cards'}`} none="nothing studied" label="Cards studied in the last 12 weeks" /></div>
            <p className="muted small mt"><Icon name="flame" size={13} className="flame-on" /> {d.streak} {d.streak === 1 ? 'day' : 'days'} in a row · longest {d.longest_streak}</p>
          </Card>
        </div>
      </div>
      {d.cards > 0 && d.due === 0 && d.done_today === 0 && <Empty title="Nothing due right now" icon="check-circle">Add new cards, or come back tomorrow.</Empty>}
    </div>
  )
}
