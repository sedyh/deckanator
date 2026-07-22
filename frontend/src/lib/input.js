// Godot-like input action system.
//
// Problems this solves on Steam Deck WebKit2GTK:
//   1. D-Pad autorepeat after ~1s hold (Steam Input forwards OS autorepeat,
//      sometimes as keydown/keyup pairs that bypass e.repeat).
//   2. Duplicate events when Steam Input native keyboard races with our
//      gamepad polling.
//   3. libmanette asserts in WebKit Gamepad backend if getGamepads() is
//      called before the backend is fully initialized. We gate polling on
//      gamepadconnected and delay the first poll.
//   4. Diagonal stick input used to trigger both axes; now a perpendicular
//      axis can suppress its neighbour via requirePairUnder.
//   5. Steam Input creates a virtual "masked" XInput copy of the physical
//      Valve gamepad. Both appear in getGamepads() with nearly identical
//      timestamps. We filter the masked copy to avoid double-firing.
//
// Design:
//   - Actions named with multiple triggers (keys, buttons, axes).
//   - Native events tracked in bubble phase; never preventDefault'd.
//   - consumeKey() debounces keydown per logical key (TTL + min-gap) so
//     autorepeat and native<->synthetic races can't double-fire.
//   - Gamepad poll uses requestAnimationFrame (vsync-aligned, pauses when
//     hidden, doesn't compete with rendering or cursor event delivery).
//   - Gamepad poll starts only after the first gamepadconnected event and
//     a 500ms warm-up to avoid libmanette init-time asserts.
//   - Input processing stops when the window loses focus so Steam Deck
//     overlays (QAM, keyboard) don't receive doubled events.
//   - Native OS autorepeat is always rejected; actions marked repeat
//     generate their own repeats in the poll loop instead (works uniformly
//     for held keys and held gamepad buttons).

const DEFAULT_DEADZONE = 0.5
const AXIS_RELEASE_DEADZONE = 0.3
const NATIVE_SAFETY_MS = 1500
const SYNTHETIC_HOLD_MS = 200
const STALE_KEY_MS = 500
const MIN_ACCEPT_GAP_MS = 100
const GAMEPAD_WARMUP_MS = 500
// Two gamepads whose timestamps differ by less than this are treated as
// the same physical device (Steam Input masked copy).
const MASKED_GAMEPAD_TIMESTAMP_DELTA = 10
// Self-driven repeat for held directional actions. The interval must stay
// above MIN_ACCEPT_GAP_MS or consumeKey would reject the repeats.
const REPEAT_DELAY_MS = 400
const REPEAT_INTERVAL_MS = 150

const actionDefs = new Map()
const actionState = new Map()
const listeners = { pressed: new Map(), released: new Map() }

const keyCodesDown = new Set()
const keyKeysDown = new Set()
const lastKeyActivity = new Map()

const consumed = new Map()
const lastAccepted = new Map()
const pendingRelease = new Map()
const KEYUP_RELEASE_DELAY_MS = 150

let rafHandle = null
let started = false
let windowFocused = true
let gamepadReady = false
let gamepadReadyAt = 0

// Stale-key eviction guards against lost keyups (Steam Input can drop
// them), relying on OS autorepeat to refresh key activity. macOS breaks
// that assumption: releasing one key mid-hold cancels the pending
// autorepeat of another (overlapped direction reversal), starving the
// refresh and evicting a genuinely held key. Keyups there are reliable
// (and blur clears state), so skip eviction entirely.
const RELIABLE_KEYUPS = /Mac/i.test(navigator.platform || navigator.userAgent)

// --- input mode ------------------------------------------------------------
// Last physical input source: 'gamepad' | 'keyboard' | 'touch'. Keyboard
// and mouse count as one mode. Steam Input mirrors gamepad presses as
// keyboard events and touches as mouse events, so the "losing" source is
// suppressed for a short window after the authoritative one fires.
// The Linux build ships to Steam Deck (controller-first); desktop
// macOS/Windows start as keyboard+mouse until proven otherwise.
const DEFAULT_INPUT_MODE = /Linux/i.test(navigator.platform || navigator.userAgent)
  ? 'gamepad'
  : 'keyboard'
