import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, postFile, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { AIStatus, APIToken, AppConnSetting, Category, EngineRow, Engines, PocketAppConn, Settings as SettingsT, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { Avatar, Confirm, CopyField, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast, errMsg } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'
import { useShell } from '../components/Layout'

type TabId = 'general' | 'engines' | 'apps' | 'ai' | 'browser' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'Searching', icon: 'search', sub: 'Safe search, language, where results open, suggestions and how long your history is kept.' },
  { id: 'engines', label: 'Engines', icon: 'globe', sub: 'Which search engines are asked for each kind of search. Your server asks them; they never see you.' },
  { id: 'apps', label: 'Connected apps', icon: 'layers', sub: 'Your Pocket apps: searched next to the web, and where one tap sends a result.' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: 'A short answer above the results, with numbered sources.' },
  { id: 'browser', label: 'Use in your browser', icon: 'monitor', sub: 'Make Pocket Search the search engine of your browser, phone or dashboard.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for scripts, Homepage widgets, Siri Shortcuts and Raycast.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Export your history, site rules and bangs, import them or a list of sites, download the database.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]
// the old ?tab= names
const ALIAS: Record<string, TabId> = { users: 'people', api: 'tokens', integrations: 'apps', search: 'general', opensearch: 'browser' }

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const legacy = new URLSearchParams(window.location.search).get('tab') || ''
  const want = raw || legacy
  const tab: TabId = TABS.find(t => t.id === (ALIAS[want] || want))?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })

  useEffect(() => { if (!raw && legacy) navigate(`/settings/${tab}`, { replace: true }) }, [raw, legacy, tab, navigate])
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])

  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back to search" title="Back to search (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
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
            <div className="set-title">
              <h2>{current.label}</h2>
              <p>{current.sub}</p>
            </div>
            {tab === 'general' && <General />}
            {tab === 'engines' && <EnginesTab />}
            {tab === 'apps' && <ConnectedApps />}
            {tab === 'ai' && <AISettings />}
            {tab === 'browser' && <Browser />}
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

/** A settings card: title, explanation, actions on the right — Pocket Calendar's section. */
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
  const save = async (patch?: Partial<SettingsT>, quiet = false) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); if (!quiet) toast('Saved') } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

/** "Test" results per integration. */
function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url = '', key = '', extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${errMsg(e)}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

