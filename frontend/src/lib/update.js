import { CheckUpdate } from '../../wailsjs/go/internal/App.js'

// One shared update check per app run, started at boot: the settings
// panel opens with the answer already cached instead of flashing
// "Checking...". While unresolved the row simply isn't shown.
// States: checking | uptodate | available | error. `supported` says
// whether this build can install updates itself (the Linux flatpak).
let state = { state: 'checking', latest: '', supported: false }
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
  CheckUpdate().then(info => {
    set({
      state: info.available ? 'available' : 'uptodate',
      latest: info.version,
      supported: info.supported,
    })
  }).catch(() => set({ state: 'error', latest: '', supported: false }))
}
