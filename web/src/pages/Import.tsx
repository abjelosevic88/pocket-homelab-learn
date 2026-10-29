// Import: Anki packages (with progress, pictures and sounds) and rows from a CSV or TSV file (Quizlet, a spreadsheet).
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Deck } from '../lib/types'
import { Card, Field, Segmented, SettingRow, Spinner, Switch, useToast } from '../components/ui'
import { Select } from '../components/Select'
import { Icon } from '../components/icons'
import { announceChange, useDecks } from '../components/Layout'

export default function Import() {
  return (
    <div className="stack loose">
      <div className="page-head"><h1 className="grow">Import</h1></div>
      <Anki />
      <CSV />
      <p className="muted small">Moving from another Pocket Learn? Use <Link to="/settings/data">Settings → Backup & data</Link> to export and import everything, with its history. Highlights and notes from your Pocket apps become cards under <Link to="/create">Make cards</Link>.</p>
    </div>
  )
}

function Anki() {
  const navigate = useNavigate()
  const [keep, setKeep] = useState(true)
  const [file, setFile] = useState<File | null>(null)
  const [pct, setPct] = useState<number | null>(null)
  const [result, setResult] = useState<Record<string, number> | null>(null)
  const [err, setErr] = useState('')
  const run = () => {
    if (!file) return
    setErr(''); setResult(null); setPct(0)
    const fd = new FormData()
    fd.append('file', file)
    const x = new XMLHttpRequest()
    x.open('POST', `${V1}/import/anki?progress=${keep ? 1 : 0}`)
    x.upload.onprogress = e => { if (e.lengthComputable) setPct(Math.round(e.loaded / e.total * 100)) }
    x.onload = () => {
      setPct(null)
      let j: Record<string, unknown> = {}
      try { j = JSON.parse(x.responseText) } catch { /* not json */ }
      if (x.status >= 300) { setErr(String(j.error || x.statusText || 'the import failed')); return }
      setResult(j as Record<string, number>); announceChange()
    }
    x.onerror = () => { setPct(null); setErr('the upload broke off') }
    x.send(fd)
  }
  return (
    <Card title="From Anki" sub="An Anki deck or collection export (.apkg or .colpkg): every deck, card, picture and sound. In Anki: File → Export, choose “Anki Deck Package”, tick “Include media”.">
      <div className="stack">
        <label className={`drop ${file ? 'has' : ''}`}>
          <Icon name="upload" size={22} />
          <span>{file ? <><b>{file.name}</b> · {(file.size / 1048576).toFixed(1)} MB</> : 'Choose an .apkg or .colpkg file'}</span>
          <input type="file" accept=".apkg,.colpkg,.zip" hidden onChange={e => { setFile(e.target.files?.[0] || null); setResult(null); setErr('') }} />
        </label>
        <SettingRow title="Keep my progress" help="Cards you already know stay known: their intervals and due dates come along. Off: everything starts as new."><Switch checked={keep} onChange={setKeep} label="Keep progress" /></SettingRow>
        <div className="row">
          <button type="button" className="btn primary" disabled={!file || pct !== null} onClick={run}>{pct !== null ? <><Spinner />{pct < 100 ? `Uploading ${pct}%` : 'Reading the cards…'}</> : 'Import'}</button>
        </div>
        {err && <div className="callout danger"><Icon name="alert" size={15} /><span>{err}</span></div>}
        {result && <div className="callout ok"><Icon name="check-circle" size={15} /><div>
          {result.notes.toLocaleString()} notes ({result.cards.toLocaleString()} cards) imported{result.decks ? ` into ${result.decks} new ${result.decks === 1 ? 'deck' : 'decks'}` : ''}{result.media ? `, ${result.media} pictures and sounds` : ''}.
          {result.already > 0 && ` ${result.already.toLocaleString()} were here already.`}{result.skipped > 0 && ` ${result.skipped} empty ones were skipped.`}
          {' '}<button type="button" className="link-btn" onClick={() => navigate('/decks')}>See your decks</button>
        </div></div>}
      </div>
    </Card>
  )
}

interface Preview { rows: number; columns: number; delimiter: string; sample: string[][] }