const LANGUAGES: [string, string][] = [
  ['auto', 'Automatic'], ['en', 'English'], ['de', 'German'], ['fr', 'French'], ['es', 'Spanish'], ['it', 'Italian'], ['nl', 'Dutch'], ['pt', 'Portuguese'],
  ['pl', 'Polish'], ['sr', 'Serbian'], ['hr', 'Croatian'], ['bs', 'Bosnian'], ['sl', 'Slovenian'], ['ru', 'Russian'], ['uk', 'Ukrainian'], ['tr', 'Turkish'],
  ['sv', 'Swedish'], ['cs', 'Czech'], ['hu', 'Hungarian'], ['ja', 'Japanese'], ['zh', 'Chinese'],
]
const CATEGORY_OPTIONS: { value: Category; label: string }[] = [
  { value: 'general', label: 'All' }, { value: 'images', label: 'Pictures' }, { value: 'videos', label: 'Videos' }, { value: 'news', label: 'News' }, { value: 'apps', label: 'Your apps' },
]

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  const { reload } = useShell()
  if (!f) return <Loading />
  const langs = LANGUAGES.some(([v]) => v === f.language) ? LANGUAGES : [...LANGUAGES, [f.language, f.language] as [string, string]]
  return (
    <div className="stack loose">
      <Section title="Results" icon="search" sub="Yours alone — everyone on the box picks their own.">
        <SettingRow title="Safe search" help="Leave out adult results. Strict also filters borderline pictures; not every engine supports it.">
          <Segmented<number> value={f.safe_search} onChange={v => setF({ ...f, safe_search: v as 0 | 1 | 2 })} size="sm" label="Safe search" options={[{ value: 0, label: 'Off' }, { value: 1, label: 'Moderate' }, { value: 2, label: 'Strict' }]} />
        </SettingRow>
        <SettingRow title="Language" help="Automatic follows what you type. A language prefers results written in it.">
          <Select ariaLabel="Language" value={f.language || 'auto'} onChange={v => setF({ ...f, language: v })} options={langs.map(([value, label]) => ({ value, label }))} />
        </SettingRow>
        <SettingRow title="Start with" help="The tab a new search opens on."><Select ariaLabel="Start with" value={f.default_category} onChange={v => setF({ ...f, default_category: v as Category })} options={CATEGORY_OPTIONS} /></SettingRow>
        <SettingRow title="Open results in a new tab"><Switch checked={f.new_tab} onChange={v => setF({ ...f, new_tab: v })} label="Open results in a new tab" /></SettingRow>
        <SettingRow title="Suggestions while typing" help="Earlier searches and the engines' suggestions. What you type goes to your server, which asks one engine."><Switch checked={f.suggestions} onChange={v => setF({ ...f, suggestions: v })} label="Suggestions while typing" /></SettingRow>
        <SettingRow title="Your apps in the results" help="Matches from your Pocket apps (notes, tasks, documents…) next to the web results. Pick the apps under Connected apps."><Switch checked={f.apps_in_results} onChange={v => setF({ ...f, apps_in_results: v })} label="Your apps in the results" /></SettingRow>
        <SettingRow title="Pictures in the reading view" help="Pictures of an article are fetched by your server too, so the site still does not see you. Off: text only."><Switch checked={f.reader_images} onChange={v => setF({ ...f, reader_images: v })} label="Pictures in the reading view" /></SettingRow>
      </Section>
      <Section title="History" icon="clock" sub="Only you see your history. Other apps searching for you never add to it.">
        <SettingRow title="Remember my searches" help="Off: nothing new is kept (what is there stays until you clear it on the History page)."><Switch checked={f.history} onChange={v => setF({ ...f, history: v })} label="Remember my searches" /></SettingRow>
        <SettingRow title="Forget after">
          <Select ariaLabel="Forget after" value={String(f.history_days)} onChange={v => setF({ ...f, history_days: Number(v) })}
            options={[{ value: '30', label: '30 days' }, { value: '90', label: '90 days' }, { value: '365', label: 'A year' }, { value: '0', label: 'Never' }, ...(![30, 90, 365, 0].includes(f.history_days) ? [{ value: String(f.history_days), label: `${f.history_days} days` }] : [])]} />
        </SettingRow>
      </Section>
      <Section title="Date & time">
        <SettingRow title="Date format"><Select value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Time format"><Select value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
      </Section>
      <Section title="This box" sub={isAdmin ? 'Applies to everyone on this box.' : 'Set by an admin for everyone.'}>
        <SettingRow title="App title" help="Shown in the sidebar, the browser tab and the browser's search-engine list."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} maxLength={40} disabled={!isAdmin} title={isAdmin ? undefined : 'Box-wide — only an admin can change it'} /></SettingRow>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save().then(reload)} disabled={busy}>Save changes</button></div>
      <Section title="How it fits together">
        <dl className="set-dl">
          <div><dt>The engine</dt><dd className="muted">SearXNG runs next to this app and asks Google, Bing, DuckDuckGo, Brave, Wikipedia and many more at once — from your server. The sites see your server, never your phone or computer, and nobody builds a profile of you.</dd></div>
          <div><dt>Your rules</dt><dd className="muted">Sites you pin, raise, lower or hide (Sites) reorder every search. Bangs (<code>!w</code>, <code>!yt</code>, your own) jump straight to a site.</dd></div>
          <div><dt>Your apps</dt><dd className="muted">Notes, tasks, documents and more from your Pocket apps show up next to the web, and a result goes to Reader, Notes, Tasks, Lookout or Wishlist in one tap.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

// ---------- engines ----------

const ENGINE_CATS: [string, string][] = [['general', 'All'], ['images', 'Pictures'], ['videos', 'Videos'], ['news', 'News'], ['music', 'Music'], ['it', 'Code'], ['science', 'Science'], ['files', 'Files'], ['map', 'Maps'], ['social media', 'Social']]

