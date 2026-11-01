import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Search', keys: [[['/'], 'Jump to the search box'], [['n'], 'New search'], [['⌘', 'K'], 'Commands'], [['!'], 'Bangs: !w, !yt, !notes… at the start or end']] },
  { title: 'Results', keys: [[['j'], 'Next result'], [['k'], 'Previous result'], [['o'], 'Open the result'], [['r'], 'Open it in the reading view'], [['1'], 'All'], [['2'], 'Pictures'], [['3'], 'Videos'], [['4'], 'News'], [['5'], 'Your apps']] },
  { title: 'Go to', keys: [[['h'], 'History'], [['s'], 'Sites'], [['b'], 'Bangs'], [[','], 'Settings']] },
  { title: 'Window', keys: [[['['], 'Show or hide the sidebar'], [['esc'], 'Close a dialog or menu']] },
  { title: 'Help', keys: [[['?'], 'This sheet']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map(k => <kbd key={k}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
    </Modal>
  )
}
