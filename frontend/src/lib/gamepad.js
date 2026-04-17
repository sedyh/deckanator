// Abstract action state shared between gamepad polling and keyboard handlers.
// Prevents duplicate navigation when both Steam Input and our polling fire
// for the same physical button press.

const active = new Set()

// Try to activate an action. Returns true if this is the first activation
// (i.e. the action was not already active). Call this before processing.
export function tryActivate(key) {
  if (active.has(key)) return false
  active.add(key)
  return true
}

// Mark an action as released so the next press is accepted.
export function release(key) {
  active.delete(key)
}
