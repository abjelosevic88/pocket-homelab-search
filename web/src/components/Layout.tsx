// The app shell: sidebar, top bar, command palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Dashboard } from '../lib/types'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { Avatar, type Anchor, type MenuItem, Menu, MenuButton, modalOpen, rectOf, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'psr-sidebar'

/** Something was added or changed from the shell (or a page): the sidebar counts and the pages reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('psr:changed'))
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('psr:changed', h)
    return () => window.removeEventListener('psr:changed', h)
  }, [])
}

/** The page's own name for the top bar breadcrumb (the search, the article). */
const CrumbCtx = createContext<(t: string) => void>(() => {})
export function useCrumb(title: string | undefined) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || ''); return () => set('') }, [title, set])
}

/** The person's summary (counts, recent searches), for every page that needs it. */
const ShellCtx = createContext<{ dash: Dashboard | null; reload: () => void }>({ dash: null, reload: () => {} })
export const useShell = () => useContext(ShellCtx)

/** Pages with a search box listen for this ("/" or the + button). */
export const focusSearch = () => window.dispatchEvent(new CustomEvent('psr:focus-search'))

/** The create menu: the sidebar's "New" button and the phone's + button. */
export function createItems(navigate: (to: string) => void): MenuItem[] {
  return [
    { label: 'New search', icon: 'search', hint: 'N', onSelect: () => { navigate('/'); setTimeout(focusSearch, 50) } },
    { label: 'Raise, lower or hide a site', icon: 'filter', onSelect: () => navigate('/sites?add=1') },
    { label: 'Add a bang', icon: 'zap', onSelect: () => navigate('/bangs?add=1') },
  ]
}

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  // Results and the reading view open without the sidebar (like a search engine's page); the
  // menu button shows it for this visit only, the saved choice is for the other pages.
  const focusPage = loc.pathname === '/search' || loc.pathname === '/read'
  const [focusOpen, setFocusOpen] = useState(false)
  useEffect(() => { if (focusPage) setFocusOpen(false) }, [focusPage])
  const open = !drawer && focusPage ? focusOpen : sidebarOpen
  const toggle = () => { if (!drawer && focusPage) setFocusOpen(o => !o); else toggleSidebar() }
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState('')
  const setCrumb = useCallback((t: string) => setCrumbRaw(t), [])

  const [dash, setDash] = useState<Dashboard | null>(null)
  const load = useCallback(() => {
    api.get<Dashboard>(`${V1}/dashboard`).then(setDash).catch(() => {})
  }, [])
  useEffect(() => { load() }, [loc.pathname, loc.search, load])
  useOnChange(load)

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [fabMenu, setFabMenu] = useState<Anchor | null>(null)

  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('psr:shortcuts', open)
    return () => window.removeEventListener('psr:shortcuts', open)
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      switch (e.key) {
        case '/': e.preventDefault(); if (loc.pathname === '/' || loc.pathname === '/search') focusSearch(); else { navigate('/'); setTimeout(focusSearch, 50) } break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'n': e.preventDefault(); navigate('/'); setTimeout(focusSearch, 50); break
        case 'h': go('/history'); break
        case 's': go('/sites'); break
        case 'b': go('/bangs'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggle(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'new', label: 'New search', icon: 'search', hint: 'N', keywords: 'web find', run: () => { navigate('/'); setTimeout(focusSearch, 50) } },
    { id: 'apps', label: 'Search your apps', icon: 'layers', keywords: 'notes tasks documents pocket', run: () => navigate('/search?cat=apps') },
    { id: 'history', label: 'History', icon: 'clock', hint: 'H', keywords: 'recent searches opened', run: () => navigate('/history') },
    { id: 'sites', label: 'Sites: raise, lower or hide', icon: 'filter', hint: 'S', keywords: 'block boost pin rules domains', run: () => navigate('/sites') },
    { id: 'bangs', label: 'Bangs', icon: 'zap', hint: 'B', keywords: 'shortcuts ! redirect', run: () => navigate('/bangs') },
    { id: 'engines', label: 'Settings: search engines', icon: 'globe', keywords: 'google bing duckduckgo brave searxng', run: () => navigate('/settings/engines') },
    { id: 'connected', label: 'Settings: connected apps', icon: 'link', keywords: 'pocket core reader notes tasks', run: () => navigate('/settings/apps') },
    { id: 'browser', label: 'Use as your browser\'s search engine', icon: 'monitor', keywords: 'safari chrome firefox opensearch default iphone', run: () => navigate('/settings/browser') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'ai', label: 'Settings: AI answers', icon: 'sparkles', keywords: 'pocket ai openai answer', run: () => navigate('/settings/ai') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  return (
    <CrumbCtx.Provider value={setCrumb}>
      <ShellCtx.Provider value={{ dash, reload: load }}>
        <div className={`app ${open ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''} ${loc.pathname === '/search' ? 'is-results' : ''}`}>
          {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
          <div className={`sidebar-wrap ${open ? 'open' : ''}`}>
            <Sidebar onNavigate={closeDrawer} dash={dash} />
          </div>
          <div className="main">
            <TopBar onMenu={toggle} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={open} crumb={crumb} results={loc.pathname === '/search'} />
            <main className="content"><div className="content-inner"><Outlet /></div></main>
          </div>
          {mobile && loc.pathname !== '/' && loc.pathname !== '/read' && <button type="button" className="fab" aria-label="New" title="New" aria-haspopup="menu" aria-expanded={!!fabMenu} onClick={e => setFabMenu(fabMenu ? null : rectOf(e.currentTarget))}><Icon name="plus" size={26} strokeWidth={2.2} /></button>}
          {fabMenu && <Menu anchor={fabMenu} align="right" items={createItems(navigate)} onClose={() => setFabMenu(null)} width={250} />}
          {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} />}
          {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
        </div>
      </ShellCtx.Provider>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, [string, string]> = { '': ['Search', '/'], search: ['Search', '/'], read: ['Reading view', '/'], history: ['History', '/history'], sites: ['Sites', '/sites'], bangs: ['Bangs', '/bangs'] }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb, results }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: string; results: boolean }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('psr:shortcuts')), hint: '?' },
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
      {results ? <>
        {/* results: the logo home and the search box (Results puts it in the slot), like a search engine */}
        {!mobile && !sidebarOpen && <Link to="/" className="tb-brand" title="Pocket Search home"><img src="/favicon.svg" alt="" /><span>Pocket <b>Search</b></span></Link>}
        <div id="psr-topbar-slot" className="topbar-slot" />
      </> : <>
        <h1 className="range-title">
          {crumb ? <><button type="button" className="crumb" onClick={() => navigate(sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb}</span></> : section}
        </h1>
        <span className="spacer" />
      </>}
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Commands" title="Commands (⌘K)"><Icon name="command" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
