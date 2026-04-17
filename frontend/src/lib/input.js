// Godot-like input action system.
//
// Problem we solve:
//   On Steam Deck, Steam Input maps gamepad -> native keyboard events while
//   our gamepad polling also fires. Both sources reach keydown handlers,
//   causing duplicates. Held D-Pad produces native autorepeat (e.repeat=true)
//   which fires handlers multiple times per press. Sticks drift can miss
//   direction changes in naive polling.
//
// Design:
//   Named actions with multiple triggers (keys, buttons, axes).
//   Per-action edge detection at a single update tick (8ms).
//   Native autorepeat is suppressed at the window capture phase.
//   Gamepad polling dispatches a synthetic keydown to window only when the
//   action transitioned to pressed via a gamepad trigger (no native source),
//   avoiding duplicates when Steam Input already sent a keyboard event.
//   Stick hysteresis (higher press deadzone, lower release deadzone) keeps
//   held directions stable across polls.

const POLL_MS = 8
const DEFAULT_DEADZONE = 0.5
const AXIS_RELEASE_DEADZONE = 0.3

const actionDefs = new Map()
const actionState = new Map()
const listeners = { pressed: new Map(), released: new Map() }

const keyCodesDown = new Set()
const keyKeysDown = new Set()

let pollTimer = null
let started = false

// triggers: Array of:
//   { type: 'key', code: 'KeyM' }       // prefer code for layout independence
//   { type: 'key', key: 'ArrowUp' }     // for named keys
//   { type: 'button', index: 0 }
//   { type: 'axis', index: 0, sign: 1, deadzone?, releaseDeadzone? }
// emitKey: optional { key, code? } - synthetic keydown dispatched to window on
//          gamepad-sourced just_pressed (native keydowns already propagate)
export function registerAction(name, triggers, emitKey = null) {
  actionDefs.set(name, { triggers, emitKey })
  actionState.set(name, {
    pressed: false,
    strength: 0,
    justPressed: false,
    justReleased: false,
    sourceKey: false,
    sourceGamepad: false,
  })
}

export function isPressed(name) {
  return actionState.get(name)?.pressed ?? false
}

export function getStrength(name) {
  return actionState.get(name)?.strength ?? 0
}

export function onJustPressed(name, cb) {
  const arr = listeners.pressed.get(name) ?? []
  arr.push(cb)
  listeners.pressed.set(name, arr)
  return () => {
    const a = listeners.pressed.get(name) ?? []
    const i = a.indexOf(cb)
    if (i >= 0) a.splice(i, 1)
  }
}

export function onJustReleased(name, cb) {
  const arr = listeners.released.get(name) ?? []
  arr.push(cb)
  listeners.released.set(name, arr)
  return () => {
    const a = listeners.released.get(name) ?? []
    const i = a.indexOf(cb)
    if (i >= 0) a.splice(i, 1)
  }
}

function evalKeys(def) {
  for (const t of def.triggers) {
    if (t.type !== 'key') continue
    if ((t.code && keyCodesDown.has(t.code)) || (t.key && keyKeysDown.has(t.key))) {
      return true
    }
  }
  return false
}

function evalGamepad(def, wasPressed) {
  let pressed = false
  let strength = 0
  for (const t of def.triggers) {
    if (t.type === 'button') {
      for (const gp of navigator.getGamepads()) {
        if (!gp) continue
        const b = gp.buttons[t.index]
        if (b?.pressed) {
          pressed = true
          if (b.value > strength) strength = b.value
        }
      }
    } else if (t.type === 'axis') {
      const enterDz = t.deadzone ?? DEFAULT_DEADZONE
      const exitDz = t.releaseDeadzone ?? AXIS_RELEASE_DEADZONE
      const dz = wasPressed ? exitDz : enterDz
      for (const gp of navigator.getGamepads()) {
        if (!gp) continue
        const raw = (gp.axes[t.index] ?? 0) * (t.sign ?? 1)
        if (raw > dz) {
          pressed = true
          if (raw > strength) strength = raw
        }
      }
    }
  }
  return { pressed, strength }
}

