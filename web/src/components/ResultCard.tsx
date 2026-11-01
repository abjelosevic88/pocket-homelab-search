// One web result: site icon and address, title, snippet, where it came from,
// why it moved (your site rules) and where you already sent it.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { fmtDate } from '../lib/format'
import { faviconURL, imgURL, type AppItem, type Hit, type PocketAppConn } from '../lib/types'
import { Icon } from './icons'
import { appIcon, savedLabel } from './send'
import { type MenuItem, MenuButton } from './ui'

const RULE_LABEL: Record<string, string> = { pin: 'Always first', boost: 'Raised', lower: 'Lowered' }

export function SiteIcon({ domain, size = 18 }: { domain: string; size?: number }) {
  const [broken, setBroken] = useState(false)
  if (!domain || broken) {
    const hue = [...(domain || '?')].reduce((a, c) => (a * 31 + c.charCodeAt(0)) % 360, 11)
    return <span className="site-tile" style={{ width: size, height: size, fontSize: size * 0.55, background: `hsl(${hue} 50% 48%)` }} aria-hidden="true">{(domain || '?')[0].toUpperCase()}</span>
  }
  return <img className="site-fav" src={faviconURL(domain)} width={size} height={size} alt="" loading="lazy" onError={() => setBroken(true)} />
}

/** "example.com › docs › getting-started" */
export function crumbs(url: string): string[] {
  try {
    const u = new URL(url)
    return u.pathname.split('/').filter(Boolean).slice(0, 3).map(p => decodeURIComponent(p).replace(/\.(html?|php|aspx?)$/, '')).filter(p => p.length < 40)
  } catch { return [] }
}

export function when(h: Hit): string {
  if (h.metadata) return h.metadata
  if (h.published) { const d = h.published.slice(0, 10); return /^\d{4}-\d{2}-\d{2}$/.test(d) ? fmtDate(d) : '' }
  return ''
}

export interface CardProps {
  hit: Hit
  active: boolean
  newTab: boolean
  apps: PocketAppConn[]
  menu: () => MenuItem[]
  onOpen: (h: Hit) => void
  thumb?: boolean // news: a small picture at the right
  onPreview?: (h: Hit) => void
  previewing?: boolean
}

export function ResultCard({ hit, active, newTab, apps, menu, onOpen, thumb, onPreview, previewing }: CardProps) {
  const target = newTab ? '_blank' : undefined
  const path = crumbs(hit.url)
  const w = when(hit)
  const pic = thumb ? imgURL(hit.thumbnail || hit.image) : ''
  return (
    <article className={`res ${active || previewing ? 'is-active' : ''} ${pic ? 'has-thumb' : ''}`} data-url={hit.url}>
      <div className="res-main">
        <div className="res-site">
          <SiteIcon domain={hit.domain} />
          <span className="res-domain">{hit.domain}</span>
          {path.length > 0 && <span className="res-path ellipsis">› {path.join(' › ')}</span>}
        </div>
        <h3 className="res-title"><a href={hit.url} target={target} rel="noreferrer noopener" onClick={() => onOpen(hit)}>{hit.title}</a></h3>
        {hit.content && <p className="res-snippet">{hit.content}</p>}
        <div className="res-meta">
          {w && <span>{w}</span>}
          {hit.author && <span>{hit.author}</span>}
          <span title={'Found by ' + hit.engines.join(', ')}>{hit.engines.length > 1 ? `${hit.engines.length} engines` : hit.engines[0] || hit.engine}</span>
          {hit.rule && <span className={`res-rule rule-${hit.rule}`}><Icon name={hit.rule === 'lower' ? 'arrow-down' : hit.rule === 'pin' ? 'pin' : 'arrow-up'} size={12} />{RULE_LABEL[hit.rule]}</span>}
          {hit.saved.map(s => s.item_url && s.item_url !== '#'
            ? <a key={s.app} className="res-saved" href={s.item_url} target="_blank" rel="noreferrer"><Icon name="check" size={12} />{savedLabel(s.app, apps)}</a>
            : <span key={s.app} className="res-saved"><Icon name="check" size={12} />{savedLabel(s.app, apps)}</span>)}
        </div>
      </div>
      {pic && <a className="res-thumb" href={hit.url} target={target} rel="noreferrer noopener" onClick={() => onOpen(hit)} tabIndex={-1}><img src={pic} alt="" loading="lazy" /></a>}
      <div className="res-actions">
        {onPreview && <button type="button" className={`btn ghost icon-only sm ${previewing ? 'on' : ''}`} title="Preview (p)" aria-label="Preview" aria-pressed={!!previewing} onClick={() => onPreview(hit)}><Icon name="eye" size={15} /></button>}
        <Link className="btn ghost icon-only sm" to={`/read?url=${encodeURIComponent(hit.url)}`} title="Open in the reading view" aria-label="Open in the reading view"><Icon name="book-open" size={15} /></Link>
        <MenuButton label="More" className="btn ghost icon-only sm" align="right" width={250} items={menu}><Icon name="more" size={15} /></MenuButton>
      </div>
    </article>
  )
}

