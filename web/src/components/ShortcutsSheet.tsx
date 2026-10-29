import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Go to', keys: [[['h'], 'Today'], [['d'], 'Decks'], [['b'], 'Browse'], [['s'], 'Statistics'], [[','], 'Settings'], [['/'], 'Search & commands'], [['⌘', 'K'], 'Search & commands']] },
  { title: 'Make', keys: [[['r'], 'Study now'], [['n'], 'Add a card'], [['a'], 'Make cards with AI'], [['i'], 'Import']] },
  { title: 'While studying', keys: [[['space'], 'Show the answer, then Good'], [['1'], 'Again'], [['2'], 'Hard'], [['3'], 'Good'], [['4'], 'Easy'], [['z'], 'Undo the last answer'], [['e'], 'Edit the card'], [['-'], 'Bury until tomorrow'], [['!'], 'Set aside (suspend)'], [['esc'], 'End']] },
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
