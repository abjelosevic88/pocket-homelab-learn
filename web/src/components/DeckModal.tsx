// Create a deck, or change one's name, colour and daily limits.
import React, { useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Deck } from '../lib/types'
import { Field, Modal, Segmented, SettingRow, Switch, useToast } from './ui'

export const DECK_COLORS = ['#4263eb', '#1c7ed6', '#0ca678', '#2f9e44', '#f08c00', '#e8590c', '#e03131', '#d6336c', '#ae3ec9', '#7048e8', '#495057']

export function DeckModal({ deck, onClose, onSaved }: { deck?: Deck; onClose: () => void; onSaved: (d: Deck) => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({
    name: deck?.name || '', description: deck?.description || '', color: deck?.color || DECK_COLORS[0],
    new_per_day: deck?.new_per_day ?? settings?.new_per_day ?? 20, reviews_per_day: deck?.reviews_per_day ?? settings?.reviews_per_day ?? 200,
    retention: deck?.retention || 0, new_order: deck?.new_order || 'added', archived: deck?.archived || false,
  })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { const d = deck ? await api.put<Deck>(`${V1}/decks/${deck.id}`, f) : await api.post<Deck>(`${V1}/decks`, f); toast(deck ? 'Deck saved' : 'Deck created'); onSaved(d) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const def = Math.round((settings?.retention || 0.9) * 100)
  return (
    <Modal title={deck ? 'Deck options' : 'New deck'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={120} placeholder="e.g. Spanish, Anatomy, Driving test" /></Field>
        <Field label="About (optional)"><input value={f.description} onChange={e => setF({ ...f, description: e.target.value })} /></Field>
        <Field group label="Colour">
          <div className="row wrap">{DECK_COLORS.map(c => <button key={c} type="button" className={`swatch ${f.color === c ? 'active' : ''}`} style={{ background: c }} aria-label={c} onClick={() => setF({ ...f, color: c })} />)}</div>
        </Field>
        <div className="two-col tight">
          <Field label="New cards a day" help="How many unseen cards you learn each day."><input type="number" min={0} max={9999} value={f.new_per_day} onChange={e => setF({ ...f, new_per_day: Number(e.target.value) })} /></Field>
          <Field label="Reviews a day (at most)" help="Cards over the limit wait until tomorrow."><input type="number" min={0} max={99999} value={f.reviews_per_day} onChange={e => setF({ ...f, reviews_per_day: Number(e.target.value) })} /></Field>
        </div>
        <SettingRow title="How sure to be" help={`The chance you still remember a card when it comes back. Higher means more reviews. ${f.retention ? '' : `Your default (${def}%).`}`}>
          <div className="row"><input type="range" min={70} max={99} value={Math.round((f.retention || settings?.retention || 0.9) * 100)} onChange={e => setF({ ...f, retention: Number(e.target.value) / 100 })} aria-label="Retention" /><span className="tnum small">{Math.round((f.retention || settings?.retention || 0.9) * 100)}%</span>{f.retention !== 0 && <button type="button" className="link-btn small" onClick={() => setF({ ...f, retention: 0 })}>Default</button>}</div>
        </SettingRow>
        <SettingRow title="New cards come"><Segmented size="sm" value={f.new_order} onChange={v => setF({ ...f, new_order: v })} options={[{ value: 'added', label: 'In the order added' }, { value: 'random', label: 'Shuffled' }]} /></SettingRow>
        {deck && <SettingRow title="Archived" help="Hidden from Today and not studied; nothing is lost."><Switch checked={f.archived} onChange={v => setF({ ...f, archived: v })} label="Archived" /></SettingRow>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{deck ? 'Save' : 'Create deck'}</button></div>
      </form>
    </Modal>
  )
}
