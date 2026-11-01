// A quick look at a result without leaving the results: the page's article,
// fetched by the server (like the reading view), in the side column on a wide
// screen or in a sheet on a phone. Previews are kept for the page's life.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, ApiError, V1 } from '../lib/api'
import { plural } from '../lib/format'
import type { Article, Hit } from '../lib/types'
import { Icon } from './icons'
import { SiteIcon } from './ResultCard'
import { errMsg } from './ui'

type Got = { a?: Article; status?: number; error?: string }
const kept = new Map<string, Got>()

function usePreview(url: string) {
  const [got, setGot] = useState<Got | null>(() => kept.get(url) || null)
  useEffect(() => {
    let alive = true
    const have = kept.get(url)
    if (have) { setGot(have); return }
    setGot(null)
    api.get<Article>(`${V1}/read?url=${encodeURIComponent(url)}`)
      .then(a => { const g = { a }; kept.set(url, g); if (alive) setGot(g) })
      .catch(e => { const g = { status: e instanceof ApiError ? e.status : 0, error: errMsg(e) }; if (e instanceof ApiError && e.status === 422) kept.set(url, g); if (alive) setGot(g) })
    return () => { alive = false }
  }, [url])
  return got
}

/** The preview itself; `onClose` shows the close button (the sheet has its own). */
export function PreviewBody({ hit, onClose }: { hit: Hit; onClose?: () => void }) {
  const got = usePreview(hit.url)
  const a = got?.a
  return (
    <div className="pv">
      <div className="pv-head">
        <SiteIcon domain={hit.domain} size={16} />
        <span className="grow ellipsis pv-site">{a?.site || hit.domain}</span>
        {onClose && <button type="button" className="btn ghost icon-only sm" onClick={onClose} title="Close the preview (Esc)" aria-label="Close the preview"><Icon name="x" size={15} /></button>}
      </div>
      <h3 className="pv-title">{a?.title || hit.title}</h3>
      {a && <div className="pv-meta">{[a.byline, plural(a.minutes, 'min') + ' read'].filter(Boolean).join(' · ')}</div>}
      <div className="pv-actions">
        <Link className="btn sm primary" to={`/read?url=${encodeURIComponent(hit.url)}`}><Icon name="book-open" size={14} />Reading view</Link>
        <a className="btn sm" href={hit.url} target="_blank" rel="noreferrer noopener"><Icon name="external" size={14} />Open site</a>
      </div>
      <div className="pv-scroll">
        {!got && <div className="pv-skel" aria-label="Fetching the page…"><span className="skeleton w90" /><span className="skeleton w90" /><span className="skeleton w70" /><span className="skeleton w90" /><span className="skeleton w30" /></div>}
        {got?.error && (
          <div className="pv-fail">
            <b>{got.status === 422 ? 'No article to preview on this page' : 'This page could not be fetched'}</b>
            <p className="muted small">{got.error}</p>
          </div>
        )}
        {a && <div className="article pv-article" dangerouslySetInnerHTML={{ __html: a.content }} />}
      </div>
    </div>
  )
}
