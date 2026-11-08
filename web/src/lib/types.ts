export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

/** SearXNG categories Pocket Search offers, plus "apps" (only your Pocket apps). */
export type Category = 'general' | 'images' | 'videos' | 'news' | 'music' | 'it' | 'science' | 'files' | 'map' | 'social media' | 'apps'
export type TimeRange = '' | 'day' | 'week' | 'month' | 'year'

/** A result already sent to another Pocket app. */
export interface Save { id: number; url: string; app: string; title: string; item_url: string; created_at: string }

export type RuleAction = 'pin' | 'boost' | 'lower' | 'block'

/** One web result (the person's site rules already applied). */
export interface Hit {
  url: string; title: string; content: string; engine: string; engines: string[]; score: number; category: string
  published?: string; thumbnail?: string; image?: string; length?: string; author?: string; metadata?: string; resolution?: string; source?: string
  domain: string
  rule?: Exclude<RuleAction, 'block'> // why it moved
  saved: Save[]
}

export interface Infobox { title: string; content: string; image?: string; url?: string; links: { title: string; url: string; official?: boolean }[]; attributes: { label: string; value: string }[] }

export interface SearchResult {
  query: string; category: Category; page: number; time: TimeRange
  results: Hit[]
  answers: { text: string; url?: string }[]
  infoboxes: Infobox[]
  suggestions: string[]
  corrections: string[]
  unresponsive: { engine: string; reason: string }[]
  hidden: number; hidden_sites: string[]
  more: boolean
  engine: 'searxng' | 'demo'
  ms: number
  /** a bang: go here instead (starts with "/" = inside Pocket Search) */
  redirect?: string
  /** worked out on the server: a sum, a conversion, the weather, your tasks today… */
  instant?: InstantAnswer
  /** slower engines are still answering: their results come with full=1 */
  pending?: boolean
}

export type InstantKind = 'calc' | 'convert' | 'currency' | 'time' | 'weather' | 'tasks' | 'trip' | 'agenda'
export interface InstantAnswer { kind: InstantKind; title: string; value: string; detail: string; rows?: { label: string; value: string; url?: string }[]; source?: string; url?: string }

/** A saved way of searching; sites, boost and hide are one domain per line. */
export interface Lens { id: number; name: string; icon: string; sites: string; boost: string; hide: string; words: string; language: string; position: number; created_at: string; updated_at: string }

export interface AppItem { title: string; snippet: string; date?: string; url: string; link?: string; kind: string; strong?: boolean }
export interface AppSection { app: string; name: string; items: AppItem[]; error?: string; off?: boolean; more?: string; ms: number }
export interface AppsResult { query: string; sections: AppSection[] }

/** A Pocket app Search can search or send results to, and how this person is connected. */
export interface PocketAppConn {
  slug: string; name: string; reason: string; scopes: string[]
  can_search: boolean; send?: string; bang?: string
  mode: 'core' | 'direct' | 'off'; via: '' | 'core' | 'direct'; detail: string
  in_results: boolean
}

export interface Answer { answer: string; sources: { n: number; title: string; url: string; read: boolean }[]; provider: string; ms: number }

export interface Suggestions { history: string[]; suggestions: string[]; bangs: Bang[] }

export interface Bang { trigger: string; name: string; url: string; home: string; group: string; own?: boolean; id?: number }
export interface OwnBang { id: number; trigger: string; name: string; url: string; uses: number; created_at: string }

export interface SiteRule { id: number; domain: string; action: RuleAction; note: string; hits: number; created_at: string; updated_at: string }

export interface HistoryEntry { id: number; kind: 'search' | 'open'; query: string; category: string; url: string; title: string; results: number; created_at: string }

export interface Article {
  url: string; title: string; byline: string; site: string; excerpt: string; image: string; lang: string; published: string
  content: string; words: number; minutes: number; embeds: number
}

export interface EngineRow { name: string; shortcut: string; categories: string[]; enabled: boolean; paging: boolean; time_range: boolean; safe_search: boolean; on: boolean }
export interface Engines { categories: Record<string, string>; engines: Record<string, EngineRow[]> }

export interface Dashboard {
  searches_today: number; searches_week: number; rules: number; hidden_total: number; bangs: number; saves: number
  recent: string[]; engine: 'searxng' | 'demo'; engine_ready: boolean
}

export interface AIStatus { ready: boolean; provider: '' | 'pocket' | 'openai'; detail: string }

export interface AppConnSetting { mode: '' | 'core' | 'direct' | 'off'; url: string; token: string; public: string; search: boolean }

export interface Settings {
  core_configured: boolean
  core_registered: boolean | undefined
  title: string
  date_format: string
  time_format: string
  safe_search: 0 | 1 | 2
  language: string
  default_category: Category
  new_tab: boolean
  suggestions: boolean
  reader_images: boolean
  engines_off: string[]
  engines_on: string[]
  history: boolean
  history_days: number
  apps_in_results: boolean
  apps: Record<string, AppConnSetting>
  ai_answers: 'off' | 'ask' | 'auto'
  ai_read_pages: boolean
  ai_provider: 'auto' | 'pocket' | 'openai'
  ai_pocket_model: string
  openai_api_key: string
  openai_model: string
  openai_base_url: string
  engine: 'searxng' | 'demo'
  engine_ready: boolean
  fetch_private: boolean
  setup_complete: boolean
  base_url: string
}

/** Picture and icon addresses go through the server, so sites never see the device. */
export const imgURL = (u?: string) => !u ? '' : u.startsWith('/') ? u : `/api/v1/img?u=${encodeURIComponent(u)}`
export const faviconURL = (domain: string) => `/api/v1/favicon?d=${encodeURIComponent(domain)}`
