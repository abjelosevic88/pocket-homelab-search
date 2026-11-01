// One row of lens chips: "All" (no lens), each lens, and a way to manage them.
// The row scrolls sideways on a phone instead of wrapping, so nothing jumps.
import { Link } from 'react-router-dom'
import { useLenses } from '../lib/lenses'
import { Icon } from './icons'

export function LensChips({ active, onPick, size = 'md' }: { active: number; onPick: (id: number) => void; size?: 'sm' | 'md' }) {
  const lenses = useLenses()
  if (!lenses?.length) return null
  return (
    <div className={`lens-row ${size === 'sm' ? 'lens-sm' : ''}`} role="group" aria-label="Lenses">
      <button type="button" className={`lens-chip ${active === 0 ? 'active' : ''}`} aria-pressed={active === 0} onClick={() => onPick(0)}>All</button>
      {lenses.map(l => (
        <button key={l.id} type="button" className={`lens-chip ${active === l.id ? 'active' : ''}`} aria-pressed={active === l.id}
          title={active === l.id ? `${l.name} — click again to search without it` : l.name} onClick={() => onPick(active === l.id ? 0 : l.id)}>
          {l.icon && <span className="lens-ico" aria-hidden="true">{l.icon}</span>}{l.name}
        </button>
      ))}
      <Link className="lens-chip ghost" to="/lenses" title="Make or change lenses"><Icon name="settings" size={13} />Lenses</Link>
    </div>
  )
}
