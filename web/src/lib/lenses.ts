// The person's lenses, loaded once and shared by the start page, the results
// page and the Lenses page (which announces changes with 'psr:lenses').
import { useEffect, useState } from 'react'
import { api, V1 } from './api'
import type { Lens } from './types'

let cache: Lens[] | null = null
let inflight: Promise<Lens[]> | null = null

export function loadLenses(force = false): Promise<Lens[]> {
  if (cache && !force) return Promise.resolve(cache)
  if (inflight && !force) return inflight
  inflight = api.get<Lens[]>(`${V1}/lenses`).then(l => { cache = l; return l }).finally(() => { inflight = null })
  return inflight
}

export const announceLenses = (list?: Lens[]) => {
  if (list) cache = list
  else cache = null
  window.dispatchEvent(new CustomEvent('psr:lenses'))
}

export function useLenses(): Lens[] | null {
  const [list, setList] = useState<Lens[] | null>(cache)
  useEffect(() => {
    let alive = true
    const load = () => { loadLenses().then(l => { if (alive) setList(l) }).catch(() => { if (alive) setList([]) }) }
    load()
    window.addEventListener('psr:lenses', load)
    return () => { alive = false; window.removeEventListener('psr:lenses', load) }
  }, [])
  return list
}

/** "Only 4 sites · 2 first · 2 hidden · adds “recipe” · Bosnian" */
export function lensSummary(l: Lens, langName: (code: string) => string): string {
  const n = (s: string) => s.split('\n').filter(x => x.trim()).length
  const parts: string[] = []
  if (n(l.sites)) parts.push(n(l.sites) === 1 ? `Only ${l.sites.trim()}` : `Only ${n(l.sites)} sites`)
  if (n(l.boost)) parts.push(`${n(l.boost)} first`)
  if (n(l.hide)) parts.push(`${n(l.hide)} hidden`)
  if (l.words) parts.push(`adds “${l.words}”`)
  if (l.language) parts.push(langName(l.language))
  return parts.join(' · ')
}
