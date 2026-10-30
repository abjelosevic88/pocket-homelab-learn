import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, ConnMode, Settings as SettingsT, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { Avatar, Confirm, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'

type TabId = 'study' | 'reminders' | 'apps' | 'ai' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'study', label: 'Studying', icon: 'brain', sub: 'How sure you want to be to remember, how many cards a day, the learning steps and the study screen.' },
  { id: 'reminders', label: 'Reminders', icon: 'bell', sub: 'A nudge when cards are due, your streak in the evening and the weekly report.' },
  { id: 'apps', label: 'Connected apps', icon: 'layers', sub: 'Pocket Reader and Pocket Library (your highlights become cards), Pocket Notes (make cards from a note) and Pocket Time (your study timer).' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: 'Who writes cards from your texts and explains them: Pocket AI through Core, or your own OpenAI key.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for Homepage widgets, Siri Shortcuts and scripts.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Download the database, export or import your decks and their history.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const tab: TabId = TABS.find(t => t.id === raw)?.id || 'study'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])
  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title"><h2>{current.label}</h2><p>{current.sub}</p></div>
            {tab === 'study' && <Study />}
            {tab === 'reminders' && <Reminders />}
            {tab === 'apps' && <Integrations />}
            {tab === 'ai' && <AITab />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

const DAYS: [number, string][] = [[1, 'Monday'], [2, 'Tuesday'], [3, 'Wednesday'], [4, 'Thursday'], [5, 'Friday'], [6, 'Saturday'], [7, 'Sunday']]
const HOURS = Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: `${String(h).padStart(2, '0')}:00` }))

