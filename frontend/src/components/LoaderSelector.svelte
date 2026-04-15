<script>
  import { createEventDispatcher } from 'svelte'

  export let loader = 'vanilla'
  export let locked = false
  const dispatch = createEventDispatcher()

  const loaders = [
    { id: 'vanilla', label: 'Vanilla' },
    { id: 'fabric',  label: 'Fabric' },
  ]

  function select(id) {
    loader = id
    dispatch('change', id)
  }

  function handleKeydown(e) {
    const idx = loaders.findIndex(l => l.id === loader)
    if (e.key === 'ArrowLeft'  && idx > 0)                  { e.preventDefault(); select(loaders[idx - 1].id) }
    if (e.key === 'ArrowRight' && idx < loaders.length - 1) { e.preventDefault(); select(loaders[idx + 1].id) }
  }
</script>

<div class="row">
  <span class="row-label">Loader</span>
  {#if locked}
    <span class="val-locked">{loaders.find(l => l.id === loader)?.label ?? loader}</span>
  {:else}
    <!-- svelte-ignore a11y-no-noninteractive-tabindex -->
    <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
    <div
      class="seg"
      tabindex="0"
      role="radiogroup"
      on:keydown={handleKeydown}
    >
      {#each loaders as l}
        <button
          class="seg-btn"
          class:active={loader === l.id}
          role="radio"
          aria-checked={loader === l.id}
          tabindex="-1"
          on:click={() => select(l.id)}
        >
          {l.label}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.33rem 0;
  }

  .row-label {
    font-size: 0.78rem;
    font-weight: 700;
    color: var(--text);
  }

  .val-locked {
    font-size: 0.78rem;
    font-weight: 400;
    color: var(--text-sub);
    padding: 0.39rem 0 0.39rem 0.78rem;
  }

  .seg {
    display: flex;
  }

  .seg:focus-visible {
    outline: none;
  }

  .seg-btn {
    padding: 0.39rem 1.22rem;
    font-size: 0.78rem;
    font-weight: 600;
    color: var(--text-sub);
    background: var(--card-btn);
    transition: background var(--t), color var(--t);
  }
  .seg-btn:hover { color: var(--text); background: rgba(255,255,255,0.06); }
  .seg-btn.active {
    background: var(--accent);
    color: #fff;
  }
</style>