export function VideoCard({ hit, active, newTab, menu, onOpen }: CardProps) {
  const pic = imgURL(hit.thumbnail || hit.image)
  return (
    <article className={`vid ${active ? 'is-active' : ''}`} data-url={hit.url}>
      <a className="vid-thumb" href={hit.url} target={newTab ? '_blank' : undefined} rel="noreferrer noopener" onClick={() => onOpen(hit)}>
        {pic ? <img src={pic} alt="" loading="lazy" /> : <span className="vid-noimg"><Icon name="play" size={22} /></span>}
        {hit.length && <span className="vid-len">{hit.length}</span>}
      </a>
      <div className="vid-body">
        <h3 className="res-title"><a href={hit.url} target={newTab ? '_blank' : undefined} rel="noreferrer noopener" onClick={() => onOpen(hit)}>{hit.title}</a></h3>
        <div className="res-site"><SiteIcon domain={hit.domain} size={14} /><span className="res-domain">{hit.domain}</span>{hit.author && <span className="muted">· {hit.author}</span>}</div>
        {hit.content && <p className="res-snippet clamp-2">{hit.content}</p>}
        <div className="res-meta">
          {when(hit) && <span>{when(hit)}</span>}
          <span title={'Found by ' + hit.engines.join(', ')}>{hit.engines.length > 1 ? `${hit.engines.length} engines` : hit.engines[0] || hit.engine}</span>
        </div>
      </div>
      <div className="res-actions">
        <MenuButton label="More" className="btn ghost icon-only sm" align="right" width={250} items={menu}><Icon name="more" size={15} /></MenuButton>
      </div>
    </article>
  )
}

/** A strong match from one of your Pocket apps, shown among the web results. */
export function AppHitCard({ item, app, appName }: { item: AppItem; app: string; appName: string }) {
  const href = item.url === '#' ? undefined : item.url
  const d = item.date && /^\d{4}-\d{2}-\d{2}/.test(item.date) ? fmtDate(item.date.slice(0, 10)) : ''
  const meta = [item.kind, d].filter(Boolean).join(' · ')
  return (
    <article className="res res-app" data-app={app}>
      <div className="res-main">
        <div className="res-site">
          <span className="res-app-ico"><Icon name={appIcon(app)} size={13} /></span>
          <span className="res-app-badge">{appName}</span>
          {meta && <span className="res-path">{meta}</span>}
        </div>
        <h3 className="res-title"><a href={href} onClick={e => { if (!href) e.preventDefault() }}>{item.title}</a></h3>
        {item.snippet && <p className="res-snippet">{item.snippet}</p>}
      </div>
    </article>
  )
}
