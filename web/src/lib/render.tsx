// Card text is plain "markdown-lite": **bold**, *italic*, `code`, ``` blocks,
// "- " lists, ![](media/x.png) pictures, [sound:x.mp3] sounds, links and
// cloze gaps {{c1::answer::hint}}. Rendered into React elements — never as
// HTML — so imported or shared cards cannot run anything.
import React, { useRef, useState } from 'react'
import { V1 } from './api'
import { Icon } from '../components/icons'

export interface RenderOpts { cloze?: number; reveal?: boolean }

export const mediaURL = (src: string) => /^https?:\/\//i.test(src) ? src : src.startsWith('media/') ? `${V1}/media/${encodeURIComponent(src.slice(6))}` : ''

const CLOZE = /\{\{c(\d+)::([\s\S]*?)(?:::([\s\S]*?))?\}\}/g

/** The sounds a text plays, in order (file names). */
export function soundsOf(text: string): string[] {
  return [...text.matchAll(/\[sound:([^\]]+)\]/g)].map(m => m[1])
}

/** The text with all markup stripped (for speech, search rows, titles). */
export function plainText(text: string, o: RenderOpts = {}): string {
  return text
    .replace(CLOZE, (_m, n, a, h) => o.cloze && Number(n) === o.cloze && !o.reveal ? (h ? `[${h}]` : '[…]') : a)
    .replace(/!\[[^\]]*\]\([^)]*\)/g, '').replace(/\[sound:[^\]]+\]/g, '')
    .replace(/\[([^\]]+)\]\((https?:[^)]+)\)/g, '$1').replace(/```[a-z]*\n?|```/g, '')
    .replace(/\*\*|__|`/g, '').replace(/(^|\s)\*(\S[^*]*)\*/g, '$1$2').replace(/^\s*[-*] /gm, '').trim()
}

export function CardText({ text, opts = {}, className = '' }: { text: string; opts?: RenderOpts; className?: string }) {
  return <div className={`card-text ${className}`}>{blocks(text, opts)}</div>
}

function blocks(text: string, o: RenderOpts): React.ReactNode[] {
  const out: React.ReactNode[] = []
  const lines = text.replace(/\r\n/g, '\n').split('\n')
  let i = 0, k = 0
  let para: string[] = []
  const flush = () => { if (para.length) { out.push(<p key={k++}>{para.map((l, j) => <React.Fragment key={j}>{j > 0 && <br />}{inline(l, o)}</React.Fragment>)}</p>); para = [] } }
  while (i < lines.length) {
    const l = lines[i]
    if (/^```/.test(l.trim())) {
      flush()
      const code: string[] = []
      i++
      while (i < lines.length && !/^```/.test(lines[i].trim())) code.push(lines[i++])
      i++
      out.push(<pre key={k++}><code>{code.join('\n')}</code></pre>)
      continue
    }
    if (/^\s*[-*] /.test(l)) {
      flush()
      const items: string[] = []
      while (i < lines.length && /^\s*[-*] /.test(lines[i])) items.push(lines[i++].replace(/^\s*[-*] /, ''))
      out.push(<ul key={k++}>{items.map((it, j) => <li key={j}>{inline(it, o)}</li>)}</ul>)
      continue
    }
    if (/^\s*\d+[.)] /.test(l)) {
      flush()
      const items: string[] = []
      while (i < lines.length && /^\s*\d+[.)] /.test(lines[i])) items.push(lines[i++].replace(/^\s*\d+[.)] /, ''))
      out.push(<ol key={k++}>{items.map((it, j) => <li key={j}>{inline(it, o)}</li>)}</ol>)
      continue
    }
    if (/^\s*---+\s*$/.test(l)) { flush(); out.push(<hr key={k++} />); i++; continue }
    if (l.trim() === '') { flush(); i++; continue }
    para.push(l)
    i++
  }
  flush()
  return out
}

// One pass over a line: cloze gaps first (they may hold other markup), then pictures, sounds, links, code, bold, italic.
const TOKEN = /(\{\{c\d+::[\s\S]*?\}\})|!\[([^\]]*)\]\(([^)\s]+)\)|\[sound:([^\]]+)\]|\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)|`([^`]+)`|\*\*([^*]+)\*\*|(?:^|(?<=[\s(]))\*([^*\s][^*]*?)\*(?=$|[\s.,;:!?)])/g

function inline(text: string, o: RenderOpts): React.ReactNode[] {
  const out: React.ReactNode[] = []
  let last = 0, k = 0
  for (const m of text.matchAll(TOKEN)) {
    if (m.index! > last) out.push(text.slice(last, m.index))
    last = m.index! + m[0].length
    if (m[1]) {
      const c = /\{\{c(\d+)::([\s\S]*?)(?:::([\s\S]*?))?\}\}/.exec(m[1])!
      const n = Number(c[1])
      if (o.cloze === n && !o.reveal) out.push(<span key={k++} className="cloze">{c[3] ? `[${c[3]}]` : '[…]'}</span>)
      else if (o.cloze === n) out.push(<span key={k++} className="cloze on">{inline(c[2], {})}</span>)
      else out.push(<React.Fragment key={k++}>{inline(c[2], {})}</React.Fragment>)
    } else if (m[3] !== undefined) {
      const src = mediaURL(m[3])
      if (src) out.push(<img key={k++} src={src} alt={m[2] || ''} className="card-img" loading="lazy" />)
    } else if (m[4]) {
      out.push(<SoundButton key={k++} name={m[4]} />)
    } else if (m[5]) {
      out.push(<a key={k++} href={m[6]} target="_blank" rel="noreferrer noopener">{m[5]}</a>)
    } else if (m[7]) {
      out.push(<code key={k++}>{m[7]}</code>)
    } else if (m[8]) {
      out.push(<b key={k++}>{inline(m[8], o)}</b>)
    } else if (m[9]) {
      out.push(<i key={k++}>{inline(m[9], o)}</i>)
    }
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

function SoundButton({ name }: { name: string }) {
  const ref = useRef<HTMLAudioElement | null>(null)
  const [playing, setPlaying] = useState(false)
  const play = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (!ref.current) { ref.current = new Audio(mediaURL('media/' + name)); ref.current.onended = () => setPlaying(false) }
    if (playing) { ref.current.pause(); ref.current.currentTime = 0; setPlaying(false); return }
    ref.current.play().then(() => setPlaying(true)).catch(() => setPlaying(false))
  }
  return <button type="button" className="sound-btn" onClick={play} aria-label={playing ? 'Stop the sound' : 'Play the sound'} title={name}><Icon name={playing ? 'stop' : 'volume'} size={15} /></button>
}

/** Plays a list of sounds one after another; returns a stop function. */
export function playSounds(names: string[]): () => void {
  let stopped = false
  let cur: HTMLAudioElement | null = null
  const next = (i: number) => {
    if (stopped || i >= names.length) return
    cur = new Audio(mediaURL('media/' + names[i]))
    cur.onended = () => next(i + 1)
    cur.play().catch(() => next(i + 1))
  }
  next(0)
  return () => { stopped = true; cur?.pause() }
}

/** The question and answer sides of a card. */
export function sides(c: { kind: string; ord: number; front: string; back: string }): { q: string; a: string; cloze?: number } {
  if (c.kind === 'cloze') return { q: c.front, a: c.front, cloze: c.ord }
  if (c.kind === 'reverse' && c.ord === 1) return { q: c.back, a: c.front }
  return { q: c.front, a: c.back }
}

/** Character diff of a typed answer against the right one: [char, ok][]. */
export function typedDiff(typed: string, want: string): { typed: [string, boolean][]; ok: boolean } {
  const norm = (s: string) => s.normalize('NFC').trim().replace(/\s+/g, ' ')
  const a = norm(typed), b = norm(plainText(want))
  if (a.toLowerCase() === b.toLowerCase()) return { typed: [[a, true]], ok: true }
  // longest common subsequence, case-insensitive
  const n = a.length, m = b.length
  const dp = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) dp[i][j] = a[i].toLowerCase() === b[j].toLowerCase() ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1])
  const out: [string, boolean][] = []
  let i = 0, j = 0
  while (i < n) {
    if (j < m && a[i].toLowerCase() === b[j].toLowerCase()) { out.push([a[i], true]); i++; j++ }
    else if (j < m && dp[i][j + 1] >= dp[i + 1][j]) j++
    else { out.push([a[i], false]); i++ }
  }
  return { typed: out, ok: false }
}

export const KIND_NAME: Record<string, string> = { basic: 'Question & answer', reverse: 'Both ways', cloze: 'Fill the gap', type: 'Type the answer' }
export const STATE_NAME = ['New', 'Learning', 'Review', 'Relearning']

/** "in 3 days", "in 2 months" for a review interval. */
export function fmtInterval(days: number): string {
  if (days < 1) return 'today'
  if (days === 1) return '1 day'
  if (days < 30) return `${days} days`
  if (days < 365) { const m = Math.round(days / 30.44 * 10) / 10; return `${m} ${m === 1 ? 'month' : 'months'}` }
  const y = Math.round(days / 365.25 * 10) / 10
  return `${y} ${y === 1 ? 'year' : 'years'}`
}

export const FLAGS = [{ v: 0, label: 'No flag', color: '' }, { v: 1, label: 'Red', color: '#e03131' }, { v: 2, label: 'Orange', color: '#f08c00' }, { v: 3, label: 'Green', color: '#2f9e44' }, { v: 4, label: 'Blue', color: '#1c7ed6' }]
