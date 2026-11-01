// History: what you searched for and which results you opened, newest first, grouped by day.
import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { HistoryEntry, Settings } from '../lib/types'
import { addDays, fmtDate, fmtISOTime, todayISO, weekday } from '../lib/format'
import { Confirm, Empty, Loading, PageHeader, Segmented, Switch, useDebounce, useToast, errMsg, MenuButton } from '../components/ui'
import { Icon } from '../components/icons'
import { useShell } from '../components/Layout'

type Kind = '' | 'search' | 'open'
const LIMIT = 100

function localDay(iso: string): string {
  const d = new Date(iso)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function dayLabel(day: string): string {
  const today = todayISO()
  if (day === today) return 'Today'
  if (day === addDays(today, -1)) return 'Yesterday'
  const sameYear = day.slice(0, 4) === today.slice(0, 4)
  return `${weekday(day, true)}, ${fmtDate(day, !sameYear)}`
}

function domainOf(u: string): string {
  try { return new URL(u).hostname.replace(/^www\./, '') } catch { return u }
}

export default function History() {
  const { settings, setSettings } = useApp()
  const { reload } = useShell()
  const { toast } = useToast()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const [kind, setKind] = useState<Kind>('')
  const [items, setItems] = useState<HistoryEntry[] | null>(null)
  const [total, setTotal] = useState(0)
  const [more, setMore] = useState(false)
  const [error, setError] = useState('')
  const [confirmAll, setConfirmAll] = useState(false)

  const url = (offset: number) => `${V1}/history?limit=${LIMIT}&offset=${offset}${kind ? `&kind=${kind}` : ''}${dq.trim() ? `&q=${encodeURIComponent(dq.trim())}` : ''}`
  const load = () => {
    setError('')
    api.get<{ items: HistoryEntry[]; total: number }>(url(0))
      .then(r => { setItems(r.items); setTotal(r.total) })
      .catch(e => setError(errMsg(e)))
  }
  useEffect(load, [dq, kind]) // eslint-disable-line react-hooks/exhaustive-deps

  const loadMore = async () => {
    if (!items) return
    setMore(true)
    try { const r = await api.get<{ items: HistoryEntry[]; total: number }>(url(items.length)); setItems([...items, ...r.items]); setTotal(r.total) } catch (e) { toast(errMsg(e), 'err') } finally { setMore(false) }
  }

  const remove = async (h: HistoryEntry) => {
    try {
      await api.del(`${V1}/history/${h.id}`)
      setItems(list => list ? list.filter(x => x.id !== h.id) : list)
      setTotal(t => Math.max(0, t - 1))
      reload()
    } catch (e) { toast(errMsg(e), 'err') }
  }

  const clear = async (range: 'hour' | 'day' | 'week' | 'all') => {
    try {
      const r = await api.post<{ removed: number }>(`${V1}/history/clear`, { range })
      toast(r.removed ? `Forgot ${r.removed} ${r.removed === 1 ? 'entry' : 'entries'}` : 'Nothing to forget there')
      load(); reload()
    } catch (e) { toast(errMsg(e), 'err') }
  }

  const resume = async () => {
    try { setSettings(await api.put<Settings>(`${V1}/settings`, { history: true })); toast('History is on again') } catch (e) { toast(errMsg(e), 'err') }
  }

  const groups = useMemo(() => {
    const out: { day: string; rows: HistoryEntry[] }[] = []
    for (const h of items || []) {
      const d = localDay(h.created_at)
      const last = out[out.length - 1]
      if (last && last.day === d) last.rows.push(h)
      else out.push({ day: d, rows: [h] })
    }
    return out
  }, [items])

  const newTab = settings?.new_tab
  return (
    <div className="manage-page">
      <PageHeader title="History" sub="What you searched for and which results you opened. Only you can see it."
        actions={<MenuButton label="Clear history" className="btn" align="right" width={220} items={[
          { heading: 'Forget' },
          { label: 'The last hour', icon: 'clock', onSelect: () => clear('hour') },
          { label: 'The last day', icon: 'today', onSelect: () => clear('day') },
          { label: 'The last week', icon: 'calendar', onSelect: () => clear('week') },
          'sep',
          { label: 'Everything', icon: 'trash', danger: true, onSelect: () => setConfirmAll(true) },
        ]}><Icon name="trash" size={15} />Clear…</MenuButton>} />

      {settings && !settings.history && (
        <div className="callout warn hist-paused">
          <Icon name="pause" size={15} />
          <div className="grow"><b>History is paused</b> — nothing new is kept. What is below stays until you clear it.</div>
          <Switch checked={false} onChange={resume} label="Remember searches again" />
        </div>
      )}

      <div className="toolbar">
        <div className="search">
          <Icon name="search" size={15} />
          <input type="search" value={q} onChange={e => setQ(e.target.value)} placeholder="Find in your history" aria-label="Find in your history" />
        </div>
        <Segmented<Kind> value={kind} onChange={setKind} size="sm" label="Show" options={[{ value: '', label: 'All' }, { value: 'search', label: 'Searches' }, { value: 'open', label: 'Opened' }]} />
      </div>

      {error && <div className="callout bad"><Icon name="alert" size={15} /><span>{error}</span></div>}
      {!items && !error && <Loading />}
      {items && items.length === 0 && (
        dq || kind
          ? <Empty icon="search" title="Nothing matches">Try other words, or show everything.</Empty>
          : <Empty icon="clock" title="No history yet">Your searches and the results you open show up here, so you can find them again.</Empty>
      )}

      {groups.map(g => (
        <section key={g.day} className="hist-day">
          <h3 className="hist-day-h">{dayLabel(g.day)}</h3>
          <ul className="hist-list">
            {g.rows.map(h => (
              <li key={h.id} className="hist-row">
                <span className={`hist-ico ${h.kind}`}><Icon name={h.kind === 'open' ? 'external' : 'search'} size={14} /></span>
                <div className="hist-main">
                  {h.kind === 'open' ? <>
                    <a className="hist-title ellipsis" href={h.url} target={newTab ? '_blank' : undefined} rel="noreferrer noopener">{h.title || h.url}</a>
                    <div className="hist-sub ellipsis">{domainOf(h.url)}{h.query && <> · from <Link to={`/search?q=${encodeURIComponent(h.query)}`}>{h.query}</Link></>}</div>
                  </> : <>
                    <Link className="hist-title ellipsis" to={`/search?q=${encodeURIComponent(h.query)}${h.category && h.category !== 'general' ? `&cat=${encodeURIComponent(h.category)}` : ''}`}>{h.query}</Link>
                    <div className="hist-sub">{catLabel(h.category)}{h.results ? ` · ${h.results} results` : ''}</div>
                  </>}
                </div>
                <span className="hist-time tnum">{fmtISOTime(h.created_at)}</span>
                <button type="button" className="icon-btn hist-del" aria-label="Forget this" title="Forget this" onClick={() => remove(h)}><Icon name="x" size={15} /></button>
              </li>
            ))}
          </ul>
        </section>
      ))}

      {items && items.length < total && (
        <div className="hist-more"><button type="button" className="btn" onClick={loadMore} disabled={more}>{more ? 'Loading…' : `Show more (${total - items.length} left)`}</button></div>
      )}

      {confirmAll && <Confirm title="Forget your whole history?" message="Every search and every opened result is deleted. Your site rules and bangs stay." confirmLabel="Forget everything" onCancel={() => setConfirmAll(false)} onConfirm={() => { setConfirmAll(false); clear('all') }} />}
    </div>
  )
}

const CATS: Record<string, string> = { general: 'All', images: 'Pictures', videos: 'Videos', news: 'News', apps: 'Your apps', music: 'Music', it: 'Code', science: 'Science', files: 'Files', map: 'Maps', 'social media': 'Social' }
function catLabel(c: string) { return CATS[c] || 'All' }
