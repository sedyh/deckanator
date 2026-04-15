<script>
  import { createEventDispatcher } from 'svelte'
  import { IconDownload, IconPlay, IconPause } from '../lib/icons.js'

  export let installed   = false
  export let installing  = false
  export let launching   = false
  export let progress    = { stage: '', current: 0, total: 100 }
  export let disabled    = false

  const dispatch = createEventDispatcher()

  let btnEl
  export function focus() { btnEl?.focus() }

  $: pct   = progress.total > 0 ? Math.round(progress.current * 100 / progress.total) : 0
  $: label = installing
    ? (progress.stage || 'Installing...')
    : launching ? 'Launching...'
    : installed ? 'Play' : 'Download'
</script>

<div class="wrap">
  <button
    bind:this={btnEl}
    class="btn"
    class:play={installed && !installing && !launching}
    class:launching
    class:installing
    disabled={disabled || installing || launching}
    on:click={() => {
      if (installing || launching) return
      if (installed) dispatch('launch')
      else           dispatch('install')
    }}
  >
    {#if installing}
      <div class="bar" style="width:{pct}%" />
    {/if}

    <span class="inner">
      {#if launching}
        <span class="icon">{@html IconPause}</span>
      {:else if installed && !installing}
        <span class="icon">{@html IconPlay}</span>
      {:else if !installing}
        <span class="icon">{@html IconDownload}</span>
      {/if}
      <span class="label">{label}</span>
    </span>
  </button>
</div>

<style>
  .wrap {
    width: 100%;
    display: flex;
    justify-content: center;
  }

  .btn {
    position: relative;
    width: 100%;
    max-width: 18.89rem;
    height: 2.67rem;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--card-btn);
    border: none;
    font-size: 0.83rem;
    font-weight: 700;
    color: var(--text);
    overflow: hidden;
    letter-spacing: 0.03em;
    transition: background var(--t), box-shadow var(--t);
  }

  .btn:not(:disabled):hover {
    background: var(--card-btn-hover);
    -webkit-text-stroke: 0.04em currentColor;
  }

  .btn:not(:disabled):focus {
    box-shadow: inset 0 0 0 2px var(--accent);
    outline: none;
  }

  .btn.play {
    background: var(--accent);
    color: #fff;
    box-shadow: 0 2px 20px rgba(30,143,255,0.3);
  }
  .btn.play:not(:disabled):hover,
  .btn.play:not(:disabled):focus {
    background: var(--accent-dim);
    box-shadow: inset 0 0 0 2px #fff, 0 2px 20px rgba(30,143,255,0.3);
    -webkit-text-stroke: 0.04em currentColor;
  }

  .btn.launching {
    background: #2d7a45;
    color: #fff;
    cursor: default;
  }

  .btn.installing { cursor: default; transition: none; }
  .btn:disabled:not(.installing):not(.launching) { opacity: 0.35; cursor: default; }

  .bar {
    position: absolute;
    inset: 0 auto 0 0;
    background: rgba(30,143,255,0.18);
    transition: width 350ms ease;
    pointer-events: none;
  }

  .inner {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.56rem;
  }

  .icon {
    display: flex;
    align-items: center;
    flex-shrink: 0;
  }
  .icon :global(svg) { width: 1rem; height: 1rem; }

  .label { flex: 0 0 auto; }

  .pct {
    font-size: 0.67rem;
    font-weight: 600;
    opacity: 0.65;
    line-height: 1;
    align-self: center;
  }
</style>
