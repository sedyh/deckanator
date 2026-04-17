// Godot-like input action system.
//
// Passive design to avoid WebKit2GTK instability on Steam Deck:
//   - Never intercepts or prevents native events (no capture phase, no
//     stopImmediatePropagation). Native keydowns propagate to handlers as usual.
//   - Tracks native key state + gamepad state separately and unifies per action.
//   - Synthetic keydown dispatched to window only for gamepad-exclusive presses,
//     so Steam Input native events don't duplicate with our polling.
//   - Handlers that want just_pressed semantics from the keyboard must check
//     e.repeat themselves.

const POLL_MS = 16
const DEFAULT_DEADZONE = 0.5
const AXIS_RELEASE_DEADZONE = 0.3

const actionDefs = new Map()
const actionState = new Map()
const listeners = { pressed: new Map(), released: new Map() }

const keyCodesDown = new Set()
const keyKeysDown = new Set()

let pollTimer = null
let started = false

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
  let pads
  try { pads = navigator.getGamepads ? navigator.getGamepads() : [] }
  catch { return { pressed: false, strength: 0 } }
  if (!pads) return { pressed: false, strength: 0 }

  for (const t of def.triggers) {
    if (t.type === 'button') {
      for (let i = 0; i < pads.length; i++) {
        const gp = pads[i]
        if (!gp) continue
        const b = gp.buttons && gp.buttons[t.index]
        if (b && b.pressed) {
          pressed = true
          if (b.value > strength) strength = b.value
        }
      }
    } else if (t.type === 'axis') {
      const enterDz = t.deadzone ?? DEFAULT_DEADZONE
      const exitDz = t.releaseDeadzone ?? AXIS_RELEASE_DEADZONE
      const dz = wasPressed ? exitDz : enterDz
      for (let i = 0; i < pads.length; i++) {
        const gp = pads[i]
        if (!gp || !gp.axes) continue
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
  try {
    const ev = new KeyboardEvent('keydown', {
      key: emitKey.key,
      code: emitKey.code ?? '',
      bubbles: true,
      cancelable: true,
    })
    window.dispatchEvent(ev)
  } catch (err) {
    console.error('[input] dispatchSynthetic', err)
  }
}

function update() {
  try {
    for (const [name, def] of actionDefs) {
      const s = actionState.get(name)
      const keyPressed = evalKeys(def)
      const { pressed: gpPressed, strength: gpStrength } = evalGamepad(def, s.sourceGamepad)
      const pressed = keyPressed || gpPressed
      const strength = keyPressed ? 1 : gpStrength
      const prev = s.pressed

      s.pressed = pressed
      s.strength = strength
      s.sourceKey = keyPressed
      s.sourceGamepad = gpPressed
      s.justPressed = pressed && !prev
      s.justReleased = !pressed && prev

      if (s.justPressed) {
        fire('pressed', name)
        // Only synthesize for gamepad-initiated press. Native keydown already
        // propagated through the browser, so we'd duplicate otherwise.
        if (!keyPressed && gpPressed && def.emitKey) {
          dispatchSynthetic(def.emitKey)
        }
      }
      if (s.justReleased) fire('released', name)
    }
  } catch (err) {
    console.error('[input] update', err)
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
  if (e.repeat) return
  if (!keyMatchesAnyAction(e)) return
  if (isEditable(e.target)) return
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
  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('keyup', onKeyUp)
  window.addEventListener('blur', onBlur)
  pollTimer = setInterval(update, POLL_MS)
}

export function destroy() {
  if (!started) return
  started = false
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('keyup', onKeyUp)
  window.removeEventListener('blur', onBlur)
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  actionDefs.clear()
  actionState.clear()
  listeners.pressed.clear()
  listeners.released.clear()
  keyCodesDown.clear()
  keyKeysDown.clear()
}
