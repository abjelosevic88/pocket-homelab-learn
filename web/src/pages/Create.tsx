// Make cards: the AI writes cards from a text, a note or your highlights; you pick the ones to keep.
import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { AICard, Deck, Source } from '../lib/types'
import { CardText, plainText } from '../lib/render'
import { Card, CoreHint, Empty, Field, Loading, Segmented, Spinner, useAsync, useDebounce, useToast } from '../components/ui'
import { Select } from '../components/Select'
import { Icon } from '../components/icons'
import { announceChange, useDecks } from '../components/Layout'

type From = 'text' | 'notes' | 'reader' | 'library'

export default function Create() {
  const { settings } = useApp()
  const [sp] = useSearchParams()
  const [from, setFrom] = useState<From>('text')
  const [text, setText] = useState('')
  const [title, setTitle] = useState('')
  const [count, setCount] = useState('8')
  const [kind, setKind] = useState('auto')
  const [language, setLanguage] = useState('')
  const [focus, setFocus] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [cards, setCards] = useState<(AICard & { on: boolean })[] | null>(null)
  const [srcRef, setSrcRef] = useState<{ ref: string; url: string }>({ ref: '', url: '' })
  const { data: ai } = useAsync(() => api.get<{ ready: boolean; provider: string; detail: string }>(`${V1}/ai/status`), [])
  const coreOn = !!settings?.core_configured && settings?.core_registered !== false
  const via = (a: 'notes' | 'reader' | 'library') => !!settings?.[`${a}_via`]

  const generate = async (t = text, ttl = title) => {
    if (!t.trim()) return
    setBusy(true); setErr(''); setCards(null)
    try {
      const r = await api.post<{ ok: boolean; cards?: AICard[]; error?: string }>(`${V1}/ai/cards`, { text: t, title: ttl, count: Number(count), kind, language, focus })
      if (!r.ok) setErr(r.error || 'the AI did not answer')
      else setCards((r.cards || []).map(c => ({ ...c, on: true })))
    } catch (e) { setErr((e as Error).message) } finally { setBusy(false) }
  }
  const fromOpts: { value: From; label: string; disabled?: boolean; title?: string }[] = [
    { value: 'text', label: 'A text' },
    { value: 'notes', label: 'Pocket Notes', disabled: !via('notes'), title: via('notes') ? 'One of your notes' : 'Connect Pocket Notes in Settings → Connected apps' },
    { value: 'reader', label: 'Reader highlights', disabled: !via('reader'), title: via('reader') ? 'Passages you highlighted in articles' : 'Connect Pocket Reader in Settings → Connected apps' },
    { value: 'library', label: 'Book highlights', disabled: !via('library'), title: via('library') ? 'Passages you highlighted in books' : 'Connect Pocket Library in Settings → Connected apps' },
  ]
  const anyApp = via('notes') || via('reader') || via('library')
  return (
    <div className="stack loose">
      <div className="page-head"><h1 className="grow">Make cards</h1></div>
      {ai && !ai.ready && <div className="callout info"><Icon name="info" size={15} /><div>No AI is connected{ai.detail ? ` (${ai.detail})` : ''}. Choose Pocket AI or add your own OpenAI key under <Link to="/settings/ai">Settings → AI</Link>. Highlights can still be added as they are.{!coreOn && <CoreHint />}</div></div>}
      <Card>
        <div className="stack">
          <Field group label="Make cards from">
            <div className="scroll-x"><Segmented value={from} onChange={v => { setFrom(v); setCards(null); setErr('') }} options={fromOpts} /></div>
          </Field>
          {!anyApp && <p className="muted small">Your highlights from Pocket Reader and Pocket Library, and your notes in Pocket Notes, can become cards too: <Link to="/settings/apps">connect them</Link>.{!coreOn && <CoreHint />}</p>}
          {from === 'text' && <>
            <Field label="Text" help="Paste a page of a book, lecture notes, an article, a list of words. Up to about 40,000 characters.">
              <textarea rows={9} value={text} onChange={e => setText(e.target.value)} placeholder="Paste what you want to remember…" />
            </Field>
            <Field label="Where it is from (optional)"><input value={title} onChange={e => setTitle(e.target.value)} placeholder="e.g. Biology, chapter 4" /></Field>
          </>}
          {from === 'notes' && <NotePicker onPick={(t, ttl, url) => { setText(t); setTitle(ttl); setSrcRef({ ref: ttl, url }); generate(t, ttl) }} busy={busy} />}
          {(from === 'reader' || from === 'library') && <Highlights app={from} aiReady={!!ai?.ready} onAI={(t, ttl) => { setText(t); setTitle(ttl); setSrcRef({ ref: ttl, url: '' }); generate(t, ttl) }} />}
          {from === 'text' && <div className="three-col tight">
            <Field group label="How many"><Select ariaLabel="How many" value={count} onChange={setCount} options={['5', '8', '12', '20', '30'].map(v => ({ value: v, label: `Up to ${v}` }))} /></Field>
            <Field group label="Kind"><Select ariaLabel="Kind" value={kind} onChange={setKind} options={[{ value: 'auto', label: 'Mixed' }, { value: 'basic', label: 'Questions & answers' }, { value: 'cloze', label: 'Fill the gap' }]} /></Field>
            <Field label="In language (optional)"><input value={language} onChange={e => setLanguage(e.target.value)} placeholder="the text’s own" /></Field>
          </div>}
          {from === 'text' && <Field label="Focus on (optional)"><input value={focus} onChange={e => setFocus(e.target.value)} placeholder="e.g. dates and names, the vocabulary, definitions" /></Field>}
          {from === 'text' && <div className="row"><button type="button" className="btn primary" disabled={!text.trim() || busy || !ai?.ready} onClick={() => { setSrcRef({ ref: title, url: '' }); generate() }}>{busy ? <><Spinner />Writing cards…</> : <><Icon name="sparkles" size={15} />Make cards</>}</button></div>}
          {from !== 'text' && busy && <p className="muted row"><Spinner />Writing cards…</p>}
          {err && <div className="callout danger"><Icon name="alert" size={15} /><span>{err}</span></div>}
        </div>
      </Card>
      {cards && <Results cards={cards} setCards={setCards} srcRef={srcRef} initialDeck={Number(sp.get('deck')) || 0} />}
    </div>
  )
}