function EnginesTab() {
  const { settings, setSettings } = useApp()
  const { toast } = useToast()
  const { run, status } = useTester()
  const [data, setData] = useState<Engines | null>(null)
  const [error, setError] = useState('')
  const [cat, setCat] = useState('general')
  const [confirmReset, setConfirmReset] = useState(false)
  useEffect(() => { api.get<Engines>(`${V1}/engines`).then(setData).catch(e => setError(errMsg(e))) }, [])
  if (!settings) return <Loading />

  const persist = async (off: string[], on: string[], msg?: string) => {
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { engines_off: off, engines_on: on }); setSettings(s); if (msg) toast(msg) } catch (e) { toast(errMsg(e), 'err') }
  }
  const toggle = (row: EngineRow, v: boolean) => {
    let off = settings.engines_off.filter(n => n !== row.name)
    let on = settings.engines_on.filter(n => n !== row.name)
    if (row.enabled && !v) off = [...off, row.name]
    if (!row.enabled && v) on = [...on, row.name]
    // the same engine can sit in several categories: switch it everywhere at once
    setData(d => d && { ...d, engines: Object.fromEntries(Object.entries(d.engines).map(([c, rows]) => [c, rows.map(r => r.name === row.name ? { ...r, on: v } : r)])) })
    persist(off, on)
  }
  const reset = async () => {
    setConfirmReset(false)
    await persist([], [], 'Back to the default engines')
    setData(d => d && { ...d, engines: Object.fromEntries(Object.entries(d.engines).map(([c, rows]) => [c, rows.map(r => ({ ...r, on: r.enabled }))])) })
  }
  const rows = data?.engines[cat] || []
  const changed = settings.engines_off.length + settings.engines_on.length
  return (
    <div className="stack loose">
      {!settings.engine_ready && <div className="callout warn"><Icon name="alert" size={15} /><div>
        <b>There is no search engine yet.</b> The <code>docker-compose.yml</code> of Pocket Search starts one (SearXNG) next to the app — run it with <code>docker compose up -d</code>. Already have a SearXNG? Set <code>SEARXNG_URL</code> to it (with <code>search.formats: [html, json]</code> and <code>server.limiter: false</code> in its settings). Until then only your Pocket apps are searched.
      </div></div>}
      {settings.engine === 'demo' && <div className="callout info"><Icon name="info" size={15} /><div>This is the demo engine: results are made up so the app can be tried without SearXNG.</div></div>}
      <Section title="Search engines" icon="globe" actions={<>
        <button type="button" className="btn sm" onClick={() => run('engine')} disabled={!settings.engine_ready}><Icon name="refresh" size={14} />Test the engine</button>
        <button type="button" className="btn sm ghost" onClick={() => setConfirmReset(true)} disabled={!changed}>Back to defaults</button>
      </>} sub="Each kind of search asks the engines switched on here. More engines find more but take a little longer; one slow or blocked engine never stops the others.">
        <div className="eng-test">{status('engine')}</div>
        {error && settings.engine_ready && <div className="callout bad"><Icon name="alert" size={15} /><span>{error}</span></div>}
        {!data && !error && <Loading />}
        {data && <>
          <div className="eng-tabs"><Tabs tabs={ENGINE_CATS.map(([id, label]) => ({ id, label }))} value={cat} onChange={setCat} /></div>
          {rows.length === 0 ? <p className="muted small">No engines for this kind of search.</p> : (
            <ul className="eng-list">
              {rows.map(r => (
                <li key={r.name} className={`eng-row ${r.on ? '' : 'is-off'}`}>
                  <div className="grow">
                    <div className="eng-name">{r.name}{!r.enabled && <span className="badge">off by default</span>}</div>
                    <div className="eng-sub">!{r.shortcut}{r.time_range ? ' · dates' : ''}{r.safe_search ? ' · safe search' : ''}{r.paging ? ' · more pages' : ''}</div>
                  </div>
                  <Switch checked={r.on} onChange={v => toggle(r, v)} label={`Use ${r.name}`} size="sm" />
                </li>
              ))}
            </ul>
          )}
          <p className="muted small mt-sm">{rows.filter(r => r.on).length} of {rows.length} on. Tip: <code>!bing</code>-style SearXNG shortcuts are not needed — use a bang like <code>!b</code> to go to the site itself.</p>
        </>}
      </Section>
      {confirmReset && <Confirm danger={false} title="Back to the default engines?" message="Every engine is switched on or off as SearXNG ships it." confirmLabel="Use the defaults" onCancel={() => setConfirmReset(false)} onConfirm={reset} />}
    </div>
  )
}

