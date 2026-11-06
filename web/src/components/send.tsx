// Sending a result to another Pocket app (Reader, Notes, Tasks…): the menu
// items for it, shared by result cards and the reading view.
import { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import type { PocketAppConn, Save } from '../lib/types'
import { errMsg, type MenuItem, useToast } from './ui'

let cache: Promise<PocketAppConn[]> | null = null
let cachedAt = 0

/** The Pocket apps Search can use, loaded once a minute at most. */
export function usePocketApps(): PocketAppConn[] {
  const [apps, setApps] = useState<PocketAppConn[]>([])
  useEffect(() => {
    if (!cache || Date.now() - cachedAt > 60_000) {
      cachedAt = Date.now()
      cache = api.get<PocketAppConn[]>(`${V1}/apps`).catch(() => { cache = null; return [] })
    }
    let alive = true
    cache.then(a => { if (alive) setApps(a) })
    return () => { alive = false }
  }, [])
  return apps
}

export interface SendItem { url: string; title: string; snippet?: string; query?: string }

/** Menu entries "Save to Reader", "Add as a task"… — greyed out with the reason when not connected. */
export function useSend(onSaved?: (s: Save) => void) {
  const apps = usePocketApps()
  const { toast } = useToast()
  const send = async (slug: string, it: SendItem) => {
    try {
      const r = await api.post<{ ok: boolean; message: string; item_url: string; save: Save }>(`${V1}/send/${slug}`, { url: it.url, title: it.title, snippet: it.snippet || '', query: it.query || '' })
      const open = r.item_url && r.item_url !== '#' ? { label: 'Open', run: () => window.open(r.item_url, '_blank', 'noopener') } : undefined
      toast(r.message || 'Sent', 'ok', open)
      onSaved?.(r.save)
    } catch (e) {
      toast(errMsg(e), 'err')
    }
  }
  const items = (it: SendItem, saved: Save[] = []): MenuItem[] => {
    const targets = apps.filter(a => a.send)
    if (!targets.length) return []
    return [
      { heading: 'Send to' },
      ...targets.map(a => {
        const done = saved.some(s => s.app === a.slug)
        const off = a.via === ''
        return {
          label: <span title={off ? a.detail : undefined}>{a.send}{done ? ' (again)' : ''}{off && <span className="muted xsmall"> — not connected</span>}</span>,
          icon: appIcon(a.slug),
          disabled: off,
          onSelect: () => send(a.slug, it),
        } as MenuItem
      }),
    ]
  }
  return { items, send, apps }
}

const ICONS: Record<string, string> = {
  reader: 'book-open', notes: 'note', tasks: 'check-circle', lookout: 'binoculars', wishlist: 'star', clips: 'copy', bookshelf: 'book-open',
  documents: 'archive', files: 'package', calendar: 'calendar', contacts: 'users', kitchen: 'habit', inventory: 'package', boards: 'columns',
  projects: 'layers', podcasts: 'rss', music: 'play', library: 'book-open', day: 'today', learn: 'sparkles', travel: 'plane', draw: 'palette',
  photos: 'image', watch: 'video', audiobooks: 'play', mail: 'mail', blog: 'edit', chat: 'hash',
}
export const appIcon = (slug: string) => ICONS[slug] || 'layers'

/** "In Reader", "In Notes"… for a saved badge. */
export function savedLabel(app: string, apps: PocketAppConn[]) {
  const a = apps.find(x => x.slug === app)
  return 'In ' + (a ? a.name.replace(/^Pocket /, '') : app[0].toUpperCase() + app.slice(1))
}
