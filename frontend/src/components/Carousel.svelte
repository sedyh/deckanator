<script>
  import { createEventDispatcher, onMount, onDestroy } from 'svelte'
  import { IconSettings, IconTrash, IconFolder } from '../lib/icons.js'
  import { OpenProfileDir } from '../../wailsjs/go/internal/App.js'

  export let profiles = []
  export let icons = []
  export let selectedIndex = 0
  export let checking = false
  export let installPct = -1
  export let installProfileId = ''
  export let installedMap = {}

  const dispatch = createEventDispatcher()

  export let mode = 'nav'
  let actionIdx = 0   // 0=files, 1=rename, 2=delete

  let carouselEl

  export function navigateLeft(keepAction = false) {
    if (selectedIndex > 0) { selectedIndex--; mode = keepAction ? 'action' : 'nav' }
  }
  export function navigateRight(keepAction = false) {
    if (selectedIndex < profiles.length - 1) { selectedIndex++; mode = keepAction ? 'action' : 'nav' }
  }
  export function focusCarousel() { carouselEl?.focus() }
  export function enterAction(idx = 2) {
    if (!profile) return
    carouselEl?.focus()
    mode = 'action'
    actionIdx = idx
  }
  let editValue = ''

  const iconMap = {}
  $: icons.forEach(ic => { iconMap[ic.id] = ic })

  function getIcon(iconId) {
    return iconMap[iconId] || { emoji: '🎮', bg: '#2a2d3d' }
  }

  $: profile = profiles[selectedIndex] ?? null

  function enterActionMode() {
    if (!profile) return
    mode = 'action'
    actionIdx = 0
  }

  function startEdit() {
    if (!profile) return
    editValue = profile.name
    mode = 'edit'
  }

  function commitEdit() {
    if (profile && editValue.trim()) dispatch('save', { ...profile, name: editValue.trim() })
    mode = 'action'
    actionIdx = 1
    carouselEl?.focus()
  }

  function cancelEdit() {
    mode = 'action'
    actionIdx = 1
    carouselEl?.focus()
  }

  function deleteProfile() {
    if (profile) dispatch('delete', profile.id)
    mode = 'nav'
  }

  let touchStartX = 0

  function handleTouchStart(e) {
    touchStartX = e.touches[0].clientX
  }

  function handleTouchEnd(e) {
    const dx = e.changedTouches[0].clientX - touchStartX
    if (Math.abs(dx) < 40) return
    if (dx < 0 && selectedIndex < profiles.length - 1) { selectedIndex++; mode = 'nav' }
    if (dx > 0 && selectedIndex > 0)                   { selectedIndex--; mode = 'nav' }
  }

  let wheelLocked = false

  function handleWheel(e) {
    if (Math.abs(e.deltaX) < Math.abs(e.deltaY)) return
    if (Math.abs(e.deltaX) < 30) return
    if (document.querySelector('.wrap.open')) return
    if (wheelLocked) return
    e.preventDefault()
    wheelLocked = true
    setTimeout(() => { wheelLocked = false }, 400)
    if (e.deltaX > 0 && selectedIndex < profiles.length - 1) { selectedIndex++; mode = 'nav' }
    if (e.deltaX < 0 && selectedIndex > 0)                   { selectedIndex--; mode = 'nav' }
  }

  function handleMousedown(e) {
    if (mode === 'action' && carouselEl && !carouselEl.contains(e.target)) {
      mode = 'nav'
    }
  }

  onMount(() => {
    window.addEventListener('wheel', handleWheel, { passive: false })
    window.addEventListener('mousedown', handleMousedown)
  })
  onDestroy(() => {
    window.removeEventListener('wheel', handleWheel)
    window.removeEventListener('mousedown', handleMousedown)
  })

  function handleKeydown(e) {
    if (mode === 'edit') return  // input handles Enter/Escape with stopPropagation

    if (mode === 'action') {
      if (e.key === 'ArrowUp')   { e.preventDefault(); e.stopPropagation(); actionIdx = Math.max(0, actionIdx - 1) }
      if (e.key === 'ArrowDown') {
        e.preventDefault(); e.stopPropagation()
        if (actionIdx < 2) { actionIdx++ }
        else { mode = 'nav'; dispatch('enterPanel') }
      }
      if (e.key === 'Enter') {
        e.preventDefault(); e.stopPropagation()
        if (actionIdx === 0) profile && OpenProfileDir(profile.id)
        else if (actionIdx === 1) startEdit()
        else deleteProfile()
      }
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); mode = 'nav' }
      return
    }

    // nav mode: only Enter is handled here; ArrowLeft/Right/Up/Down bubble to App's global handler
    if (e.key === 'Enter' && profile) { e.preventDefault(); e.stopPropagation(); enterActionMode() }
  }

  $: hints = []
