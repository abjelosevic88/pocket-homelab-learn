// Add or edit a card: the deck, what kind of card, both sides, pictures and sounds, tags — with a live preview.
import React, { useEffect, useRef, useState } from 'react'
import { api, upload, V1 } from '../lib/api'
import type { Deck, Kind, Note } from '../lib/types'
import { CardText, KIND_NAME, STATE_NAME, fmtInterval } from '../lib/render'
import { fmtDate } from '../lib/format'
import { Confirm, Field, Modal, Segmented, useAsync, useIsMobile, useToast } from './ui'
import { Select } from './Select'
import { Icon } from './icons'
import { useDecks } from './Layout'

const KINDS: { value: Kind; label: string; title: string }[] = [
  { value: 'basic', label: 'Q & A', title: 'A question on the front, the answer on the back' },
  { value: 'reverse', label: 'Both ways', title: 'Two cards: front → back and back → front (words in two languages)' },
  { value: 'cloze', label: 'Fill the gap', title: 'Hide part of a sentence: {{c1::Paris}} is the capital of France' },
  { value: 'type', label: 'Type it', title: 'Type the answer; it is checked letter by letter' },
]

export function NoteEditor({ note, deckId, onClose, onSaved, onDeleted, initial }: {
  note?: Note; deckId?: number; onClose: () => void; onSaved?: (n: Note) => void; onDeleted?: () => void
  initial?: Partial<Pick<Note, 'front' | 'back' | 'extra' | 'kind' | 'tags'>>
}) {
  const { toast } = useToast()
  const ctx = useDecks()
  const { data: own } = useAsync(() => ctx.decks ? Promise.resolve(ctx.decks) : api.get<Deck[]>(`${V1}/decks`), [])
  const decks = ctx.decks || own
  const mobile = useIsMobile()
  const lastDeck = Number(localStorage.getItem('pln-last-deck')) || 0
  const [f, setF] = useState({
    deck_id: note?.deck_id || deckId || (decks?.some(d => d.id === lastDeck) ? lastDeck : decks?.[0]?.id) || 0,
    kind: (note?.kind || initial?.kind || localStorage.getItem('pln-last-kind') || 'basic') as Kind,
    front: note?.front ?? initial?.front ?? '', back: note?.back ?? initial?.back ?? '', extra: note?.extra ?? initial?.extra ?? '', tags: note?.tags ?? initial?.tags ?? '',
  })
  const [newDeck, setNewDeck] = useState('')
  const [busy, setBusy] = useState(false)
  const [added, setAdded] = useState(0)
  const [del, setDel] = useState(false)
  const [preview, setPreview] = useState(!mobile)
  const [showExtra, setShowExtra] = useState(!!(note?.extra || initial?.extra))
  const front = useRef<HTMLTextAreaElement>(null)
  const focused = useRef<HTMLTextAreaElement | null>(null)
  useEffect(() => { if (!f.deck_id && decks?.length) setF(x => ({ ...x, deck_id: decks[0].id })) }, [decks]) // eslint-disable-line react-hooks/exhaustive-deps

  const set = (k: 'front' | 'back' | 'extra', v: string) => setF(x => ({ ...x, [k]: v }))
  const nextCloze = () => { const ns = [...f.front.matchAll(/\{\{c(\d+)::/g)].map(m => Number(m[1])); return ns.length ? Math.max(...ns) + 1 : 1 }
  // wrap the selection of a textarea (or insert at the cursor)
  const wrap = (el: HTMLTextAreaElement | null, before: string, after = '', placeholder = '') => {
    if (!el) return
    const k = el.dataset.side as 'front' | 'back' | 'extra'
    const v = el.value, a = el.selectionStart, b = el.selectionEnd
    const mid = v.slice(a, b) || placeholder
    const next = v.slice(0, a) + before + mid + after + v.slice(b)
    set(k, next)
    requestAnimationFrame(() => { el.focus(); el.setSelectionRange(a + before.length, a + before.length + mid.length) })
  }
  const gap = () => wrap(front.current, `{{c${nextCloze()}::`, '}}', 'word')
  const onKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    const mod = e.metaKey || e.ctrlKey
    if (mod && e.key === 'Enter') { e.preventDefault(); save() }
    else if (mod && e.key.toLowerCase() === 'b') { e.preventDefault(); wrap(e.currentTarget, '**', '**') }
    else if (mod && e.key.toLowerCase() === 'i') { e.preventDefault(); wrap(e.currentTarget, '*', '*') }
    else if (mod && e.shiftKey && e.key.toLowerCase() === 'c') { e.preventDefault(); if (f.kind !== 'cloze') setF(x => ({ ...x, kind: 'cloze' })); gap() }
  }
  const addFile = async (file: File, el: HTMLTextAreaElement | null) => {
    try {
      const r = await upload<{ name: string }>(`${V1}/media`, 'file', file)
      const tag = file.type.startsWith('audio/') ? `[sound:${r.name}]` : `![](media/${r.name})`
      wrap(el || front.current, tag + ' ', '')
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const onPaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const file = [...e.clipboardData.files].find(x => x.type.startsWith('image/') || x.type.startsWith('audio/'))
    if (file) { e.preventDefault(); addFile(file, e.currentTarget) }
  }
  const onDrop = (e: React.DragEvent<HTMLTextAreaElement>) => {
    const file = [...e.dataTransfer.files][0]
    if (file) { e.preventDefault(); addFile(file, e.currentTarget) }
  }
  const save = async (e?: React.FormEvent) => {
    e?.preventDefault()
    if (busy) return
    setBusy(true)
    try {
      let deck_id = f.deck_id
      if (deck_id === -1) {
        if (!newDeck.trim()) throw new Error('name the new deck')
        const d = await api.post<Deck>(`${V1}/decks`, { name: newDeck.trim() })
        deck_id = d.id
        setF(x => ({ ...x, deck_id: d.id }))
      }
      const body = { deck_id, kind: f.kind, front: f.front, back: f.kind === 'cloze' ? '' : f.back, extra: f.extra, tags: f.tags }
      localStorage.setItem('pln-last-deck', String(deck_id)); localStorage.setItem('pln-last-kind', f.kind)
      if (note) {
        const n = await api.put<Note>(`${V1}/notes/${note.id}`, body)
        toast('Saved'); onSaved?.(n); onClose()
      } else {
        const r = await api.post<{ note: Note; duplicate: boolean }>(`${V1}/notes`, body)
        const n = r.note
        setAdded(a => a + 1)
        toast(`Added: ${n.cards?.length === 1 ? '1 card' : `${n.cards?.length || 1} cards`}`)
        onSaved?.(n)
        setF(x => ({ ...x, front: '', back: '', extra: '' }))
        requestAnimationFrame(() => front.current?.focus())
      }
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const remove = async () => { try { await api.del(`${V1}/notes/${note!.id}`); toast('Card deleted'); onDeleted?.(); onClose() } catch (e) { toast((e as Error).message, 'err') } }
  const deckOptions = [...(decks || []).map(d => ({ value: String(d.id), label: d.name })), { value: '-1', label: 'New deck…' }]
  const ta = (side: 'front' | 'back' | 'extra', label: string, help: string, rows: number, auto = false) => (
    <Field label={label} help={help}>
      <textarea ref={side === 'front' ? front : undefined} data-side={side} rows={rows} value={f[side]} autoFocus={auto} onFocus={e => { focused.current = e.currentTarget }}
        onChange={e => set(side, e.target.value)} onKeyDown={onKey} onPaste={onPaste} onDrop={onDrop} className="note-ta" />
    </Field>
  )
  const clozeN = [...new Set([...f.front.matchAll(/\{\{c(\d+)::/g)].map(m => Number(m[1])))].sort((a, b) => a - b)
  const footer = (
    <div className="row wrap" style={{ width: '100%' }}>
      {note && <button type="button" className="btn ghost danger-text" onClick={() => setDel(true)}><Icon name="trash" size={15} />Delete</button>}
      {!note && added > 0 && <span className="muted small">{added} added</span>}
      <span className="grow" />
      <button type="button" className="btn" onClick={onClose}>{note || added === 0 ? 'Cancel' : 'Done'}</button>
      <button type="button" className="btn primary" disabled={busy} onClick={() => save()} title="⌘/Ctrl + Enter">{note ? 'Save' : 'Add card'}</button>
    </div>
  )
  return (
    <Modal title={note ? 'Edit card' : 'Add a card'} onClose={onClose} size="xl" footer={footer}>
      <form onSubmit={save} className={`note-editor ${preview ? 'with-preview' : ''}`}>
        <div className="stack">
          <div className="two-col tight">
            <Field group label="Deck">
              <Select ariaLabel="Deck" value={String(f.deck_id)} onChange={v => setF(x => ({ ...x, deck_id: Number(v) }))} options={deckOptions} placeholder="Choose a deck…" />
            </Field>
            {f.deck_id === -1 ? <Field label="New deck"><input value={newDeck} onChange={e => setNewDeck(e.target.value)} placeholder="e.g. Spanish" autoFocus /></Field>
              : <Field label="Tags" help="Separate with spaces"><input value={f.tags} onChange={e => setF(x => ({ ...x, tags: e.target.value }))} placeholder="verbs chapter-3" /></Field>}
          </div>
          <Field group label="Kind of card">
            <Segmented size="sm" value={f.kind} onChange={v => setF(x => ({ ...x, kind: v }))} options={KINDS} className="kind-seg" />
          </Field>
          <div className="editor-tools row wrap">
            <button type="button" className="btn sm ghost" title="Bold (⌘B)" onClick={() => wrap(focused.current || front.current, '**', '**')}><b>B</b></button>
            <button type="button" className="btn sm ghost" title="Italic (⌘I)" onClick={() => wrap(focused.current || front.current, '*', '*')}><i>I</i></button>
            <button type="button" className="btn sm ghost" title="Code" onClick={() => wrap(focused.current || front.current, '`', '`')}><Icon name="code" size={14} /></button>
            {f.kind === 'cloze' && <button type="button" className="btn sm" title="Hide the selected words (⌘⇧C)" onClick={gap}><Icon name="eye-off" size={14} />Make a gap</button>}
            <label className="btn sm ghost" title="A picture or a sound (or paste / drop one into a side)"><Icon name="upload" size={14} />Picture or sound<input type="file" accept="image/*,audio/*" hidden onChange={e => { const x = e.target.files?.[0]; if (x) addFile(x, focused.current); e.target.value = '' }} /></label>
            <span className="grow" />
            <button type="button" className={`btn sm ghost ${preview ? 'active' : ''}`} onClick={() => setPreview(p => !p)}><Icon name="eye" size={14} />Preview</button>
          </div>
          {f.kind === 'cloze'
            ? ta('front', 'Text', 'Select words and press “Make a gap”: {{c1::Paris}} is the capital of France. Each gap number is one card.', 4, !note)
            : ta('front', f.kind === 'reverse' ? 'Front (e.g. the word)' : 'Front — the question', f.kind === 'reverse' ? 'You are asked both sides, as two cards.' : '**bold**, *italic*, pictures and sounds work here.', 3, !note)}
          {f.kind !== 'cloze' && ta('back', f.kind === 'type' ? 'The answer to type' : f.kind === 'reverse' ? 'Back (e.g. the meaning)' : 'Back — the answer', f.kind === 'type' ? 'Keep it short: it is compared letter by letter.' : '', 3)}
          {showExtra ? ta('extra', 'More (shown under the answer)', 'An example, a source, a memory aid.', 2) : <button type="button" className="link-btn small left" onClick={() => setShowExtra(true)}>+ Add more (an example, a source)</button>}
        </div>
        {preview && (
          <div className="note-preview">
            <div className="np-h muted small">{KIND_NAME[f.kind]}{f.kind === 'cloze' && clozeN.length > 0 && ` · ${clozeN.length} ${clozeN.length === 1 ? 'card' : 'cards'}`}{f.kind === 'reverse' && ' · 2 cards'}</div>
            <div className="np-card">
              {f.front.trim() ? <CardText text={f.front} opts={f.kind === 'cloze' ? { cloze: clozeN[0] || 1 } : {}} /> : <p className="faint">The front</p>}
              <hr />
              {f.kind === 'cloze' ? (f.front.trim() ? <CardText text={f.front} opts={{ cloze: clozeN[0] || 1, reveal: true }} /> : <p className="faint">The text with the gap filled</p>)
                : f.back.trim() ? <CardText text={f.back} /> : <p className="faint">The back</p>}
              {f.extra.trim() && <CardText text={f.extra} className="extra" />}
            </div>
          </div>
        )}
      </form>
      {note?.cards && note.cards.length > 0 && (
        <div className="note-cards">
          {note.cards.map(c => (
            <span key={c.id} className="chip">
              {note.kind === 'cloze' ? `Gap ${c.ord}` : note.kind === 'reverse' ? (c.ord === 0 ? 'Front → back' : 'Back → front') : 'Card'}: {c.suspended ? 'set aside' : STATE_NAME[c.state].toLowerCase()}
              {c.state === 2 && ` · every ${fmtInterval(c.interval)}`}{c.due && c.state !== 0 && ` · next ${fmtDate(c.due.slice(0, 10), false)}`}{c.lapses > 0 && ` · forgotten ${c.lapses}×`}
            </span>
          ))}
        </div>
      )}
      {del && <Confirm title="Delete this card?" message={note?.kind === 'reverse' || (note?.cards?.length || 0) > 1 ? 'Both of its cards and their history are deleted.' : 'The card and its history are deleted.'} confirmLabel="Delete" onCancel={() => setDel(false)} onConfirm={() => { setDel(false); remove() }} />}
    </Modal>
  )
}
