// Lenses: saved ways of searching, one click under the search box. Only some
// sites, some sites first, some never, words added to every search, a language.
import { type FormEvent, useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, ApiError, V1 } from '../lib/api'
import { LANGUAGES } from '../lib/languages'
import { announceLenses, lensSummary, loadLenses } from '../lib/lenses'
import type { Lens } from '../lib/types'
import { Confirm, Empty, Loading, PageHeader, useToast, errMsg } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'

const EMOJIS = ['🔎', '🍲', '💻', '💬', '📍', '🔬', '🛒', '🎬', '🎵', '📚', '🏡', '✈️', '🩺', '⚽', '🧒', '📰']
const langName = (code: string) => LANGUAGES.find(([v]) => v === code)?.[1] || code

export default function Lenses() {
  const { toast } = useToast()
  const [params, setParams] = useSearchParams()
  const [list, setList] = useState<Lens[] | null>(null)
  const [error, setError] = useState('')
  const [edit, setEdit] = useState<Partial<Lens> | null>(params.get('add') === '1' ? {} : null)
  const [del, setDel] = useState<Lens | null>(null)

  useEffect(() => { loadLenses(true).then(setList).catch(e => setError(errMsg(e))) }, [])
  useEffect(() => { if (params.get('add') === '1') setEdit(e => e || {}) }, [params])
  const closeEdit = () => { setEdit(null); if (params.get('add')) { params.delete('add'); setParams(params, { replace: true }) } }
  const changed = (next: Lens[]) => { setList(next); announceLenses(next) }

  const remove = async (l: Lens) => {
    try {
      await api.del(`${V1}/lenses/${l.id}`)
      changed((list || []).filter(x => x.id !== l.id))
      toast(`${l.name} removed`)
    } catch (e) { toast(errMsg(e), 'err') }
  }

  return (
    <div className="manage-page">
      <PageHeader title="Lenses" sub="A lens is a saved way of searching: only some sites, some sites first, some never, words added, a language. Pick one under the search box."
        actions={!edit && <button type="button" className="btn primary" onClick={() => setEdit({})}><Icon name="plus" size={15} />New lens</button>} />

      {error && <div className="callout bad"><Icon name="alert" size={15} /><span>{error}</span></div>}
      {!list && !error && <Loading />}
      {edit && !edit.id && <LensForm lens={edit} onClose={closeEdit} onSaved={l => { changed([...(list || []), l]); closeEdit() }} />}

      {list && list.length === 0 && !edit && <Empty icon="columns" title="No lenses">Make one for the searches you do often: recipes without the noise, code from the sites you trust, news from home.</Empty>}
      {list && list.length > 0 && (
        <ul className="lens-list">
          {list.map(l => edit?.id === l.id
            ? <li key={l.id} className="lens-edit-row"><LensForm lens={edit} onClose={closeEdit} onSaved={nl => { changed(list.map(x => x.id === nl.id ? nl : x)); closeEdit() }} /></li>
            : (
              <li key={l.id} className="lens-item">
                <span className="lens-item-ico" aria-hidden="true">{l.icon || '🔎'}</span>
                <div className="lens-item-main">
                  <div className="lens-item-name ellipsis">{l.name}</div>
                  <div className="lens-item-sub">{lensSummary(l, langName) || 'Nothing set'}</div>
                </div>
                <Link className="btn ghost sm" to={`/?lens=${l.id}`} title="Search with this lens">Use</Link>
                <button type="button" className="icon-btn" aria-label={`Edit ${l.name}`} title="Edit" onClick={() => setEdit(l)}><Icon name="edit" size={15} /></button>
                <button type="button" className="icon-btn" aria-label={`Remove ${l.name}`} title="Remove" onClick={() => setDel(l)}><Icon name="trash" size={15} /></button>
              </li>
            ))}
        </ul>
      )}
      {list && <p className="faint small lens-tip"><Icon name="info" size={13} /> Your site rules (<Link to="/sites">Sites</Link>) still apply inside a lens; for a site both name, yours wins.</p>}

      {del && <Confirm title={`Remove ${del.name}?`} message="The lens goes from under the search box. Your searches and site rules stay as they are." confirmLabel="Remove" onCancel={() => setDel(null)} onConfirm={() => { const l = del; setDel(null); remove(l) }} />}
    </div>
  )
}

