<script>
  import { createEventDispatcher, onDestroy, tick } from 'svelte'

  export let value = ''
  export let options = []
  export let disabled = false

  const dispatch = createEventDispatcher()
  const ITEM_HEIGHT_REM = 1.67
  const VISIBLE_ITEMS   = 10

  let open = false
  let triggerEl
  let openUpward = false

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
    document.removeEventListener('click', handleOutside)
  }

  async function openDropdown() {
    checkPosition()
    open = true
    await tick()
    document.addEventListener('click', handleOutside)
  }

  function select(v) {
    value = v
    closeDropdown()
    dispatch('change', v)
  }

  async function toggle() {
    if (disabled) return
    if (open) closeDropdown()
    else await openDropdown()
  }

  function handleKeydown(e) {
    if (!open) {
      if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown') {
        e.preventDefault()
        openDropdown()
      }
      return
    }
    const idx = options.findIndex(o => o.value === value)
    if (e.key === 'Escape')    { closeDropdown() }
    if (e.key === 'ArrowDown') { e.preventDefault(); select(options[Math.min(idx + 1, options.length - 1)].value) }
    if (e.key === 'ArrowUp')   { e.preventDefault(); select(options[Math.max(idx - 1, 0)].value) }
    if (e.key === 'Enter')     { closeDropdown() }
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
      {#each options as opt}
        <button
          class="item"
          class:active={opt.value === value}
          role="option"
          aria-selected={opt.value === value}
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
    max-height: calc(10 * 1.67rem);
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
  .item:hover { background: rgba(255,255,255,0.14); color: var(--text); font-weight: 700; }
  .item.active { color: var(--text); background: rgba(30,143,255,0.15); font-weight: 700; }
</style>
