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

  const dispatch = createEventDispatcher()

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

  function onMCChange(e)     { selectedMC     = e.detail; dispatch('change', { field: 'mc' }) }
  function onFabricChange(e) { selectedFabric = e.detail; dispatch('change', { field: 'fabric' }) }
  function onJavaChange(e)   { selectedJava   = e.detail; dispatch('change', { field: 'java' }) }
</script>

<div class="rows">
  <div class="row">
    <span class="row-label">Minecraft</span>
    {#if locked}
      <span class="val-locked">{selectedMC || '—'}</span>
    {:else}
      <SteamSelect
        value={selectedMC}
        options={mcOptions}
        disabled={mcVersions.length === 0}
        on:change={onMCChange}
      />
    {/if}
  </div>

  {#if loader === 'fabric'}
    <div class="row">
      <span class="row-label">Fabric</span>
      {#if locked}
        <span class="val-locked">{selectedFabric || '—'}</span>
      {:else}
        <SteamSelect
          value={selectedFabric}
          options={fabricOptions}
          disabled={fabricVersions.length === 0}
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
        value={selectedJava}
        options={javaOptions}
        disabled={javaOptions.length === 0}
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
