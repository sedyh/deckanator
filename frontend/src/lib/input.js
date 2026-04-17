// Godot-like input action system.
//
// Problems this solves on Steam Deck WebKit2GTK:
//   1. D-Pad autorepeat after ~1s hold (Steam Input forwards OS autorepeat).
//   2. Duplicate events when Steam Input native keyboard races with our
//      gamepad polling.
//   3. Sticks randomly missing direction changes between polls.
//   4. Stale keys when a native keyup is lost (app loses focus during nav).
//
// Design (no interception to avoid past WebKit2GTK crashes):
//   - Actions named with multiple triggers (keys, buttons, axes).
//   - Native events are tracked in bubble phase only; never preventDefault'd.
//   - Autorepeat is filtered by each keydown handler via consumeKey().
//   - consumeKey() is a central dedup: rejects a key that's already in flight
//     until the matching keyup arrives (or a safety timeout elapses).
//   - Gamepad poll at 16ms with stick hysteresis (enter 0.5, exit 0.3).
//   - Synthetic keydowns are only dispatched for gamepad-only presses so we
//     don't double-fire when Steam Input already sent a native event.

const POLL_MS = 16
const DEFAULT_DEADZONE = 0.5
const AXIS_RELEASE_DEADZONE = 0.3
const NATIVE_SAFETY_MS = 1500
const SYNTHETIC_HOLD_MS = 200
const STALE_KEY_MS = 500

const actionDefs = new Map()
const actionState = new Map()
const listeners = { pressed: new Map(), released: new Map() }

const keyCodesDown = new Set()
const keyKeysDown = new Set()
const lastKeyActivity = new Map()

const consumed = new Map()

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

// Call at the top of every keydown handler. Returns true if the event should
// be processed; false if it's a duplicate (autorepeat, Steam Input echo,
// native+gamepad race). State is cleared on native keyup, synthetic timeout,
// or a safety timeout so things never get permanently stuck.
export function consumeKey(e) {
  if (e.repeat) return false
  const key = e.key
  if (!key) return true
  if (consumed.has(key)) return false

  const ttl = e.isTrusted ? NATIVE_SAFETY_MS : SYNTHETIC_HOLD_MS
  const timer = setTimeout(() => consumed.delete(key), ttl)
  consumed.set(key, timer)
  return true
}

function releaseConsumed(key) {
  const t = consumed.get(key)
  if (t) clearTimeout(t)
  consumed.delete(key)
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

function pruneStale() {
  const now = performance.now()
  for (const code of Array.from(keyCodesDown)) {
    const t = lastKeyActivity.get('code:' + code)
    if (t === undefined || now - t > STALE_KEY_MS) {
      keyCodesDown.delete(code)
      lastKeyActivity.delete('code:' + code)
    }
  }
  for (const key of Array.from(keyKeysDown)) {
    const t = lastKeyActivity.get('key:' + key)
    if (t === undefined || now - t > STALE_KEY_MS) {
      keyKeysDown.delete(key)
      lastKeyActivity.delete('key:' + key)
    }
  }
}

function update() {
  try {
    pruneStale()
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
        if (!keyPressed && gpPressed && def.emitKey) {
          dispatchSynthetic(def.emitKey)
        }
      }
      if (s.justReleased) {
        fire('released', name)
        if (def.emitKey?.key) releaseConsumed(def.emitKey.key)
      }
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
  if (!keyMatchesAnyAction(e)) return
  if (isEditable(e.target)) return
  const now = performance.now()
  if (e.code) lastKeyActivity.set('code:' + e.code, now)
  if (e.key) lastKeyActivity.set('key:' + e.key, now)
  if (e.repeat) return
  if (e.code) keyCodesDown.add(e.code)
  if (e.key) keyKeysDown.add(e.key)
}

function onKeyUp(e) {
  if (!e.isTrusted) return
  if (e.code) { keyCodesDown.delete(e.code); lastKeyActivity.delete('code:' + e.code) }
  if (e.key) { keyKeysDown.delete(e.key); lastKeyActivity.delete('key:' + e.key); releaseConsumed(e.key) }
}

function onBlur() {
  keyCodesDown.clear()
  keyKeysDown.clear()
  lastKeyActivity.clear()
  for (const [, t] of consumed) clearTimeout(t)
  consumed.clear()
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
  for (const [, t] of consumed) clearTimeout(t)
  consumed.clear()
  actionDefs.clear()
  actionState.clear()
  listeners.pressed.clear()
  listeners.released.clear()
  keyCodesDown.clear()
  keyKeysDown.clear()
  lastKeyActivity.clear()
}