// ---------- connected apps ----------

type Mode = 'core' | 'direct' | 'off'
const BLANK: AppConnSetting = { mode: '', url: '', token: '', public: '', search: true }

function ConnectedApps() {
  const { settings, setSettings } = useApp()
  const { toast } = useToast()
  const [apps, setApps] = useState<PocketAppConn[] | null>(null)
  const [error, setError] = useState('')
  const load = () => api.get<PocketAppConn[]>(`${V1}/apps`).then(setApps).catch(e => setError(errMsg(e)))
  useEffect(() => { load() }, [])
  if (!settings) return <Loading />
  const coreOn = settings.core_configured && settings.core_registered !== false

  const saveApp = async (slug: string, patch: Partial<AppConnSetting>, msg = 'Saved') => {
    const cur = { ...BLANK, ...(settings.apps?.[slug] || {}) }
    try {
      const s = await api.put<SettingsT>(`${V1}/settings`, { apps: { [slug]: { ...cur, ...patch } } })
      setSettings(s); toast(msg); load()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const disconnect = async (slug: string) => {
    try { await api.post(`${V1}/apps/${slug}/disconnect`); const s = await api.get<SettingsT>(`${V1}/settings`); setSettings(s); toast('Disconnected'); load() } catch (e) { toast(errMsg(e), 'err') }
  }

  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>
        These connections are yours. Through <b>Pocket Core</b> Search acts as you in your own Pocket apps with nothing to paste; <b>Direct</b> uses an address and an API token you enter.
        {coreOn && <> New links start switched off: turn them on in Pocket Core → Links (Search → the app).</>}
        {!coreOn && <CoreHint />}
      </div></div>
      <SettingRow title="Your apps in the results" help="Show matches from the apps below next to the web results. The “Your apps” tab always searches them.">
        <Switch checked={settings.apps_in_results} onChange={async v => { try { setSettings(await api.put<SettingsT>(`${V1}/settings`, { apps_in_results: v })) } catch (e) { toast(errMsg(e), 'err') } }} label="Your apps in the results" />
      </SettingRow>
      {error && <div className="callout bad"><Icon name="alert" size={15} /><span>{error}</span></div>}
      {!apps && !error && <Loading />}
      {apps?.map(a => <AppRow key={a.slug} app={a} saved={{ ...BLANK, ...(settings.apps?.[a.slug] || {}) }} coreOn={coreOn} appsInResults={settings.apps_in_results}
        onSave={(p, msg) => saveApp(a.slug, p, msg)} onDisconnect={() => disconnect(a.slug)} />)}
      <Section title="Other Pocket apps searching for you" icon="link" sub="Search offers web search to the rest of the box through Pocket Core (switched on per app in Pocket Core → Links).">
        <dl className="set-dl">
          <div><dt>search:read</dt><dd className="muted">Run a web search with your engines and site rules, read a page's article text — for Pocket AI answers, Pocket Flows and the like. Nothing is added to your history; your history, rules and settings stay closed.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function AppRow({ app, saved, coreOn, appsInResults, onSave, onDisconnect }: {
  app: PocketAppConn; saved: AppConnSetting; coreOn: boolean; appsInResults: boolean
  onSave: (p: Partial<AppConnSetting>, msg?: string) => void; onDisconnect: () => void
}) {
  const [advanced, setAdvanced] = useState(app.mode === 'direct')
  const [d, setD] = useState({ url: saved.url, public: saved.public, token: saved.token })
  useEffect(() => { setD({ url: saved.url, public: saved.public, token: saved.token }) }, [saved.url, saved.public, saved.token])
  const mode: Mode = !coreOn && app.mode === 'core' ? 'off' : app.mode
  const ok = app.via !== '' && mode !== 'off'
  const showDirect = !coreOn || mode === 'direct' || advanced
  const line = mode === 'off' ? `Off: Search does not talk to ${app.name}.`
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${app.detail ? ` (${app.detail})` : ''}.` : `Not connected: ${app.detail}.`)
    : (ok ? `Direct connection with your own ${app.name} address and token.` : `Not connected: ${app.detail || 'add the address and a token'}.`)
  const what = [app.can_search && 'searched', app.send && app.send.toLowerCase()].filter(Boolean).join(' · ')
  return (
    <Section title={app.name} icon="layers" actions={mode === 'off' ? <span className="badge">off</span> : ok ? <span className="badge ok">{app.via === 'core' ? 'Pocket Core' : 'direct'}</span> : <span className="badge warn">not connected</span>}
      sub={<>{app.reason}{what && <span className="app-what"> — {what}</span>}{app.bang && <> · bang <code>!{app.bang}</code></>}</>}>
      <div className="conn-block">
        <div className="conn-mode">
          <Segmented<Mode> size="sm" value={mode} label={`How Search reaches ${app.name}`} onChange={m => onSave({ mode: m })} options={[
            { value: 'core', label: 'Pocket Core', title: coreOn ? 'Through Pocket Core: per person, nothing to paste' : 'Needs Pocket Core: there is none on this box', disabled: !coreOn },
            ...(showDirect ? [{ value: 'direct' as Mode, label: 'Direct', title: 'Your own address and API token' }] : []),
            { value: 'off', label: 'Off' },
          ]} />
          {!showDirect && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with an address and token</button>}
          {app.can_search && <label className="app-inres"><Switch size="sm" checked={app.in_results} disabled={mode === 'off'} onChange={v => onSave({ search: v }, v ? `${app.name} shows up in results` : `${app.name} left out of results`)} label={`${app.name} in results`} /><span className={`small ${appsInResults ? '' : 'muted'}`}>In results</span></label>}
        </div>
        <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
        {mode === 'direct' && <>
          <div className="two-col tight">
            <Field label="Address" help="How this server reaches it, e.g. http://host.docker.internal:8182"><input value={d.url} onChange={e => setD({ ...d, url: e.target.value })} placeholder="http://host.docker.internal:PORT" /></Field>
            <Field label="API token" help={`${app.name} → Settings → API & shortcuts. Stored on the server, shown masked.`}><input value={d.token} onChange={e => setD({ ...d, token: e.target.value })} autoComplete="off" /></Field>
          </div>
          <Field label="Address in a browser (optional)" help="For links to items, when it differs from the address above."><input value={d.public} onChange={e => setD({ ...d, public: e.target.value })} placeholder="https://notes.example.com" /></Field>
          <div className="row wrap">
            <button type="button" className="btn sm primary" onClick={() => onSave({ mode: 'direct', url: d.url, public: d.public, token: d.token })}>Save</button>
            {(saved.token || saved.url) && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved address and token">Disconnect</button>}
          </div>
        </>}
      </div>
    </Section>
  )
}

// ---------- AI ----------

function AISettings() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const [ai, setAI] = useState<AIStatus | null>(null)
  useEffect(() => { api.get<AIStatus>(`${V1}/ai/status`).then(setAI).catch(() => {}) }, [])
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="AI answers" icon="sparkles" sub="A short answer above the results, built only from the results you see. Every sentence points to a numbered source, and when the results don't say, the answer says so. The search, the result titles, snippets and (if you allow it) the text of the top pages go to the AI you pick.">
        {ai && (ai.ready
          ? <p className="muted small row" style={{ gap: 6 }}><Icon name="sparkles" size={13} />{ai.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} writes the answers.</p>
          : <div className="callout info"><Icon name="info" size={15} /><div>No AI is connected{ai.detail ? ` (${ai.detail})` : ''}, so there are no answers — the results are unchanged.{!coreOn && <CoreHint />}</div></div>)}
        <SettingRow title="Show an answer" help="A button: you ask for it. Automatically: every web search gets one (it costs an AI call per search).">
          <Segmented size="sm" value={f.ai_answers} onChange={v => setF({ ...f, ai_answers: v })} label="Show an answer" options={[{ value: 'off', label: 'Off' }, { value: 'ask', label: 'A button' }, { value: 'auto', label: 'Automatically' }]} />
        </SettingRow>
        <SettingRow title="Read the top pages" help="Your server reads the first three pages so the answer goes beyond the snippets. Slower by a few seconds."><Switch checked={f.ai_read_pages} onChange={v => setF({ ...f, ai_read_pages: v })} label="Read the top pages" /></SettingRow>
      </Section>
      <Section title="Which AI" icon="sparkles">
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Provider" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings.">
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
        <button type="button" className="btn primary" onClick={() => save().then(() => api.get<AIStatus>(`${V1}/ai/status`).then(setAI).catch(() => {}))} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

// ---------- browser ----------

function Browser() {
  const { settings } = useApp()
  const base = (settings?.base_url || window.location.origin).replace(/\/$/, '')
  return (
    <div className="stack loose">
      <Section title="The addresses" icon="link" sub="Everything a browser needs. Bangs work from the address bar too: type “!yt cats” there.">
        <Field group label="Search address"><CopyField value={`${base}/search?q=%s`} label="Search address" /></Field>
        <Field group label="Suggestions address (optional)"><CopyField value={`${base}/api/v1/suggest?format=opensearch&q=%s`} label="Suggestions address" /></Field>
        <p className="muted small">Some browsers want <code>{'{searchTerms}'}</code> instead of <code>%s</code>. You stay signed in through the browser, so nothing else is needed.</p>
      </Section>
      <Section title="Chrome, Edge, Brave, Vivaldi" icon="globe">
        <ol className="set-steps">
          <li>Open <b>Settings → Search engine → Manage search engines</b> (Edge: Privacy, search and services → Address bar and search).</li>
          <li>Next to <b>Site search</b>, click <b>Add</b>: name “Pocket Search”, shortcut “s”, address <code>{base}/search?q=%s</code>.</li>
          <li>In its ⋮ menu choose <b>Make default</b>. Visiting this page once also offers it automatically.</li>
        </ol>
      </Section>
      <Section title="Firefox" icon="globe">
        <ol className="set-steps">
          <li>With Pocket Search open, click the ⋯ (or right-click) in the address bar and choose <b>Add “Pocket Search”</b>.</li>
          <li>Then <b>Settings → Search → Default search engine</b> → Pocket Search.</li>
        </ol>
      </Section>
      <Section title="Safari on iPhone, iPad and Mac" icon="smartphone">
        <ol className="set-steps">
          <li>Safari only offers its own list of engines. A Safari extension such as “Customize Search Engine” can send searches to <code>{base}/search?q=%s</code> instead.</li>
          <li>Or keep it one tap away: on iPhone open this site, tap <b>Share → Add to Home Screen</b>. It opens full screen like an app.</li>
          <li>A Shortcut “Search with Pocket Search” can ask for text and open <code>{base}/search?q=</code> followed by it.</li>
        </ol>
      </Section>
      <Section title="Homepage dashboard" icon="grid" sub="Make the dashboard's search box use Pocket Search.">
        <pre className="set-pre">{`search:\n  provider: custom\n  url: ${base}/search?q=\n  target: _self\n  suggestionUrl: ${base}/api/v1/suggest?format=opensearch&q=\n  showSearchSuggestions: true`}</pre>
      </Section>
    </div>
  )
}

// ---------- the common tail ----------

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own history, site rules, bangs and connections. Admins manage accounts; they do not see anyone else's searches.">
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
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their history, site rules and bangs." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
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
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
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
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast(errMsg(e), 'err') }
  }
  const base = (settings?.base_url || window.location.origin).replace(/\/$/, '')
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can search, read your history and change your rules. Pocket apps don't need one — they search for you through Pocket Core (<code>search:read</code>). See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Raycast)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Handy endpoints" icon="smartphone" sub={<>At <code>{base}/api/v1</code>, with a token.</>}>
        <dl className="set-dl">
          <div><dt><code>GET /search?q=…&amp;cat=news</code></dt><dd className="muted">Web results as JSON, your site rules applied (<code>cat</code>: general, images, videos, news; <code>page</code>, <code>time</code>: day, week, month, year). Add <code>nohistory=1</code> to leave no trace.</dd></div>
          <div><dt><code>GET /search/apps?q=…</code></dt><dd className="muted">Matches in your connected Pocket apps.</dd></div>
          <div><dt><code>GET /read?url=…</code></dt><dd className="muted">The readable article of a page, without scripts and trackers.</dd></div>
          <div><dt><code>POST /answer</code></dt><dd className="muted"><code>{'{"q":"…"}'}</code> — an AI answer with sources (when an AI is set up).</dd></div>
          <div><dt><code>GET /dashboard</code></dt><dd className="muted">Searches today and this week, rules, bangs — for a Homepage widget.</dd></div>
          <div><dt><code>{base}/search?q=…</code></dt><dd className="muted">The results page itself — from a Raycast quicklink or an iOS Shortcut's “Open URLs”.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const { reload } = useShell()
  const [busy, setBusy] = useState(false)
  const [sites, setSites] = useState('')
  const [withHistory, setWithHistory] = useState(false)
  const [res, setRes] = useState<{ rules: number; bangs: number; history: number; skipped: number } | null>(null)
  const send = async (body: File | string) => {
    setBusy(true); setRes(null)
    try {
      const r = await postFile<{ rules: number; bangs: number; history: number; skipped: number }>(`${V1}/import${withHistory ? '?history=1' : ''}`, body)
      setRes(r); reload()
      const n = r.rules + r.bangs + r.history
      toast(n ? 'Imported' : 'Nothing new to import')
      if (typeof body === 'string' && r.rules) setSites('')
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      <Section title="Import a list of sites" icon="filter" sub="One site per line, optionally followed by what to do: hide (the default), lower, raise or pin. Sites you already have a rule for get the new one.">
        <textarea rows={6} value={sites} onChange={e => setSites(e.target.value)} placeholder={'pinterest.com\nmedium.com lower\nwikipedia.org pin\ndeveloper.mozilla.org raise'} />
        <div className="row wrap mt-sm"><button type="button" className="btn primary" disabled={busy || !sites.trim()} onClick={() => send(sites)}><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Add these rules'}</button></div>
      </Section>
      <Section title="Import an export" icon="archive" sub="A Pocket Search export (from here or another box): site rules and bangs are merged, and importing twice changes nothing.">
        <label className="check-row"><input type="checkbox" checked={withHistory} onChange={e => setWithHistory(e.target.checked)} />Include the search history</label>
        <div className="row wrap mt-sm"><label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Choose a file…'}<input type="file" accept=".json,application/json" hidden onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) send(file) }} /></label></div>
      </Section>
      {res && <div className={`callout ${res.rules + res.bangs + res.history ? 'info' : ''}`}><Icon name="check-circle" size={15} /><div>
        <b>{res.rules}</b> site rule{res.rules === 1 ? '' : 's'}, <b>{res.bangs}</b> bang{res.bangs === 1 ? '' : 's'}{withHistory ? <>, <b>{res.history}</b> history entr{res.history === 1 ? 'y' : 'ies'}</> : ''} imported{res.skipped ? `, ${res.skipped} skipped` : ''}.
      </div></div>}
      <Section title="Export" icon="download" sub="Your history, site rules, bangs and the results you sent to other apps, as one JSON file.">
        <a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export my data</a>
      </Section>
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Restore by stopping the app and replacing <code>pocket-search.db</code> in the data directory. The search engine keeps nothing worth backing up.</p>
        <a className="btn primary" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout bad"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const core = (data.core || {}) as { configured?: boolean; url?: string; registered?: boolean; error?: string }
  const tiles: [string, number | undefined, string][] = [['Searches remembered', counts.history, 'clock'], ['Site rules', counts.site_rules, 'filter'], ['Your bangs', counts.bangs, 'zap'], ['Searches since start', Number(data.searches_run), 'search']]
  const engine = data.engine === 'demo' ? 'Demo engine (made-up results)' : data.engine_ready ? <span key="e">SearXNG at <code>{String(data.engine_url)}</code></span> : 'not set up (SEARXNG_URL)'
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Search engine', engine],
    ['Pocket Core', !core.configured ? 'not connected (CORE_URL)' : core.registered ? <span key="c">connected · <code>{core.url}</code></span> : `not registered yet${core.error ? ` (${core.error})` : ''}`],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))}</span>],
    ['Reading view', data.fetch_private ? 'pages on private networks allowed' : 'public pages only'],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Search</h3>
          <p className="muted small">Private web search: SearXNG asks the search engines from your own server, your Pocket apps are searched alongside, and you decide which sites rise, sink or vanish. Bangs, a reading view without trackers, AI answers with sources. One Go binary, one SQLite file, one volume.</p>
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
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-search" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Reader, Notes, Tasks, Lookout, Wishlist, Pocket AI and more.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
