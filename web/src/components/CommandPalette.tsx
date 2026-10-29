// ⌘K / "/" — find a card, open a deck, jump to a page, or run a command from the keyboard.
import React, { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Deck, Note } from '../lib/types'
import { plainText } from '../lib/render'
import { Icon } from './icons'

export interface PaletteCommand { id: string; label: string; icon: string; hint?: string; keywords?: string; run: () => void }

export function CommandPalette({ onClose, commands, decks }: { onClose: () => void; commands: PaletteCommand[]; decks: Deck[] | null }) {
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const [hi, setHi] = useState(0)
  const list = useRef<HTMLDivElement>(null)
  const [found, setFound] = useState<Note[]>([])
  useEffect(() => {
    const n = q.trim()
    if (n.length < 2) { setFound([]); return }
    const t = setTimeout(() => { api.get<{ notes: Note[] }>(`${V1}/notes?limit=8&q=${encodeURIComponent(n)}`).then(r => setFound(r.notes)).catch(() => {}) }, 180)
    return () => clearTimeout(t)
  }, [q])

  type Row = { key: string; group: string; icon: React.ReactNode; label: React.ReactNode; sub?: React.ReactNode; hint?: string; run: () => void }
  const rows = useMemo<Row[]>(() => {
    const out: Row[] = []
    const n = q.trim().toLowerCase()
    const words = n.split(/\s+/).filter(Boolean)
    const hit = (text: string) => words.every(w => text.toLowerCase().includes(w))
    const cmds = commands.filter(c => !n || hit(`${c.label} ${c.keywords || ''}`)).map(c => ({ key: c.id, group: 'Commands', icon: <Icon name={c.icon} size={16} />, label: c.label, hint: c.hint, run: c.run }))
    const dks = (decks || []).filter(d => n && hit(`${d.name} ${d.description} deck`)).slice(0, 6)
      .map(d => ({ key: `d${d.id}`, group: 'Decks', icon: <Icon name="layers" size={16} />, label: d.name, sub: `${d.counts?.total || 0} cards`, run: () => navigate(`/decks/${d.id}`) }))
    const cards = found.map(c => ({ key: `n${c.id}`, group: 'Cards', icon: <Icon name="note" size={16} />, label: plainText(c.front, { cloze: 0, reveal: true }).slice(0, 90), sub: c.deck_name, run: () => navigate(`/browse?note=${c.id}`) }))
    if (n) out.push(...dks, ...cards, ...cmds)
    else out.push(...cmds)
    return out
  }, [q, decks, found, commands, navigate])
  useEffect(() => setHi(0), [q])
  useEffect(() => { list.current?.querySelector(`[data-i="${hi}"]`)?.scrollIntoView({ block: 'nearest' }) }, [hi])

  const run = (r: Row | undefined) => { if (!r) return; onClose(); r.run() }
  let last = ''
  return createPortal(
    <div className="palette-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="palette-box" role="dialog" aria-label="Search and commands">
        <div className="pal-input">
          <Icon name="search" size={18} />
          <input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Find a card or a deck, or run a command…" aria-label="Search"
            onKeyDown={e => {
              if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min(rows.length - 1, h + 1)) }
              else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(0, h - 1)) }
              else if (e.key === 'Enter') { e.preventDefault(); run(rows[hi]) }
              else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onClose() }
            }} />
          <kbd>esc</kbd>
        </div>
        <div className="pal-list" ref={list} role="listbox">
          {rows.length === 0 && <div className="pal-empty">Nothing matches.</div>}
          {rows.map((r, i) => {
            const head = r.group !== last ? <div className="pal-group" key={'g' + r.group}>{r.group}</div> : null
            last = r.group
            return (
              <React.Fragment key={r.key}>
                {head}
                <div data-i={i} role="option" aria-selected={i === hi} className={`pal-row ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onClick={() => run(r)}>
                  <span className="pal-ico">{r.icon}</span>
                  <span className="grow"><span className="pal-label">{r.label}</span>{r.sub && <span className="pal-sub">{r.sub}</span>}</span>
                  {r.hint && <kbd>{r.hint}</kbd>}
                  {i === hi && <Icon name="chevron-right" size={14} className="muted" />}
                </div>
              </React.Fragment>
            )
          })}
        </div>
      </div>
    </div>, document.body)
}