function Results({ cards, setCards, srcRef, initialDeck }: { cards: (AICard & { on: boolean })[]; setCards: (c: (AICard & { on: boolean })[] | null) => void; srcRef: { ref: string; url: string }; initialDeck: number }) {
  const { toast } = useToast()
  const { decks } = useDecks()
  const [deck, setDeck] = useState(String(initialDeck || Number(localStorage.getItem('pln-last-deck')) || decks?.[0]?.id || ''))
  const [newDeck, setNewDeck] = useState('')
  const [tags, setTags] = useState('ai')
  const [busy, setBusy] = useState(false)
  const n = cards.filter(c => c.on).length
  const upd = (i: number, p: Partial<AICard & { on: boolean }>) => setCards(cards.map((c, j) => j === i ? { ...c, ...p } : c))
  const add = async () => {
    setBusy(true)
    try {
      let deckId = Number(deck)
      if (deck === '-1') { const d = await api.post<Deck>(`${V1}/decks`, { name: newDeck.trim() }); deckId = d.id }
      let ok = 0
      for (const c of cards.filter(x => x.on)) {
        await api.post(`${V1}/notes`, { deck_id: deckId, kind: c.kind, front: c.front, back: c.back, extra: c.extra, tags, source: 'ai', source_ref: srcRef.ref, source_url: srcRef.url })
        ok++
      }
      localStorage.setItem('pln-last-deck', String(deckId))
      toast(`${ok} ${ok === 1 ? 'card' : 'cards'} added`)
      setCards(null); announceChange()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Card title={`${cards.length} cards from the AI`} sub="Untick what you don’t want, fix what is off, then add them. Nothing is saved until you do.">
      <ul className="ai-cards">
        {cards.map((c, i) => (
          <li key={i} className={`ai-card ${c.on ? '' : 'off'}`}>
            <input type="checkbox" checked={c.on} onChange={e => upd(i, { on: e.target.checked })} aria-label="Keep" />
            <div className="grow stack tight">
              <textarea rows={2} value={c.front} onChange={e => upd(i, { front: e.target.value })} aria-label="Front" />
              {c.kind === 'cloze' ? <div className="muted small ai-cloze"><CardText text={c.front} opts={{ cloze: 1 }} /></div>
                : <textarea rows={1} value={c.back} onChange={e => upd(i, { back: e.target.value })} aria-label="Back" className="ai-back" />}
              {c.extra && <span className="muted small">{plainText(c.extra)}</span>}
            </div>
            <span className="badge">{c.kind === 'cloze' ? 'gap' : 'Q&A'}</span>
          </li>
        ))}
      </ul>
      <div className="two-col tight mt">
        <Field group label="Add to deck"><Select ariaLabel="Deck" value={deck} onChange={setDeck} options={[...(decks || []).map(d => ({ value: String(d.id), label: d.name })), { value: '-1', label: 'New deck…' }]} placeholder="Choose a deck…" /></Field>
        {deck === '-1' ? <Field label="New deck"><input value={newDeck} onChange={e => setNewDeck(e.target.value)} autoFocus /></Field> : <Field label="Tags"><input value={tags} onChange={e => setTags(e.target.value)} /></Field>}
      </div>
      <div className="row mt"><button type="button" className="btn primary" disabled={!n || busy || !deck || (deck === '-1' && !newDeck.trim())} onClick={add}>{busy ? 'Adding…' : `Add ${n} ${n === 1 ? 'card' : 'cards'}`}</button><button type="button" className="btn ghost" onClick={() => setCards(null)}>Discard</button></div>
    </Card>
  )
}

function NotePicker({ onPick, busy }: { onPick: (text: string, title: string, url: string) => void; busy: boolean }) {
  const { toast } = useToast()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 300)
  const { data } = useAsync(() => api.get<{ connected: boolean; detail?: string; items: Source[] }>(`${V1}/sources/notes?q=${encodeURIComponent(dq)}`), [dq])
  const pick = async (s: Source) => {
    try {
      const n = await api.get<{ title: string; content: string; open_url: string }>(`${V1}/sources/notes/${s.id.split(':')[1]}`)
      if (!n.content.trim()) { toast('That note is empty', 'err'); return }
      onPick(n.content, n.title, n.open_url)
    } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <div className="stack">
      <div className="search-box"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Find a note" /></div>
      {!data ? <Loading /> : !data.connected ? <p className="bad small">{data.detail}</p> : data.items.length === 0 ? <p className="muted small">No notes found.</p> : (
        <ul className="src-list">{data.items.map(s => (
          <li key={s.id}><button type="button" className="src-row" disabled={busy} onClick={() => pick(s)}>
            <Icon name="note" size={16} /><span className="grow" style={{ minWidth: 0 }}><b className="ellipsis">{s.title}</b><span className="muted small ellipsis">{s.category ? `${s.category} · ` : ''}{s.text}</span></span><Icon name="sparkles" size={14} />
          </button></li>
        ))}</ul>
      )}
    </div>
  )
}

