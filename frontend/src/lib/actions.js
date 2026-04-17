// Action map for the launcher.
// Button indices match the standard Gamepad API layout (A=0, B=1, X=2, Y=3,
// D-Pad Up/Down/Left/Right = 12/13/14/15). Left stick is axes 0/1.

import { registerAction, init } from './input.js'

export function setupActions() {
  registerAction('ui_up', [
    { type: 'key', key: 'ArrowUp' },
    { type: 'button', index: 12 },
    { type: 'axis', index: 1, sign: -1 },
  ], { key: 'ArrowUp' })

  registerAction('ui_down', [
    { type: 'key', key: 'ArrowDown' },
    { type: 'button', index: 13 },
    { type: 'axis', index: 1, sign: 1 },
  ], { key: 'ArrowDown' })

  registerAction('ui_left', [
    { type: 'key', key: 'ArrowLeft' },
    { type: 'button', index: 14 },
    { type: 'axis', index: 0, sign: -1 },
  ], { key: 'ArrowLeft' })

  registerAction('ui_right', [
    { type: 'key', key: 'ArrowRight' },
    { type: 'button', index: 15 },
    { type: 'axis', index: 0, sign: 1 },
  ], { key: 'ArrowRight' })

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

  init()
}
