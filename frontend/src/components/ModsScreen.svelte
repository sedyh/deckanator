<script>
  import { onMount, onDestroy, tick } from 'svelte'
  import {
    SearchMods, GetModVersions, InstallMod, DeleteMod, ListMods, FetchModInfo
  } from '../../wailsjs/go/internal/App.js'
  import SteamSelect from './SteamSelect.svelte'
  import { IconSearch, IconTrash, IconArrowLeft, IconDownload, IconBan } from '../lib/icons.js'
  import { consumeKey } from '../lib/input.js'

  export let profile
  export let onClose = () => {}

  const PAGE_SIZE = 20

  let query         = ''
  let sortBy        = 'downloads'
  let filterInstalled  = false
  let filterMods       = true
  let filterDatapacks  = true
  let page          = 0
  let totalHits     = 0

  let results      = []
  let installedMods = []
  let listFading    = false
  let searchTimer   = null
  let searchActive  = false

  let selectedMod       = null
  let modVersions       = []
  let selectedVersionId = ''
  let installing        = false
  let installError      = ''

  // 'left' | 'right'
  let focusCol  = 'right'
  // right zones: search | f-installed | f-mods | f-datapacks | sort | pg-prev | pg-next | version | install | back
  let focusZone = 'search'
  let listIdx   = 0

  let searchInputEl
  let listEl
  let installBtnEl
  let backBtnEl
  let versionSelRef
  let sortSelRef
  let pagerEl
  let fInstalledEl
  let fModsEl
  let fDatapacksEl

  $: installedSet = new Set(installedMods.map(m => m.project_id))

  $: installedList = installedMods.map(m => {
      const found = results.find(r => r.project_id === m.project_id)
      return found ?? {
        project_id:   m.project_id,
        title:        m.title,
        description:  m.description ?? '',
        icon_url:     m.icon_url ?? '',
        downloads:    0,
        project_type: m.project_type ?? 'mod',
        categories:   m.project_type === 'datapack' ? ['datapack'] : [],
      }
    })

  $: effectiveTotal = filterInstalled ? installedList.length : totalHits
  $: totalPages = Math.max(1, Math.ceil(effectiveTotal / PAGE_SIZE))

  $: displayList = filterInstalled
    ? installedList.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)
    : results

  $: versionOptions = modVersions.map(v => ({ value: v.id, label: v.version_number }))

  $: rightZones = buildRightZones(selectedMod, versionOptions)

  function buildRightZones(mod, versions) {
    const z = ['search', 'f-installed', 'f-mods', 'f-datapacks', 'sort', 'pager']
    if (mod && versions.length > 0) z.push('version')
    z.push('install', 'back')
    return z
  }

  const SORT_OPTIONS = [
    { value: 'relevance', label: 'Relevance' },
    { value: 'downloads', label: 'Downloads' },
    { value: 'follows',   label: 'Follows'   },
    { value: 'newest',    label: 'Newest'    },
    { value: 'updated',   label: 'Updated'   },
  ]

  async function fetchMissingInfo(mods) {
    const missing = mods.filter(m => !m.icon_url || !m.description)
    if (missing.length === 0) return
    await Promise.all(missing.map(async m => {
      try {
        const info = await FetchModInfo(profile.id, m.project_id)
        if (info.icon_url || info.description) {
          installedMods = installedMods.map(x =>
            x.project_id === m.project_id
              ? { ...x, icon_url: info.icon_url || x.icon_url, description: info.description || x.description }
              : x
          )
        }
      } catch (_) {}
    }))
  }

  onMount(async () => {
    console.log('[mods] mount profile:', profile?.id, 'mcVersion:', profile?.mcVersion, 'loader:', profile?.loader)
    installedMods = await ListMods(profile.id)
    console.log('[mods] installedMods:', installedMods)
    fetchMissingInfo(installedMods)
    await doSearch()
    await tick()
    searchInputEl?.focus()
    window.addEventListener('keydown', handleKey, true)
  })

  onDestroy(() => {
    window.removeEventListener('keydown', handleKey, true)
  })

  async function doSearch(resetPage = false) {
    if (resetPage) page = 0
    listFading = true
    const params = {
      query,
      mcVersion: profile.mcVersion ?? '',
      loader:    profile.loader    ?? '',
      sortBy,
      offset:    page * PAGE_SIZE,
      filterMods,
      filterDatapacks,
    }
    console.log('[mods] doSearch', params)
    try {
      const [res] = await Promise.all([
        SearchMods(
          params.query, params.mcVersion, params.loader,
          params.sortBy, params.offset,
          params.filterMods, params.filterDatapacks,
        ).catch(err => { console.error('[mods] SearchMods error:', err); return { hits: [], total_hits: 0 } }),
        new Promise(r => setTimeout(r, 500)),
      ])
      console.log('[mods] raw res keys:', res ? Object.keys(res) : 'null', 'res:', res)
      console.log('[mods] results:', res?.hits?.length, 'total:', res?.total_hits)
      results   = res.hits       ?? []
      totalHits = res.total_hits ?? 0
      console.log('[mods] results set, length:', results.length, 'displayList will be:', results.length)
    } catch (err) {
      console.error('[mods] doSearch catch:', err)
      results   = []
      totalHits = 0
    }
    listFading = false
  }

  function onQueryInput() {
    clearTimeout(searchTimer)
    searchTimer = setTimeout(() => doSearch(true), 400)
  }

  let pendingModId        = null
  let versionsCompatible  = true

  async function selectMod(mod) {
    if (selectedMod?.project_id === mod.project_id) return
    installError  = ''
    pendingModId  = mod.project_id
    try {
      const effectiveType = mod.categories?.includes('datapack') ? 'datapack' : (mod.project_type ?? 'mod')
      const versions = await GetModVersions(
        mod.project_id,
        profile.mcVersion ?? '',
        effectiveType,
        profile.loader    ?? '',
      )
      if (pendingModId !== mod.project_id) return
      const mcVer = profile.mcVersion ?? ''
      const compatible = mcVer === '' || versions.some(v => v.game_versions?.includes(mcVer))
      selectedMod          = mod
      modVersions          = versions
      selectedVersionId    = versions.length > 0 ? versions[0].id : ''
      versionsCompatible   = compatible
    } catch (_) {
      if (pendingModId !== mod.project_id) return
      selectedMod          = mod
      modVersions          = []
      selectedVersionId    = ''
      versionsCompatible   = true
    }
  }

  async function handleInstall() {
    if (!selectedMod || !selectedVersionId || installing) return
    const ver  = modVersions.find(v => v.id === selectedVersionId)
    if (!ver) return
    const file = ver.files.find(f => f.primary) ?? ver.files[0]
    if (!file) return
    installing   = true
    installError = ''
    try {
      const effectiveType = selectedMod.categories?.includes('datapack') ? 'datapack' : (selectedMod.project_type ?? 'mod')
      await InstallMod(
        profile.id,
        selectedMod.project_id,
        selectedMod.title,
        selectedMod.description ?? '',
        effectiveType,
        selectedMod.icon_url ?? '',
        ver.id,
        file.url,
        file.filename,
      )
      installedMods = await ListMods(profile.id)
    } catch (err) {
      installError = String(err)
    }
    installing = false
  }

  async function handleDelete(projectID, e) {
    e?.stopPropagation()
    try {
      await DeleteMod(profile.id, projectID)
      installedMods = await ListMods(profile.id)
      if (selectedMod?.project_id === projectID) {
        selectedMod       = null
        modVersions       = []
        selectedVersionId = ''
      }
    } catch (err) {
      installError = String(err)
    }
  }

  async function goPrev() {
    if (page <= 0) return
    page--
    if (!filterInstalled) await doSearch()
    listIdx = 0
  }

  async function goNext() {
    if (page + 1 >= totalPages) return
    page++
    if (!filterInstalled) await doSearch()
    listIdx = 0
  }

  function handleKey(e) {
    if (!consumeKey(e)) return
    if (document.querySelector('.wrap.open')) return

    if (e.key === 'Escape') {
      e.preventDefault(); e.stopPropagation()
      if (searchActive) {
        searchActive = false
        return
      }
      onClose()
      return
    }

    if (focusZone === 'search' && searchActive) {
      if (e.key === 'Enter') {
        e.preventDefault(); e.stopPropagation()
        searchActive = false
        return
      }
      e.stopPropagation()
      return
    }

    if (['ArrowUp','ArrowDown','ArrowLeft','ArrowRight','Enter'].includes(e.key)) {
      e.stopPropagation()
    }

    if (focusCol === 'left') handleLeftKey(e)
    else                     handleRightKey(e)
  }

  function handleLeftKey(e) {
    if (e.key === 'ArrowUp') {
      e.preventDefault(); e.stopPropagation()
      if (listIdx > 0) {
        listIdx--
        scrollToItem(listIdx)
        if (displayList[listIdx]) selectMod(displayList[listIdx])
      }
    } else if (e.key === 'ArrowDown') {
      e.preventDefault(); e.stopPropagation()
      if (listIdx < displayList.length - 1) {
        listIdx++
        scrollToItem(listIdx)
        if (displayList[listIdx]) selectMod(displayList[listIdx])
      }
    } else if (e.key === 'ArrowRight') {
      e.preventDefault(); e.stopPropagation()
      focusCol  = 'right'
      focusZone = 'search'
      tick().then(() => searchInputEl?.focus())
    } else if (e.key === 'Enter') {
      e.preventDefault(); e.stopPropagation()
      focusCol  = 'right'
      focusZone = 'install'
      tick().then(() => installBtnEl?.focus())
    }
  }

  function handleRightKey(e) {
    if (focusZone === 'search') {
      if (e.key === 'Enter') {
        e.preventDefault(); e.stopPropagation()
        searchActive = true
        searchInputEl?.focus()
      } else if (e.key === 'ArrowDown') {
        e.preventDefault(); e.stopPropagation()
        moveFocusRight(1)
      } else if (e.key === 'ArrowLeft') {
        e.preventDefault(); e.stopPropagation()
        goToList()
      }
      return
    }

    if (focusZone === 'pager') {
      if (e.key === 'ArrowLeft') {
        e.preventDefault(); e.stopPropagation()
        goPrev()
      } else if (e.key === 'ArrowRight') {
        e.preventDefault(); e.stopPropagation()
        goNext()
      } else if (e.key === 'ArrowDown') {
        e.preventDefault(); e.stopPropagation()
        moveFocusRight(1)
      } else if (e.key === 'ArrowUp') {
        e.preventDefault(); e.stopPropagation()
        moveFocusRight(-1)
      }
      return
    }

    if (e.key === 'ArrowDown') {
      e.preventDefault(); e.stopPropagation()
      moveFocusRight(1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault(); e.stopPropagation()
      moveFocusRight(-1)
    } else if (e.key === 'ArrowLeft') {
      e.preventDefault(); e.stopPropagation()
      goToList()
    } else if (e.key === 'Enter') {
      e.preventDefault(); e.stopPropagation()
      activateZone(focusZone)
    }
  }

  function moveFocusRight(delta) {
    const idx  = rightZones.indexOf(focusZone)
    const next = Math.max(0, Math.min(rightZones.length - 1, idx + delta))
    if (next === idx) return
    focusZone = rightZones[next]
    applyFocus(focusZone)
  }

  function goToList() {
    if (displayList.length === 0) return
    focusCol = 'left'
    listIdx  = Math.min(listIdx, displayList.length - 1)
    document.activeElement?.blur()
    tick().then(() => scrollToItem(listIdx))
  }

  function applyFocus(zone) {
    tick().then(() => {
      if      (zone === 'search')      searchInputEl?.focus()
      else if (zone === 'f-installed') fInstalledEl?.focus()
      else if (zone === 'f-mods')      fModsEl?.focus()
      else if (zone === 'f-datapacks') fDatapacksEl?.focus()
      else if (zone === 'sort')        sortSelRef?.focus()
      else if (zone === 'pager')       pagerEl?.focus()
      else if (zone === 'version')     versionSelRef?.focus()
      else if (zone === 'install')     installBtnEl?.focus()
      else if (zone === 'back')        backBtnEl?.focus()
    })
  }

  function activateZone(zone) {
    if      (zone === 'f-installed')  { filterInstalled = !filterInstalled; page = 0 }
    else if (zone === 'f-mods')       { filterMods      = !filterMods;      doSearch(true) }
    else if (zone === 'f-datapacks')  { filterDatapacks = !filterDatapacks; doSearch(true) }
    else if (zone === 'sort')         sortSelRef?.openMenu()
    else if (zone === 'pager')        goNext()
    else if (zone === 'install')      selectedMod && installedSet.has(selectedMod.project_id) ? handleDelete(selectedMod.project_id) : handleInstall()
    else if (zone === 'back')         onClose()
  }

  function scrollToItem(idx) {
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
  <div class="layout">

    <!-- Left column: mod list -->
    <div class="col-left">
      <div class="mod-list" class:fading={listFading} bind:this={listEl}>
        {#if displayList.length === 0}
          <div class="list-hint">No results</div>
        {:else}
          {#each displayList as mod, i}
            {@const installed = installedSet.has(mod.project_id)}
            <div
              class="mod-row"
              class:focused={focusCol === 'left' && listIdx === i}
              class:selected={selectedMod?.project_id === mod.project_id}
              on:click={() => { focusCol = 'left'; listIdx = i; selectMod(mod) }}
              role="button"
              tabindex="-1"
            >
              <div class="mod-icon" style="background:{iconBg(mod.title)}">
                {#if mod.icon_url}
                  <img src={mod.icon_url} alt="" class="mod-img" />
                {:else}
                  <span class="mod-init">{initials(mod.title)}</span>
                {/if}
              </div>
              <div class="mod-body">
                <div class="mod-name">{mod.title}</div>
                {#if mod.description}
                  <div class="mod-desc">{mod.description}</div>
                {/if}
              </div>
              <div class="mod-badges">
                {#if mod.categories?.includes('datapack')}
                  <span class="badge badge-dp">Datapack</span>
                {:else}
                  <span class="badge badge-mod">Mod</span>
                {/if}
                {#if installed}
                  <span class="badge badge-ok">Installed</span>
                {/if}
              </div>
            </div>
          {/each}
        {/if}
      </div>
    </div>

    <!-- Right column: controls -->
    <div class="col-right">

      <!-- Search -->
      <div
        class="search-row"
        class:zone-focused={focusZone === 'search' && focusCol === 'right'}
        class:search-active={searchActive}
      >
        <span class="search-icon">{@html IconSearch}</span>
        <input
          bind:this={searchInputEl}
          class="search-input"
          placeholder="Search..."
          bind:value={query}
          readonly={!searchActive}
          on:input={onQueryInput}
          on:mousedown={() => { searchActive = true; focusCol = 'right'; focusZone = 'search' }}
          on:focus={() => { focusCol = 'right'; focusZone = 'search' }}
          on:blur={() => { searchActive = false }}
          tabindex="-1"
        />
        {#if listFading}<span class="spinner" />{/if}
      </div>

      <!-- Show only -->
      <div class="section">
        <div class="section-label">Show only</div>
        <button
          bind:this={fInstalledEl}
          class="toggle-row"
          class:zone-focused={focusZone === 'f-installed' && focusCol === 'right'}
          on:click={() => { filterInstalled = !filterInstalled; page = 0 }}
          on:focus={() => { focusCol = 'right'; focusZone = 'f-installed' }}
          tabindex="-1"
        >
          <span class="checkbox" class:checked={filterInstalled} />
          Installed
        </button>
      </div>

      <!-- Include -->
      <div class="section">
        <div class="section-label">Include</div>
        <button
          bind:this={fModsEl}
          class="toggle-row"
          class:zone-focused={focusZone === 'f-mods' && focusCol === 'right'}
          on:click={() => { filterMods = !filterMods; doSearch(true) }}
          on:focus={() => { focusCol = 'right'; focusZone = 'f-mods' }}
          tabindex="-1"
        >
          <span class="checkbox" class:checked={filterMods} />
          Mods
        </button>
        <button
          bind:this={fDatapacksEl}
          class="toggle-row"
          class:zone-focused={focusZone === 'f-datapacks' && focusCol === 'right'}
          on:click={() => { filterDatapacks = !filterDatapacks; doSearch(true) }}
          on:focus={() => { focusCol = 'right'; focusZone = 'f-datapacks' }}
          tabindex="-1"
        >
          <span class="checkbox" class:checked={filterDatapacks} />
          Datapacks
        </button>
      </div>

      <!-- Sort -->
      <div class="section">
        <div class="section-label">Sort by</div>
        <div on:mousedown={() => { focusCol = 'right'; focusZone = 'sort' }} role="none">
          <SteamSelect
            bind:this={sortSelRef}
            bind:value={sortBy}
            options={SORT_OPTIONS}
            on:change={() => doSearch(true)}
            on:focus={() => { focusCol = 'right'; focusZone = 'sort' }}
          />
        </div>
      </div>

      <!-- Pagination -->
      <div class="section">
        <div class="section-label">Page</div>
        <div
          class="pager"
          class:zone-focused={focusZone === 'pager' && focusCol === 'right'}
          role="group"
          tabindex="-1"
          bind:this={pagerEl}
          on:focus={() => { focusCol = 'right'; focusZone = 'pager' }}
        >
          <button
            class="pg-arrow"
            disabled={page <= 0}
            on:click={goPrev}
            tabindex="-1"
          >&#8249;</button>
          <span class="pg-info">{page + 1} / {totalPages}</span>
          <button
            class="pg-arrow"
            disabled={page + 1 >= totalPages}
            on:click={goNext}
            tabindex="-1"
          >&#8250;</button>
        </div>
      </div>

      <div class="spacer" />

      {#if installError}
        <div class="error-msg">{installError}</div>
      {/if}

      {#if selectedMod && versionOptions.length > 0}
        <div class="version-row">
          <span class="version-label">Version</span>
          <div
            class="version-sel"
            on:mousedown={() => { focusCol = 'right'; focusZone = 'version' }}
            role="none"
          >
            <SteamSelect
              bind:this={versionSelRef}
              bind:value={selectedVersionId}
              options={versionOptions}
              disabled={false}
              on:focus={() => { focusCol = 'right'; focusZone = 'version' }}
            />
          </div>
        </div>
      {/if}

      <button
        bind:this={installBtnEl}
        class="action-btn"
        class:install-btn={!(selectedMod && installedSet.has(selectedMod.project_id))}
        class:delete-btn={selectedMod && installedSet.has(selectedMod.project_id)}
        class:btn-focused={focusZone === 'install' && focusCol === 'right'}
        disabled={!selectedMod || installing || (!installedSet.has(selectedMod?.project_id ?? '') && (!selectedVersionId || !versionsCompatible))}
        on:click={() => selectedMod && installedSet.has(selectedMod.project_id) ? handleDelete(selectedMod.project_id) : handleInstall()}
        on:focus={() => { focusCol = 'right'; focusZone = 'install' }}
        tabindex="-1"
      >
        {#if installing}
          <span class="spinner" />Installing...
        {:else if selectedMod && installedSet.has(selectedMod.project_id)}
          <span class="btn-icon">{@html IconTrash}</span>Delete
        {:else if selectedMod && !versionsCompatible}
          <span class="btn-icon">{@html IconBan}</span>Not supported
        {:else}
          <span class="btn-icon">{@html IconDownload}</span>Install
        {/if}
      </button>

      <button
        bind:this={backBtnEl}
        class="action-btn back-btn"
        class:btn-focused={focusZone === 'back' && focusCol === 'right'}
        on:click={onClose}
        on:focus={() => { focusCol = 'right'; focusZone = 'back' }}
        tabindex="-1"
      >
        <span class="btn-icon">{@html IconArrowLeft}</span>Back
      </button>

    </div>
  </div>
</div>

<style>
  .mods-screen {
    position: absolute;
    inset: 0;
    background: var(--bg);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }

  .layout {
    display: flex;
    gap: 1.5rem;
    width: min(54rem, calc(100vw - 2rem));
    height: calc(100vh - 2rem);
  }

  /* ── Left column ── */
  .col-left {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    gap: 0;
  }

  .mod-list {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 0.11rem;
    scrollbar-width: thin;
    scrollbar-color: rgba(255,255,255,0.12) transparent;
    transition: opacity 0.45s ease;
  }
  .mod-list.fading {
    opacity: 0.12;
    pointer-events: none;
  }

  .list-hint {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--text-sub);
    font-size: 0.78rem;
  }

  /* Mod row */
  .mod-row {
    display: flex;
    align-items: center;
    gap: 0.67rem;
    padding: 0.5rem 0.78rem;
    background: var(--card);
    cursor: pointer;
    transition: background var(--t);
    min-height: 3.56rem;
    box-sizing: border-box;
    flex-shrink: 0;
  }

  .mod-row:hover,
  .mod-row.focused,
  .mod-row.selected { background: var(--card-btn-hover); }

  .mod-row.selected { box-shadow: inset 0 0 0 2px rgba(255,255,255,0.12); }
  .mod-row.focused  { box-shadow: inset 0 0 0 2px rgba(255,255,255,0.8); }

  .mod-icon {
    width: 2.22rem;
    height: 2.22rem;
    border-radius: 0.22rem;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    overflow: hidden;
  }

  .mod-img { width: 100%; height: 100%; object-fit: cover; }

  .mod-init {
    font-size: 0.67rem;
    font-weight: 900;
    color: rgba(255,255,255,0.85);
  }

  .mod-body {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0.11rem;
  }

  .mod-name {
    font-size: 0.78rem;
    font-weight: 700;
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .mod-desc {
    font-size: 0.67rem;
    color: var(--text-sub);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .mod-badges {
    display: flex;
    flex-direction: column;
    gap: 0.22rem;
    flex-shrink: 0;
    align-items: flex-end;
  }

  .badge {
    font-size: 0.5rem;
    font-weight: 700;
    padding: 0.11rem 0;
    width: 4.2rem;
    text-align: center;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    white-space: nowrap;
  }

  .badge-mod { color: #8bc4e8; background: rgba(139,196,232,0.15); }
  .badge-dp  { color: #8be8a0; background: rgba(139,232,160,0.15); }
  .badge-ok  { color: var(--accent); background: rgba(30,143,255,0.15); }

  .del-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 1.78rem;
    height: 1.78rem;
    background: transparent;
    border: none;
    cursor: pointer;
    color: var(--text-sub);
    flex-shrink: 0;
    transition: color var(--t), background var(--t);
  }
  .del-btn:hover, .del-btn:focus { color: #e05050; background: rgba(224,80,80,0.12); outline: none; }
  .del-btn :global(svg) { width: 0.89rem; height: 0.89rem; }

  /* ── Right column ── */
  .col-right {
    width: 18rem;
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    gap: 0.67rem;
  }

  .search-row {
    display: flex;
    align-items: center;
    gap: 0.56rem;
    background: var(--card);
    padding: 0 0.78rem;
    height: 2.44rem;
    flex-shrink: 0;
    transition: box-shadow var(--t);
  }
  .search-row.zone-focused { box-shadow: inset 0 0 0 2px var(--accent); }
  .search-row.search-active { box-shadow: inset 0 0 0 2px #fff; }

  .search-icon { display: flex; align-items: center; color: var(--text-sub); flex-shrink: 0; }
  .search-icon :global(svg) { width: 0.89rem; height: 0.89rem; }

  .search-input {
    flex: 1;
    background: transparent;
    border: none;
    outline: none;
    color: var(--text);
    font-size: 0.78rem;
    font-family: inherit;
    caret-color: transparent;
    cursor: default;
  }
  .search-active .search-input {
    caret-color: var(--accent);
    cursor: text;
  }
  .search-input::placeholder { color: var(--text-sub); }

  .section {
    display: flex;
    flex-direction: column;
    gap: 0.22rem;
    flex-shrink: 0;
  }

  .section-label {
    font-size: 0.56rem;
    font-weight: 700;
    color: var(--text-sub);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    padding-left: 0.11rem;
  }

  .section :global(.wrap) { width: 100%; }

  .toggle-row {
    display: flex;
    align-items: center;
    gap: 0.56rem;
    height: 1.89rem;
    padding: 0 0.78rem;
    background: var(--card);
    color: var(--text-sub);
    font-size: 0.78rem;
    font-family: inherit;
    font-weight: 400;
    cursor: pointer;
    border: none;
    transition: background var(--t), color var(--t), box-shadow var(--t);
    text-align: left;
  }
  .toggle-row:hover,
  .toggle-row.zone-focused { background: var(--card-btn-hover); color: var(--text); outline: none; }
  .toggle-row.zone-focused  { box-shadow: inset 0 0 0 2px var(--accent); }

  .checkbox {
    width: 0.83rem;
    height: 0.83rem;
    border: 2px solid rgba(255,255,255,0.3);
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background var(--t), border-color var(--t);
  }
  .checkbox.checked { background: var(--accent); border-color: var(--accent); }
  .checkbox.checked::after {
    content: '';
    width: 0.39rem;
    height: 0.22rem;
    border-left: 2px solid #fff;
    border-bottom: 2px solid #fff;
    transform: rotate(-45deg) translateY(-0.06rem);
    display: block;
  }

  .pager {
    display: flex;
    align-items: center;
    background: var(--card);
    outline: none;
    transition: box-shadow var(--t);
  }
  .pager.zone-focused {
    box-shadow: inset 0 0 0 2px var(--accent);
  }

  .pg-arrow {
    width: 1.89rem;
    height: 1.89rem;
    background: transparent;
    color: var(--text-sub);
    font-size: 1.11rem;
    font-family: inherit;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    transition: color var(--t), background var(--t);
  }
  .pager.zone-focused .pg-arrow:not(:disabled) { color: var(--text); }
  .pg-arrow:not(:disabled):hover { background: var(--card-btn-hover); color: var(--text); }
  .pg-arrow:disabled { opacity: 0.3; cursor: default; }

  .pg-info {
    flex: 1;
    text-align: center;
    font-size: 0.72rem;
    color: var(--text-sub);
    user-select: none;
  }

  .spacer { flex: 1; }

  .error-msg {
    font-size: 0.67rem;
    color: #e05050;
    background: rgba(224,80,80,0.08);
    padding: 0.33rem 0.56rem;
    flex-shrink: 0;
  }

  .version-row {
    display: flex;
    align-items: center;
    gap: 0.56rem;
    flex-shrink: 0;
  }

  .version-label {
    font-size: 0.72rem;
    color: var(--text-sub);
    white-space: nowrap;
  }

  .version-sel { flex: 1; min-width: 0; }
  .version-sel :global(.wrap) { width: 100%; }

  .action-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.44rem;
    height: 2.44rem;
    padding: 0 1.11rem;
    font-size: 0.78rem;
    font-weight: 700;
    font-family: inherit;
    cursor: pointer;
    border: none;
    transition: background var(--t), color var(--t), box-shadow var(--t);
    flex-shrink: 0;
  }
  .action-btn:disabled { opacity: 0.4; cursor: default; }

  .install-btn { background: var(--accent); color: #fff; }
  .install-btn:not(:disabled):hover,
  .install-btn:not(:disabled):focus,
  .install-btn.btn-focused:not(:disabled) {
    background: var(--accent-dim);
    box-shadow: inset 0 0 0 2px #fff, 0 2px 20px rgba(30,143,255,0.3);
    outline: none;
  }

  .delete-btn { background: rgba(224,80,80,0.2); color: #e05050; }
  .delete-btn:not(:disabled):hover,
  .delete-btn:not(:disabled):focus,
  .delete-btn.btn-focused:not(:disabled) {
    background: rgba(224,80,80,0.35);
    box-shadow: inset 0 0 0 2px #e05050;
    outline: none;
  }

  .back-btn { background: var(--card); color: var(--text-sub); }
  .back-btn:hover,
  .back-btn:focus,
  .back-btn.btn-focused {
    background: var(--card-btn-hover);
    color: var(--text);
    box-shadow: inset 0 0 0 2px rgba(255,255,255,0.2);
    outline: none;
  }

  .btn-icon { display: flex; align-items: center; }
  .btn-icon :global(svg) { width: 1rem; height: 1rem; }

  .spinner {
    width: 0.89rem;
    height: 0.89rem;
    border: 2px solid rgba(255,255,255,0.15);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
    flex-shrink: 0;
  }

  @keyframes spin { to { transform: rotate(360deg); } }
</style>
