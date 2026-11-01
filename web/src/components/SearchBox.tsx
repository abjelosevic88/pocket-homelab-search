// The search input with suggestions: earlier searches, the engine's
// suggestions, and bangs while typing "!x". The list floats under the input,
// so nothing below it moves.
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Suggestions } from '../lib/types'
import { Icon } from './icons'

type Row = { kind: 'history' | 'suggestion' | 'bang'; text: string; label?: string }

export function SearchBox({ initial = '', cat = '', lens = 0, size = 'lg', autoFocus = false, placeholder = 'Search the web and your apps' }: {
  initial?: string; cat?: string; lens?: number; size?: 'lg' | 'md'; autoFocus?: boolean; placeholder?: string
}) {
  const { settings } = useApp()
  const navigate = useNavigate()
  const [q, setQ] = useState(initial)
  const [rows, setRows] = useState<Row[]>([])
  const [open, setOpen] = useState(false)
  const [hi, setHi] = useState(-1)
  const input = useRef<HTMLInputElement>(null)
  const root = useRef<HTMLFormElement>(null)
  const typed = useRef(false)

  useEffect(() => { setQ(initial); typed.current = false }, [initial])

  useEffect(() => {
    const f = () => { input.current?.focus(); input.current?.select() }
    window.addEventListener('psr:focus-search', f)
    return () => window.removeEventListener('psr:focus-search', f)
  }, [])

  useEffect(() => {
    const h = (e: MouseEvent) => { if (root.current && !root.current.contains(e.target as Node)) setOpen(false) }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [])

  // suggestions, debounced; bangs always, the rest only when the person wants them
  useEffect(() => {
    if (!typed.current) return
    const t = q.trim()
    const bang = t.startsWith('!') && !t.includes(' ')
    if (!t || (!bang && settings?.suggestions === false)) { setRows([]); return }
    let alive = true
    const id = window.setTimeout(() => {
      api.get<Suggestions>(`${V1}/suggest?q=${encodeURIComponent(q)}`).then(s => {
        if (!alive) return
        const out: Row[] = [
          ...s.bangs.map(b => ({ kind: 'bang' as const, text: '!' + b.trigger + ' ', label: b.name })),
          ...s.history.map(h => ({ kind: 'history' as const, text: h })),
          ...s.suggestions.map(h => ({ kind: 'suggestion' as const, text: h })),
        ]
        setRows(out)
        setHi(-1)
        setOpen(out.length > 0)
      }).catch(() => {})
    }, 150)
    return () => { alive = false; window.clearTimeout(id) }
  }, [q, settings?.suggestions])

  const go = (text: string) => {
    const t = text.trim()
    if (!t) return
    setOpen(false)
    input.current?.blur()
    const c = cat ? `&cat=${encodeURIComponent(cat)}` : ''
    const l = lens && !t.startsWith('!') ? `&lens=${lens}` : ''
    navigate(`/search?q=${encodeURIComponent(t)}${c}${l}`)
  }

  const pickRow = (r: Row) => {
    if (r.kind === 'bang') { setQ(r.text); typed.current = false; setOpen(false); input.current?.focus(); return }
    setQ(r.text)
    go(r.text)
  }

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') { if (open) { e.preventDefault(); setOpen(false) } else input.current?.blur(); return }
    if (!open || rows.length === 0) return
    if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => (h + 1) % rows.length) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => (h <= 0 ? rows.length - 1 : h - 1)) }
    else if (e.key === 'Tab' && hi >= 0 && rows[hi].kind === 'bang') { e.preventDefault(); pickRow(rows[hi]) }
  }

  return (
    <form ref={root} className={`sbox sbox-${size} ${open ? 'is-open' : ''}`} role="search" onSubmit={e => { e.preventDefault(); if (open && hi >= 0) pickRow(rows[hi]); else go(q) }}>
      <Icon name="search" size={size === 'lg' ? 20 : 18} className="sbox-ico" />
      <input ref={input} className="sbox-input" type="search" value={q} autoFocus={autoFocus} placeholder={placeholder} aria-label="Search"
        autoComplete="off" autoCorrect="off" autoCapitalize="off" spellCheck={false} enterKeyHint="search"
        role="combobox" aria-expanded={open} aria-autocomplete="list" aria-controls="sbox-list"
        onChange={e => { typed.current = true; setQ(e.target.value) }} onKeyDown={onKey} onFocus={() => { if (rows.length && typed.current) setOpen(true) }} />
      {q && <button type="button" className="sbox-clear" aria-label="Clear" title="Clear" onClick={() => { setQ(''); setRows([]); setOpen(false); input.current?.focus() }}><Icon name="x" size={16} /></button>}
      <button type="submit" className="sbox-go" aria-label="Search" title="Search"><Icon name="arrow-up" size={16} className="sbox-go-ico" /></button>
      {open && rows.length > 0 && (
        <ul id="sbox-list" className="sbox-pop" role="listbox">
          {rows.map((r, i) => (
            <li key={r.kind + r.text} role="option" aria-selected={i === hi}>
              <button type="button" className={`sbox-row ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onMouseDown={e => e.preventDefault()} onClick={() => pickRow(r)}>
                <Icon name={r.kind === 'history' ? 'clock' : r.kind === 'bang' ? 'zap' : 'search'} size={15} className="sbox-row-ico" />
                {r.kind === 'bang'
                  ? <span className="grow ellipsis"><b className="sbox-bang">{r.text.trim()}</b> <span className="muted">— {r.label}</span></span>
                  : <span className="grow ellipsis">{r.text}</span>}
                {r.kind === 'history' && <span className="sbox-row-tag">Earlier</span>}
              </button>
            </li>
          ))}
        </ul>
      )}
    </form>
  )
}
