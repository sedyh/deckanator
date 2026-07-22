import { CheckUpdate } from '../../wailsjs/go/internal/App.js'

// A shared update check, first run at boot: the settings panel opens
// with the answer already cached instead of flashing "Checking...".
// Until the first answer (`resolved`) the panel shows nothing; manual
// re-checks keep the button visible in its checking state.
// States: checking | uptodate | available | error. `supported` says
// whether this build can install updates itself (the Linux flatpak).
let state = { state: 'checking', latest: '', supported: false, resolved: false }
const listeners = new Set()

export function getUpdateState() { return state }

export function onUpdateState(cb) {
  listeners.add(cb)
  return () => listeners.delete(cb)
}

function set(next) {
  state = next
  for (const cb of Array.from(listeners)) {
    try { cb(state) } catch (err) { console.error('[update]', err) }
  }
}

export function startUpdateCheck() {
  set({ ...state, state: 'checking' })
  CheckUpdate().then(info => {
    set({
      state: info.available ? 'available' : 'uptodate',
      latest: info.version,
      supported: info.supported,
      resolved: true,
    })
  }).catch(() => set({ ...state, state: 'error', resolved: true }))
}