let inputMode = DEFAULT_INPUT_MODE
const modeListeners = new Set()
let lastGamepadActivity = 0
let lastTouchActivity = 0
let modeMouseX = -1
let modeMouseY = -1
const MODE_SUPPRESS_MS = 800

export function getInputMode() { return inputMode }

// While Steam's on-screen keyboard is up, gamescope double-routes its
// trackpad pointers into the app as mouse moves; the lock keeps those
// from flipping the mode away from gamepad (and unhiding the cursor).
let modeLock = false
export function setInputModeLock(v) { modeLock = v }

export function onInputModeChange(cb) {
  modeListeners.add(cb)
  return () => modeListeners.delete(cb)
}

function setInputMode(mode) {
  if (mode === inputMode) return
  inputMode = mode
  for (const cb of Array.from(modeListeners)) {
    try { cb(mode) } catch (err) { console.error('[input] mode listener', err) }
  }
}

export function registerAction(name, triggers, emitKey = null, opts = {}) {
  actionDefs.set(name, { triggers, emitKey, repeat: opts.repeat === true })
  actionState.set(name, {
    pressed: false,
    strength: 0,
    justPressed: false,
    justReleased: false,
    sourceKey: false,
    sourceGamepad: false,
    nextRepeatAt: 0,
    suppressed: false,
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

// Call at the top of every keydown handler. Rejects a keydown when:
//   - it's browser autorepeat (e.repeat === true),
//   - the same key already accepted a press that hasn't been released yet
//     (consumed map, released on keyup or TTL),
//   - the same key was accepted less than MIN_ACCEPT_GAP_MS ago (catches
//     Steam Input's keydown/keyup/keydown autorepeat that bypasses e.repeat).
export function consumeKey(e) {
  if (e.repeat) return false
  const key = e.key
  if (!key) return true

  if (pendingRelease.has(key)) {
    cancelPendingRelease(key)
    return false
  }

  const now = performance.now()
  const prev = lastAccepted.get(key) ?? 0
  if (now - prev < MIN_ACCEPT_GAP_MS) return false

  if (consumed.has(key)) return false

  const ttl = e.isTrusted ? NATIVE_SAFETY_MS : SYNTHETIC_HOLD_MS
  const timer = setTimeout(() => consumed.delete(key), ttl)
  consumed.set(key, timer)
  lastAccepted.set(key, now)
  return true
}

function releaseConsumed(key) {
  const t = consumed.get(key)
  if (t) clearTimeout(t)
  consumed.delete(key)
  cancelPendingRelease(key)
}

function cancelPendingRelease(key) {
  const t = pendingRelease.get(key)
  if (t) { clearTimeout(t); pendingRelease.delete(key) }
}

// On native keyup we don't release `consumed` immediately. Steam Input can
// deliver autorepeat as keydown/keyup/keydown pairs without e.repeat=true,
// and an eager release would let every other press through. We defer the
// release, and every fresh keyup reschedules it, so as long as the key is
// being autorepeated `consumed` stays latched. A genuine release (no new
// keydown for 150ms) finally fires the release and unlatches the key.
function scheduleReleaseConsumed(key) {
  cancelPendingRelease(key)
  const t = setTimeout(() => {
    pendingRelease.delete(key)
    const ct = consumed.get(key)
    if (ct) clearTimeout(ct)
    consumed.delete(key)
  }, KEYUP_RELEASE_DELAY_MS)
  pendingRelease.set(key, t)
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

function safeGetGamepads() {
  if (!gamepadReady) return null
  if (performance.now() - gamepadReadyAt < GAMEPAD_WARMUP_MS) return null
  try { return navigator.getGamepads ? navigator.getGamepads() : null }
  catch (err) { console.error('[input] getGamepads', err); return null }
}

// Returns true if gamepad is a Valve device (physical Steam Deck controller
// or Steam Controller). Valve's USB vendor ID is 0x28de.
function isValveGamepad(gp) {
  return gp != null && gp.id.includes('Vendor: 28de')
}

// Returns true if gamepad is a virtual XInput copy created by Steam Input.
// Steam Input mirrors the physical Valve gamepad as a standard controller;
// both share nearly identical timestamps. We skip the masked copy so each
// physical button press only fires once.
function isMaskedGamepad(pads, gp) {
  for (let i = 0; i < pads.length; i++) {
    const valve = pads[i]
    if (!valve || !isValveGamepad(valve)) continue
    if (Math.abs(valve.timestamp - gp.timestamp) <= MASKED_GAMEPAD_TIMESTAMP_DELTA) {
      return true
    }
  }
  return false
}

// On Linux the evdev -> libmanette -> WebKit chain swaps X/Y relative
// to the standard gamepad layout (the BTN_NORTH/BTN_WEST confusion).
// This hits every pad it surfaces - including Steam's virtual gamepad
// in Deck gaming mode, whose id carries no Valve vendor marker - so the
// swap is keyed on the platform, not the device.
const SWAP_XY = /Linux/i.test(navigator.platform || navigator.userAgent)

function padButtonIndex(index) {
  if (SWAP_XY && (index === 2 || index === 3)) return 5 - index
  return index
}

function evalGamepad(def, wasPressed) {
  let pressed = false
  let strength = 0
  const pads = safeGetGamepads()
  if (!pads) return { pressed: false, strength: 0 }

  for (let i = 0; i < pads.length; i++) {
    const gp = pads[i]
    if (!gp) continue
    if (!isValveGamepad(gp) && isMaskedGamepad(pads, gp)) continue

    for (const t of def.triggers) {
      if (t.type === 'button') {
        const b = gp.buttons && gp.buttons[padButtonIndex(t.index)]
        if (b && b.pressed) {
          pressed = true
          if (b.value > strength) strength = b.value
        }
      } else if (t.type === 'axis') {
        if (!gp.axes) continue
        const enterDz = t.deadzone ?? DEFAULT_DEADZONE
        const exitDz = t.releaseDeadzone ?? AXIS_RELEASE_DEADZONE
        const dz = wasPressed ? exitDz : enterDz
        const raw = (gp.axes[t.index] ?? 0) * (t.sign ?? 1)
        if (raw <= dz) continue
        // requirePairUnder: suppress this axis when the paired perpendicular
        // axis exceeds its threshold. Used to bias diagonal stick input to
        // vertical only, so carousel (horizontal) triggers only on strictly
        // horizontal stick motion.
        if (t.requirePairUnder) {
          const other = Math.abs(gp.axes[t.requirePairUnder.axis] ?? 0)
          if (other > t.requirePairUnder.threshold) continue
        }
        pressed = true
        if (raw > strength) strength = raw
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
    // Dispatch to the focused element so component-local keydown handlers
    // (e.g. SteamSelect dropdown on the trigger button) receive the event.
    // It still bubbles up to window for the global router.
    const ae = document.activeElement
    const target = ae && ae !== document.body && ae !== document.documentElement
      ? ae
      : window
    const unhandled = target.dispatchEvent(ev)
    // Native buttons only activate on trusted events, so a synthetic
    // Enter that nothing handled clicks the focused control explicitly
    // (gamepad A on plain buttons like New Profile).
    if (unhandled && emitKey.key === 'Enter' && target !== window && typeof target.click === 'function') {
      target.click()
    }
  } catch (err) {
    console.error('[input] dispatchSynthetic', err)
  }
}

function pruneStale() {
  if (RELIABLE_KEYUPS) return
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
    // The blur event alone can't be trusted: the Steam overlay in
    // gaming mode may take input focus without one. Ask the document
    // directly every frame so overlay navigation doesn't leak in.
    if (windowFocused && !document.hasFocus()) onWindowBlur()
    else if (!windowFocused && document.hasFocus()) onWindowFocus()
    if (windowFocused) {
      const now = performance.now()
      let anyGamepad = false
      for (const [name, def] of actionDefs) {
        const s = actionState.get(name)
        const keyPressed = evalKeys(def)
        const { pressed: gpPressed, strength: gpStrength } = evalGamepad(def, s.sourceGamepad)
        if (gpPressed) anyGamepad = true
        const pressed = keyPressed || gpPressed
        // A button held across a focus loss must not re-fire on refocus:
        // closing a screen shuffles window focus, and a held B would
        // otherwise cascade one Escape per focus cycle until it backed
        // out of the whole app.
        if (s.suppressed) {
          if (!pressed) s.suppressed = false
          continue
        }
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
          s.nextRepeatAt = now + REPEAT_DELAY_MS
        } else if (pressed && def.repeat && def.emitKey && now >= s.nextRepeatAt) {
          // Self-driven repeat while held (key or gamepad). Native OS
          // autorepeat never gets through consumeKey, so we unlatch the
          // key and emit a synthetic press on our own cadence.
          releaseConsumed(def.emitKey.key)
          dispatchSynthetic(def.emitKey)
          s.nextRepeatAt = now + REPEAT_INTERVAL_MS
        }
        if (s.justReleased) {
          fire('released', name)
          if (def.emitKey?.key) releaseConsumed(def.emitKey.key)
        }
      }
      if (anyGamepad) {
        lastGamepadActivity = now
        setInputMode('gamepad')
      }
    }
  } catch (err) {
    console.error('[input] update', err)
  }
  rafHandle = requestAnimationFrame(update)
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
  // Steam Input mirrors gamepad presses as keyboard events; don't let
  // them flip the mode away from gamepad.
  if (performance.now() - lastGamepadActivity > MODE_SUPPRESS_MS) {
    setInputMode('keyboard')
  }
  // Steam Input's desktop-style template mirrors the Deck's Y button as
  // Space, which a focused control treats as a click (dropdowns opened
  // as if A was pressed). Swallow Space entirely outside editable
  // fields; our own navigation only ever uses Enter.
  if (e.key === ' ' && !isEditable(e.target)) {
    e.preventDefault()
    e.stopPropagation()
    return
  }
  if (!keyMatchesAnyAction(e)) return
  if (isEditable(e.target)) return
  // Escape, M and O have no useful default action outside editable
  // fields; cancel them unconditionally so macOS never beeps for them,
  // even when the press ends up doing nothing.
  if (e.key === 'Escape' || e.code === 'KeyM' || e.code === 'KeyO') e.preventDefault()
  const now = performance.now()
  if (e.code) lastKeyActivity.set('code:' + e.code, now)
  if (e.key) lastKeyActivity.set('key:' + e.key, now)
  if (e.repeat) {
    // OS autorepeat is always discarded (we repeat on our own cadence).
    // Cancel it too: on macOS WKWebView plays the system beep for every
    // keydown that ends up unhandled.
    e.preventDefault()
    return
  }
  if (e.code) keyCodesDown.add(e.code)
  if (e.key) keyKeysDown.add(e.key)
}

function onKeyUp(e) {
  if (!e.isTrusted) return
  if (e.code) { keyCodesDown.delete(e.code); lastKeyActivity.delete('code:' + e.code) }
  if (e.key) {
    keyKeysDown.delete(e.key)
    lastKeyActivity.delete('key:' + e.key)
    scheduleReleaseConsumed(e.key)
  }
}

function onWindowFocus() {
  windowFocused = true
}

function onWindowBlur() {
  windowFocused = false
  keyCodesDown.clear()
  keyKeysDown.clear()
  lastKeyActivity.clear()
  for (const [, t] of consumed) clearTimeout(t)
  consumed.clear()
  for (const [, t] of pendingRelease) clearTimeout(t)
  pendingRelease.clear()
  lastAccepted.clear()
  // Actions frozen mid-press would otherwise resume with stale repeat
  // timers on refocus and burst-fire (e.g. after the Steam overlay).
  // The suppression flag holds until the control is physically released.
  for (const [, s] of actionState) {
    s.pressed = false
    s.strength = 0
    s.justPressed = false
    s.justReleased = false
    s.sourceKey = false
    s.sourceGamepad = false
    s.nextRepeatAt = 0
    s.suppressed = true
  }
}

function onModeMouseMove(e) {
  if (modeLock) return
  // WebKit synthesizes mousemove after scroll and touch taps emit compat
  // mouse events: only physical movement (changed coordinates outside the
  // touch suppression window) counts as mouse usage.
  if (e.clientX === modeMouseX && e.clientY === modeMouseY) return
  modeMouseX = e.clientX
  modeMouseY = e.clientY
  if (performance.now() - lastTouchActivity > MODE_SUPPRESS_MS) {
    setInputMode('keyboard')
  }
}

function onModeMouseDown() {
  if (modeLock) return
  if (performance.now() - lastTouchActivity > MODE_SUPPRESS_MS) {
    setInputMode('keyboard')
  }
}

function onModeTouchStart() {
  lastTouchActivity = performance.now()
  setInputMode('touch')
}

function onGamepadConnected() {
  if (!gamepadReady) {
    gamepadReady = true
    gamepadReadyAt = performance.now()
  }
}

function onGamepadDisconnected() {
  try {
    const pads = navigator.getGamepads ? navigator.getGamepads() : null
    const anyLeft = pads && Array.from(pads).some(p => p != null)
    if (!anyLeft) gamepadReady = false
  } catch { gamepadReady = false }
}

export function init() {
  if (started) return
  started = true
  window.addEventListener('keydown', onKeyDown, true)
  window.addEventListener('keyup', onKeyUp, true)
  window.addEventListener('focus', onWindowFocus)
  window.addEventListener('blur', onWindowBlur)
  window.addEventListener('gamepadconnected', onGamepadConnected)
  window.addEventListener('gamepaddisconnected', onGamepadDisconnected)
  window.addEventListener('mousemove', onModeMouseMove, { capture: true, passive: true })
  window.addEventListener('mousedown', onModeMouseDown, { capture: true, passive: true })
  window.addEventListener('wheel', onModeMouseDown, { capture: true, passive: true })
  window.addEventListener('touchstart', onModeTouchStart, { capture: true, passive: true })
  rafHandle = requestAnimationFrame(update)
}

export function destroy() {
  if (!started) return
  started = false
  window.removeEventListener('keydown', onKeyDown, true)
  window.removeEventListener('keyup', onKeyUp, true)
  window.removeEventListener('focus', onWindowFocus)
  window.removeEventListener('blur', onWindowBlur)
  window.removeEventListener('gamepadconnected', onGamepadConnected)
  window.removeEventListener('gamepaddisconnected', onGamepadDisconnected)
  window.removeEventListener('mousemove', onModeMouseMove, { capture: true })
  window.removeEventListener('mousedown', onModeMouseDown, { capture: true })
  window.removeEventListener('wheel', onModeMouseDown, { capture: true })
  window.removeEventListener('touchstart', onModeTouchStart, { capture: true })
  if (rafHandle != null) { cancelAnimationFrame(rafHandle); rafHandle = null }
  for (const [, t] of consumed) clearTimeout(t)
  consumed.clear()
  for (const [, t] of pendingRelease) clearTimeout(t)
  pendingRelease.clear()
  lastAccepted.clear()
  actionDefs.clear()
  actionState.clear()
  listeners.pressed.clear()
  listeners.released.clear()
  keyCodesDown.clear()
  keyKeysDown.clear()
  lastKeyActivity.clear()
  gamepadReady = false
  windowFocused = true
  inputMode = DEFAULT_INPUT_MODE
  modeListeners.clear()
  lastGamepadActivity = 0
  lastTouchActivity = 0
  modeMouseX = -1
  modeMouseY = -1
}