function Study() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  const [voices, setVoices] = useState<SpeechSynthesisVoice[]>([])
  useEffect(() => {
    if (!('speechSynthesis' in window)) return
    const load = () => setVoices(speechSynthesis.getVoices())
    load()
    speechSynthesis.addEventListener('voiceschanged', load)
    return () => speechSynthesis.removeEventListener('voiceschanged', load)
  }, [])
  if (!f) return <Loading />
  const langs = [...new Set(voices.map(v => v.lang))].sort()
  const r = Math.round(f.retention * 100)
  return (
    <div className="stack loose">
      <Section title="Remembering" icon="brain" sub="Pocket Learn uses FSRS, the scheduler Anki uses today: it learns how well you remember each card and shows it again just before you would forget it.">
        <SettingRow title="How sure to be" help={`The chance you still remember a card when it comes back. ${r >= 95 ? 'Very sure: many more reviews.' : r <= 80 ? 'Fewer reviews, more forgetting.' : '90% is a good balance for most people.'}`}>
          <div className="row"><input type="range" min={70} max={99} value={r} onChange={e => setF({ ...f, retention: Number(e.target.value) / 100 })} aria-label="Retention" /><span className="tnum small">{r}%</span></div>
        </SettingRow>
        <SettingRow title="Learning steps" help="When a new card comes back on the first day, e.g. “1m 10m” (minutes m, hours h, days d)."><input value={f.learning_steps} onChange={e => setF({ ...f, learning_steps: e.target.value })} className="set-short" /></SettingRow>
        <SettingRow title="After forgetting" help="When a card you forgot comes back, e.g. “10m”."><input value={f.relearning_steps} onChange={e => setF({ ...f, relearning_steps: e.target.value })} className="set-short" /></SettingRow>
        <SettingRow title="Longest interval" help="In days: a card never waits longer than this."><input type="number" min={1} max={36500} value={f.max_interval} onChange={e => setF({ ...f, max_interval: Number(e.target.value) })} className="set-short" /></SettingRow>
      </Section>
      <Section title="Every day" icon="calendar">
        <SettingRow title="New cards a day" help="For new decks; each deck has its own in its options."><input type="number" min={0} max={9999} value={f.new_per_day} onChange={e => setF({ ...f, new_per_day: Number(e.target.value) })} className="set-short" /></SettingRow>
        <SettingRow title="Reviews a day, at most" help="For new decks."><input type="number" min={0} max={99999} value={f.reviews_per_day} onChange={e => setF({ ...f, reviews_per_day: Number(e.target.value) })} className="set-short" /></SettingRow>
        <SettingRow title="New cards come" help="Among the day’s reviews."><Segmented size="sm" value={f.new_mix} onChange={v => setF({ ...f, new_mix: v })} options={[{ value: 'mix', label: 'Mixed in' }, { value: 'first', label: 'First' }, { value: 'last', label: 'Last' }]} /></SettingRow>
        <SettingRow title="Daily goal" help="Cards a day, for the bar on Today. 0 = no goal."><input type="number" min={0} max={9999} value={f.daily_goal} onChange={e => setF({ ...f, daily_goal: Number(e.target.value) })} className="set-short" /></SettingRow>
        <SettingRow title="A new day starts at" help="Studying after midnight still counts for the evening before."><Select ariaLabel="Day start" value={String(f.day_start_hour)} onChange={v => setF({ ...f, day_start_hour: Number(v) })} options={HOURS.slice(0, 13)} /></SettingRow>
      </Section>
      <Section title="The study screen" icon="sliders">
        <SettingRow title="Show when cards come back" help="The next interval on each answer button (“10m”, “4d”)."><Switch checked={f.show_intervals} onChange={v => setF({ ...f, show_intervals: v })} label="Show intervals" /></SettingRow>
        <SettingRow title="Show a timer"><Switch checked={f.show_timer} onChange={v => setF({ ...f, show_timer: v })} label="Timer" /></SettingRow>
        <SettingRow title="Play sounds by themselves" help="A card’s sounds play when it is shown and when the answer is."><Switch checked={f.autoplay} onChange={v => setF({ ...f, autoplay: v })} label="Autoplay" /></SettingRow>
        <SettingRow title="Read the question aloud" help="With your device’s voice — good for language decks."><Switch checked={f.speak} onChange={v => setF({ ...f, speak: v })} label="Read aloud" /></SettingRow>
        {f.speak && langs.length > 0 && <SettingRow title="Language of the voice"><Select ariaLabel="Voice language" value={f.speak_lang} onChange={v => setF({ ...f, speak_lang: v })} options={[{ value: '', label: 'The device’s own' }, ...langs.map(l => ({ value: l, label: l }))]} /></SettingRow>}
      </Section>
      <Section title="Dates" icon="calendar">
        <SettingRow title="Week starts on"><Segmented size="sm" value={f.week_start} onChange={v => setF({ ...f, week_start: v })} options={[{ value: 1, label: 'Monday' }, { value: 6, label: 'Saturday' }, { value: 7, label: 'Sunday' }]} /></SettingRow>
        <SettingRow title="Date format"><Select ariaLabel="Date format" value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Time format"><Select ariaLabel="Time format" value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        {isAdmin && <SettingRow title="App title" help="Shown in the sidebar for everyone on this box."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} /></SettingRow>}
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function Reminders() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const weekly = async () => { try { const r = await api.post<{ ok: boolean; sent?: boolean; error?: string; report?: string }>(`${V1}/notify/weekly`); if (r.ok) toast(r.sent ? 'Weekly report sent' : 'Report ready (no push URL): ' + (r.report || '').split('\n')[0]); else toast(r.error || 'failed', 'err') } catch (e) { toast((e as Error).message, 'err') } }
  const toggleDay = (d: number) => { const s = String(d); setF({ ...f, reminder_days: f.reminder_days.includes(s) ? f.reminder_days.replace(s, '') : [...f.reminder_days, s].sort().join('') }) }
  return (
    <div className="stack loose">
      <Section title="Push" icon="bell" actions={<span className={`badge ${f.notify_configured ? 'ok' : ''}`}>{f.notify_configured ? 'on' : 'not set'}</span>}
        sub={<>Where reminders go: an Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON.</>}>
        <SettingRow title="URL" stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://apprise:8000/notify/learn" /></SettingRow>
        <SettingRow title="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></SettingRow>
      </Section>
      <Section title="Study reminder" icon="clock" sub="Only sent when cards are due. Server time.">
        <SettingRow title="At" help="Empty = no reminder."><div className="row"><input type="time" value={f.reminder_time} onChange={e => setF({ ...f, reminder_time: e.target.value })} className="set-time" aria-label="Reminder time" />{f.reminder_time && <button type="button" className="btn ghost sm" onClick={() => setF({ ...f, reminder_time: '' })}>Off</button>}</div></SettingRow>
        <SettingRow title="On" stack>
          <div className="row wrap">{DAYS.map(([d, l]) => <button key={d} type="button" className={`chip day-chip ${f.reminder_days.includes(String(d)) ? 'on' : ''}`} onClick={() => toggleDay(d)}>{l.slice(0, 3)}</button>)}</div>
        </SettingRow>
        <SettingRow title="When at least" help="Cards due for the reminder to go out."><input type="number" min={1} max={9999} value={f.reminder_min} onChange={e => setF({ ...f, reminder_min: Number(e.target.value) })} className="set-time" /></SettingRow>
      </Section>
      <Section title="Other pushes" icon="flame">
        <SettingRow title="Streak at risk" help="One evening nudge when you have a streak and nothing studied yet today.">
          <div className="row"><Switch checked={f.notify_streak} onChange={v => setF({ ...f, notify_streak: v })} label="Streak nudge" />{f.notify_streak && <Select ariaLabel="Streak hour" value={String(f.streak_hour)} onChange={v => setF({ ...f, streak_hour: Number(v) })} options={HOURS} />}</div>
        </SettingRow>
        <SettingRow title="Streak milestones" help="7, 30, 100, 365 days in a row."><Switch checked={f.notify_milestones} onChange={v => setF({ ...f, notify_milestones: v })} label="Milestones" /></SettingRow>
        <SettingRow title="Weekly report" help="Cards studied, how much you remembered, what is coming up.">
          <div className="row wrap"><Switch checked={f.notify_weekly} onChange={v => setF({ ...f, notify_weekly: v })} label="Weekly report" />{f.notify_weekly && <><Select ariaLabel="Weekly day" value={String(f.weekly_day)} onChange={v => setF({ ...f, weekly_day: Number(v) })} options={DAYS.map(([d, l]) => ({ value: String(d), label: l }))} /><Select ariaLabel="Weekly hour" value={String(f.weekly_hour)} onChange={v => setF({ ...f, weekly_hour: Number(v) })} options={HOURS} /></>}</div>
        </SettingRow>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}><Icon name="mail" size={15} />Send a test</button>
        <button type="button" className="btn" onClick={weekly}><Icon name="chart" size={15} />Send the weekly report now</button>
        {status('notify')}
      </div>
    </div>
  )
}

