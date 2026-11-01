// Bangs: "!w eiffel tower" jumps straight to a site's own search. Your own ones, then the built-in list.
import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, ApiError, V1 } from '../lib/api'
import type { Bang, OwnBang } from '../lib/types'
import { Confirm, Empty, Loading, PageHeader, useToast, errMsg } from '../components/ui'
import { Icon } from '../components/icons'
import { useShell } from '../components/Layout'

function hostOf(u: string): string {
  if (u.startsWith('/')) return 'Pocket Search'
  try { return new URL(u.replace('{q}', 'x')).hostname.replace(/^www\./, '') } catch { return u }
}

export default function Bangs() {
  const { reload } = useShell()
  const { toast } = useToast()
  const [params, setParams] = useSearchParams()
  const [data, setData] = useState<{ mine: OwnBang[]; builtin: Bang[] } | null>(null)
  const [error, setError] = useState('')
  const [edit, setEdit] = useState<Partial<OwnBang> | null>(params.get('add') === '1' ? {} : null)
  const [del, setDel] = useState<OwnBang | null>(null)
  const [filter, setFilter] = useState('')

  const load = () => api.get<{ mine: OwnBang[]; builtin: Bang[] }>(`${V1}/bangs`).then(setData).catch(e => setError(errMsg(e)))
  useEffect(() => { load() }, [])
  useEffect(() => { if (params.get('add') === '1') setEdit(e => e || {}) }, [params])
  const closeEdit = () => { setEdit(null); if (params.get('add')) { params.delete('add'); setParams(params, { replace: true }) } }

  const remove = async (b: OwnBang) => {
    try { await api.del(`${V1}/bangs/${b.id}`); setData(d => d && { ...d, mine: d.mine.filter(x => x.id !== b.id) }); reload(); toast(`!${b.trigger} removed`) } catch (e) { toast(errMsg(e), 'err') }
  }

  const groups = useMemo(() => {
    const f = filter.trim().toLowerCase().replace(/^!/, '')
    const out = new Map<string, Bang[]>()
    for (const b of data?.builtin || []) {
      if (f && !b.trigger.includes(f) && !b.name.toLowerCase().includes(f) && !hostOf(b.url).includes(f)) continue
      out.set(b.group, [...(out.get(b.group) || []), b])
    }
    return [...out.entries()]
  }, [data, filter])

  const mine = data?.mine || []
  return (
    <div className="manage-page">
      <PageHeader title="Bangs" sub={<>Type <code>!w eiffel tower</code> to jump straight to Wikipedia. A bang can be first or last, and works from your browser's address bar too.</>}
        actions={!edit && <button type="button" className="btn primary" onClick={() => setEdit({})}><Icon name="plus" size={15} />Add a bang</button>} />

      {error && <div className="callout bad"><Icon name="alert" size={15} /><span>{error}</span></div>}
      {!data && !error && <Loading />}

      {data && <>
        <section className="bang-sec">
          <h3 className="bang-sec-h">Your bangs</h3>
          {edit && !edit.id && <BangForm bang={edit} onClose={closeEdit} onSaved={b => { setData(d => d && { ...d, mine: [...d.mine, b].sort((x, y) => x.trigger.localeCompare(y.trigger)) }); reload(); closeEdit() }} />}
          {mine.length === 0 && !edit && <p className="muted small bang-empty">None yet. Add one for a site you search often — your shop, your wiki, your Jellyfin. Yours win over built-in ones with the same shortcut.</p>}
          {mine.length > 0 && (
            <ul className="bang-list">
              {mine.map(b => edit?.id === b.id
                ? <li key={b.id} className="bang-edit-row"><BangForm bang={edit} onClose={closeEdit} onSaved={nb => { setData(d => d && { ...d, mine: d.mine.map(x => x.id === nb.id ? { ...x, ...nb } : x) }); closeEdit() }} /></li>
                : (
                  <li key={b.id} className="bang-row">
                    <code className="bang-trigger">!{b.trigger}</code>
                    <div className="bang-main">
                      <div className="ellipsis">{b.name}</div>
                      <div className="bang-sub ellipsis">{hostOf(b.url)} · {b.uses ? `used ${b.uses} time${b.uses === 1 ? '' : 's'}` : 'not used yet'}</div>
                    </div>
                    <Link className="btn ghost sm" to={`/search?q=${encodeURIComponent('!' + b.trigger)}`}>Try</Link>
                    <button type="button" className="icon-btn" aria-label={`Edit !${b.trigger}`} title="Edit" onClick={() => setEdit(b)}><Icon name="edit" size={15} /></button>
                    <button type="button" className="icon-btn" aria-label={`Remove !${b.trigger}`} title="Remove" onClick={() => setDel(b)}><Icon name="trash" size={15} /></button>
                  </li>
                ))}
            </ul>
          )}
        </section>

        <section className="bang-sec">
          <div className="bang-sec-head">
            <h3 className="bang-sec-h">Built in</h3>
            <div className="toolbar bang-filter"><div className="search"><Icon name="search" size={15} /><input type="search" value={filter} onChange={e => setFilter(e.target.value)} placeholder="Find a bang" aria-label="Find a bang" /></div></div>
          </div>
          {groups.length === 0 && <Empty icon="zap" title="No bang matches">Add your own above.</Empty>}
          {groups.map(([group, list]) => (
            <div key={group} className="bang-group">
              <h4 className="bang-group-h">{group}</h4>
              <ul className="bang-grid">
                {list.map(b => (
                  <li key={b.trigger} className="bang-cell">
                    <code className="bang-trigger">!{b.trigger}</code>
                    <span className="bang-main"><span className="ellipsis">{b.name}</span><span className="bang-sub ellipsis">{hostOf(b.url)}</span></span>
                    <Link className="bang-try" to={`/search?q=${encodeURIComponent('!' + b.trigger)}`} title={`Open ${b.name}`}>Try</Link>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </section>
      </>}

      {del && <Confirm title={`Remove !${del.trigger}?`} message={`Typing !${del.trigger} stops going to ${del.name}${data?.builtin.some(b => b.trigger === del.trigger) ? ' and uses the built-in one again' : ''}.`} confirmLabel="Remove" onCancel={() => setDel(null)} onConfirm={() => { const b = del; setDel(null); remove(b) }} />}
    </div>
  )
}

function BangForm({ bang, onClose, onSaved }: { bang: Partial<OwnBang>; onClose: () => void; onSaved: (b: OwnBang) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ trigger: bang.trigger || '', name: bang.name || '', url: bang.url || '' })
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const save = async (e: FormEvent) => {
    e.preventDefault(); setBusy(true); setErr('')
    try {
      const b = bang.id ? await api.put<OwnBang>(`${V1}/bangs/${bang.id}`, f) : await api.post<OwnBang>(`${V1}/bangs`, f)
      toast(bang.id ? `!${b.trigger} saved` : `!${b.trigger} added — try it`)
      onSaved(b)
    } catch (e) { if (e instanceof ApiError && (e.status === 400 || e.status === 409)) setErr(e.message); else toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <form className="card add-card" onSubmit={save}>
      <div className="add-card-body">
        <div className="add-grid bang-form-grid">
          <label className="field"><span className="label">Shortcut</span>
            <span className="bang-input"><span className="bang-bang">!</span><input value={f.trigger} onChange={e => { setF({ ...f, trigger: e.target.value.replace(/^!/, '').toLowerCase() }); setErr('') }} placeholder="kp" required maxLength={24} autoFocus autoCapitalize="off" autoCorrect="off" spellCheck={false} /></span>
          </label>
          <label className="field"><span className="label">Name</span><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder="KupujemProdajem" maxLength={60} /></label>
        </div>
        <label className="field"><span className="label">Address</span>
          <input value={f.url} onChange={e => { setF({ ...f, url: e.target.value }); setErr('') }} placeholder="https://example.com/search?q={q}" required inputMode="url" autoCapitalize="off" autoCorrect="off" spellCheck={false} />
          <span className="help">Search the site once, copy the address of the results page and put <code>{'{q}'}</code> where your words were.</span>
        </label>
        {err && <p className="field-err">{err}</p>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || !f.trigger || !f.url}>{busy ? 'Saving…' : bang.id ? 'Save' : 'Add bang'}</button></div>
      </div>
    </form>
  )
}
