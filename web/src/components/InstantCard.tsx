// An instant answer above the results: a sum, a conversion, a currency, the
// time somewhere, the weather, or something from your own Pocket apps.
import type { InstantAnswer } from '../lib/types'
import { Icon } from './icons'
import { copyText, useToast } from './ui'

const ICON: Record<string, string> = { calc: 'hash', convert: 'repeat', currency: 'wallet', time: 'clock', weather: 'sun', tasks: 'check-circle', trip: 'plane', agenda: 'calendar' }
const COPYABLE = new Set(['calc', 'convert', 'currency'])

function ext(url?: string) { return !!url && /^https?:\/\//.test(url) }

export function InstantCard({ a }: { a: InstantAnswer }) {
  const { toast } = useToast()
  const linkable = (u?: string) => !!u && u !== '#'
  return (
    <section className="instant" aria-label="Instant answer">
      <div className="instant-head">
        <span className="instant-ico"><Icon name={ICON[a.kind] || 'zap'} size={14} /></span>
        <span className="grow ellipsis">{a.title}</span>
        {COPYABLE.has(a.kind) && (
          <button type="button" className="btn ghost icon-only sm" title="Copy" aria-label="Copy the answer"
            onClick={async () => toast(await copyText(a.value) ? 'Copied' : 'Could not copy', 'ok')}><Icon name="copy" size={14} /></button>
        )}
      </div>
      <div className="instant-value">{a.value}</div>
      {a.detail && <div className="instant-detail">{a.detail}</div>}
      {!!a.rows?.length && (
        <ul className="instant-rows">
          {a.rows.map((r, i) => (
            <li key={i}>
              {linkable(r.url)
                ? <a className="instant-row" href={r.url} target={ext(r.url) ? '_blank' : undefined} rel="noreferrer"><span className="grow ellipsis">{r.label}</span><span className="instant-row-v">{r.value}</span></a>
                : <span className="instant-row"><span className="grow ellipsis">{r.label}</span><span className="instant-row-v">{r.value}</span></span>}
            </li>
          ))}
        </ul>
      )}
      {(a.source || linkable(a.url)) && (
        <div className="instant-foot">
          {a.source && <span className="grow">{a.source}</span>}
          {linkable(a.url) && <a href={a.url} target={ext(a.url) ? '_blank' : undefined} rel="noreferrer">Open<Icon name="external" size={12} /></a>}
        </div>
      )}
    </section>
  )
}
