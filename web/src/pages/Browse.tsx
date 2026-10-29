// Browse: every card, searchable and filterable; edit one, or change many at once.
import { useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Note, TagCount } from '../lib/types'
import { FLAGS, STATE_NAME, fmtInterval, plainText } from '../lib/render'
import { fmtDate } from '../lib/format'
import { Card, Confirm, Empty, Loading, useAsync, useDebounce, useToast } from '../components/ui'
import { Select } from '../components/Select'
import { Icon } from '../components/icons'
import { NoteEditor } from '../components/NoteEditor'
import { announceChange, useDecks, useOnChange } from '../components/Layout'

const STATES = [{ value: '', label: 'All cards' }, { value: 'due', label: 'Due today' }, { value: 'new', label: 'New' }, { value: 'learning', label: 'Learning' }, { value: 'review', label: 'In review' }, { value: 'hard', label: 'Often forgotten' }, { value: 'flagged', label: 'Flagged' }, { value: 'suspended', label: 'Set aside' }]
const SORTS = [{ value: '', label: 'Newest first' }, { value: 'oldest', label: 'Oldest first' }, { value: 'updated', label: 'Recently changed' }, { value: 'front', label: 'A to Z' }, { value: 'due', label: 'Due soonest' }]
const PAGE = 50

export default function Browse() {
  return (
    <div className="stack loose">
      <div className="page-head"><h1 className="grow">Browse</h1></div>
      <NoteList />
    </div>
  )
}