const APPS: { slug: 'reader' | 'library' | 'notes' | 'time'; name: string; icon: string; sub: string; port: number; where: string }[] = [
  { slug: 'reader', name: 'Pocket Reader — your highlights in articles', icon: 'rss', sub: 'The passages you highlight while reading become cards: as they are, or as questions the AI writes about them (Make cards → Reader highlights).', port: 8211, where: 'Pocket Reader → Settings → API & shortcuts' },
  { slug: 'library', name: 'Pocket Library — your highlights in books', icon: 'journal', sub: 'Highlights and notes you made in your books (in the browser or on KOReader) become cards.', port: 8204, where: 'Pocket Library → Settings → API & shortcuts' },
  { slug: 'notes', name: 'Pocket Notes — make cards from a note', icon: 'note', sub: 'Pick one of your notes and the AI writes cards from it.', port: 8182, where: 'Pocket Notes → Settings → API & shortcuts' },
  { slug: 'time', name: 'Pocket Time — your study timer', icon: 'timer', sub: 'Studying starts your one timer in Pocket Time, and finishing stops it: start in Learn and it runs there, stop it in either place and both stop. Pocket Time then keeps the time and writes it back here as a study session. If another timer is running, Learn asks before switching. Off: Learn times your sessions on its own.', port: 8219, where: 'Pocket Time → Settings → API & shortcuts' },
]

