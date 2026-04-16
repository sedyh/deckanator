<script>
  import { onMount, onDestroy, tick } from 'svelte'
  import {
    SearchMods, GetModVersions, InstallMod, DeleteMod, ListMods
  } from '../../wailsjs/go/internal/App.js'
  import SteamSelect from './SteamSelect.svelte'
  import { IconSearch, IconTrash, IconArrowLeft, IconDownload } from '../lib/icons.js'

  export let profile
  export let onClose = () => {}

  let query = ''
  let searchResults = []
  let installedMods = []
  let loadingSearch = false
  let searchTimer = null

  let selectedMod = null
  let modVersions = []
  let selectedVersionId = ''
  let loadingVersions = false

  let installing = false
  let installError = ''

  let focusZone = 'search'
  let focusedCardIdx = 0

  let searchInputEl
  let listEl
  let installBtnEl
  let backBtnEl
  let versionSelRef

  $: installedSet = new Set(installedMods.map(m => m.project_id))

  $: displayList = query.trim()
    ? searchResults
    : installedMods.map(m => ({
        project_id: m.project_id,
        title: m.title,
        description: '',
        icon_url: '',
        slug: m.project_id,
      }))

  $: versionOptions = modVersions.map(v => ({
    id: v.id,
    label: v.version_number,
  }))

  onMount(async () => {
    installedMods = await ListMods(profile.id)
    searchInputEl?.focus()
    window.addEventListener('keydown', handleKey, true)
  })

  onDestroy(() => {
    window.removeEventListener('keydown', handleKey, true)
  })

  async function doSearch() {
    if (!query.trim()) {
      searchResults = []
      return
    }
    loadingSearch = true
    try {
      searchResults = await SearchMods(query, profile.mcVersion, profile.loader)
    } catch (e) {
      searchResults = []
    }
    loadingSearch = false
  }

  function onQueryInput() {
    clearTimeout(searchTimer)
    searchTimer = setTimeout(doSearch, 400)
  }

  async function selectMod(mod) {
    if (selectedMod?.project_id === mod.project_id) return
    selectedMod = mod
    selectedVersionId = ''
    modVersions = []
    installError = ''
    loadingVersions = true
    try {
      modVersions = await GetModVersions(mod.project_id, profile.mcVersion, profile.loader)
      if (modVersions.length > 0) selectedVersionId = modVersions[0].id
    } catch (e) {
      modVersions = []
    }
    loadingVersions = false
  }

  async function handleInstall() {
    if (!selectedMod || !selectedVersionId || installing) return
    const ver = modVersions.find(v => v.id === selectedVersionId)
    if (!ver) return
    const file = ver.files.find(f => f.primary) ?? ver.files[0]
    if (!file) return
    installing = true
    installError = ''
    try {
      await InstallMod(
        profile.id,
        selectedMod.project_id,
        selectedMod.title,
        ver.id,
        file.url,
        file.filename,
      )
      installedMods = await ListMods(profile.id)
    } catch (e) {
      installError = String(e)
    }
    installing = false
  }

  async function handleDelete(projectID, e) {
    e.stopPropagation()
    try {
      await DeleteMod(profile.id, projectID)
      installedMods = await ListMods(profile.id)
      if (selectedMod?.project_id === projectID) {
        selectedMod = null
        modVersions = []
        selectedVersionId = ''
      }
    } catch (e) {
      installError = String(e)
    }
  }

  function goToBottom() {
    if (versionOptions.length > 0) {
      focusZone = 'version'
      tick().then(() => versionSelRef?.focus())
    } else {
      focusZone = 'install'
      installBtnEl?.focus()
    }
  }

  function goToList() {
    if (displayList.length > 0) {
      focusZone = 'list'
      scrollCardIntoView(focusedCardIdx)
    } else {
      focusZone = 'search'
      searchInputEl?.focus()
    }
  }

  function handleKey(e) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
      return
    }

    if (focusZone === 'search') {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        if (displayList.length > 0) {
          focusZone = 'list'
          focusedCardIdx = 0
          scrollCardIntoView(0)
          tick().then(() => selectMod(displayList[0]))
        } else {
          goToBottom()
        }
      }
      return
    }

    if (focusZone === 'list') {
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        if (focusedCardIdx > 0) {
          focusedCardIdx--
          scrollCardIntoView(focusedCardIdx)
          selectMod(displayList[focusedCardIdx])
        } else {
          focusZone = 'search'
          searchInputEl?.focus()
        }
        return
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        if (focusedCardIdx < displayList.length - 1) {
          focusedCardIdx++
          scrollCardIntoView(focusedCardIdx)
          selectMod(displayList[focusedCardIdx])
        } else {
          goToBottom()
        }
        return
      }
      if (e.key === 'Enter') {
        e.preventDefault()
        if (displayList[focusedCardIdx]) {
          selectMod(displayList[focusedCardIdx])
          tick().then(goToBottom)
        }
        return
      }
      return
    }

    if (focusZone === 'version') {
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        goToList()
        return
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        focusZone = 'install'
        installBtnEl?.focus()
        return
      }
      return
    }

    if (focusZone === 'install') {
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        if (versionOptions.length > 0) {
          focusZone = 'version'
          versionSelRef?.focus()
        } else {
          goToList()
        }
        return
      }
      if (e.key === 'ArrowRight') {
        e.preventDefault()
        focusZone = 'back'
        backBtnEl?.focus()
        return
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        focusZone = 'back'
        backBtnEl?.focus()
        return
      }
      if (e.key === 'Enter') {
        e.preventDefault()
        handleInstall()
        return
      }
    }

    if (focusZone === 'back') {
      if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
        e.preventDefault()
        focusZone = 'install'
        installBtnEl?.focus()
        return
      }
      if (e.key === 'Enter') {
        e.preventDefault()
        onClose()
        return
      }
    }
  }

  function scrollCardIntoView(idx) {
    tick().then(() => {
      const el = listEl?.children[idx]
      el?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    })
  }

  function initials(title) {
    return title.split(/\s+/).slice(0, 2).map(w => w[0]?.toUpperCase() ?? '').join('')
  }

  function iconBg(title) {
    const colors = ['#1a5c8a','#1a7a4a','#7a1a6a','#7a4a1a','#1a6a7a','#5a1a7a']
    let h = 0
    for (const c of title) h = (h * 31 + c.charCodeAt(0)) & 0xffffffff
    return colors[Math.abs(h) % colors.length]
  }