function LensForm({ lens, onClose, onSaved }: { lens: Partial<Lens>; onClose: () => void; onSaved: (l: Lens) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: lens.name || '', icon: lens.icon || '', sites: lens.sites || '', boost: lens.boost || '', hide: lens.hide || '', words: lens.words || '', language: lens.language || '' })
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (k: keyof typeof f, v: string) => { setF({ ...f, [k]: v }); setErr('') }
  const langs = [['', 'Your usual language'], ...LANGUAGES.filter(([v]) => v !== 'auto'), ...(f.language && !LANGUAGES.some(([v]) => v === f.language) ? [[f.language, f.language]] : [])] as [string, string][]
  const save = async (e: FormEvent) => {
    e.preventDefault(); setBusy(true); setErr('')
    try {
      const l = lens.id ? await api.put<Lens>(`${V1}/lenses/${lens.id}`, f) : await api.post<Lens>(`${V1}/lenses`, f)
      toast(lens.id ? `${l.name} saved` : `${l.name} added — it's under the search box`)
      onSaved(l)
    } catch (e) { if (e instanceof ApiError && e.status === 400) setErr(e.message); else toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <form className="card add-card lens-form" onSubmit={save}>
      <div className="add-card-body">
        <div className="lens-form-top">
          <label className="field lens-form-icon"><span className="label">Icon</span>
            <input value={f.icon} onChange={e => set('icon', e.target.value)} placeholder="🔎" maxLength={8} aria-label="Icon (one emoji)" />
          </label>
          <label className="field"><span className="label">Name</span><input value={f.name} onChange={e => set('name', e.target.value)} placeholder="Recipes" required maxLength={40} autoFocus /></label>
        </div>
        <div className="lens-emojis" role="group" aria-label="Quick icons">
          {EMOJIS.map(em => <button key={em} type="button" className={`lens-emoji ${f.icon === em ? 'on' : ''}`} onClick={() => set('icon', em)} aria-label={`Icon ${em}`}>{em}</button>)}
        </div>
        <div className="lens-form-grid">
          <label className="field"><span className="label">Only these sites</span>
            <textarea rows={4} value={f.sites} onChange={e => set('sites', e.target.value)} placeholder={'reddit.com\nnews.ycombinator.com'} spellCheck={false} />
            <span className="help">One per line. Results come only from these. <code>ba</code> means every .ba site.</span>
          </label>
          <label className="field"><span className="label">These first</span>
            <textarea rows={4} value={f.boost} onChange={e => set('boost', e.target.value)} placeholder={'seriouseats.com\nbbcgoodfood.com'} spellCheck={false} />
            <span className="help">Raised above the rest when they appear.</span>
          </label>
          <label className="field"><span className="label">Never these</span>
            <textarea rows={4} value={f.hide} onChange={e => set('hide', e.target.value)} placeholder={'pinterest.com'} spellCheck={false} />
            <span className="help">Hidden while the lens is on.</span>
          </label>
        </div>
        <div className="lens-form-top lens-form-bottom">
          <label className="field"><span className="label">Add these words</span>
            <input value={f.words} onChange={e => set('words', e.target.value)} placeholder="recipe" maxLength={200} />
            <span className="help">Added to every search, e.g. “recipe” or “review”.</span>
          </label>
          <div className="field"><span className="label">Language</span>
            <Select value={f.language} onChange={v => set('language', v)} options={langs.map(([value, label]) => ({ value, label }))} ariaLabel="Language" />
          </div>
        </div>
        {err && <p className="field-err">{err}</p>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || !f.name.trim()}>{busy ? 'Saving…' : lens.id ? 'Save' : 'Add lens'}</button></div>
      </div>
    </form>
  )
}