function Integrations() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const eff = (m: ConnMode): ConnMode => !coreOn && m === 'core' ? 'off' : m
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>These connections are yours. Through <b>Pocket Core</b> Learn reaches these apps as you, with nothing to paste (switch it off for yourself in Pocket Core → Settings → Apps acting for me); <b>Direct</b> uses a URL and an API token you enter. Learn only reads from Reader, Library and Notes; in Pocket Time it only starts and stops your timer.{!coreOn && <CoreHint />}</div></div>
      {APPS.map(a => (
        <Section key={a.slug} title={a.name} icon={a.icon} actions={connBadge(eff(f[`${a.slug}_mode`]), f[`${a.slug}_via`])} sub={a.sub}>
          <Connection app={a.name.split(' — ')[0]} mode={f[`${a.slug}_mode`]} via={f[`${a.slug}_via`]} detail={f[`${a.slug}_detail`]} core={coreOn} hasToken={!!f[`${a.slug}_token`]}
            onMode={m => save({ [`${a.slug}_mode`]: m } as Partial<SettingsT>)} onDisconnect={() => save({ [`${a.slug}_url`]: '', [`${a.slug}_token`]: '' } as Partial<SettingsT>)}
            test={<><button type="button" className="btn sm" onClick={() => run(a.slug, f[`${a.slug}_url`], f[`${a.slug}_token`])}>Test connection</button>{status(a.slug)}</>}
            direct={<div className="two-col tight">
              <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={f[`${a.slug}_url`]} onChange={e => setF({ ...f, [`${a.slug}_url`]: e.target.value })} placeholder={`http://host.docker.internal:${a.port}`} /></Field>
              <Field label="API token" help={a.where}><input value={f[`${a.slug}_token`]} onChange={e => setF({ ...f, [`${a.slug}_token`]: e.target.value })} autoComplete="off" /></Field>
            </div>} />
        </Section>
      ))}
      <Section title="Other Pocket apps" icon="layers" sub="Learn offers your studying to the rest of the box through Pocket Core: Pocket Day writes what you studied into your journal, a dashboard shows what is due, another app can add a card (a word from a note, a passage you highlighted), Pocket Time shows your study sessions with the rest of your time.">
        <dl className="set-dl">
          <div><dt>learn:read</dt><dd className="muted">Decks, cards due, what you studied each day, your streak — never the cards themselves.</dd></div>
          <div><dt>learn:write</dt><dd className="muted">Add cards; an app can change or remove only the cards it added itself.</dd></div>
          <div><dt>worklog:read</dt><dd className="muted">Your study sessions — which deck, when, how long — for Pocket Time. Never the cards.</dd></div>
          <div><dt>worklog:write</dt><dd className="muted">Log study time for a deck (Pocket Time writes back a timer you stopped there); it can change or remove only the sessions it logged.</dd></div>
        </dl>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function AITab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { data: ai } = useAsync(() => api.get<{ ready: boolean; provider: string; detail: string }>(`${V1}/ai/status`), [])
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="What the AI does" icon="sparkles" sub="Writes cards from a text you paste, one of your notes or your highlights (Make cards), explains a card while you study, and answers questions about your studying. Nothing is sent unless you press the button, and nothing is saved until you pick the cards.">
        {ai && (ai.ready ? <p className="muted small row" style={{ gap: 6 }}><Icon name="sparkles" size={13} />{ai.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} is ready.</p>
          : <div className="callout info"><Icon name="info" size={15} /><div>No AI connected{ai.detail ? ` (${ai.detail})` : ''}.{!coreOn && <CoreHint />}</div></div>)}
      </Section>
      <Section title="Provider" icon="sparkles">
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Who writes" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings.">
            <Select ariaLabel="AI provider" value={shown} onChange={v => setF({ ...f, ai_provider: v as SettingsT['ai_provider'] })} options={[{ value: 'auto', label: coreOn ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !coreOn, hint: coreOn ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} />
          </Field>
          {!coreOn && <CoreHint />}
          {coreOn && shown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(shown === 'openai' || (!coreOn && shown === 'auto')) && <>
            <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
            <div className="two-col tight">
              <Field label="Model" help="Default gpt-4o-mini."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
              <Field label="Base URL (optional)" help="Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
            </div>
          </>}
        </div>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

function Connection({ app, mode: saved, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const [advanced, setAdvanced] = useState(saved === 'direct')
  const mode: ConnMode = !core && saved === 'core' ? 'off' : saved
  const ok = via !== '' && mode !== 'off'
  const showDirect = !core || mode === 'direct' || advanced
  const line = mode === 'off' ? (app === 'Pocket Time' ? 'Off: Learn times your study sessions on its own.' : `Off: Learn does not read from ${app}.`)
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${detail ? ` (${detail})` : ''}. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} URL and token; use Test connection to check it.` : `Not connected: ${detail || 'add the URL and a token'}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', title: core ? 'Through Pocket Core: per person, nothing to paste' : 'Needs Pocket Core: there is none on this box', disabled: !core },
          ...(showDirect ? [{ value: 'direct', label: 'Direct', title: 'Your own URL and API token' }] : []),
          { value: 'off', label: 'Off' },
        ]} />
        {!showDirect && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and key</button>}
      </div>
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge ok">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own decks, cards, progress and connections. Admins manage accounts; they do not see anyone else’s cards.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with all their decks, cards and history." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/dashboard</code>) or add a card from a Shortcut. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Endpoints" icon="code" sub={<>At <code>{base}/api/v1</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>POST /notes</code></dt><dd className="muted"><code>{'{"deck":"Spanish","kind":"reverse","front":"la casa","back":"the house"}'}</code> — add a card (a Siri Shortcut, a script).</dd></div>
          <div><dt><code>GET /dashboard</code></dt><dd className="muted">Cards due today, studied today, the streak — for Homepage.</dd></div>
          <div><dt><code>GET /decks</code></dt><dd className="muted">Your decks with today’s numbers.</dd></div>
          <div><dt><code>GET /stats?range=m</code></dt><dd className="muted">Everything on the Statistics page.</dd></div>
          <div><dt><code>GET /day/YYYY-MM-DD</code></dt><dd className="muted">One day’s studying — what Pocket Day reads.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const importJSON = async (file: File) => {
    setBusy(true)
    try { const r = await api.post<Record<string, number>>(`${V1}/import`, JSON.parse(await file.text())); toast('Imported: ' + (Object.entries(r).map(([k, v]) => `${v} ${k}`).join(', ') || 'nothing new')); window.dispatchEvent(new CustomEvent('pln:changed')) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Restore by stopping the app and replacing <code>pocket-learn.db</code> in the data directory. Pictures and sounds are files next to it (<code>files/</code>).</p>
        <a className="btn primary" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
      <Section title="Export & import" icon="archive" sub="Everything of yours as one JSON file (decks, cards with where they stand, and every answer), or the cards as a CSV. Importing the same file twice adds nothing. Anki decks and CSV rows: Import in the sidebar menu.">
        <div className="row wrap"><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export JSON</a><a className="btn" href={`${V1}/export.csv`}><Icon name="download" size={15} />Cards CSV</a><label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Import JSON…'}<input type="file" accept="application/json,.json" hidden onChange={e => e.target.files?.[0] && importJSON(e.target.files[0])} /></label></div>
      </Section>
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const core = (data.core || {}) as { configured?: boolean; registered?: boolean; url?: string; error?: string }
  const tiles: [string, number | undefined, string][] = [['Decks', counts.decks, 'layers'], ['Cards', counts.cards, 'note'], ['Answers', counts.reviews, 'check-circle'], ['Pictures & sounds', counts.media, 'upload']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Pocket Core', core.configured ? (core.registered ? <span key="c" className="ok">connected · {core.url}</span> : <span key="c" className="bad">{core.error || 'not registered yet'}</span>) : <span key="c" className="muted">not configured</span>],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))}</span>],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Learn</h3>
          <p className="muted small">Learn anything and keep it: flashcards with spaced repetition (FSRS) that show each card just before you would forget it. Decks, cloze and type-in cards, pictures and sounds, Anki and CSV import, cards made by AI from a text, a note or your highlights, streaks and statistics. One binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{(n ?? 0).toLocaleString()}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <Link className="btn" to="/stats"><Icon name="chart" size={15} />Statistics</Link>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-learn" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Reader, Library, Notes, Day and more. The scheduler is FSRS-5 (open-spaced-repetition).</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
