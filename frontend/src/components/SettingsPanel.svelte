<script>
  import { createEventDispatcher, onMount, onDestroy, tick } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import { consumeKey } from '../lib/input.js'

  export let settings = { closeAfterLaunch: true }

  const dispatch = createEventDispatcher()

  let idx = 0 // 0 = checkbox, 1 = done
  let toggleEl, doneEl

  function focusIdx(i) {
    idx = i
    tick().then(() => (i === 0 ? toggleEl : doneEl)?.focus())
  }

  function toggle() {
    dispatch('change', { ...settings, closeAfterLaunch: !settings.closeAfterLaunch })
  }

  function handleKey(e) {
    if (!consumeKey(e)) return
    const isSettingsKey = e.code === 'KeyO' || e.key === 'o' || e.key === 'O' || e.key === 'щ' || e.key === 'Щ'
    if (e.key === 'Escape' || isSettingsKey) {
      e.preventDefault(); e.stopPropagation()
      dispatch('close')
      return
    }
    if (e.key === 'ArrowUp')   { e.preventDefault(); e.stopPropagation(); focusIdx(0); return }
    if (e.key === 'ArrowDown') { e.preventDefault(); e.stopPropagation(); focusIdx(1); return }
    if (e.key === 'Enter') {
      e.preventDefault(); e.stopPropagation()
      if (idx === 0) toggle()
      else dispatch('close')
      return
    }
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault(); e.stopPropagation()
    }
  }

  onMount(() => {
    window.addEventListener('keydown', handleKey, true)
    focusIdx(0)
  })
  onDestroy(() => window.removeEventListener('keydown', handleKey, true))
</script>

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="overlay" transition:fade={{ duration: 150 }} on:click={() => dispatch('close')} />

<aside class="panel" transition:fly={{ x: 280, duration: 220 }}>
  <div class="title">Settings</div>

  <button
    bind:this={toggleEl}
    class="row"
    class:focused={idx === 0}
    on:click={toggle}
    on:focus={() => { idx = 0 }}
    tabindex="-1"
  >
    <span class="checkbox" class:checked={settings.closeAfterLaunch} />
    <span class="row-text">Close launcher after game start</span>
  </button>

  <div class="spacer" />

  <button
    bind:this={doneEl}
    class="done"
    class:focused={idx === 1}
    on:click={() => dispatch('close')}
    on:focus={() => { idx = 1 }}
    tabindex="-1"
  >
    Done
  </button>
</aside>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    z-index: 200;
  }

  .panel {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: 17rem;
    z-index: 201;
    display: flex;
    flex-direction: column;
    gap: 0.44rem;
    padding: 1rem;
    background: var(--bg);
    border-left: 1px solid rgba(255, 255, 255, 0.08);
    box-sizing: border-box;
  }

  .title {
    font-size: 0.56rem;
    font-weight: 700;
    color: var(--text-sub);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    margin-bottom: 0.33rem;
  }

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
