// Left column: the New button, sections with today's numbers, the decks — the Pocket Calendar sidebar.
import { useState } from 'react'
import { Link, NavLink, useNavigate } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import type { Dashboard, Deck } from '../lib/types'
import { Icon } from './icons'
import { createItems, type CreateKind } from './Layout'
import { type Anchor, Menu, rectOf } from './ui'

export function Sidebar({ onCreate, onNavigate, decks, dash }: { onCreate: (k: CreateKind) => void; onNavigate: () => void; decks: Deck[] | null; dash: Dashboard | null }) {
  const { status, settings } = useApp()
  const navigate = useNavigate()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const title = settings?.title || status?.title || 'Pocket Learn'
  const nav: { to: string; label: string; icon: string; end?: boolean; count?: string }[] = [
    { to: '/', label: 'Today', icon: 'home', end: true, count: dash?.due ? String(dash.due) : undefined },
    { to: '/decks', label: 'Decks', icon: 'layers', count: decks?.length ? String(decks.length) : undefined },
    { to: '/browse', label: 'Browse', icon: 'list', count: dash?.cards ? dash.cards.toLocaleString() : undefined },
    { to: '/create', label: 'Make cards', icon: 'sparkles' },
    { to: '/stats', label: 'Statistics', icon: 'chart' },
  ]
  const go = (url: string) => { onNavigate(); navigate(url) }
  const due = (d: Deck) => (d.counts?.due_new || 0) + (d.counts?.due_learn || 0) + (d.counts?.due_review || 0)
  return (
    <aside className="sidebar" aria-label="Learn">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Learn' ? title : <>Pocket <b>Learn</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={15} strokeWidth={2.2} />New<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={createItems(onCreate)} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.count && <span className="sb-count">{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      {dash && (
        <section className="sb-sec sb-streak" aria-label="Streak">
          <div className="sb-streak-row">
            <Icon name="flame" size={16} className={dash.streak > 0 ? 'flame-on' : 'muted'} />
            <span><b>{dash.streak}</b> {dash.streak === 1 ? 'day' : 'days'} in a row</span>
          </div>
          <div className="sb-streak-sub muted small">{dash.done_today} studied today · best {dash.longest_streak}</div>
        </section>
      )}

      {decks && decks.length > 0 && (
        <section className="sb-sec">
          <h4 className="sb-h"><button type="button" className="sb-h-btn" onClick={() => go('/decks')}>Decks</button></h4>
          <div className="sb-decks">
            {decks.slice(0, 12).map(d => (
              <button key={d.id} type="button" className="sb-deck" onClick={() => go(`/decks/${d.id}`)} title={d.name}>
                <i style={{ background: d.color || 'var(--accent)' }} />
                <span className="ellipsis grow">{d.name}</span>
                {due(d) > 0 && <span className="sb-count">{due(d)}</span>}
              </button>
            ))}
          </div>
        </section>
      )}

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pln:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
