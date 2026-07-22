<script>
  import { createEventDispatcher, onMount, onDestroy, tick } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import { consumeKey } from '../lib/input.js'
  import { InstallUpdate, QuitLauncher } from '../../wailsjs/go/internal/App.js'
  import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime.js'
  import { getUpdateState, onUpdateState, startUpdateCheck } from '../lib/update.js'

  export let settings = { closeAfterLaunch: true, memoryMinMb: 0, memoryMaxMb: 0, fullscreen: false }
  export let version = ''

  const dispatch = createEventDispatcher()

  // Self-update row. Deliberately Flathub-free: the bundle comes from
  // GitHub and installs over the present runtime, so it works on
  // networks where Flathub is blocked. Steam needs no restart - the
  // next launch runs the new version. The check itself runs once at app
  // boot (lib/update.js); the panel opens with the cached answer and
  // the row appears only when there is one.
  // Extra local states: working | done
  let upd = { ...getUpdateState(), stage: '', pct: -1 }
  const unsubUpd = onUpdateState(s => {
    // A shared re-check must not clobber an install in flight.
    if (upd.state === 'working' || upd.state === 'done') return
    upd = { ...upd, ...s }
  })

  async function updateAction() {
    if (upd.state === 'error') { upd = { ...upd, state: 'checking' }; startUpdateCheck(); return }
    if (upd.state === 'done') { QuitLauncher(); return }
    if (upd.state !== 'available' || !upd.supported) return
    upd = { ...upd, state: 'working', stage: 'Preparing...', pct: -1 }
    try {
      await InstallUpdate(upd.latest)
      upd = { ...upd, state: 'done' }
    } catch {
      upd = { ...upd, state: 'error' }
    }
  }

  $: updStatus =
    upd.state === 'uptodate'  ? 'Up to date' :
    upd.state === 'available' ? (upd.supported ? `Update to ${upd.latest}` : `New version ${upd.latest}`) :
    upd.state === 'working'   ? (upd.pct >= 0 ? `${upd.stage} ${upd.pct}%` : upd.stage) :
    upd.state === 'done'      ? 'Restart to finish' :
    'Retry update check'

  $: updInteractive =
    (upd.state === 'available' && upd.supported) ||
    ['done', 'error'].includes(upd.state)
  $: updVisible = upd.state !== 'checking'

  // Heap slider: index 0 is Auto (no flag passed, the JVM decides),
  // index 1 is an empty spacer position (no tick, skipped when moving)
  // and the rest map evenly to 4..16 GB.
  const GB_MIN = 4
  const GB_MAX = 16
  const GAP_IDX = 1
  const STEPS = GB_MAX - GB_MIN + 3
  const MEM_TICKS = Array.from({ length: STEPS }, (_, i) => {
    if (i === GAP_IDX) return null
    const gb = i === 0 ? 0 : GB_MIN + i - 2
    return { idx: i, label: i === 0 ? 'Auto' : gb % 4 === 0 ? `${gb}` : '' }
  }).filter(Boolean)

  const mbToIdx = mb =>
    mb <= 0 ? 0 : Math.max(2, Math.min(STEPS - 1, Math.round(mb / 1024) - GB_MIN + 2))
  const idxToMb = i => (i <= 0 ? 0 : (GB_MIN + i - 2) * 1024)
  const memPct = mb => (mbToIdx(mb) / (STEPS - 1)) * 100
  const memLabel = mb => (mb > 0 ? `${mb / 1024} GB` : 'Auto')

  // Focus order: close toggle, fullscreen, memory slider, update
  // (when the build supports it), done.
  let idx = 0
  let closeEl, fsEl, memEl, updEl, doneEl, trackEl
  $: els = updVisible
    ? [closeEl, fsEl, memEl, updEl, doneEl]
    : [closeEl, fsEl, memEl, doneEl]

  // A fixed heap (-Xms == -Xmx) is the standard Minecraft
  // recommendation: a resizing pool causes GC stutter, so one slider
  // drives both flags.
  $: memoryMb = settings.memoryMaxMb ?? 0

  function focusIdx(i) {
    idx = Math.max(0, Math.min(els.length - 1, i))
    tick().then(() => els[idx]?.focus())
  }

  function change(patch) {
    dispatch('change', { ...settings, ...patch })
  }

  function changeMem(mb) {
    change({ memoryMinMb: mb, memoryMaxMb: mb })
  }

  function bumpMem(dir) {
    let next = mbToIdx(memoryMb) + dir
    if (next === GAP_IDX) next += dir // hop over the spacer position
    next = Math.max(0, Math.min(STEPS - 1, next))
    changeMem(idxToMb(next))
  }

  function setMemFromPointer(e) {
    const rect = trackEl.getBoundingClientRect()
    const frac = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
    const pos = frac * (STEPS - 1)
    let next = Math.round(pos)
    if (next === GAP_IDX) next = pos < GAP_IDX ? 0 : GAP_IDX + 1
    changeMem(idxToMb(next))
  }

  function onTrackPointerDown(e) {
    focusIdx(2)
    setMemFromPointer(e)
    const move = ev => setMemFromPointer(ev)
    const up = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
  }

  function activate() {
    const el = els[idx]
    if (el === closeEl) change({ closeAfterLaunch: !settings.closeAfterLaunch })
    else if (el === fsEl) change({ fullscreen: !settings.fullscreen })
    else if (el === updEl) updateAction()
    else if (el === doneEl) dispatch('close')
  }

  function handleKey(e) {
    if (!consumeKey(e)) return
    const isSettingsKey = e.code === 'KeyO' || e.key === 'o' || e.key === 'O' || e.key === 'щ' || e.key === 'Щ'
    if (e.key === 'Escape' || isSettingsKey) {
      e.preventDefault(); e.stopPropagation()
      dispatch('close')
      return
    }
    if (e.key === 'ArrowUp')   { e.preventDefault(); e.stopPropagation(); focusIdx(idx - 1); return }
    if (e.key === 'ArrowDown') { e.preventDefault(); e.stopPropagation(); focusIdx(idx + 1); return }
    if (e.key === 'Enter')     { e.preventDefault(); e.stopPropagation(); activate(); return }
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault(); e.stopPropagation()
      if (els[idx] === memEl) bumpMem(e.key === 'ArrowRight' ? 1 : -1)
    }
  }

  onMount(() => {
    window.addEventListener('keydown', handleKey, true)
    focusIdx(0)
    // Values persisted under an older range snap back into bounds.
    const mb = settings.memoryMaxMb ?? 0
    const normalized = idxToMb(mbToIdx(mb))
    if (normalized !== mb) changeMem(normalized)
    EventsOn('update:progress', d => {
      upd = {
        ...upd,
        state: 'working',
        stage: d.stage,
        pct: d.total > 0 ? Math.round(d.current * 100 / d.total) : -1,
      }
    })
  })
  onDestroy(() => {
    window.removeEventListener('keydown', handleKey, true)
    EventsOff('update:progress')
    unsubUpd()
  })
