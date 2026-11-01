// The reading view: a page's article, fetched by your server with the
// scripts, trackers and adverts left out. The site sees the server, not you.
import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Icon } from '../components/icons'
import { useCrumb } from '../components/Layout'
import { SiteIcon } from '../components/ResultCard'
import { useSend } from '../components/send'
import { copyText, Empty, errMsg, MenuButton, useToast } from '../components/ui'
import { api, ApiError, V1 } from '../lib/api'
import { fmtDate, plural } from '../lib/format'
import type { Article } from '../lib/types'

const SIZES = [15, 16, 17, 18.5, 20, 22]
const SIZE_KEY = 'psr-read-size'

export default function Read() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  // the phone's share sheet sends the link as url, or inside text
  const shared = params.get('text') || ''
  const url = params.get('url') || shared.match(/https?:\/\/\S+/)?.[0] || ''
  const [a, setA] = useState<Article | null>(null)
  const [fail, setFail] = useState<{ status: number; message: string; title?: string } | null>(null)
  const [loading, setLoading] = useState(true)
  const [size, setSize] = useState(() => { const n = Number(localStorage.getItem(SIZE_KEY)); return SIZES.includes(n) ? n : 17 })
  const send = useSend()
  useCrumb(a?.title || (loading ? undefined : fail?.title))

  useEffect(() => {
    let alive = true
    setA(null); setFail(null); setLoading(true)
    if (!url) {
      setLoading(false)
      const words = (shared || params.get('title') || '').trim()
      if (words) navigate(`/search?q=${encodeURIComponent(words)}`, { replace: true })
      return
    }
    api.get<Article>(`${V1}/read?url=${encodeURIComponent(url)}`)
      .then(r => { if (alive) setA(r) })
      .catch(e => { if (alive) setFail({ status: e instanceof ApiError ? e.status : 0, message: errMsg(e), title: e instanceof ApiError ? String(e.data.title || '') : '' }) })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
  }, [url]) // eslint-disable-line react-hooks/exhaustive-deps

  const resize = (d: number) => {
    const i = Math.max(0, Math.min(SIZES.length - 1, SIZES.indexOf(size) + d))
    setSize(SIZES[i]); localStorage.setItem(SIZE_KEY, String(SIZES[i]))
  }
  let domain = ''
  try { domain = new URL(url).hostname.replace(/^www\./, '') } catch { /* not a url */ }
  const back = () => { if (window.history.length > 1) navigate(-1); else navigate('/') }

  return (
    <div className="reader">
      <div className="reader-bar">
        <button type="button" className="btn ghost sm" onClick={back}><Icon name="chevron-left" size={15} />Back</button>
        <span className="spacer" />
        {a && <>
          <button type="button" className="btn ghost icon-only sm" onClick={() => resize(-1)} disabled={size === SIZES[0]} title="Smaller text" aria-label="Smaller text"><span className="reader-a sm">A</span></button>
          <button type="button" className="btn ghost icon-only sm" onClick={() => resize(1)} disabled={size === SIZES[SIZES.length - 1]} title="Larger text" aria-label="Larger text"><span className="reader-a lg">A</span></button>
        </>}
        {url && <MenuButton label="Send or copy" className="btn ghost sm" align="right" width={250} items={() => [
          { label: 'Copy link', icon: 'copy', onSelect: async () => toast(await copyText(url) ? 'Link copied' : 'Could not copy', 'ok') },
          ...send.items({ url, title: a?.title || fail?.title || url, snippet: a?.excerpt || '' }),
        ]}><Icon name="send" size={15} />Send</MenuButton>}
        {url && <a className="btn sm" href={url} target="_blank" rel="noreferrer noopener"><Icon name="external" size={14} />Open original</a>}
      </div>

      {!url && <Empty title="Nothing to read" icon="book-open">Open a result in the reading view from its menu, or use <code>!read</code> followed by an address.</Empty>}
      {loading && url && (
        <div className="article-wrap" aria-label="Fetching the page…">
          <span className="skeleton w30" /><span className="skeleton reader-skel-title" /><span className="skeleton w90" /><span className="skeleton w90" /><span className="skeleton w70" />
        </div>
      )}
      {fail && (
        <div className="article-wrap">
          <div className="card reader-fail">
            <div className="empty-ico"><Icon name="book-open" size={22} /></div>
            <h2>{fail.status === 422 ? 'No article to read here' : 'This page could not be fetched'}</h2>
            <p className="muted">{fail.message}</p>
            <div className="row reader-fail-actions">
              <a className="btn primary" href={url} target="_blank" rel="noreferrer noopener"><Icon name="external" size={14} />Open original</a>
              <button type="button" className="btn" onClick={back}>Back</button>
            </div>
          </div>
        </div>
      )}
      {a && (
        <article className="article-wrap" style={{ fontSize: size }} lang={a.lang || undefined}>
          <div className="article-site">
            <SiteIcon domain={domain} size={16} />
            <span>{a.site || domain}</span>
            {a.byline && <span>· {a.byline}</span>}
            {a.published && <span>· {fmtDate(a.published.slice(0, 10))}</span>}
            <span>· {plural(a.minutes, 'min')} read</span>
          </div>
          <h1 className="article-title">{a.title}</h1>
          {a.image && !a.content.includes(a.image) && <img className="article-hero" src={a.image} alt="" />}
          <div className="article" dangerouslySetInnerHTML={{ __html: a.content }} />
          <footer className="article-foot">
            {a.embeds > 0 && <p className="muted small"><Icon name="video" size={13} /> {plural(a.embeds, 'video or player', 'videos or players')} {a.embeds === 1 ? 'was' : 'were'} left out (they would load from the site). <a href={url} target="_blank" rel="noreferrer noopener">Open the original</a> to see {a.embeds === 1 ? 'it' : 'them'}.</p>}
            <p className="faint xsmall"><Icon name="lock" size={12} /> Fetched by your server: no scripts, trackers or adverts, and {domain || 'the site'} never saw your device.</p>
          </footer>
        </article>
      )}
    </div>
  )
}
