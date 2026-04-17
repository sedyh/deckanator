// Shared gamepad fire tracker.
// Allows components to skip native (isTrusted) keyboard events
// that are duplicates of synthetic events we already dispatched.
const COOLDOWN_MS = 120

const lastFired = {}

export function trackFire(key) {
  lastFired[key] = Date.now()
}

export function wasFiredRecently(key) {
  return !!lastFired[key] && Date.now() - lastFired[key] < COOLDOWN_MS
}
