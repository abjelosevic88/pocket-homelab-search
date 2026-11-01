// The start page: one search box, your recent searches, a few tips.
import { Link } from 'react-router-dom'
import { useShell } from '../components/Layout'
import { Icon } from '../components/icons'
import { SearchBox } from '../components/SearchBox'
import { useApp } from '../lib/app-context'

export default function Home() {
  const { settings, status } = useApp()
  const { dash } = useShell()
  const title = settings?.title || status?.title || 'Pocket Search'
  const noEngine = dash ? !dash.engine_ready : settings ? !settings.engine_ready : false
  return (
    <div className="home">
      <div className="home-brand">
        <img src="/favicon.svg" alt="" className="home-logo" />
        <h1 className="home-title">{title === 'Pocket Search' ? <>Pocket <b>Search</b></> : title}</h1>
      </div>
      <div className="home-box">
        <SearchBox autoFocus={!window.matchMedia('(max-width: 767px)').matches} />
      </div>
      <p className="home-private"><Icon name="lock" size={13} />Searches go through your own server; the sites you find never see you.</p>

      {noEngine && (
        <div className="callout warn home-note">
          <Icon name="alert" size={16} />
          <div>
            <b>The web search engine is not running yet.</b> Your Pocket apps can still be searched. The search engine (SearXNG) comes with Pocket Search: start it with <code>docker compose up -d</code> in the app's folder, or point <code>SEARXNG_URL</code> at one you already run.
          </div>
        </div>
      )}

      {!!dash?.recent?.length && (
        <div className="home-recent">
          <div className="home-h">Recent</div>
          <div className="chips">
            {dash.recent.map(r => <Link key={r} className="chip" to={`/search?q=${encodeURIComponent(r)}`}><Icon name="clock" size={13} />{r}</Link>)}
          </div>
        </div>
      )}

      <div className="home-tips">
        <span><Icon name="zap" size={13} /><b>Bangs</b> jump straight to a site: <code>!w</code> Wikipedia, <code>!yt</code> YouTube, <code>!notes</code> your notes. <Link to="/bangs">All bangs</Link></span>
        <span><Icon name="filter" size={13} /><Link to="/sites">Raise, lower or hide sites</Link> in every search.</span>
        <span><Icon name="monitor" size={13} /><Link to="/settings/browser">Add to your browser</Link> as its search engine.</span>
      </div>
    </div>
  )
}
