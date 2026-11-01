// Sites: the person's rules for sites — pinned first, raised, lowered or hidden in every search.
import { type FormEvent, useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, ApiError, V1 } from '../lib/api'
import type { RuleAction, SiteRule } from '../lib/types'
import { faviconURL } from '../lib/types'
import { hostColor } from '../lib/format'
import { Confirm, Empty, Loading, PageHeader, Segmented, useToast, errMsg } from '../components/ui'
import { Icon } from '../components/icons'
import { useShell } from '../components/Layout'

export const ACTIONS: { value: RuleAction; label: string; icon: string; group: string; help: string }[] = [
  { value: 'pin', label: 'Pin', icon: 'pin', group: 'Pinned', help: 'Always first' },
  { value: 'boost', label: 'Raise', icon: 'arrow-up', group: 'Raised', help: 'Higher up' },
  { value: 'lower', label: 'Lower', icon: 'arrow-down', group: 'Lowered', help: 'Further down' },
  { value: 'block', label: 'Hide', icon: 'eye-off', group: 'Hidden', help: 'Never shown' },
]

/** A site's icon (fetched by your server), or its first letter when it has none. */
export function SiteIcon({ domain, size = '' }: { domain: string; size?: '' | 'sm' }) {
  const [failed, setFailed] = useState(false)
  if (failed) return <span className={`site-ico ${size}`} style={{ background: hostColor(domain) }} aria-hidden="true">{domain.replace(/^www\./, '').slice(0, 1)}</span>
  return <img className={`site-ico site-ico-img ${size}`} src={faviconURL(domain)} alt="" loading="lazy" onError={() => setFailed(true)} />
}

export default function Sites() {
  const { reload } = useShell()
  const { toast } = useToast()
  const [params, setParams] = useSearchParams()
  const [rules, setRules] = useState<SiteRule[] | null>(null)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState(params.get('add') === '1')
  const [del, setDel] = useState<SiteRule | null>(null)

  const load = () => api.get<SiteRule[]>(`${V1}/rules`).then(setRules).catch(e => setError(errMsg(e)))
  useEffect(() => { load() }, [])
  useEffect(() => { if (params.get('add') === '1') setAdding(true) }, [params])

  const closeAdd = () => { setAdding(false); if (params.get('add')) { params.delete('add'); setParams(params, { replace: true }) } }

  const change = async (r: SiteRule, action: RuleAction) => {
    const before = rules
    setRules(list => list?.map(x => x.id === r.id ? { ...x, action } : x) || list)
    try { const saved = await api.put<SiteRule>(`${V1}/rules/${r.id}`, { domain: r.domain, action, note: r.note }); setRules(list => list?.map(x => x.id === r.id ? saved : x) || list) } catch (e) { setRules(before); toast(errMsg(e), 'err') }
  }
  const remove = async (r: SiteRule) => {
    try { await api.del(`${V1}/rules/${r.id}`); setRules(list => list?.filter(x => x.id !== r.id) || list); reload(); toast(`${r.domain} is back to normal`) } catch (e) { toast(errMsg(e), 'err') }
  }

  return (
    <div className="manage-page">
      <PageHeader title="Sites" sub="Pin a site to always see it first, raise or lower it, or hide it from every search. Subdomains count too."
        actions={!adding && <button type="button" className="btn primary" onClick={() => setAdding(true)}><Icon name="plus" size={15} />Add a site</button>} />

      {adding && <AddRule onClose={closeAdd} onSaved={r => { setRules(list => [r, ...(list || []).filter(x => x.id !== r.id)]); reload(); closeAdd() }} />}

      {error && <div className="callout bad"><Icon name="alert" size={15} /><span>{error}</span></div>}
      {!rules && !error && <Loading />}
      {rules && rules.length === 0 && !adding && (
        <Empty icon="filter" title="No site rules yet">Hide the sites you never want to see (content farms, endless pin boards) and raise the ones you trust. You can also do it from any result's menu.</Empty>
      )}

      {rules && ACTIONS.map(a => {
        const list = rules.filter(r => r.action === a.value)
        if (!list.length) return null
        return (
          <section key={a.value} className="rule-group">
            <h3 className="rule-group-h"><Icon name={a.icon} size={14} />{a.group}<span className="muted small">· {a.help}</span></h3>
            <ul className="rule-list">
              {list.map(r => (
                <li key={r.id} className="rule-row">
                  <SiteIcon domain={r.domain} />
                  <div className="rule-main">
                    <div className="rule-domain ellipsis">{r.domain}</div>
                    <div className="rule-sub ellipsis">{r.note && <span>{r.note} · </span>}{r.hits ? `changed ${r.hits.toLocaleString()} result${r.hits === 1 ? '' : 's'}` : 'not in a search yet'}</div>
                  </div>
                  <Segmented<RuleAction> className="rule-seg" size="sm" value={r.action} onChange={v => change(r, v)} label={`Rule for ${r.domain}`}
                    options={ACTIONS.map(x => ({ value: x.value, label: x.label, title: x.help }))} />
                  <button type="button" className="icon-btn" aria-label={`Remove the rule for ${r.domain}`} title="Remove the rule" onClick={() => setDel(r)}><Icon name="trash" size={15} /></button>
                </li>
              ))}
            </ul>
          </section>
        )
      })}

      <p className="muted small rule-foot">Have a list already? <Link to="/settings/data">Import a list of sites</Link> (one per line, like <code>example.com hide</code>).</p>
      {del && <Confirm title={`Remove the rule for ${del.domain}?`} message="Its results go back to where the search engines put them." confirmLabel="Remove" onCancel={() => setDel(null)} onConfirm={() => { const r = del; setDel(null); remove(r) }} />}
    </div>
  )
}

function AddRule({ onClose, onSaved }: { onClose: () => void; onSaved: (r: SiteRule) => void }) {
  const { toast } = useToast()
  const [domain, setDomain] = useState('')
  const [action, setAction] = useState<RuleAction>('block')
  const [note, setNote] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const save = async (e: FormEvent) => {
    e.preventDefault(); setBusy(true); setErr('')
    try {
      const r = await api.post<SiteRule>(`${V1}/rules`, { domain, action, note })
      toast(`${r.domain}: ${ACTIONS.find(a => a.value === r.action)?.help.toLowerCase()}`)
      onSaved(r)
    } catch (e) { if (e instanceof ApiError && e.status === 400) setErr(e.message); else toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <form className="card add-card" onSubmit={save}>
      <div className="add-card-body">
        <div className="add-grid">
          <label className="field"><span className="label">Site</span><input value={domain} onChange={e => { setDomain(e.target.value); setErr('') }} placeholder="example.com" autoFocus required inputMode="url" autoCapitalize="off" autoCorrect="off" spellCheck={false} /></label>
          <div className="field"><span className="label">What to do</span>
            <Segmented<RuleAction> value={action} onChange={setAction} label="What to do" options={ACTIONS.map(a => ({ value: a.value, label: a.label, title: a.help }))} />
          </div>
        </div>
        <label className="field"><span className="label">Note (optional)</span><input value={note} onChange={e => setNote(e.target.value)} placeholder="Why — so you remember later" maxLength={200} /></label>
        {err && <p className="field-err">{err}</p>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || !domain.trim()}>{busy ? 'Saving…' : 'Save'}</button></div>
      </div>
    </form>
  )
}