function fire(mapName, action) {
  const arr = listeners[mapName].get(action)
  if (!arr) return
  for (const cb of arr.slice()) {
    try { cb() } catch (err) { console.error('[input]', mapName, action, err) }
  }
}

function dispatchSynthetic(emitKey) {
  const { key, code } = emitKey
  window.dispatchEvent(new KeyboardEvent('keydown', {
    key,
    code: code ?? '',
    bubbles: true,
    cancelable: true,
  }))
}

function update() {
  for (const [name, def] of actionDefs) {
    const s = actionState.get(name)
    const keyPressed = evalKeys(def)
    const { pressed: gpPressed, strength: gpStrength } = evalGamepad(def, s.sourceGamepad)
    const pressed = keyPressed || gpPressed
    const strength = keyPressed ? 1 : gpStrength
    const prev = s.pressed
    const prevGp = s.sourceGamepad

    s.pressed = pressed
    s.strength = strength
    s.sourceKey = keyPressed
    s.sourceGamepad = gpPressed
    s.justPressed = pressed && !prev
    s.justReleased = !pressed && prev

    if (s.justPressed) {
      fire('pressed', name)
      // Only synthesize for gamepad-initiated press. Native keydown already
      // propagated its own event via the browser's normal dispatch.
      if (!keyPressed && gpPressed && def.emitKey) {
        dispatchSynthetic(def.emitKey)
      }
    } else if (pressed && gpPressed && !prevGp && !keyPressed && def.emitKey) {
      // Edge: key was already holding the action, then user pressed a gamepad
      // button. Don't re-fire. But if key released and gamepad still holds,
      // we don't want a new fire either (prev was true, still is). This branch
      // intentionally does nothing.
    }
    if (s.justReleased) fire('released', name)
  }
}

function isEditable(el) {
  if (!el) return false
  const tag = el.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA') return !el.readOnly && !el.disabled
  return el.isContentEditable === true
}

function keyMatchesAnyAction(e) {
  for (const def of actionDefs.values()) {
    for (const t of def.triggers) {
      if (t.type !== 'key') continue
      if (t.code && t.code === e.code) return true
      if (t.key && t.key === e.key) return true
    }
  }
  return false
}

function onKeyDown(e) {
  if (!e.isTrusted) return
  if (!keyMatchesAnyAction(e)) return
  // Let text inputs handle their own keys (incl. autorepeat for typing).
  if (isEditable(e.target)) return
  // Suppress keyboard autorepeat - we want just_pressed semantics.
  // This is the fix for held D-Pad duplicating navigation.
  if (e.repeat) {
    e.preventDefault()
    e.stopImmediatePropagation()
    return
  }
  if (e.code) keyCodesDown.add(e.code)
  if (e.key) keyKeysDown.add(e.key)
}

function onKeyUp(e) {
  if (!e.isTrusted) return
  if (e.code) keyCodesDown.delete(e.code)
  if (e.key) keyKeysDown.delete(e.key)
}

function onBlur() {
  keyCodesDown.clear()
  keyKeysDown.clear()
}

export function init() {
  if (started) return
  started = true
  window.addEventListener('keydown', onKeyDown, true)
  window.addEventListener('keyup', onKeyUp, true)
  window.addEventListener('blur', onBlur)
  pollTimer = setInterval(update, POLL_MS)
}

export function destroy() {
  if (!started) return
  started = false
  window.removeEventListener('keydown', onKeyDown, true)
  window.removeEventListener('keyup', onKeyUp, true)
  window.removeEventListener('blur', onBlur)
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  actionDefs.clear()
  actionState.clear()
  listeners.pressed.clear()
  listeners.released.clear()
  keyCodesDown.clear()
  keyKeysDown.clear()
}
