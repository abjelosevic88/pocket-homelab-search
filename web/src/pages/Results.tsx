// The results page: web results (your site rules applied), pictures, videos,
// news, your Pocket apps, an AI answer with sources and the infobox.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { Icon } from '../components/icons'
import { useCrumb, announceChange } from '../components/Layout'
import { AnswerCard, AppSectionList, AppsPanel, InfoboxCard } from '../components/Panels'
import { ResultCard, SiteIcon, VideoCard } from '../components/ResultCard'
import { SearchBox } from '../components/SearchBox'
import { Select } from '../components/Select'
import { useSend } from '../components/send'
import { copyText, Empty, errMsg, type MenuItem, Menu, modalOpen, Modal, rectOf, type Anchor, useMediaQuery, useToast } from '../components/ui'
import { api, ApiError, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { plural } from '../lib/format'
import { imgURL, type AppSection, type AppsResult, type Category, type Hit, type RuleAction, type Save, type SearchResult, type SiteRule, type TimeRange } from '../lib/types'

const TABS: { id: Category; label: string; icon: string }[] = [
  { id: 'general', label: 'All', icon: 'search' },
  { id: 'images', label: 'Pictures', icon: 'image' },
  { id: 'videos', label: 'Videos', icon: 'video' },
  { id: 'news', label: 'News', icon: 'newspaper' },
  { id: 'apps', label: 'Your apps', icon: 'layers' },
]
const MORE: { id: Category; label: string; icon: string }[] = [
  { id: 'music', label: 'Music', icon: 'play' },
  { id: 'it', label: 'Code', icon: 'code' },
  { id: 'science', label: 'Science', icon: 'sparkles' },
  { id: 'files', label: 'Files', icon: 'download' },
  { id: 'social media', label: 'Social', icon: 'users' },
]
const TIMES: { value: TimeRange; label: string }[] = [
  { value: '', label: 'Any time' }, { value: 'day', label: 'Past day' }, { value: 'week', label: 'Past week' }, { value: 'month', label: 'Past month' }, { value: 'year', label: 'Past year' },
]
const RULES: { action: RuleAction; label: (d: string) => string; icon: string; done: string }[] = [
  { action: 'pin', label: d => `Always first: ${d}`, icon: 'pin', done: 'will always come first' },
  { action: 'boost', label: d => `Raise ${d}`, icon: 'arrow-up', done: 'is raised' },
  { action: 'lower', label: d => `Lower ${d}`, icon: 'arrow-down', done: 'is lowered' },
  { action: 'block', label: d => `Hide ${d}`, icon: 'eye-off', done: 'is hidden from now on' },
]

type Failure = { status: number; message: string }

export default function Results() {
  // the search box lives in the top bar on this page (Layout's slot), like a search engine
  const [slot, setSlot] = useState<HTMLElement | null>(null)
  useEffect(() => { setSlot(document.getElementById('psr-topbar-slot')) }, [])
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const { settings } = useApp()
  const { toast } = useToast()
  const wide = useMediaQuery('(min-width: 1100px)')
  const q = (params.get('q') || '').trim()
  const defaultCat = (settings?.default_category || 'general') as Category
  const cat = ((params.get('cat') || defaultCat) as Category)
  const tabValue = (id: Category) => id === defaultCat ? '' : id
  const time = (params.get('time') || '') as TimeRange
  const onlyApp = params.get('app') || ''
  const aiParam = params.get('ai') === '1'
  const web = cat !== 'apps'
  useCrumb(q || undefined)

  const [res, setRes] = useState<SearchResult | null>(null)
  const [hits, setHits] = useState<Hit[]>([])
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [moreBusy, setMoreBusy] = useState(false)
  const [fail, setFail] = useState<Failure | null>(null)
  const [apps, setApps] = useState<AppSection[] | null>(null)
  const [appsLoading, setAppsLoading] = useState(false)
  const [rules, setRules] = useState<SiteRule[]>([])
  const [focus, setFocus] = useState(-1)
  const [lightbox, setLightbox] = useState<Hit | null>(null)
  const [moreMenu, setMoreMenu] = useState<Anchor | null>(null)
  const [reload, setReload] = useState(0)
  const seq = useRef(0)

  const markSaved = useCallback((s: Save) => setHits(hs => hs.map(h => h.url === s.url ? { ...h, saved: [...h.saved.filter(x => x.app !== s.app), s] } : h)), [])
  const send = useSend(markSaved)

  const setParam = (k: string, v: string) => {
    const p = new URLSearchParams(params)
    if (v) p.set(k, v); else p.delete(k)
    if (k === 'cat') { p.delete('app'); p.delete('ai'); if (v === 'apps') p.delete('time') }
    setParams(p)
  }

  // the web results
  useEffect(() => {
    const id = ++seq.current
    setRes(null); setHits([]); setPage(1); setFail(null); setFocus(-1)
    if (!q || !web) { setLoading(false); return }
    setLoading(true)
    const t = time ? `&time=${time}` : ''
    api.get<SearchResult>(`${V1}/search?q=${encodeURIComponent(q)}&cat=${encodeURIComponent(cat)}${t}`)
      .then(r => {
        if (id !== seq.current) return
        if (r.redirect) {
          if (r.redirect.startsWith('/')) navigate(r.redirect, { replace: true })
          else window.location.replace(r.redirect)
          return
        }
        setRes(r); setHits(r.results)
        announceChange()
      })
      .catch(e => { if (id === seq.current) setFail({ status: e instanceof ApiError ? e.status : 0, message: errMsg(e) }) })
      .finally(() => { if (id === seq.current) setLoading(false) })
  }, [q, cat, time, web, reload]) // eslint-disable-line react-hooks/exhaustive-deps

  // your Pocket apps, at the same time (never holding up the web results)
  const wantApps = !!q && (cat === 'apps' || (cat === 'general' && (settings?.apps_in_results !== false)))
  useEffect(() => {
    setApps(null)
    if (!wantApps || q.startsWith('!')) return
    let alive = true
    setAppsLoading(true)
    const extra = cat === 'apps' ? (onlyApp ? `&app=${encodeURIComponent(onlyApp)}` : '&all=1') : ''
    api.get<AppsResult>(`${V1}/search/apps?q=${encodeURIComponent(q)}${extra}`)
      .then(r => { if (alive) setApps(r.sections) })
      .catch(() => { if (alive) setApps([]) })
      .finally(() => { if (alive) setAppsLoading(false) })
    return () => { alive = false }
  }, [q, cat, onlyApp, wantApps])

  // when the engine is missing, still show the apps next to the notice
  const engineMissing = fail?.status === 503
  useEffect(() => {
    if (!engineMissing || apps || !q || cat !== 'general' || wantApps) return
    api.get<AppsResult>(`${V1}/search/apps?q=${encodeURIComponent(q)}`).then(r => setApps(r.sections)).catch(() => {})
  }, [engineMissing]) // eslint-disable-line react-hooks/exhaustive-deps

  const loadRules = useCallback(() => { api.get<SiteRule[]>(`${V1}/rules`).then(setRules).catch(() => {}) }, [])
  useEffect(() => { loadRules() }, [loadRules])

  const more = async () => {
    if (!res || moreBusy) return
    const next = page + 1
    setMoreBusy(true)
    try {
      const t = time ? `&time=${time}` : ''
      const r = await api.get<SearchResult>(`${V1}/search?q=${encodeURIComponent(q)}&cat=${encodeURIComponent(cat)}${t}&page=${next}`)
      setHits(hs => { const seen = new Set(hs.map(h => h.url)); return [...hs, ...r.results.filter(h => !seen.has(h.url))] })
      setRes(prev => prev ? { ...prev, more: r.more && r.results.length > 0, hidden: prev.hidden + r.hidden } : prev)
      setPage(next)
    } catch (e) {
      toast(errMsg(e), 'err')
    } finally {
      setMoreBusy(false)
    }
  }

  const opened = (h: Hit) => { api.post(`${V1}/history/open`, { url: h.url, title: h.title, query: q }).catch(() => {}) }

  const setRule = async (domain: string, action: RuleAction) => {
    try {
      await api.post(`${V1}/rules`, { domain, action })
      const r = RULES.find(x => x.action === action)!
      toast(`${domain} ${r.done}`, 'ok', { label: 'Manage', run: () => navigate('/sites') })
      loadRules(); setReload(n => n + 1); announceChange()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const removeRule = async (rule: SiteRule) => {
    try {
      await api.del(`${V1}/rules/${rule.id}`)
      toast(`The rule for ${rule.domain} is gone`)
      loadRules(); setReload(n => n + 1); announceChange()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const ruleOf = (domain: string) => {
    let best: SiteRule | undefined
    for (const r of rules) if ((domain === r.domain || domain.endsWith('.' + r.domain)) && (!best || r.domain.length > best.domain.length)) best = r
    return best
  }

  const menuFor = (h: Hit) => (): MenuItem[] => {
    const rule = ruleOf(h.domain)
    const site = rule?.domain || h.domain
    return [
      { label: 'Open in the reading view', icon: 'book-open', hint: 'R', onSelect: () => navigate(`/read?url=${encodeURIComponent(h.url)}`) },
      { label: 'Copy link', icon: 'copy', onSelect: async () => toast(await copyText(h.url) ? 'Link copied' : 'Could not copy', 'ok') },
      ...(() => { const s = send.items({ url: h.url, title: h.title, snippet: h.content, query: q }, h.saved); return s.length ? ['sep' as const, ...s] : [] })(),
      'sep',
      { heading: site },
      ...RULES.filter(r => r.action !== rule?.action).map(r => ({ label: r.label(site), icon: r.icon, danger: r.action === 'block', onSelect: () => setRule(site, r.action) })),
      ...(rule ? [{ label: `Remove the rule for ${rule.domain}`, icon: 'undo', onSelect: () => removeRule(rule) }] : []),
    ]
  }

  // keyboard: 1–5 tabs, j/k move, o open, r reading view
  const listRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return
      const t = e.target as HTMLElement
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const n = Number(e.key)
      if (n >= 1 && n <= TABS.length) { e.preventDefault(); setParam('cat', tabValue(TABS[n - 1].id)); return }
      if (!hits.length || cat === 'images') return
      if (e.key === 'j' || e.key === 'k') {
        e.preventDefault()
        setFocus(f => {
          const next = e.key === 'j' ? Math.min(hits.length - 1, f + 1) : Math.max(0, f - 1)
          listRef.current?.querySelectorAll('[data-url]')[next]?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
          return next
        })
      } else if ((e.key === 'o' || e.key === 'Enter') && focus >= 0 && hits[focus]) {
        e.preventDefault(); opened(hits[focus])
        if (settings?.new_tab) window.open(hits[focus].url, '_blank', 'noopener,noreferrer'); else window.location.href = hits[focus].url
      } else if (e.key === 'r' && focus >= 0 && hits[focus]) {
        e.preventDefault(); navigate(`/read?url=${encodeURIComponent(hits[focus].url)}`)
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const moreTab = MORE.find(m => m.id === cat)
  const showSide = web && cat !== 'images' && cat !== 'videos'
  const box = res?.infoboxes?.[0]
  const aiMode = settings?.ai_answers || 'ask'
  const side = showSide && !!q && (
    <aside className="srp-side">
      {(aiMode !== 'off' || aiParam) && !engineMissing && <AnswerCard q={q} cat={cat} time={time} mode={aiMode} auto={aiParam} />}
      {cat === 'general' && (wantApps || engineMissing) && <AppsPanel sections={apps} loading={appsLoading} q={q} />}
      {box && <InfoboxCard box={box} />}
    </aside>
  )

  return (
    <div className="srp">
      <div className="srp-bar">
        {slot ? createPortal(<SearchBox initial={q} cat={tabValue(cat)} size="md" />, slot) : <SearchBox initial={q} cat={tabValue(cat)} size="md" />}
        <div className="srp-tabs">
          <nav className="srp-tablist" role="tablist" aria-label="What to search">
            {TABS.map((t, i) => (
              <button key={t.id} type="button" role="tab" aria-selected={cat === t.id} className={`srp-tab ${cat === t.id ? 'active' : ''}`} title={`${t.label} (${i + 1})`}
                onClick={() => setParam('cat', tabValue(t.id))}>
                <Icon name={t.icon} size={15} />{t.label}
              </button>
            ))}
            <button type="button" className={`srp-tab ${moreTab ? 'active' : ''}`} aria-haspopup="menu" aria-expanded={!!moreMenu} onClick={e => setMoreMenu(moreMenu ? null : rectOf(e.currentTarget))}>
              {moreTab ? <><Icon name={moreTab.icon} size={15} />{moreTab.label}</> : 'More'}<Icon name="chevron-down" size={14} />
            </button>
          </nav>
          {moreMenu && <Menu anchor={moreMenu} width={180} items={MORE.map(m => ({ label: m.label, icon: m.icon, checked: cat === m.id ? true : undefined, onSelect: () => setParam('cat', tabValue(m.id)) }))} onClose={() => setMoreMenu(null)} />}
          {web && <div className="srp-time"><Select value={time} onChange={v => setParam('time', v)} options={TIMES} ariaLabel="When" /></div>}
        </div>
      </div>

      {!q ? (
        <Empty title="What are you looking for?" icon="search">Type above and press Enter. Start with <code>!</code> to jump to a site (<code>!w</code>, <code>!yt</code>).</Empty>
      ) : cat === 'apps' ? (
        <AppsPage sections={apps} loading={appsLoading} q={q} onlyApp={onlyApp} onAll={() => setParam('app', '')} />
      ) : (
        <div className={`srp-body ${side ? 'with-side' : ''} ${wide ? 'is-wide' : ''}`}>
          <div className="srp-main" ref={listRef}>
            {loading && <ResultsSkeleton images={cat === 'images'} />}
            {fail && (engineMissing
              ? <div className="callout warn"><Icon name="alert" size={16} /><div><b>The web search engine is not running.</b> {fail.message.replace(/^There is no search engine yet: /, '')}<div className="mt-sm small muted">The engine (SearXNG) comes with Pocket Search in its docker-compose file: run <code>docker compose up -d</code> again, or set <code>SEARXNG_URL</code>.</div></div></div>
              : <div className="callout danger"><Icon name="alert" size={16} /><div className="grow"><b>The search did not go through.</b> {fail.message}</div><button type="button" className="btn sm" onClick={() => setReload(n => n + 1)}><Icon name="refresh" size={14} />Try again</button></div>)}
            {res && <>
              {res.corrections.length > 0 && <p className="srp-fix">Did you mean <Link to={`/search?q=${encodeURIComponent(res.corrections[0])}${cat !== 'general' ? `&cat=${encodeURIComponent(cat)}` : ''}`}>{res.corrections[0]}</Link>?</p>}
              {res.answers.map((a, i) => <div key={i} className="srp-answer"><Icon name="zap" size={15} /><span className="grow">{a.text}</span>{a.url && <a className="small" href={a.url} target="_blank" rel="noreferrer noopener">Source</a>}</div>)}
              {hits.length === 0 && <Empty title="No results" icon="search">Nothing came back for “{q}”{time ? ' in this time range' : ''}.{res.suggestions.length > 0 && <div className="chips srp-sugg-empty">{res.suggestions.slice(0, 6).map(s => <Link key={s} className="chip" to={`/search?q=${encodeURIComponent(s)}`}>{s}</Link>)}</div>}</Empty>}
              {cat === 'images'
                ? <ImageGrid hits={hits} onPick={setLightbox} />
                : <div className={cat === 'videos' ? 'vid-list' : 'res-list'}>
                  {hits.map((h, i) => cat === 'videos'
                    ? <VideoCard key={h.url} hit={h} active={i === focus} newTab={!!settings?.new_tab} apps={send.apps} menu={menuFor(h)} onOpen={opened} />
                    : <ResultCard key={h.url} hit={h} active={i === focus} newTab={!!settings?.new_tab} apps={send.apps} menu={menuFor(h)} onOpen={opened} thumb={cat === 'news'} />)}
                </div>}
              {res.more && hits.length > 0 && <div className="srp-more"><button type="button" className="btn" disabled={moreBusy} onClick={more}>{moreBusy ? 'Loading…' : 'More results'}</button></div>}
              <div className="srp-foot">
                {res.hidden > 0 && <p className="small muted"><Icon name="eye-off" size={13} /> {plural(res.hidden, 'result')} hidden by your site rules{res.hidden_sites.length ? ` (${res.hidden_sites.slice(0, 3).join(', ')}${res.hidden_sites.length > 3 ? '…' : ''})` : ''} · <Link to="/sites">Manage</Link></p>}
                {res.suggestions.length > 0 && hits.length > 0 && <div className="srp-related"><div className="home-h">Related searches</div><div className="chips">{res.suggestions.slice(0, 8).map(s => <Link key={s} className="chip" to={`/search?q=${encodeURIComponent(s)}${cat !== 'general' ? `&cat=${encodeURIComponent(cat)}` : ''}`}><Icon name="search" size={12} />{s}</Link>)}</div></div>}
                {res.unresponsive.length > 0 && <p className="xsmall faint">Not answering this time: {res.unresponsive.map(u => `${u.engine} (${u.reason})`).join(', ')}</p>}
              </div>
            </>}
          </div>
          {side}
        </div>
      )}
      {lightbox && <Lightbox hit={lightbox} onClose={() => setLightbox(null)} onOpen={opened} />}
    </div>
  )
}

function ResultsSkeleton({ images }: { images: boolean }) {
  if (images) return <div className="img-grid">{Array.from({ length: 12 }, (_, i) => <span key={i} className="skeleton img-skel" style={{ height: 120 + ((i * 37) % 80) }} />)}</div>
  return <div className="res-list" aria-label="Searching…">{Array.from({ length: 5 }, (_, i) => <div key={i} className="res res-skel"><span className="skeleton w30" /><span className="skeleton w70" /><span className="skeleton w90" /></div>)}</div>
}

function ImageGrid({ hits, onPick }: { hits: Hit[]; onPick: (h: Hit) => void }) {
  return (
    <div className="img-grid">
      {hits.map(h => {
        const src = imgURL(h.thumbnail || h.image)
        if (!src) return null
        return (
          <button key={h.url + (h.image || '')} type="button" className="img-cell" data-url={h.url} onClick={() => onPick(h)} title={h.title}>
            <img src={src} alt={h.title} loading="lazy" onError={e => { (e.currentTarget.parentElement as HTMLElement).style.display = 'none' }} />
            <span className="img-cap ellipsis">{h.source || h.domain}</span>
          </button>
        )
      })}
    </div>
  )
}

function Lightbox({ hit, onClose, onOpen }: { hit: Hit; onClose: () => void; onOpen: (h: Hit) => void }) {
  const full = imgURL(hit.image || hit.thumbnail)
  const [loaded, setLoaded] = useState(false)
  const site = useMemo(() => hit.source || hit.domain, [hit])
  return (
    <Modal title={<span className="ellipsis">{hit.title}</span>} onClose={onClose} size="xl" className="lightbox">
      <div className="lb-img">
        {!loaded && <img src={imgURL(hit.thumbnail || hit.image)} alt="" className="lb-low" />}
        <img src={full} alt={hit.title} onLoad={() => setLoaded(true)} style={loaded ? undefined : { position: 'absolute', opacity: 0 }} />
      </div>
      <div className="lb-meta">
        <SiteIcon domain={hit.domain} size={16} />
        <span className="grow ellipsis">{site}{hit.resolution ? ` · ${hit.resolution}` : ''}</span>
        <a className="btn sm" href={hit.url} target="_blank" rel="noreferrer noopener" onClick={() => onOpen(hit)}><Icon name="external" size={14} />Visit page</a>
        {hit.image && !hit.image.startsWith('/') && <a className="btn sm ghost" href={hit.image} target="_blank" rel="noreferrer noopener"><Icon name="image" size={14} />Open image</a>}
      </div>
    </Modal>
  )
}

function AppsPage({ sections, loading, q, onlyApp, onAll }: { sections: AppSection[] | null; loading: boolean; q: string; onlyApp: string; onAll: () => void }) {
  if (loading && !sections) return <ResultsSkeleton images={false} />
  if (!sections) return null
  const found = sections.filter(s => s.items.length > 0)
  const empty = sections.filter(s => s.items.length === 0 && !s.error)
  const off = sections.filter(s => s.off)
  const failed = sections.filter(s => s.error && !s.off)
  const one = onlyApp ? sections.find(s => s.app === onlyApp) : undefined
  return (
    <div className="apps-page">
      {onlyApp && <p className="small muted">Searching only {one?.name || onlyApp} · <button type="button" className="link-btn" onClick={onAll}>Search all your apps</button></p>}
      {found.length === 0 && off.length === 0 && failed.length === 0 && <Empty title="Nothing in your apps" icon="layers">None of your Pocket apps has anything for “{q}”.</Empty>}
      {found.map(s => <section key={s.app} className="card apps-card"><AppSectionList s={s} /></section>)}
      {empty.length > 0 && found.length > 0 && <p className="small faint">Nothing in {empty.map(s => s.name.replace(/^Pocket /, '')).join(', ')}.</p>}
      {failed.length > 0 && <div className="apps-off">{failed.map(s => <p key={s.app} className="small"><Icon name="alert" size={13} /> <b>{s.name}</b> <span className="muted">{s.error}</span></p>)}</div>}
      {off.length > 0 && (
        <div className="apps-off">
          <div className="home-h">Not connected</div>
          {off.map(s => <p key={s.app} className="small"><span className="apps-ico"><Icon name="layers" size={13} /></span> <b>{s.name}</b> <span className="muted">— {s.error}</span></p>)}
          <Link className="btn sm" to="/settings/apps"><Icon name="link" size={14} />Connected apps</Link>
        </div>
      )}
    </div>
  )
}