</script>

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="overlay" transition:fade={{ duration: 150 }} on:click={() => dispatch('close')} />

<aside class="panel" transition:fly={{ x: 280, duration: 220 }}>
  <div class="title">Settings</div>

  <button
    bind:this={closeEl}
    class="row"
    class:focused={els[idx] === closeEl}
    on:click={() => change({ closeAfterLaunch: !settings.closeAfterLaunch })}
    on:focus={() => { idx = els.indexOf(closeEl) }}
    tabindex="-1"
  >
    <span class="checkbox" class:checked={settings.closeAfterLaunch} />
    <span class="row-text">Close launcher after game start</span>
  </button>

  <button
    bind:this={fsEl}
    class="row"
    class:focused={els[idx] === fsEl}
    on:click={() => change({ fullscreen: !settings.fullscreen })}
    on:focus={() => { idx = els.indexOf(fsEl) }}
    tabindex="-1"
  >
    <span class="checkbox" class:checked={settings.fullscreen} />
    <span class="row-text">Fullscreen game</span>
  </button>

  <!-- svelte-ignore a11y-no-noninteractive-tabindex a11y-no-static-element-interactions -->
  <div
    bind:this={memEl}
    class="row slider-row"
    class:focused={els[idx] === memEl}
    role="slider"
    aria-valuenow={memoryMb}
    on:focus={() => { idx = els.indexOf(memEl) }}
    tabindex="-1"
  >
    <div class="slider-top">
      <span class="row-text">Recommended memory</span>
      <span class="mem-val">{memLabel(memoryMb)}</span>
    </div>
    <div class="slider" bind:this={trackEl} on:pointerdown={onTrackPointerDown}>
      <div class="slider-track">
        <div class="slider-fill" style="width:{memPct(memoryMb)}%" />
        <div class="slider-knob" style="left:{memPct(memoryMb)}%" />
      </div>
      <div class="ticks">
        {#each MEM_TICKS as t}
          <div class="tick" class:reached={t.idx <= mbToIdx(memoryMb)} style="left:{(t.idx / (STEPS - 1)) * 100}%">
            <div class="tick-mark" class:major={t.label !== ''} />
            {#if t.label}<div class="tick-label">{t.label}</div>{/if}
          </div>
        {/each}
      </div>
    </div>
  </div>

  <div class="title about-title">About</div>

  <div class="about-row">
    <span class="row-text">Version</span>
    <span class="about-val">{version || '...'}</span>
  </div>

  {#if updVisible}
    <button
      bind:this={updEl}
      class="update-btn upd-{upd.state}"
      class:focused={els[idx] === updEl}
      class:dim={!updInteractive}
      on:click={updateAction}
      on:focus={() => { idx = els.indexOf(updEl) }}
      tabindex="-1"
    >
      {#if upd.state === 'working' && upd.pct >= 0}
        <span class="upd-fill" style="width:{upd.pct}%" />
      {/if}
      <span class="upd-label">{updStatus}</span>
    </button>
  {/if}

  <div class="spacer" />

  <button
    bind:this={doneEl}
    class="done"
    class:focused={els[idx] === doneEl}
    on:click={() => dispatch('close')}
    on:focus={() => { idx = els.indexOf(doneEl) }}
    tabindex="-1"
  >
    Done
  </button>
</aside>

<style>
  /* Both stop above the footer (2.44rem): the nav bar stays visible
     and undimmed, SteamOS-style. */
  .overlay {
    position: fixed;
    inset: 0 0 2.44rem 0;
    background: rgba(0, 0, 0, 0.55);
    z-index: 200;
  }

  .panel {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 2.44rem;
    width: 17rem;
    z-index: 201;
    display: flex;
    flex-direction: column;
    gap: 0.44rem;
    padding: 1rem;
    background: var(--bg);
    border-left: 1px solid rgba(255, 255, 255, 0.08);
    box-sizing: border-box;
    user-select: none;
    -webkit-user-select: none;
  }

  .title {
    font-size: 0.56rem;
    font-weight: 700;
    color: var(--text-sub);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    margin-bottom: 0.33rem;
  }

  .about-title { margin-top: 0.44rem; }

  /* Info-only line: not part of the focus order. */
  .about-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 1.89rem;
    padding: 0.33rem 0.78rem;
    background: var(--card);
  }

  .about-val {
    font-size: 0.67rem;
    color: var(--text-sub);
  }

  /* Update button: shaped like Done, tinted by state; install progress
     washes across it like the main install button. */
  .update-btn {
    position: relative;
    overflow: hidden;
    height: 2.22rem;
    background: var(--card-btn);
    color: var(--text);
    font-size: 0.78rem;
    font-weight: 700;
    cursor: pointer;
    transition: background var(--t);
  }
  .update-btn:hover,
  .update-btn.focused {
    background: var(--card-btn-hover);
    outline: none;
  }
  .update-btn.focused { box-shadow: inset 0 0 0 2px var(--accent); }
  .update-btn.dim { cursor: default; color: var(--text-sub); }

  .update-btn.upd-available:not(.dim) { color: var(--accent); }
  .update-btn.upd-working { color: var(--accent); }
  .update-btn.upd-done { color: var(--green); }
  .update-btn.upd-error { color: var(--red); }

  .upd-fill {
    position: absolute;
    inset: 0;
    background: rgba(30, 143, 255, 0.28);
    transition: width 150ms linear;
  }

  .upd-label { position: relative; }

  .row {
    display: flex;
    align-items: center;
    gap: 0.56rem;
    min-height: 1.89rem;
    padding: 0.33rem 0.78rem;
    background: var(--card);
    color: var(--text-sub);
    font-size: 0.72rem;
    text-align: left;
    cursor: pointer;
    transition: background var(--t), color var(--t);
  }
  .row:hover,
  .row.focused {
    background: var(--card-btn-hover);
    color: var(--text);
    outline: none;
  }
  .row.focused { box-shadow: inset 0 0 0 2px var(--accent); }

  .row-text { flex: 1; line-height: 1.4; }

  .checkbox {
    width: 0.83rem;
    height: 0.83rem;
    border: 2px solid rgba(255, 255, 255, 0.3);
    flex-shrink: 0;
    box-sizing: border-box;
  }
  .checkbox.checked {
    background: var(--accent) center / 70% no-repeat
      url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 10 10'%3E%3Cpath d='M1.5 5.5l2.5 2.5 4.5-5' stroke='white' stroke-width='1.8' fill='none'/%3E%3C/svg%3E");
    border-color: var(--accent);
  }

  /* SteamOS-style slider: label on top, thick track with a round white
     knob, blue fill to the left, tick marks with labels below. The row
     itself has no card box; focus highlights the knob instead. */
  .slider-row {
    flex-direction: column;
    align-items: stretch;
    gap: 0.44rem;
    padding-bottom: 1.11rem;
    cursor: default;
    background: transparent;
  }
  .slider-row:hover,
  .slider-row.focused {
    background: transparent;
    box-shadow: none;
    color: var(--text);
  }
  .slider-row.focused .slider-knob {
    box-shadow: 0 0 0 3px var(--accent), 0 1px 3px rgba(0, 0, 0, 0.4);
  }

  .slider-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .mem-val {
    font-size: 0.72rem;
    font-weight: 700;
    color: var(--text);
  }

  .slider {
    position: relative;
    margin: 0 0.39rem;
    cursor: pointer;
    touch-action: none;
  }

  /* Above the ticks layer so the knob covers marks passing under it. */
  .slider-track {
    position: relative;
    z-index: 1;
    height: 7px;
    border-radius: 4px;
    background: rgba(255, 255, 255, 0.15);
  }
  .slider-fill {
    height: 100%;
    border-radius: 4px;
    background: var(--accent);
    transition: width 100ms ease;
  }
  .slider-knob {
    position: absolute;
    top: 50%;
    transform: translate(-50%, -50%);
    width: 0.94rem;
    height: 0.94rem;
    border-radius: 50%;
    background: #fff;
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.4);
    transition: left 100ms ease, box-shadow var(--t);
  }

  .ticks {
    position: relative;
    height: 0.89rem;
    margin-top: 0.28rem;
  }
  .tick {
    position: absolute;
    top: 0;
    transform: translateX(-50%);
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.11rem;
  }
  /* Ticks and labels light up blue only once the value reaches them. */
  .tick-mark {
    width: 2px;
    height: 0.22rem;
    background: rgba(255, 255, 255, 0.2);
    transition: background var(--t);
  }
  .tick-mark.major { height: 0.33rem; }
  .tick.reached .tick-mark { background: var(--accent); }

  .tick-label {
    font-size: 0.5rem;
    font-weight: 700;
    color: var(--text-sub);
    transition: color var(--t);
  }
  .tick.reached .tick-label { color: var(--accent); }

  .spacer { flex: 1; }

  .done {
    height: 2.22rem;
    background: var(--card-btn);
    color: var(--text);
    font-size: 0.78rem;
    font-weight: 700;
    cursor: pointer;
    transition: background var(--t);
  }
  .done:hover,
  .done.focused {
    background: var(--card-btn-hover);
    outline: none;
  }
  .done.focused { box-shadow: inset 0 0 0 2px var(--accent); }
</style>
