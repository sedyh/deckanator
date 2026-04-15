<script>
  import { createEventDispatcher, onDestroy, tick } from 'svelte'

  export let value = ''
  export let options = []
  export let disabled = false

  const dispatch = createEventDispatcher()
  const ITEM_HEIGHT_REM = 1.67
  const VISIBLE_ITEMS   = 4

  let open = false
  let triggerEl
  let openUpward = false
  let highlightedIdx = -1
  let itemEls = []

  export function focus() {
    triggerEl?.querySelector('.trigger')?.focus()
  }

  function checkPosition() {
    if (!triggerEl) return
    const rect = triggerEl.getBoundingClientRect()
    const rootFontSize = parseFloat(getComputedStyle(document.documentElement).fontSize)
    const listHeight = ITEM_HEIGHT_REM * VISIBLE_ITEMS * rootFontSize
    openUpward = rect.bottom + listHeight > window.innerHeight
  }

  $: label = options.find(o => o.value === value)?.label ?? value

  function closeDropdown() {
    open = false
    highlightedIdx = -1
    document.removeEventListener('click', handleOutside)
  }

  async function openDropdown() {
    checkPosition()
    highlightedIdx = options.findIndex(o => o.value === value)
    open = true
    await tick()
    document.addEventListener('click', handleOutside)
  }

  function select(v) {
    value = v
    closeDropdown()
    dispatch('change', v)
  }

  async function scrollToHighlighted() {
    await tick()
    itemEls[highlightedIdx]?.scrollIntoView({ block: 'nearest' })
  }

  function confirmHighlighted() {
    if (highlightedIdx >= 0 && highlightedIdx < options.length) {
      select(options[highlightedIdx].value)
    } else {
      closeDropdown()
    }
  }

  async function toggle() {
    if (disabled) return
    if (open) closeDropdown()
    else await openDropdown()
  }

  function handleKeydown(e) {
    if (!open) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        e.stopPropagation()
        openDropdown()
      }
      // ArrowDown/Up bubble to global panel navigation handler
      return
    }
    if (e.key === 'Escape')    { e.stopPropagation(); closeDropdown() }
    if (e.key === 'ArrowDown') {
      e.preventDefault(); e.stopPropagation()
      highlightedIdx = Math.min(highlightedIdx + 1, options.length - 1)
      scrollToHighlighted()
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault(); e.stopPropagation()
      highlightedIdx = Math.max(highlightedIdx - 1, 0)
      scrollToHighlighted()
    }
    if (e.key === 'Enter')     { e.preventDefault(); e.stopPropagation(); confirmHighlighted() }
  }

  function handleOutside(e) {
    if (triggerEl && !triggerEl.contains(e.target)) closeDropdown()
  }

  onDestroy(() => document.removeEventListener('click', handleOutside))
</script>

<div class="wrap" class:open class:disabled bind:this={triggerEl}>
  <button
    class="trigger"
    {disabled}
    on:click={toggle}
    on:keydown={handleKeydown}
  >
    <span class="val">{label || '—'}</span>
    <span class="arrow" class:flip={open}>
      <svg width="10" height="10" viewBox="0 0 10 10" fill="none">
        <path d="M1 3l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="square"/>
      </svg>
    </span>
  </button>

  {#if open}
    <div class="list" class:up={openUpward} role="listbox">
      {#each options as opt, i}
        <button
          bind:this={itemEls[i]}
          class="item"
          class:active={opt.value === value}
          class:highlighted={i === highlightedIdx}
          role="option"
          aria-selected={opt.value === value}
          on:mouseenter={() => highlightedIdx = i}
          on:click|stopPropagation={() => select(opt.value)}
        >
          {opt.label}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .wrap {
    position: relative;
    min-width: 7rem;
  }

  .trigger {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.56rem;
    padding: 0.39rem 0.78rem;
    background: var(--card-btn);
    font-size: 0.78rem;
    font-weight: 400;
    color: var(--text);
    cursor: pointer;
    transition: background var(--t), font-weight var(--t);
  }
  .trigger:hover { background: var(--card-btn-hover); font-weight: 700; }
  .trigger:focus-visible { outline: none; box-shadow: inset 0 0 0 2px var(--accent); }
  .trigger:disabled { opacity: 0.35; cursor: default; }

  .val {
    flex: 1;
    text-align: left;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .arrow {
    display: flex;
    align-items: center;
    flex-shrink: 0;
    color: var(--text-sub);
    transition: transform var(--t);
  }
  .arrow.flip { transform: rotate(180deg); }

  .list {
    position: absolute;
    top: 100%;
    right: 0;
    z-index: 100;
    min-width: 100%;
    max-height: calc(4 * 1.67rem);
    overflow-y: auto;
    background: #1a1d2b;
    box-shadow: 0 0.44rem 1.78rem rgba(0,0,0,0.7);
    scrollbar-width: thin;
    scrollbar-color: rgba(255,255,255,0.15) transparent;
  }

  .list.up {
    top: auto;
    bottom: 100%;
    box-shadow: 0 -0.44rem 1.78rem rgba(0,0,0,0.7);
  }

  .item {
    display: block;
    width: 100%;
    padding: 0.44rem 0.78rem;
    font-size: 0.78rem;
    font-weight: 400;
    color: var(--text-sub);
    text-align: left;
    background: transparent;
    transition: background var(--t), color var(--t), font-weight var(--t);
    white-space: nowrap;
  }
  .item:hover,
  .item.highlighted { background: rgba(255,255,255,0.14); color: var(--text); font-weight: 700; }
  .item.active { color: var(--text); background: rgba(30,143,255,0.15); font-weight: 700; }
  .item.active.highlighted { background: rgba(30,143,255,0.3); }
</style>
