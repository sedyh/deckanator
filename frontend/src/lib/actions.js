// Action map for the launcher.
// Button indices match the standard Gamepad API layout (A=0, B=1, X=2, Y=3,
// D-Pad Up/Down/Left/Right = 12/13/14/15). Left stick is axes 0/1.

import { registerAction, init } from './input.js'

export function setupActions() {
  registerAction('ui_up', [
    { type: 'key', key: 'ArrowUp' },
    { type: 'button', index: 12 },
    { type: 'axis', index: 1, sign: -1 },
  ], { key: 'ArrowUp' }, { repeat: true })

  registerAction('ui_down', [
    { type: 'key', key: 'ArrowDown' },
    { type: 'button', index: 13 },
    { type: 'axis', index: 1, sign: 1 },
  ], { key: 'ArrowDown' }, { repeat: true })

  registerAction('ui_left', [
    { type: 'key', key: 'ArrowLeft' },
    { type: 'button', index: 14 },
    // Diagonal stick motion biases to vertical: horizontal fires only when
    // the perpendicular (Y) axis is quiet.
    { type: 'axis', index: 0, sign: -1, requirePairUnder: { axis: 1, threshold: 0.4 } },
  ], { key: 'ArrowLeft' }, { repeat: true })

  registerAction('ui_right', [
    { type: 'key', key: 'ArrowRight' },
    { type: 'button', index: 15 },
    { type: 'axis', index: 0, sign: 1, requirePairUnder: { axis: 1, threshold: 0.4 } },
  ], { key: 'ArrowRight' }, { repeat: true })

  registerAction('ui_accept', [
    { type: 'key', key: 'Enter' },
    { type: 'button', index: 0 },
  ], { key: 'Enter' })

  registerAction('ui_cancel', [
    { type: 'key', key: 'Escape' },
    { type: 'button', index: 1 },
  ], { key: 'Escape' })

  registerAction('ui_mods', [
    { type: 'key', code: 'KeyM' },
    { type: 'button', index: 3 },
  ], { key: 'm', code: 'KeyM' })

  // Settings lives on X. Note for the Deck: the shortcut's default
  // layout starts in its "desktop" action set, where Steam binds X to
  // "Show Keyboard" (and Start toggles action sets, so Start is off
  // limits for us). The layout's "gamepad" action set leaves X clean.
  registerAction('ui_settings', [
    { type: 'key', code: 'KeyO' },
    { type: 'button', index: 2 },
  ], { key: 'o', code: 'KeyO' })

  init()
}