function Highlights({ app, aiReady, onAI }: { app: 'reader' | 'library'; aiReady: boolean; onAI: (text: string, title: string) => void }) {
  const { toast } = useToast()
  const { decks } = useDecks()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 300)
  const { data, reload } = useAsync(() => api.get<{ connected: boolean; detail?: string; items: Source[] }>(`${V1}/sources/${app}?q=${encodeURIComponent(dq)}`), [app, dq])
  const [sel, setSel] = useState<Set<string>>(new Set())
  const [deck, setDeck] = useState('')
  useEffect(() => setSel(new Set()), [app, dq])
  useEffect(() => { if (!deck && decks?.length) setDeck(String(decks.find(d => /highlight/i.test(d.name))?.id || decks[0].id)) }, [decks, deck])
  const items = (data?.items || [])
  const chosen = items.filter(s => sel.has(s.id))
  const toggle = (id: string) => setSel(s => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n })
  const addAsIs = async () => {
    try {
      const r = await api.post<{ notes: number; already: number }>(`${V1}/sources/highlights`, deck === '-1' ? { deck: 'Highlights', items: chosen } : { deck_id: Number(deck), items: chosen })
      toast(`${r.notes} added${r.already ? `, ${r.already} were already cards` : ''}`); setSel(new Set()); reload(); announceChange()
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const ai = () => {
    const titles = [...new Set(chosen.map(s => s.title).filter(Boolean))]
    onAI(chosen.map(s => `${s.text}${s.note ? `\n(My note: ${s.note})` : ''}${s.title ? `\n— ${s.title}` : ''}`).join('\n\n'), titles.length === 1 ? titles[0] : `${chosen.length} highlights`)
  }
  return (
    <div className="stack">
      <div className="search-box"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Find a highlight" /></div>
      {!data ? <Loading /> : !data.connected ? <p className="bad small">{data.detail}</p> : items.length === 0 ? <Empty title="No highlights" icon="pen">Highlight passages in {app === 'reader' ? 'Pocket Reader' : 'Pocket Library'} and they appear here.</Empty> : (
        <ul className="src-list">{items.map(s => (
          <li key={s.id}><label className={`src-row hl ${sel.has(s.id) ? 'on' : ''} ${s.added ? 'added' : ''}`}>
            <input type="checkbox" checked={sel.has(s.id)} onChange={() => toggle(s.id)} />
            <span className="grow" style={{ minWidth: 0 }}><span className="hl-text">“{s.text}”</span><span className="muted small ellipsis">{s.title}{s.added ? ' · already a card' : ''}</span></span>
          </label></li>
        ))}</ul>
      )}
      <div className="row wrap">
        <Select ariaLabel="Deck" value={deck} onChange={setDeck} options={[...(decks || []).map(d => ({ value: String(d.id), label: d.name })), { value: '-1', label: 'A new “Highlights” deck' }]} />
        <button type="button" className="btn" disabled={!chosen.length || !deck} onClick={addAsIs} title="The passage on the front, where it is from on the back">Add {chosen.length || ''} as they are</button>
        <button type="button" className="btn primary" disabled={!chosen.length || !aiReady} onClick={ai}><Icon name="sparkles" size={15} />Make questions with AI</button>
      </div>
    </div>
  )
}
