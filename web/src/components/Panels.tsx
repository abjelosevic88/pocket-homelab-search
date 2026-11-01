// Next to the results: the AI answer, the infobox, and matches from your Pocket apps.
import { Fragment, type ReactNode, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, ApiError, V1 } from '../lib/api'
import { fmtDate, plural } from '../lib/format'
import { imgURL, type Answer, type AppSection, type Infobox } from '../lib/types'
import { Icon } from './icons'
import { appIcon } from './send'
import { SiteIcon } from './ResultCard'
import { errMsg } from './ui'

const host = (u: string) => { try { return new URL(u).hostname.replace(/^www\./, '') } catch { return u } }

/** **bold** and [n] citations inside one line of the answer. */
function inline(text: string, sources: Answer['sources'], key: string): ReactNode[] {
  const out: ReactNode[] = []
  const re = /(\*\*[^*]+\*\*|\[\d+\])/g
  let last = 0, m: RegExpExecArray | null, i = 0
  while ((m = re.exec(text))) {
    if (m.index > last) out.push(text.slice(last, m.index))
    const tok = m[0]
    if (tok.startsWith('**')) out.push(<b key={key + i++}>{tok.slice(2, -2)}</b>)
    else {
      const n = Number(tok.slice(1, -1))
      const s = sources[n - 1]
      out.push(s ? <a key={key + i++} className="cite" href={s.url} target="_blank" rel="noreferrer noopener" title={s.title}>{n}</a> : tok)
    }
    last = m.index + tok.length
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

function AnswerText({ a }: { a: Answer }) {
  const blocks: ReactNode[] = []
  let list: string[] = []
  const flush = (k: number) => { if (list.length) { blocks.push(<ul key={'u' + k}>{list.map((l, j) => <li key={j}>{inline(l, a.sources, `l${k}-${j}-`)}</li>)}</ul>); list = [] } }
  a.answer.split('\n').forEach((raw, k) => {
    const line = raw.trim()
    if (/^[-*•] /.test(line)) { list.push(line.replace(/^[-*•] /, '')); return }
    flush(k)
    if (line) blocks.push(<p key={'p' + k}>{inline(line, a.sources, `p${k}-`)}</p>)
  })
  flush(9999)
  return <div className="ans-text">{blocks}</div>
}

export function AnswerCard({ q, cat, time, mode, auto }: { q: string; cat: string; time: string; mode: 'off' | 'ask' | 'auto'; auto: boolean }) {
  const [a, setA] = useState<Answer | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const key = `${q}|${cat}|${time}`
  const ran = useRef('')
  const run = () => {
    ran.current = key
    setBusy(true); setErr(''); setA(null)
    api.post<Answer>(`${V1}/answer`, { q, cat, time })
      .then(r => { if (ran.current === key) setA(r) })
      .catch(e => { if (ran.current === key) setErr(e instanceof ApiError ? e.message : errMsg(e)) })
      .finally(() => { if (ran.current === key) setBusy(false) })
  }
  useEffect(() => {
    setA(null); setErr(''); setBusy(false); ran.current = ''
    if (mode === 'auto' || auto) run()
  }, [key, mode, auto]) // eslint-disable-line react-hooks/exhaustive-deps
  if (mode === 'off' && !auto) return null
  if (!a && !busy && !err) {
    return (
      <button type="button" className="ans-ask" onClick={run}>
        <Icon name="sparkles" size={16} /><span className="grow">Answer with AI</span><span className="muted small">from these results, with sources</span>
      </button>
    )
  }
  return (
    <section className="panel ans" aria-live="polite">
      <div className="panel-head"><Icon name="sparkles" size={15} /><span className="grow">AI answer</span>
        {a && <span className="muted xsmall">{a.provider === 'pocket' ? 'Pocket AI' : a.provider === 'openai' ? 'OpenAI' : a.provider === 'demo' ? 'Demo' : ''}</span>}
        {(a || err) && <button type="button" className="btn ghost icon-only xs" title="Ask again" aria-label="Ask again" onClick={run}><Icon name="refresh" size={13} /></button>}
      </div>
      {busy && <div className="ans-skel" aria-label="Writing the answer…"><span className="skeleton" /><span className="skeleton" /><span className="skeleton short" /></div>}
      {err && <p className="muted small">{err}</p>}
      {a && <>
        <AnswerText a={a} />
        {a.sources.length > 0 && <ol className="ans-sources">
          {a.sources.map(s => <li key={s.n}><a href={s.url} target="_blank" rel="noreferrer noopener" title={s.title}><span className="ans-n">{s.n}</span><SiteIcon domain={host(s.url)} size={14} /><span className="ellipsis">{s.title}</span></a>{s.read && <span className="ans-read" title="The whole page was read, not only the snippet">read</span>}</li>)}
        </ol>}
        <p className="faint xsmall">AI can be wrong — check the sources.</p>
      </>}
    </section>
  )
}

export function InfoboxCard({ box }: { box: Infobox }) {
  const [more, setMore] = useState(false)
  const long = box.content.length > 360
  return (
    <section className="panel ibox">
      {box.image && <img className="ibox-img" src={imgURL(box.image)} alt="" loading="lazy" />}
      <h3 className="ibox-title">{box.url ? <a href={box.url} target="_blank" rel="noreferrer noopener">{box.title}</a> : box.title}</h3>
      {box.content && <p className="ibox-text">{long && !more ? box.content.slice(0, 340).replace(/\s\S*$/, '') + '…' : box.content}{long && <button type="button" className="link-btn small" onClick={() => setMore(m => !m)}>{more ? ' less' : ' more'}</button>}</p>}
      {box.attributes.length > 0 && <dl className="kv ibox-kv">{box.attributes.slice(0, 8).map(a => <Fragment key={a.label}><dt>{a.label}</dt><dd>{a.value}</dd></Fragment>)}</dl>}
      {box.links.length > 0 && <div className="ibox-links">{box.links.slice(0, 6).map(l => <a key={l.url} className="chip sm" href={l.url} target="_blank" rel="noreferrer noopener">{l.official && <Icon name="globe" size={12} />}{l.title}</a>)}</div>}
    </section>
  )
}

function itemDate(d?: string) { return d && /^\d{4}-\d{2}-\d{2}/.test(d) ? fmtDate(d.slice(0, 10)) : '' }

export function AppSectionList({ s, compact }: { s: AppSection; compact?: boolean }) {
  return (
    <div className={`apps-sec ${compact ? 'compact' : ''}`}>
      <div className="apps-sec-head">
        <span className="apps-ico"><Icon name={appIcon(s.app)} size={14} /></span>
        <span className="grow apps-name">{s.name.replace(/^Pocket /, '')}</span>
        {s.more && s.more !== '#' && <a className="muted xsmall" href={s.more} target="_blank" rel="noreferrer">Open app</a>}
      </div>
      <ul className="apps-items">
        {s.items.map((it, i) => (
          <li key={i}>
            <a href={it.url} target={it.url === '#' ? undefined : '_blank'} rel="noreferrer" className="apps-item" onClick={e => { if (it.url === '#') e.preventDefault() }}>
              <span className="apps-item-title ellipsis">{it.title}</span>
              {it.snippet && <span className="apps-item-snip">{it.snippet}</span>}
              <span className="apps-item-meta">{[it.kind, itemDate(it.date)].filter(Boolean).join(' · ')}</span>
            </a>
          </li>
        ))}
      </ul>
    </div>
  )
}

/** "From your apps" next to web results: only apps that found something. */
export function AppsPanel({ sections, loading, q }: { sections: AppSection[] | null; loading: boolean; q: string }) {
  if (loading && !sections) return <section className="panel apps"><div className="panel-head"><Icon name="layers" size={15} /><span className="grow">From your apps</span></div><div className="ans-skel"><span className="skeleton" /><span className="skeleton short" /></div></section>
  if (!sections) return null
  const found = sections.filter(s => s.items.length > 0)
  const quiet = sections.filter(s => s.items.length === 0 && !s.error)
  const off = sections.filter(s => s.off)
  const failed = sections.filter(s => s.error && !s.off)
  if (!found.length && !off.length && !failed.length && !quiet.length) return null
  return (
    <section className="panel apps">
      <div className="panel-head"><Icon name="layers" size={15} /><span className="grow">From your apps</span>
        <Link className="muted xsmall" to={`/search?cat=apps&q=${encodeURIComponent(q)}`}>See all</Link>
      </div>
      {found.length === 0 && <p className="muted small">Nothing in your apps matches “{q}”.</p>}
      {found.map(s => <AppSectionList key={s.app} s={s} compact />)}
      {quiet.length > 0 && found.length > 0 && <p className="faint xsmall apps-quiet" title={quiet.map(s => s.name).join(', ')}>{plural(quiet.length, 'more app')} had nothing.</p>}
      {failed.map(s => <p key={s.app} className="faint xsmall" title={s.error}><Icon name="alert" size={12} /> {s.name.replace(/^Pocket /, '')} did not answer.</p>)}
      {off.length > 0 && <p className="faint xsmall">{plural(off.length, 'app')} not connected · <Link to="/settings/apps">Connect more apps</Link></p>}
    </section>
  )
}
