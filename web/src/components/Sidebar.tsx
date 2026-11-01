// Left column: the create button, sections with counts and the person's recent searches — the Pocket Calendar sidebar.
import { type ReactNode, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import type { Dashboard } from '../lib/types'
import { Icon } from './icons'
import { createItems } from './Layout'
import { type Anchor, Menu, rectOf } from './ui'

export function Sidebar({ onNavigate, dash }: { onNavigate: () => void; dash: Dashboard | null }) {
  const { status, settings } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const title = settings?.title || status?.title || 'Pocket Search'
  const q = new URLSearchParams(loc.search)
  const nav: { to: string; label: string; icon: string; active: boolean; count?: number; title?: string }[] = [
    { to: '/', label: 'Search', icon: 'search', active: loc.pathname === '/' || (loc.pathname === '/search' && q.get('cat') !== 'apps') },
    { to: '/search?cat=apps' + (q.get('q') ? '&q=' + encodeURIComponent(q.get('q')!) : ''), label: 'Your apps', icon: 'layers', active: loc.pathname === '/search' && q.get('cat') === 'apps' },
    { to: '/history', label: 'History', icon: 'clock', active: loc.pathname === '/history', count: dash?.searches_week, title: 'searches this week' },
    { to: '/sites', label: 'Sites', icon: 'filter', active: loc.pathname === '/sites', count: dash?.rules, title: 'site rules' },
    { to: '/lenses', label: 'Lenses', icon: 'columns', active: loc.pathname === '/lenses' },
    { to: '/bangs', label: 'Bangs', icon: 'zap', active: loc.pathname === '/bangs', count: dash?.bangs, title: 'your own bangs' },
  ]
  return (
    <aside className="sidebar" aria-label="Search">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Search' ? title : <>Pocket <b>Search</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={17} strokeWidth={2.2} />New<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={createItems(to => { onNavigate(); navigate(to) })} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} onClick={onNavigate} className={() => `sb-nav-item ${n.active ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.count && <span className="sb-count" title={`${n.count} ${n.title || ''}`}>{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      <Group title="Recent">
        {!dash?.recent?.length
          ? <p className="sb-empty">Your latest searches show up here{dash && !dash.engine_ready ? '' : ' (History can be paused in Settings)'}.</p>
          : <ul className="cal-list">{dash.recent.map(r => (
            <li key={r} className="cal-row sb-row">
              <NavLink to={`/search?q=${encodeURIComponent(r)}`} onClick={onNavigate} className={() => `cal-check ${loc.pathname === '/search' && q.get('q') === r ? 'is-active' : ''}`} title={r}>
                <span className="sb-ico"><Icon name="clock" size={13} /></span>
                <span className="cal-name ellipsis">{r}</span>
              </NavLink>
            </li>
          ))}</ul>}
      </Group>

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('psr:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  const key = `psr-sb-${title}`
  const [open, setOpen] = useState(() => localStorage.getItem(key) !== '0')
  const toggle = () => setOpen(o => { localStorage.setItem(key, o ? '0' : '1'); return !o })
  return (
    <section className="sb-sec">
      <h4 className="sb-h">
        <button type="button" className="sb-h-btn" aria-expanded={open} onClick={toggle}><Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} />{title}</button>
      </h4>
      {open && children}
    </section>
  )
}