</script>

<!-- svelte-ignore a11y-no-noninteractive-tabindex -->
<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<div
  class="carousel"
  tabindex="0"
  role="listbox"
  bind:this={carouselEl}
  on:keydown={handleKeydown}
>
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="track"
    on:touchstart={handleTouchStart}
    on:touchend={handleTouchEnd}
  >
    {#each profiles as item, i}
      {@const offset = i - selectedIndex}
      {@const isActive = offset === 0}
      {@const isAdjacent = Math.abs(offset) === 1}

      {#if Math.abs(offset) <= 2}
        <!-- svelte-ignore a11y-click-events-have-key-events -->
        <div
          class="card-wrap"
          class:adjacent={isAdjacent}
          style="
            transform: translateX({offset * 13.89}rem) scale({isActive ? 1 : 0.82}) translateZ(0);
            opacity: {isActive ? 1 : Math.abs(offset) === 1 ? 0.4 : 0};
            z-index: {10 - Math.abs(offset)};
            pointer-events: {isActive || isAdjacent ? 'auto' : 'none'};
          "
          on:click={() => { if (!isActive) { selectedIndex = i; mode = 'nav' } }}
        >
          <div class="card-label">
            {#if mode === 'edit' && isActive}
              <!-- svelte-ignore a11y-autofocus -->
              <input
                class="name-input"
                autofocus
                maxlength="15"
                bind:value={editValue}
                on:blur={commitEdit}
                on:keydown={(e) => {
                  if (e.key === 'Enter')  { e.stopPropagation(); commitEdit() }
                  if (e.key === 'Escape') { e.stopPropagation(); cancelEdit() }
                }}
              />
            {:else}
              <span class="label-name">{item.name.slice(0, 15)}</span>
              <span class="label-id">#{item.id}</span>
            {/if}
          </div>

          <div class="card profile-card" class:active={isActive}>
            <div class="art" style="background:{getIcon(item.icon).bg}">
              <span
                class="emoji"
                class:dim={isActive && checking || (item.id === installProfileId && installPct >= 0)}
                class:not-installed={installedMap[item.id] !== true && !(item.id === installProfileId && installPct >= 0)}
              >{getIcon(item.icon).emoji}</span>
              {#if item.id === installProfileId && installPct >= 0}
                <div class="art-fill" style="--reveal:{installPct}%" />
              {:else if isActive && checking}
                <span class="art-spinner" />
              {/if}
            </div>

            <div class="btns" class:btns-inactive={!isActive}>
              {#if mode === 'action' && isActive}
                <button
                  class="action-btn"
                  class:focused={actionIdx === 0}
                  tabindex="-1"
                  on:click|stopPropagation={() => OpenProfileDir(item.id)}
                  on:mouseenter={() => actionIdx = 0}
                >
                  <span class="btn-icon">{@html IconFolder}</span>
                  <span>Files</span>
                </button>
                <div class="btn-sep"></div>
                <button
                  class="action-btn"
                  class:focused={actionIdx === 1}
                  tabindex="-1"
                  on:click|stopPropagation={startEdit}
                  on:mouseenter={() => actionIdx = 1}
                >
                  <span class="btn-icon">{@html IconSettings}</span>
                  <span>Rename</span>
                </button>
                <div class="btn-sep"></div>
                <button
                  class="action-btn danger"
                  class:focused={actionIdx === 2}
                  tabindex="-1"
                  on:click|stopPropagation={deleteProfile}
                  on:mouseenter={() => actionIdx = 2}
                >
                  <span class="btn-icon">{@html IconTrash}</span>
                  <span>Delete</span>
                </button>
              {:else}
                <button
                  class="action-btn"
                  tabindex="-1"
                  on:click|stopPropagation={() => OpenProfileDir(item.id)}
                >
                  <span class="btn-icon">{@html IconFolder}</span>
                  <span>Files</span>
                </button>
                <div class="btn-sep"></div>
                <button
                  class="action-btn"
                  tabindex="-1"
                  on:click|stopPropagation={startEdit}
                >
                  <span class="btn-icon">{@html IconSettings}</span>
                  <span>Rename</span>
                </button>
                <div class="btn-sep"></div>
                <button
                  class="action-btn danger"
                  tabindex="-1"
                  on:click|stopPropagation={deleteProfile}
                >
                  <span class="btn-icon">{@html IconTrash}</span>
                  <span>Delete</span>
                </button>
              {/if}
            </div>
          </div>
        </div>
      {/if}
    {/each}

    {#if profiles.length === 0}
      <div class="empty-state">No profiles yet</div>
    {/if}
  </div>

  <div class="dots">
    {#each profiles as _, i}
      <button
        class="dot"
        class:active={i === selectedIndex}
        on:click={() => { selectedIndex = i; mode = 'nav' }}
        tabindex="-1"
      />
    {/each}
  </div>

</div>

<style>
  .carousel {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.56rem;
    outline: none;
  }

  .track {
    position: relative;
    width: 13.33rem;
    height: 8.22rem;
    display: flex;
    align-items: flex-end;
    justify-content: center;
    overflow: visible;
  }

  .card-wrap {
    position: absolute;
    bottom: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.33rem;
    transition: transform 200ms cubic-bezier(.25,.46,.45,.94),
                opacity   200ms ease;
    will-change: transform, opacity;
  }
  .card-wrap.adjacent { cursor: pointer; }

  .card-label {
    font-size: 0.78rem;
    font-weight: 700;
    color: var(--text);
    height: 1.11rem;
    white-space: nowrap;
    width: 13.33rem;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }

  .label-name {
    overflow: hidden;
    text-overflow: ellipsis;
    flex-shrink: 0;
  }

  .label-id {
    font-size: 0.61rem;
    font-weight: 400;
    color: var(--text-sub);
    font-family: monospace;
    flex-shrink: 0;
  }

  .name-input {
    font-size: 0.78rem;
    font-weight: 700;
    background: transparent;
    color: var(--text);
    border: none;
    outline: none;
    border-bottom: 2px solid var(--accent);
    width: 100%;
    padding: 0 0 0.11rem;
    caret-color: var(--accent);
  }

  .card {
    width: 13.33rem;
    height: 6.67rem;
    display: flex;
    flex-direction: row;
  }

  .art {
    width: 6.67rem;
    height: 6.67rem;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    position: relative;
  }

  .emoji {
    font-size: 3.33rem;
    line-height: 1;
    filter: drop-shadow(0 0.11rem 0.44rem rgba(0,0,0,0.5));
    transition: opacity 150ms ease;
  }
  .emoji.dim { opacity: 0.25; }
  .emoji.not-installed { opacity: 0.4; filter: drop-shadow(0 0.11rem 0.44rem rgba(0,0,0,0.5)) grayscale(0.6); }

  .art-fill {
    position: absolute;
    inset: 0;
    background: rgba(0,0,0,0.72);
    clip-path: inset(0 0 var(--reveal) 0);
    transition: clip-path 350ms ease;
  }

  .art-spinner {
    position: absolute;
    width: 1.67rem;
    height: 1.67rem;
    border: 0.17rem solid rgba(255,255,255,0.15);
    border-top-color: rgba(255,255,255,0.8);
    border-radius: 50%;
    animation: art-spin 0.7s linear infinite;
  }

  @keyframes art-spin { to { transform: rotate(360deg); } }

  .btns {
    flex: 1;
    display: flex;
    flex-direction: column;
    background: var(--card-btn);
  }
  .btns-inactive { pointer-events: none; }

  .action-btn {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 0.56rem;
    padding: 0 0.78rem;
    font-size: 0.72rem;
    font-weight: 400;
    color: var(--text-sub);
    background: transparent;
    transition: background var(--t), color var(--t), font-weight var(--t);
    white-space: nowrap;
  }
  .action-btn:hover { background: rgba(255,255,255,0.16); color: var(--text); font-weight: 700; }
  .action-btn.focused { background: rgba(255,255,255,0.16); color: var(--text); font-weight: 700; box-shadow: inset 0 0 0 2px var(--accent); }
  .action-btn.danger:hover { background: rgba(217,95,95,0.12); color: var(--red); font-weight: 700; }
  .action-btn.danger.focused { background: rgba(217,95,95,0.12); color: var(--red); font-weight: 700; box-shadow: inset 0 0 0 2px var(--accent); }

  .btn-icon {
    display: flex;
    align-items: center;
    flex-shrink: 0;
    color: inherit;
  }
  .btn-icon :global(svg) { width: 0.78rem; height: 0.78rem; }

  .btn-sep {
    height: 1px;
    background: var(--card-sep);
  }


  /* ── Dots ── */
  .dots { display: flex; gap: 0.33rem; align-items: center; min-height: 0.33rem; margin-top: 0.33rem; }
  .dot {
    width: 0.33rem;
    height: 0.33rem;
    border-radius: 50%;
    background: rgba(255,255,255,0.2);
    transition: background var(--t), transform var(--t);
    cursor: pointer;
  }
  .dot.active {
    background: var(--accent);
    transform: scale(1.5);
  }


  .empty-state {
    color: var(--text-sub);
    font-size: 0.72rem;
    position: absolute;
    top: 50%;
    transform: translateY(-50%);
  }
</style>
