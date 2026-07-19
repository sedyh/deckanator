<script>
  import { createEventDispatcher } from 'svelte'
  import SteamSelect from './SteamSelect.svelte'

  export let loader = 'vanilla'
  export let mcVersions = []
  export let selectedMC = ''
  export let fabricVersions = []
  export let selectedFabric = ''
  export let selectedJava = ''
  export let locked = false
  export let disabled = false

  const dispatch = createEventDispatcher()

  let loaderSel, mcSel, fabricSel, javaSel

  const loaderOptions = [
    { value: 'vanilla', label: 'Vanilla' },
    { value: 'fabric',  label: 'Fabric' },
    { value: 'quilt',   label: 'Quilt' },
  ]

  $: loaderLabel = loaderOptions.find(o => o.value === loader)?.label ?? loader
  $: loaderVersionLabel = loader === 'quilt' ? 'Quilt' : 'Fabric'

  export function focusLoader() { loaderSel?.focus() }
  export function focusMC()     { mcSel?.focus() }
  export function focusFabric() { fabricSel?.focus() }
  export function focusJava()   { javaSel?.focus() }

  export function fieldOfNode(n) {
    if (loaderSel?.containsNode(n)) return 'loader'
    if (mcSel?.containsNode(n))     return 'mc'
    if (fabricSel?.containsNode(n)) return 'fabric'
    if (javaSel?.containsNode(n))   return 'java'
    return null
  }

  $: mcOptions     = mcVersions.map(v => ({ value: v.id, label: v.id }))
  $: fabricOptions = fabricVersions.map(v => ({ value: v.version, label: v.version }))
  $: javaOptions   = javaOptionsForMC(selectedMC)

  $: {
    if (javaOptions.length > 0 && !javaOptions.find(o => o.value === selectedJava)) {
      selectedJava = javaOptions[0].value
    }
  }

  $: javaLabel = javaOptions.find(o => o.value === selectedJava)?.label ?? selectedJava

  function javaOptionsForMC(id) {
    if (!id) return []
    const parts = id.split('.')
    const major = parseInt(parts[0] || '0')
    if (major >= 26) return [
      { value: 'java-runtime-epsilon', label: 'Java 25' }
    ]
    if (major !== 1) return [
      { value: 'java-runtime-delta', label: 'Java 21' }
    ]
    const minor = parseInt(parts[1] || '0')
    if (minor >= 21) return [
      { value: 'java-runtime-delta', label: 'Java 21' }
    ]
    if (minor >= 18) return [
      { value: 'java-runtime-gamma', label: 'Java 17' },
      { value: 'java-runtime-delta', label: 'Java 21' }
    ]
    if (minor === 17) return [
      { value: 'java-runtime-alpha', label: 'Java 16' },
      { value: 'java-runtime-gamma', label: 'Java 17' }
    ]
    return [{ value: 'jre-legacy', label: 'Java 8' }]
  }

  function onLoaderChange(e) { loader         = e.detail; dispatch('change', { field: 'loader' }) }
  function onMCChange(e)     { selectedMC     = e.detail; dispatch('change', { field: 'mc' }) }
  function onFabricChange(e) { selectedFabric = e.detail; dispatch('change', { field: 'fabric' }) }
  function onJavaChange(e)   { selectedJava   = e.detail; dispatch('change', { field: 'java' }) }
</script>

<div class="rows">
  <div class="row">
    <span class="row-label">Loader</span>
    {#if locked}
      <span class="val-locked">{loaderLabel}</span>
    {:else}
      <SteamSelect
        bind:this={loaderSel}
        value={loader}
        options={loaderOptions}
        {disabled}
        on:change={onLoaderChange}
      />
    {/if}
  </div>

  <div class="row">
    <span class="row-label">Minecraft</span>
    {#if locked}
      <span class="val-locked">{selectedMC || '—'}</span>
    {:else}
      <SteamSelect
        bind:this={mcSel}
        value={selectedMC}
        options={mcOptions}
        disabled={disabled || mcVersions.length === 0}
        on:change={onMCChange}
      />
    {/if}
  </div>

  {#if loader !== 'vanilla'}
    <div class="row">
      <span class="row-label">{loaderVersionLabel}</span>
      {#if locked}
        <span class="val-locked">{selectedFabric || '—'}</span>
      {:else}
        <SteamSelect
          bind:this={fabricSel}
          value={selectedFabric}
          options={fabricOptions}
          disabled={disabled || fabricVersions.length === 0}
          on:change={onFabricChange}
        />
      {/if}
    </div>
  {/if}

  <div class="row">
    <span class="row-label">Java</span>
    {#if locked}
      <span class="val-locked">{javaLabel || '—'}</span>
    {:else}
      <SteamSelect
        bind:this={javaSel}
        value={selectedJava}
        options={javaOptions}
        disabled={disabled || javaOptions.length === 0}
        on:change={onJavaChange}
      />
    {/if}
  </div>
</div>

<style>
  .rows {
    display: flex;
    flex-direction: column;
    gap: 0;
    width: 100%;
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.33rem 0;
    gap: 0.5rem;
  }

  .row :global(.wrap) {
    width: 9rem;
    flex-shrink: 0;
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
</style>