function CSV() {
  const { toast } = useToast()
  const { decks } = useDecks()
  const [text, setText] = useState('')
  const [header, setHeader] = useState(false)
  const [delimiter, setDelimiter] = useState('auto')
  const [prev, setPrev] = useState<Preview | null>(null)
  const [cols, setCols] = useState({ front: 0, back: 1, extra: -1, tags_col: -1 })
  const [kind, setKind] = useState('basic')
  const [deck, setDeck] = useState('')
  const [newDeck, setNewDeck] = useState('')
  const [tags, setTags] = useState('')
  const [busy, setBusy] = useState(false)
  const preview = async (t = text, h = header, d = delimiter) => {
    if (!t.trim()) { setPrev(null); return }
    try { setPrev(await api.post<Preview>(`${V1}/import/csv`, { text: t, header: h, delimiter: d, dry_run: true })) } catch (e) { toast((e as Error).message, 'err') }
  }
  const run = async () => {
    setBusy(true)
    try {
      const target = deck === '-1' || !deck ? { deck: newDeck.trim() } : { deck_id: Number(deck) }
      const r = await api.post<{ notes: number; already: number; skipped: number; deck: string }>(`${V1}/import/csv`, { text, header, delimiter, kind, tags, ...cols, ...target })
      toast(`${r.notes} added to ${r.deck}${r.already ? `, ${r.already} were there already` : ''}${r.skipped ? `, ${r.skipped} rows skipped` : ''}`)
      setText(''); setPrev(null); announceChange()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const colOpts = (none: boolean) => [...(none ? [{ value: '-1', label: '—' }] : []), ...Array.from({ length: prev?.columns || 2 }, (_, i) => ({ value: String(i), label: `Column ${i + 1}${prev?.sample[0]?.[i] ? `: ${prev.sample[0][i].slice(0, 24)}` : ''}` }))]
  const readFile = async (f: File) => { const t = await f.text(); setText(t); preview(t) }
  return (
    <Card title="From a CSV or TSV file" sub="One card per row — Quizlet’s export, a spreadsheet saved as CSV, or lines like “word - meaning”. Importing the same rows again adds nothing.">
      <div className="stack">
        <Field label="Rows" help={<>Paste them, or <label className="link-btn">choose a file<input type="file" accept=".csv,.tsv,.txt,text/*" hidden onChange={e => e.target.files?.[0] && readFile(e.target.files[0])} /></label>.</>}>
          <textarea rows={6} value={text} onChange={e => setText(e.target.value)} onBlur={() => preview()} placeholder={'casa\thouse\nperro\tdog'} className="mono" />
        </Field>
        <div className="row wrap">
          <Segmented size="sm" value={delimiter} onChange={v => { setDelimiter(v); preview(text, header, v) }} options={[{ value: 'auto', label: 'Detect' }, { value: 'tab', label: 'Tab' }, { value: ',', label: 'Comma' }, { value: ';', label: 'Semicolon' }]} />
          <label className="check-row"><input type="checkbox" checked={header} onChange={e => { setHeader(e.target.checked); preview(text, e.target.checked) }} />The first row is a header</label>
          <button type="button" className="btn sm" onClick={() => preview()} disabled={!text.trim()}>Preview</button>
        </div>
        {prev && <>
          <p className="muted small">{prev.rows.toLocaleString()} rows, {prev.columns} columns.</p>
          <div className="scroll-x"><table className="table compact"><tbody>{prev.sample.map((r, i) => <tr key={i}>{r.map((c, j) => <td key={j} className={j === cols.front ? 'col-front' : j === cols.back ? 'col-back' : ''}>{c.slice(0, 60)}</td>)}</tr>)}</tbody></table></div>
          <div className="two-col tight">
            <Field group label="Front"><Select ariaLabel="Front column" value={String(cols.front)} onChange={v => setCols({ ...cols, front: Number(v) })} options={colOpts(false)} /></Field>
            <Field group label="Back"><Select ariaLabel="Back column" value={String(cols.back)} onChange={v => setCols({ ...cols, back: Number(v) })} options={colOpts(true)} /></Field>
            <Field group label="More (under the answer)"><Select ariaLabel="Extra column" value={String(cols.extra)} onChange={v => setCols({ ...cols, extra: Number(v) })} options={colOpts(true)} /></Field>
            <Field group label="Tags from"><Select ariaLabel="Tags column" value={String(cols.tags_col)} onChange={v => setCols({ ...cols, tags_col: Number(v) })} options={colOpts(true)} /></Field>
          </div>
          <div className="two-col tight">
            <Field group label="Kind of card"><Select ariaLabel="Kind" value={kind} onChange={setKind} options={[{ value: 'basic', label: 'Question & answer' }, { value: 'reverse', label: 'Both ways' }, { value: 'type', label: 'Type the answer' }, { value: 'cloze', label: 'Fill the gap (front has {{c1::…}})' }]} /></Field>
            <Field label="Tags for all"><input value={tags} onChange={e => setTags(e.target.value)} placeholder="e.g. quizlet" /></Field>
            <Field group label="Deck"><Select ariaLabel="Deck" value={deck} onChange={setDeck} options={[...(decks || []).map((d: Deck) => ({ value: String(d.id), label: d.name })), { value: '-1', label: 'New deck…' }]} placeholder="Choose a deck…" /></Field>
            {(deck === '-1' || !deck) && <Field label="New deck"><input value={newDeck} onChange={e => setNewDeck(e.target.value)} placeholder="e.g. Spanish" /></Field>}
          </div>
          <div className="row"><button type="button" className="btn primary" disabled={busy || (!deck || deck === '-1') && !newDeck.trim()} onClick={run}>{busy ? 'Importing…' : `Import ${prev.rows.toLocaleString()} rows`}</button></div>
        </>}
      </div>
    </Card>
  )
}
