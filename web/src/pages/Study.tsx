// The study screen: one card at a time, the answer, four buttons. Full screen, keyboard first.
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note, NextResult, StudyCard } from '../lib/types'
import { CardText, FLAGS, playSounds, plainText, sides, soundsOf, typedDiff } from '../lib/render'
import { fmtTime } from '../lib/format'
import { Loading, MenuButton, modalOpen, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { AskButton } from '../components/AskButton'
import { NoteEditor } from '../components/NoteEditor'

const RATINGS = [
  { r: 1, label: 'Again', key: '1', cls: 'again' },
  { r: 2, label: 'Hard', key: '2', cls: 'hard' },
  { r: 3, label: 'Good', key: '3', cls: 'good' },
  { r: 4, label: 'Easy', key: '4', cls: 'easy' },
] as const

export default function Study() {
  const [sp] = useSearchParams()
  const deck = Number(sp.get('deck')) || 0
  const navigate = useNavigate()
  const { settings } = useApp()
  const { toast } = useToast()
  const [res, setRes] = useState<NextResult | null>(null)
  const [shown, setShown] = useState(false)
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)
  const [undo, setUndo] = useState<number[]>([])
  const [session, setSession] = useState({ cards: 0, again: 0, ms: 0, started: Date.now() })
  const [edit, setEdit] = useState<Note | null>(null)
  const [explain, setExplain] = useState<{ id: number; text: string; busy: boolean } | null>(null)
  const [summary, setSummary] = useState<{ streak: number } | null>(null)
  const [now, setNow] = useState(Date.now())
  const shownAt = useRef(Date.now())
  const stopSound = useRef<() => void>(() => {})
  const typeRef = useRef<HTMLInputElement>(null)
  const card = res?.card || null

  const load = useCallback(async () => {
    try { setRes(await api.get<NextResult>(`${V1}/study/next?deck=${deck}`)) } catch (e) { toast((e as Error).message, 'err') }
  }, [deck, toast])
  useEffect(() => { load() }, [load])

  // a new card: reset, play its sounds, read it aloud
  useEffect(() => {
    setShown(false); setTyped(''); setExplain(null)
    shownAt.current = Date.now()
    stopSound.current()
    if (!card) return
    const { q, cloze } = sides(card)
    if (settings?.autoplay) stopSound.current = playSounds(soundsOf(q))
    if (settings?.speak && 'speechSynthesis' in window) {
      speechSynthesis.cancel()
      const u = new SpeechSynthesisUtterance(plainText(q, { cloze }))
      if (settings.speak_lang) u.lang = settings.speak_lang
      speechSynthesis.speak(u)
    }
    if (card.kind === 'type') requestAnimationFrame(() => typeRef.current?.focus())
  }, [card?.id, card?.reps]) // eslint-disable-line react-hooks/exhaustive-deps

  // nothing left but a card in learning later today: come back for it
  useEffect(() => {
    if (card || !res?.come_back) return
    const t = setInterval(() => setNow(Date.now()), 1000)
    const wait = new Date(res.come_back).getTime() - Date.now()
    const back = setTimeout(load, Math.max(1000, wait + 500))
    return () => { clearInterval(t); clearTimeout(back) }
  }, [card, res?.come_back, load])

  const reveal = () => {
    if (!card || shown) return
    setShown(true)
    if (settings?.autoplay) { stopSound.current(); const { a, q } = sides(card); stopSound.current = playSounds(soundsOf(a).filter(x => !soundsOf(q).includes(x)).concat(soundsOf(card.extra))) }
  }
  const answer = async (rating: number) => {
    if (!card || busy) return
    setBusy(true)
    const ms = Date.now() - shownAt.current
    try {
      const r = await api.post<{ review_id: number; next: NextResult }>(`${V1}/study/answer`, { card_id: card.id, rating, duration_ms: ms, deck })
      setUndo(u => [...u.slice(-49), r.review_id])
      setSession(s => ({ ...s, cards: s.cards + 1, again: s.again + (rating === 1 ? 1 : 0), ms: s.ms + Math.min(ms, 120000) }))
      setRes(r.next)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const undoLast = async () => {
    const id = undo[undo.length - 1]
    if (!id || busy) return
    setBusy(true)
    try {
      setRes(await api.post<NextResult>(`${V1}/study/undo`, { review_id: id, deck }))
      setUndo(u => u.slice(0, -1))
      setSession(s => ({ ...s, cards: Math.max(0, s.cards - 1) }))
      toast('Answer undone')
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const act = async (action: string, flag?: number) => {
    if (!card) return
    try {
      await api.post(`${V1}/cards/${card.id}/action`, { action, flag })
      if (action === 'flag') { setRes(r => r && r.card ? { ...r, card: { ...r.card, flag: flag || 0 } } : r); return }
      toast(action === 'bury' ? 'Back tomorrow' : 'Set aside — find it in Browse to bring it back')
      load()
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const openEdit = async () => { if (card) try { setEdit(await api.get<Note>(`${V1}/notes/${card.note_id}`)) } catch (e) { toast((e as Error).message, 'err') } }
  const askExplain = async () => {
    if (!card) return
    setExplain({ id: card.id, text: '', busy: true })
    try {
      const r = await api.post<{ ok: boolean; text?: string; error?: string }>(`${V1}/ai/explain`, { card_id: card.id })
      setExplain({ id: card.id, text: r.ok ? r.text || '' : `✕ ${r.error}`, busy: false })
    } catch (e) { setExplain({ id: card.id, text: `✕ ${(e as Error).message}`, busy: false }) }
  }
  const end = async () => {
    stopSound.current()
    if (session.cards > 0 && !summary) { try { await api.post(`${V1}/study/finish`, { deck, cards: session.cards, again: session.again, duration_ms: session.ms }) } catch { /* only a summary */ } }
    navigate(deck ? `/decks/${deck}` : '/')
  }
  // the session ended by itself: publish it once and show the summary
  useEffect(() => {
    if (!res || res.card || res.come_back || summary || session.cards === 0) return
    api.post<{ streak: number }>(`${V1}/study/finish`, { deck, cards: session.cards, again: session.again, duration_ms: session.ms }).then(setSummary).catch(() => setSummary({ streak: 0 }))
  }, [res]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (modalOpen() || document.querySelector('.menu')) return
      const inInput = (e.target as HTMLElement).closest('input, textarea')
      if (e.key === 'Escape') { e.preventDefault(); if (inInput) (e.target as HTMLElement).blur(); else end(); return }
      if (inInput) { if (e.key === 'Enter') { e.preventDefault(); reveal() } return }
      if (e.metaKey || e.ctrlKey || e.altKey) { if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'z') { e.preventDefault(); undoLast() } return }
      if (!card) return
      if (e.key === ' ' || e.key === 'Enter') { e.preventDefault(); if (shown) answer(3); else reveal(); return }
      if (shown && ['1', '2', '3', '4'].includes(e.key)) { e.preventDefault(); answer(Number(e.key)); return }
      switch (e.key) {
        case 'z': case 'u': e.preventDefault(); undoLast(); break
        case 'e': e.preventDefault(); openEdit(); break
        case '-': e.preventDefault(); act('bury'); break
        case '!': e.preventDefault(); act('suspend'); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  })

  const c = res?.counts
  const total = session.cards + (c ? c.new + c.learn + c.review : 0)
  const pct = total ? session.cards / total * 100 : 0
  const cur = card ? (card.state === 0 ? 'new' : card.state === 2 ? 'review' : 'learn') : ''
  const mins = Math.round(session.ms / 60000)
  return (
    <div className="study">
      <header className="study-top">
        <button type="button" className="btn ghost icon-only" onClick={end} aria-label="End" title="End (esc)"><Icon name="x" size={19} /></button>
        <div className="study-title ellipsis">{res?.deck?.name || 'All decks'}</div>
        {c && <span className="study-counts" aria-label="Left today">
          <span className={`dc new ${cur === 'new' ? 'cur' : ''}`} title="New">{c.new}</span>
          <span className={`dc learn ${cur === 'learn' ? 'cur' : ''}`} title="Learning">{c.learn}</span>
          <span className={`dc review ${cur === 'review' ? 'cur' : ''}`} title="Reviews">{c.review}</span>
        </span>}
        <span className="spacer" />
        <button type="button" className="btn ghost icon-only" onClick={undoLast} disabled={!undo.length || busy} aria-label="Undo" title="Undo the last answer (z)"><Icon name="undo" size={18} /></button>
        <MenuButton label="Card" className="btn ghost icon-only" align="right" width={240} items={() => card ? [
          { label: 'Edit the card', icon: 'edit', hint: 'E', onSelect: openEdit },
          { label: 'Bury until tomorrow', icon: 'clock', hint: '−', onSelect: () => act('bury') },
          { label: 'Set aside (suspend)', icon: 'eye-off', hint: '!', onSelect: () => act('suspend') },
          'sep', { heading: 'Flag' },
          ...FLAGS.map(f => ({ label: f.label, checked: card.flag === f.v, onSelect: () => act('flag', f.v) })),
        ] : [{ label: 'No card', disabled: true }]}><Icon name="more" size={18} /></MenuButton>
        <AskButton />
      </header>
      <div className="study-progress"><span style={{ width: `${pct}%` }} /></div>
      <main className="study-main">
        {!res ? <Loading /> : card ? (
          <div className={`study-card ${shown ? 'shown' : ''}`} key={card.id + ':' + card.reps}>
            <div className="sc-meta muted small">
              {card.flag > 0 && <i className="flag-dot" style={{ background: FLAGS[card.flag].color }} />}
              {!res.deck && <span>{card.deck}</span>}
              {card.state === 0 ? <span className="badge st-0">New</span> : card.lapses >= 4 ? <span className="badge warn" title="Forgotten often: maybe rewrite it">Forgotten {card.lapses}×</span> : null}
              {settings?.show_timer && <Timer from={shownAt.current} />}
            </div>
            <Face card={card} shown={shown} typed={typed} />
            {card.kind === 'type' && !shown && <input ref={typeRef} className="type-in" value={typed} onChange={e => setTyped(e.target.value)} placeholder="Type the answer, then Enter" autoComplete="off" autoCapitalize="off" spellCheck={false} />}
            {shown && <div className="sc-tools">
              {card.source_url && <a className="btn ghost sm" href={card.source_url} target="_blank" rel="noreferrer"><Icon name="external" size={14} />{card.source_ref || 'Source'}</a>}
              <button type="button" className="btn ghost sm" onClick={askExplain} disabled={explain?.busy}><Icon name="sparkles" size={14} />{explain?.busy ? 'Thinking…' : 'Explain'}</button>
              <button type="button" className="btn ghost sm" onClick={openEdit}><Icon name="edit" size={14} />Edit</button>
            </div>}
            {explain && explain.id === card.id && !explain.busy && <div className="recap explain">{explain.text.split('\n').filter(Boolean).map((l, i) => <p key={i}>{l}</p>)}</div>}
          </div>
        ) : res.come_back ? (
          <div className="study-done">
            <Icon name="hourglass" size={34} />
            <h2>Back in {countdown(new Date(res.come_back).getTime() - now)}</h2>
            <p className="muted">A card you are learning comes back at {fmtTime(new Date(res.come_back))}. Stay here, or come back later; nothing is lost.</p>
            <div className="row wrap center"><button type="button" className="btn" onClick={end}>Done for now</button></div>
          </div>
        ) : (
          <div className="study-done">
            <Icon name="check-circle" size={40} className="ok" />
            <h2>{session.cards ? 'Well done' : 'Nothing to study here'}</h2>
            <p className="muted">{session.cards ? <>You studied {session.cards} {session.cards === 1 ? 'card' : 'cards'}{mins ? ` in ${mins} min` : ''}{session.cards >= 5 ? ` and remembered ${Math.round((1 - session.again / session.cards) * 100)}%` : ''}.{summary?.streak && summary.streak > 1 ? ` ${summary.streak} days in a row.` : ''}</> : 'Everything due is done for today.'}</p>
            <div className="row wrap center">
              {deck > 0 && <button type="button" className="btn" onClick={async () => { await api.post(`${V1}/decks/${deck}/extend`, { new: 10 }); setSummary(null); load() }}><Icon name="plus" size={15} />10 more new cards</button>}
              <button type="button" className="btn primary" onClick={end}>Back</button>
            </div>
          </div>
        )}
      </main>
      {card && <footer className="study-bar">
        {!shown ? <button type="button" className="btn primary lg block show-btn" onClick={reveal}>Show answer <kbd>space</kbd></button> : (
          <div className="rate-row">
            {RATINGS.map(x => {
              const o = card.answers.find(a => a.rating === x.r)
              return <button key={x.r} type="button" className={`rate ${x.cls}`} onClick={() => answer(x.r)} disabled={busy}>
                {settings?.show_intervals !== false && <span className="rate-ivl">{o?.label}</span>}
                <span className="rate-l">{x.label}</span>
                <kbd>{x.key}</kbd>
              </button>
            })}
          </div>
        )}
      </footer>}
      {edit && <NoteEditor note={edit} onClose={() => setEdit(null)} onSaved={() => load()} onDeleted={() => load()} />}
    </div>
  )
}

function Face({ card, shown, typed }: { card: StudyCard; shown: boolean; typed: string }) {
  const { q, a, cloze } = sides(card)
  return (
    <div className="sc-face">
      <CardText text={q} opts={cloze ? { cloze, reveal: shown } : {}} className="sc-q" />
      {shown && <>
        {card.kind !== 'cloze' && <hr className="sc-hr" />}
        {card.kind === 'type' && <TypedResult typed={typed} want={a} />}
        {card.kind !== 'cloze' && <CardText text={a} className="sc-a" />}
        {card.extra && <CardText text={card.extra} className="sc-extra" />}
      </>}
    </div>
  )
}

function TypedResult({ typed, want }: { typed: string; want: string }) {
  if (!typed.trim()) return <p className="muted small">You did not type an answer.</p>
  const d = typedDiff(typed, want)
  return (
    <div className={`typed ${d.ok ? 'ok' : 'bad'}`}>
      <Icon name={d.ok ? 'check-circle' : 'x-circle'} size={16} />
      <span className="typed-text">{d.typed.map(([ch, ok], i) => <span key={i} className={ok ? '' : 'wrong'}>{ch}</span>)}</span>
    </div>
  )
}

function Timer({ from }: { from: number }) {
  const [, tick] = useState(0)
  useEffect(() => { const t = setInterval(() => tick(x => x + 1), 1000); return () => clearInterval(t) }, [])
  const s = Math.floor((Date.now() - from) / 1000)
  return <span className="tnum">{Math.floor(s / 60)}:{String(s % 60).padStart(2, '0')}</span>
}

function countdown(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000))
  return s >= 60 ? `${Math.ceil(s / 60)} min` : `${s} s`
}
