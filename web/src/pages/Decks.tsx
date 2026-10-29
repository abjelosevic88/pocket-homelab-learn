// Decks: the list, and one deck with its numbers, its cards and its options.
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Deck } from '../lib/types'
import { Card, Confirm, Empty, Loading, MenuButton, useAsync, useToast } from '../components/ui'
import { Stacked } from '../components/Chart'
import { Icon } from '../components/icons'
import { DeckModal } from '../components/DeckModal'
import { announceChange, openCreate, useCrumb, useDecks, useOnChange } from '../components/Layout'
import { DeckRow, DueCounts } from './Today'
import { NoteList } from './Browse'

export default function Decks() {
  const { decks } = useDecks()
  const { data: archived, reload } = useAsync(() => api.get<Deck[]>(`${V1}/decks?archived=1`), [])
  useOnChange(reload)
  const [showArchived, setShowArchived] = useState(false)
  if (!decks) return <Loading />
  const old = (archived || []).filter(d => d.archived)
  return (
    <div className="stack loose">
      <div className="page-head">
        <h1 className="grow">Decks</h1>
        <button type="button" className="btn" onClick={() => openCreate('import')}><Icon name="upload" size={15} />Import</button>
        <button type="button" className="btn primary" onClick={() => openCreate('deck')}><Icon name="plus" size={15} />New deck</button>
      </div>
      {decks.length === 0 ? <Empty title="No decks yet" icon="layers">A deck is a set of cards you study together: a language, a subject, a course. <button type="button" className="link-btn" onClick={() => openCreate('deck')}>Create one</button> or <button type="button" className="link-btn" onClick={() => openCreate('import')}>import from Anki</button>.</Empty>
        : <Card flush><div className="deck-list">{decks.map(d => <DeckRow key={d.id} d={d} />)}</div></Card>}
      {old.length > 0 && <div>
        <button type="button" className="link-btn small" onClick={() => setShowArchived(s => !s)}>{showArchived ? 'Hide' : 'Show'} {old.length} archived {old.length === 1 ? 'deck' : 'decks'}</button>
        {showArchived && <Card flush className="mt"><div className="deck-list">{old.map(d => <DeckRow key={d.id} d={d} />)}</div></Card>}
      </div>}
    </div>
  )
}

export function DeckPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const { data: deck, reload, error } = useAsync(() => api.get<Deck>(`${V1}/decks/${id}`), [id])
  useOnChange(reload)
  useCrumb(deck?.name)
  const [edit, setEdit] = useState(false)
  const [del, setDel] = useState(false)
  if (error) return <Empty title="This deck is not here" icon="layers">It may have been deleted.</Empty>
  if (!deck) return <Loading />
  const c = deck.counts!
  const due = c.due_new + c.due_learn + c.due_review
  const more = async (what: 'new' | 'reviews') => {
    try { await api.post(`${V1}/decks/${deck.id}/extend`, what === 'new' ? { new: 10 } : { reviews: 50 }); toast(what === 'new' ? '10 more new cards today' : '50 more reviews today'); announceChange() } catch (e) { toast((e as Error).message, 'err') }
  }
  const remove = async () => { try { await api.del(`${V1}/decks/${deck.id}`); toast('Deck deleted'); announceChange(); navigate('/decks') } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="stack loose">
      <div className="deck-hero" style={{ ['--tile' as string]: deck.color || 'var(--accent)' }}>
        <span className="deck-hero-ico"><Icon name="layers" size={22} /></span>
        <div className="grow" style={{ minWidth: 0 }}>
          <h1 className="ellipsis">{deck.name}</h1>
          {deck.description && <p className="muted">{deck.description}</p>}
          {deck.archived && <span className="badge">archived</span>}
        </div>
        <div className="row wrap">
          <button type="button" className="btn primary" disabled={!due} onClick={() => navigate(`/study?deck=${deck.id}`)}><Icon name="play" size={15} />{due ? `Study ${due}` : 'Done for today'}</button>
          <button type="button" className="btn" onClick={() => openCreate('card', deck.id)}><Icon name="plus" size={15} />Add a card</button>
          <MenuButton label="More" className="btn icon-only" align="right" width={240} items={[
            { label: 'Deck options', icon: 'sliders', onSelect: () => setEdit(true) },
            { label: '10 more new cards today', icon: 'plus', onSelect: () => more('new') },
            { label: '50 more reviews today', icon: 'repeat', onSelect: () => more('reviews') },
            { label: 'Make cards with AI', icon: 'sparkles', onSelect: () => navigate(`/create?deck=${deck.id}`) },
            { label: 'Export this deck (JSON)', icon: 'download', onSelect: () => { window.location.href = `${V1}/export.json?deck=${deck.id}` } },
            'sep',
            { label: 'Delete deck', icon: 'trash', danger: true, onSelect: () => setDel(true) },
          ]}><Icon name="more" size={17} /></MenuButton>
        </div>
      </div>
      <div className="stats-row">
        <div className="stat"><div className="stat-l">Due today</div><div className="stat-v"><DueCounts d={c} /></div></div>
        <div className="stat"><div className="stat-l">Cards</div><div className="stat-v tnum">{c.total.toLocaleString()}</div><div className="stat-s small">{c.suspended ? `${c.suspended} set aside` : `${deck.new_per_day} new a day`}</div></div>
        <div className="stat"><div className="stat-l">Well known</div><div className="stat-v tnum">{c.mature.toLocaleString()}</div><div className="stat-s small">interval 3 weeks or more</div></div>
        <div className="stat"><div className="stat-l">Tomorrow</div><div className="stat-v tnum">{c.due_tomorrow}</div><div className="stat-s small">reviews due</div></div>
      </div>
      {c.total > 0 && <Card title="Where the cards stand">
        <Stacked parts={[{ value: c.new, color: 'var(--dc-new)', label: 'New' }, { value: c.learning, color: 'var(--dc-learn)', label: 'Learning' }, { value: c.young, color: 'var(--dc-young)', label: 'Young' }, { value: c.mature, color: 'var(--dc-review)', label: 'Well known' }, { value: c.suspended, color: 'var(--faint)', label: 'Set aside' }]} height={12} />
        <div className="legend small muted mt">
          <span><i style={{ background: 'var(--dc-new)' }} />New {c.new}</span><span><i style={{ background: 'var(--dc-learn)' }} />Learning {c.learning}</span><span><i style={{ background: 'var(--dc-young)' }} />Young {c.young}</span><span><i style={{ background: 'var(--dc-review)' }} />Well known {c.mature}</span>{c.suspended > 0 && <span><i style={{ background: 'var(--faint)' }} />Set aside {c.suspended}</span>}
        </div>
      </Card>}
      <NoteList deckId={deck.id} />
      {edit && <DeckModal deck={deck} onClose={() => setEdit(false)} onSaved={() => { setEdit(false); reload(); announceChange() }} />}
      {del && <Confirm title={`Delete “${deck.name}”?`} message={`Its ${c.total.toLocaleString()} cards and their whole history are deleted. Export it first if you may want it back.`} confirmLabel="Delete deck" onCancel={() => setDel(false)} onConfirm={() => { setDel(false); remove() }} />}
    </div>
  )
}