</script>

<div class="mods-screen">
  <div class="inner">

    <div class="search-row">
      <span class="search-icon">{@html IconSearch}</span>
      <input
        bind:this={searchInputEl}
        class="search-input"
        placeholder="Search mods..."
        bind:value={query}
        on:input={onQueryInput}
        on:focus={() => focusZone = 'search'}
        tabindex="-1"
      />
      {#if loadingSearch}
        <span class="search-spinner" />
      {/if}
    </div>

    <div class="list" bind:this={listEl}>
      {#if displayList.length === 0 && !loadingSearch}
        <div class="empty-hint">
          {#if query.trim()}
            No mods found
          {:else}
            No mods installed
          {/if}
        </div>
      {/if}
      {#each displayList as mod, i}
        {@const installed = installedSet.has(mod.project_id)}
        <div
          class="mod-card"
          class:focused={focusZone === 'list' && focusedCardIdx === i}
          class:selected={selectedMod?.project_id === mod.project_id}
          on:click={() => { focusZone = 'list'; focusedCardIdx = i; selectMod(mod) }}
          role="button"
          tabindex="-1"
        >
          <div class="mod-icon" style="background:{iconBg(mod.title)}">
            {#if mod.icon_url}
              <img src={mod.icon_url} alt="" class="mod-icon-img" />
            {:else}
              <span class="mod-initials">{initials(mod.title)}</span>
            {/if}
          </div>
          <div class="mod-info">
            <div class="mod-title">
              {mod.title}
              {#if installed}
                <span class="mod-badge">Installed</span>
              {/if}
            </div>
            {#if mod.description}
              <div class="mod-desc">{mod.description}</div>
            {/if}
          </div>
          {#if installed}
            <button
              class="mod-delete-btn"
              on:click={(e) => handleDelete(mod.project_id, e)}
              tabindex="-1"
              title="Remove"
            >
              {@html IconTrash}
            </button>
          {/if}
        </div>
      {/each}
    </div>

    <div class="bottom">
      {#if installError}
        <div class="install-error">{installError}</div>
      {/if}

      <div class="bottom-row">
        <div class="version-wrap" class:invisible={!selectedMod || versionOptions.length === 0}>
          <span class="version-label">Version</span>
          <SteamSelect
            bind:this={versionSelRef}
            bind:value={selectedVersionId}
            options={versionOptions}
            disabled={loadingVersions || !selectedMod}
            on:focus={() => focusZone = 'version'}
          />
        </div>

        <div class="action-btns">
          <button
            bind:this={installBtnEl}
            class="action-btn install-btn"
            class:btn-focused={focusZone === 'install'}
            disabled={!selectedMod || !selectedVersionId || installing || installedSet.has(selectedMod?.project_id ?? '')}
            on:click={handleInstall}
            on:focus={() => focusZone = 'install'}
            tabindex="-1"
          >
            {#if installing}
              <span class="btn-spinner" />
              Installing...
            {:else if selectedMod && installedSet.has(selectedMod.project_id)}
              Installed
            {:else}
              <span class="btn-icon">{@html IconDownload}</span>
              Install
            {/if}
          </button>

          <button
            bind:this={backBtnEl}
            class="action-btn back-btn"
            class:btn-focused={focusZone === 'back'}
            on:click={onClose}
            on:focus={() => focusZone = 'back'}
            tabindex="-1"
          >
            <span class="btn-icon">{@html IconArrowLeft}</span>
            Back
          </button>
        </div>
      </div>
    </div>

  </div>
</div>

<style>
  .mods-screen {
    position: absolute;
    inset: 0;
    background: var(--bg);
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }

  .inner {
    width: 38rem;
    display: flex;
    flex-direction: column;
    gap: 0.78rem;
  }

  /* ── Search ── */
  .search-row {
    display: flex;
    align-items: center;
    gap: 0.67rem;
    background: var(--card);
    padding: 0 0.89rem;
    height: 2.44rem;
  }

  .search-icon {
    display: flex;
    align-items: center;
    color: var(--text-sub);
    flex-shrink: 0;
  }
  .search-icon :global(svg) { width: 1rem; height: 1rem; }

  .search-input {
    flex: 1;
    background: transparent;
    border: none;
    outline: none;
    color: var(--text);
    font-size: 0.83rem;
    font-family: inherit;
    caret-color: var(--accent);
  }
  .search-input::placeholder { color: var(--text-sub); }

  .search-spinner {
    width: 0.89rem;
    height: 0.89rem;
    border: 2px solid rgba(255,255,255,0.15);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
    flex-shrink: 0;
  }

  /* ── Mod list ── */
  .list {
    display: flex;
    flex-direction: column;
    gap: 0.22rem;
    max-height: calc(3 * 5.33rem + 2 * 0.22rem);
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: rgba(255,255,255,0.12) transparent;
  }

  .empty-hint {
    padding: 1.56rem;
    text-align: center;
    color: var(--text-sub);
    font-size: 0.78rem;
  }

  .mod-card {
    display: flex;
    align-items: center;
    gap: 0.89rem;
    padding: 0.67rem 0.89rem;
    background: var(--card);
    cursor: pointer;
    transition: background var(--t);
    min-height: 5.33rem;
    box-sizing: border-box;
  }

  .mod-card:hover,
  .mod-card.focused,
  .mod-card.selected {
    background: var(--card-btn-hover);
  }

  .mod-card.focused {
    box-shadow: inset 2px 0 0 var(--accent);
  }

  .mod-card.selected {
    box-shadow: inset 2px 0 0 var(--accent);
  }

  .mod-icon {
    width: 3.11rem;
    height: 3.11rem;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    overflow: hidden;
  }

  .mod-icon-img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .mod-initials {
    font-size: 1rem;
    font-weight: 900;
    color: rgba(255,255,255,0.85);
    letter-spacing: -0.02em;
  }

  .mod-info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0.22rem;
  }

  .mod-title {
    font-size: 0.83rem;
    font-weight: 700;
    color: var(--text);
    display: flex;
    align-items: center;
    gap: 0.44rem;
  }

  .mod-badge {
    font-size: 0.56rem;
    font-weight: 700;
    color: var(--accent);
    background: rgba(30,143,255,0.15);
    padding: 0.1rem 0.33rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .mod-desc {
    font-size: 0.72rem;
    color: var(--text-sub);
    overflow: hidden;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    line-height: 1.4;
  }

  .mod-delete-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 2.22rem;
    height: 2.22rem;
    background: transparent;
    border: none;
    cursor: pointer;
    color: var(--text-sub);
    flex-shrink: 0;
    transition: color var(--t), background var(--t);
  }
  .mod-delete-btn:hover,
  .mod-delete-btn:focus {
    color: #e05050;
    background: rgba(224,80,80,0.12);
    outline: none;
  }
  .mod-delete-btn :global(svg) { width: 1.11rem; height: 1.11rem; }

  /* ── Bottom ── */
  .bottom {
    display: flex;
    flex-direction: column;
    gap: 0.56rem;
  }

  .install-error {
    font-size: 0.72rem;
    color: #e05050;
    background: rgba(224,80,80,0.08);
    padding: 0.44rem 0.67rem;
  }

  .bottom-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.89rem;
  }

  .version-wrap {
    display: flex;
    align-items: center;
    gap: 0.67rem;
  }

  .version-wrap.invisible {
    visibility: hidden;
  }

  .version-label {
    font-size: 0.72rem;
    color: var(--text-sub);
    white-space: nowrap;
  }

  .version-wrap :global(.wrap) {
    width: 9rem;
    flex-shrink: 0;
  }

  .action-btns {
    display: flex;
    gap: 0.56rem;
    flex-shrink: 0;
  }

  .action-btn {
    display: flex;
    align-items: center;
    gap: 0.44rem;
    height: 2.44rem;
    padding: 0 1.11rem;
    font-size: 0.78rem;
    font-weight: 700;
    font-family: inherit;
    cursor: pointer;
    border: none;
    transition: background var(--t), color var(--t), box-shadow var(--t);
  }

  .action-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }

  .install-btn {
    background: var(--accent);
    color: #fff;
  }
  .install-btn:not(:disabled):hover,
  .install-btn:not(:disabled):focus,
  .install-btn.btn-focused:not(:disabled) {
    background: var(--accent-dim);
    box-shadow: inset 0 0 0 2px #fff, 0 2px 20px rgba(30,143,255,0.3);
    outline: none;
  }

  .back-btn {
    background: var(--card);
    color: var(--text-sub);
  }
  .back-btn:hover,
  .back-btn:focus,
  .back-btn.btn-focused {
    background: var(--card-btn-hover);
    color: var(--text);
    box-shadow: inset 0 0 0 2px rgba(255,255,255,0.2);
    outline: none;
  }

  .btn-icon {
    display: flex;
    align-items: center;
  }
  .btn-icon :global(svg) { width: 1rem; height: 1rem; }

  .btn-spinner {
    width: 0.89rem;
    height: 0.89rem;
    border: 2px solid rgba(255,255,255,0.3);
    border-top-color: #fff;
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }
</style>
