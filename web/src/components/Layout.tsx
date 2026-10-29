// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Dashboard, Deck } from '../lib/types'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { NoteEditor } from './NoteEditor'
import { DeckModal } from './DeckModal'
import { Avatar, type Anchor, type MenuItem, Menu, MenuButton, modalOpen, rectOf, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'pln-sidebar'

/** Something was added or changed from the shell (or a page): the counts and the pages reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('pln:changed'))
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('pln:changed', h)
    return () => window.removeEventListener('pln:changed', h)
  }, [])
}

/** The page's own name for the top bar breadcrumb. */
const CrumbCtx = createContext<(t: string) => void>(() => {})
export function useCrumb(title: string | undefined) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || ''); return () => set('') }, [title, set])
}

/** The person's decks with today's numbers, for every page that needs them; reload() after a change. */
const DecksCtx = createContext<{ decks: Deck[] | null; dash: Dashboard | null; reload: () => void }>({ decks: null, dash: null, reload: () => {} })
export const useDecks = () => useContext(DecksCtx)

export type CreateKind = 'study' | 'card' | 'ai' | 'import' | 'deck'

/** Open a create action from anywhere; "card" opens the card editor (in a deck when given). */
export const openCreate = (k: CreateKind, deck?: number) => window.dispatchEvent(new CustomEvent('pln:create', { detail: { kind: k, deck } }))

/** The "New" menu: the sidebar's button and the phone's + button. */
export function createItems(onCreate: (k: CreateKind) => void, withStudy = false): MenuItem[] {
  return [
    ...(withStudy ? [{ label: 'Study now', icon: 'play', hint: 'R', onSelect: () => onCreate('study') }, 'sep' as const] : []),
    { label: 'Add a card', icon: 'plus', hint: 'N', onSelect: () => onCreate('card') },
    { label: 'Make cards with AI', icon: 'sparkles', hint: 'A', onSelect: () => onCreate('ai') },
    { label: 'Import (Anki, CSV)', icon: 'upload', hint: 'I', onSelect: () => onCreate('import') },
    'sep',
    { label: 'New deck', icon: 'layers', onSelect: () => onCreate('deck') },
  ]
}

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const [sp, setSp] = useSearchParams()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState('')
  const setCrumb = useCallback((t: string) => setCrumbRaw(t), [])

  const [decks, setDecks] = useState<Deck[] | null>(null)
  const [dash, setDash] = useState<Dashboard | null>(null)
  const load = useCallback(() => {
    api.get<Deck[]>(`${V1}/decks`).then(setDecks).catch(() => {})
    api.get<Dashboard>(`${V1}/dashboard`).then(setDash).catch(() => {})
  }, [])
  useEffect(() => { load() }, [loc.pathname, load])
  useOnChange(load)

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [modal, setModal] = useState<{ kind: 'card'; deck?: number } | { kind: 'deck' } | null>(null)
  const [fabMenu, setFabMenu] = useState<Anchor | null>(null)

  // /?add=1 (the PWA shortcut) opens the card editor
  useEffect(() => { if (sp.get('add')) { setModal({ kind: 'card' }); setSp(new URLSearchParams(), { replace: true }) } }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const create = (k: CreateKind, deck?: number) => {
    closeDrawer()
    switch (k) {
      case 'study': navigate('/study'); break
      case 'card': setModal({ kind: 'card', deck }); break
      case 'ai': navigate('/create'); break
      case 'import': navigate('/import'); break
      case 'deck': setModal({ kind: 'deck' }); break
    }
  }
  const createRef = useRef(create)
  createRef.current = create

  useEffect(() => {
    const open = () => setShortcuts(true)
    const make = (e: Event) => { const d = (e as CustomEvent<{ kind: CreateKind; deck?: number }>).detail; createRef.current(d.kind, d.deck) }
    window.addEventListener('pln:shortcuts', open)
    window.addEventListener('pln:create', make)
    return () => { window.removeEventListener('pln:shortcuts', open); window.removeEventListener('pln:create', make) }
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      const mk = (k: CreateKind) => { e.preventDefault(); createRef.current(k) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'r': mk('study'); break
        case 'n': mk('card'); break
        case 'a': mk('ai'); break
        case 'i': mk('import'); break
        case 'h': go('/'); break
        case 'd': go('/decks'); break
        case 'b': go('/browse'); break
        case 's': go('/stats'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'study', label: 'Study now', icon: 'play', hint: 'R', keywords: 'review learn due', run: () => create('study') },
    { id: 'add', label: 'Add a card', icon: 'plus', hint: 'N', keywords: 'new note flashcard', run: () => create('card') },
    { id: 'ai', label: 'Make cards with AI', icon: 'sparkles', hint: 'A', keywords: 'generate text note highlight', run: () => create('ai') },
    { id: 'import', label: 'Import from Anki or a CSV file', icon: 'upload', hint: 'I', keywords: 'apkg quizlet spreadsheet tsv', run: () => create('import') },
    { id: 'deck', label: 'New deck', icon: 'layers', keywords: 'create collection', run: () => create('deck') },
    { id: 'home', label: 'Today', icon: 'home', hint: 'H', run: () => navigate('/') },
    { id: 'decks', label: 'Decks', icon: 'layers', hint: 'D', run: () => navigate('/decks') },
    { id: 'browse', label: 'Browse cards', icon: 'list', hint: 'B', keywords: 'search find edit', run: () => navigate('/browse') },
    { id: 'stats', label: 'Statistics', icon: 'chart', hint: 'S', keywords: 'stats streak retention forecast heatmap', run: () => navigate('/stats') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'study-set', label: 'Settings: studying', icon: 'brain', keywords: 'retention steps limits day start', run: () => navigate('/settings/study') },
    { id: 'apps', label: 'Settings: connected apps', icon: 'layers', keywords: 'reader library notes core integrations', run: () => navigate('/settings/apps') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  const closeModal = () => setModal(null)
  return (
    <CrumbCtx.Provider value={setCrumb}>
      <DecksCtx.Provider value={{ decks, dash, reload: load }}>
        <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
          {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
          <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
            <Sidebar onCreate={create} onNavigate={closeDrawer} decks={decks} dash={dash} />
          </div>
          <div className="main">
            <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} />
            <main className="content"><div className="content-inner"><Outlet /></div></main>
          </div>
          {mobile && <button type="button" className="fab" aria-label="New" title="New" aria-haspopup="menu" aria-expanded={!!fabMenu} onClick={e => setFabMenu(fabMenu ? null : rectOf(e.currentTarget))}><Icon name="plus" size={24} strokeWidth={2.2} /></button>}
          {fabMenu && <Menu anchor={fabMenu} align="right" items={createItems(create, true)} onClose={() => setFabMenu(null)} width={250} />}
          {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} decks={decks} />}
          {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
          {modal?.kind === 'card' && <NoteEditor deckId={modal.deck} onClose={closeModal} onSaved={() => announceChange()} />}
          {modal?.kind === 'deck' && <DeckModal onClose={closeModal} onSaved={d => { closeModal(); announceChange(); navigate(`/decks/${d.id}`) }} />}
        </div>
      </DecksCtx.Provider>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, [string, string]> = { '': ['Today', '/'], decks: ['Decks', '/decks'], browse: ['Browse', '/browse'], create: ['Make cards', '/create'], import: ['Import', '/import'], stats: ['Statistics', '/stats'] }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: string }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pln:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb ? <><button type="button" className="crumb" onClick={() => navigate(sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb}</span></> : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