export function NoteList({ deckId }: { deckId?: number }) {
  const { toast } = useToast()
  const { decks } = useDecks()
  const [sp, setSp] = useSearchParams()
  const [q, setQ] = useState(sp.get('q') || '')
  const dq = useDebounce(q, 250)
  const [deck, setDeck] = useState(deckId ? String(deckId) : sp.get('deck') || '')
  const [state, setState] = useState(sp.get('state') || '')
  const [sort, setSort] = useState('')
  const [tag, setTag] = useState('')
  const [limit, setLimit] = useState(PAGE)
  const [sel, setSel] = useState<Set<number>>(new Set())
  const [edit, setEdit] = useState<Note | null>(null)
  const [confirmDel, setConfirmDel] = useState(false)
  const { data: tags } = useAsync(() => api.get<TagCount[]>(`${V1}/tags`), [])
  const qs = useMemo(() => {
    const p = new URLSearchParams({ limit: String(limit) })
    if (dq.trim()) p.set('q', dq.trim())
    if (deck) p.set('deck', deck)
    if (state) p.set('state', state)
    if (sort) p.set('sort', sort)
    if (tag) p.set('tag', tag)
    return p.toString()
  }, [dq, deck, state, sort, tag, limit])
  const { data, reload } = useAsync(() => api.get<{ notes: Note[]; total: number }>(`${V1}/notes?${qs}`), [qs])
  useOnChange(reload)
  useEffect(() => { setSel(new Set()); setLimit(PAGE) }, [dq, deck, state, sort, tag])
  // /browse?note=12 opens that card (from the search palette)
  useEffect(() => {
    const id = Number(sp.get('note'))
    if (!id) return
    api.get<Note>(`${V1}/notes/${id}`).then(setEdit).catch(() => {})
    sp.delete('note'); setSp(sp, { replace: true })
  }, [sp]) // eslint-disable-line react-hooks/exhaustive-deps

  const notes = data?.notes || []
  const allOn = notes.length > 0 && notes.every(n => sel.has(n.id))
  const toggle = (id: number) => setSel(s => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n })
  const bulk = async (action: string, extra: Record<string, unknown> = {}) => {
    try {
      const r = await api.post<{ changed: number }>(`${V1}/notes/bulk`, { ids: [...sel], action, ...extra })
      toast(`${r.changed} changed`); setSel(new Set()); reload(); announceChange()
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const [moveTo, setMoveTo] = useState('')
  const [tagTo, setTagTo] = useState('')
  const deckOpts = [{ value: '', label: 'All decks' }, ...(decks || []).map(d => ({ value: String(d.id), label: d.name }))]
  const n = sel.size
  return (
    <Card flush title={deckId ? 'Cards' : undefined} sub={data ? `${data.total.toLocaleString()} ${data.total === 1 ? 'note' : 'notes'}` : undefined}>
      <div className="toolbar browse-bar">
        <div className="search-box"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search the cards (#tag for a tag)" aria-label="Search" /></div>
        {!deckId && <Select ariaLabel="Deck" value={deck} onChange={setDeck} options={deckOpts} />}
        <Select ariaLabel="Which cards" value={state} onChange={setState} options={STATES} />
        {(tags?.length || 0) > 0 && <Select ariaLabel="Tag" value={tag} onChange={setTag} options={[{ value: '', label: 'Any tag' }, ...(tags || []).map(t => ({ value: t.tag, label: t.tag, hint: String(t.count) }))]} />}
        <Select ariaLabel="Order" value={sort} onChange={setSort} options={SORTS} />
      </div>
      <div className={`bulk-bar ${n ? 'on' : ''}`}>
        <label className="check-row"><input type="checkbox" checked={allOn} onChange={() => setSel(allOn ? new Set() : new Set(notes.map(x => x.id)))} aria-label="Select all shown" />{n ? `${n} selected` : 'Select'}</label>
        <span className="grow" />
        <Select ariaLabel="Move to deck" value={moveTo} disabled={!n} onChange={v => { setMoveTo(''); bulk('move', { deck_id: Number(v) }) }} options={(decks || []).map(d => ({ value: String(d.id), label: d.name }))} placeholder="Move to…" />
        <div className="input-group tag-group"><input value={tagTo} disabled={!n} onChange={e => setTagTo(e.target.value)} placeholder="tag" aria-label="Tag" /><button type="button" className="btn sm" disabled={!n || !tagTo.trim()} onClick={() => { bulk('tag', { tag: tagTo }); setTagTo('') }}>Tag</button></div>
        <button type="button" className="btn sm" disabled={!n} onClick={() => bulk('suspend')} title="Not shown until you bring them back">Set aside</button>
        <button type="button" className="btn sm" disabled={!n} onClick={() => bulk('unsuspend')}>Bring back</button>
        <button type="button" className="btn sm" disabled={!n} onClick={() => bulk('forget')} title="Start learning them again from scratch">Make new</button>
        <button type="button" className="btn sm danger-text" disabled={!n} onClick={() => setConfirmDel(true)}><Icon name="trash" size={14} />Delete</button>
      </div>
      {!data ? <Loading /> : notes.length === 0 ? <Empty title={dq || state || tag ? 'No cards match' : 'No cards yet'} icon="note">{dq || state || tag ? 'Try another search.' : 'Add a card, or import a deck.'}</Empty> : (
        <ul className="note-list">
          {notes.map(x => <NoteRow key={x.id} n={x} on={sel.has(x.id)} onToggle={() => toggle(x.id)} onOpen={() => setEdit(x)} showDeck={!deckId} />)}
        </ul>
      )}
      {data && data.total > notes.length && <div className="pad"><button type="button" className="btn block" onClick={() => setLimit(l => l + PAGE)}>Show more ({(data.total - notes.length).toLocaleString()} left)</button></div>}
      {edit && <NoteEditor note={edit} onClose={() => setEdit(null)} onSaved={() => { reload(); announceChange() }} onDeleted={() => { reload(); announceChange() }} />}
      {confirmDel && <Confirm title={`Delete ${n} ${n === 1 ? 'note' : 'notes'}?`} message="Their cards and history are deleted." confirmLabel="Delete" onCancel={() => setConfirmDel(false)} onConfirm={() => { setConfirmDel(false); bulk('delete') }} />}
    </Card>
  )
}

function NoteRow({ n, on, onToggle, onOpen, showDeck }: { n: Note; on: boolean; onToggle: () => void; onOpen: () => void; showDeck: boolean }) {
  const cards = n.cards || []
  const live = cards.filter(x => !x.suspended)
  const c = [...(live.length ? live : cards)].sort((a, b) => (a.state === 0 ? 1 : 0) - (b.state === 0 ? 1 : 0) || a.due.localeCompare(b.due))[0]
  const susp = cards.length > 0 && cards.every(x => x.suspended)
  const flag = cards.find(x => x.flag)?.flag || 0
  const lapses = cards.reduce((s, x) => s + x.lapses, 0)
  const front = plainText(n.front, n.kind === 'cloze' ? { cloze: 0, reveal: true } : {})
  const back = n.kind === 'cloze' ? plainText(n.extra) : plainText(n.back)
  let when = ''
  if (c) when = susp ? 'set aside' : c.state === 0 ? 'new' : c.state === 2 ? `every ${fmtInterval(c.interval)}` : 'learning'
  return (
    <li className={`note-row ${on ? 'sel' : ''} ${susp ? 'off' : ''}`} onClick={onOpen}>
      <input type="checkbox" checked={on} onClick={e => e.stopPropagation()} onChange={onToggle} aria-label="Select" />
      <div className="grow" style={{ minWidth: 0 }}>
        <div className="nr-front ellipsis">{flag > 0 && <i className="flag-dot" style={{ background: FLAGS[flag].color }} />}{front || <span className="faint">(empty)</span>}</div>
        <div className="nr-back muted small ellipsis">{back}</div>
      </div>
      {showDeck && <span className="nr-deck muted small ellipsis">{n.deck_name}</span>}
      <span className="nr-state small">
        <span className={`badge st-${c ? (susp ? 'off' : c.state) : 0}`}>{c ? (susp ? 'Set aside' : STATE_NAME[c.state]) : 'New'}</span>
        <span className="muted">{when !== 'new' && when !== 'set aside' ? when : ''}{c?.due && c.state !== 0 && !susp ? ` · ${fmtDate(c.due.slice(0, 10), false)}` : ''}{lapses >= 4 ? ` · forgotten ${lapses}×` : ''}</span>
      </span>
    </li>
  )
}
