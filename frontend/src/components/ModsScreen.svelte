<script>
  import { onMount, onDestroy, tick } from 'svelte'
  import {
    SearchMods, GetModVersions, InstallMod, DeleteMod, ListMods, FetchModInfo, CountWorlds,
    GetDatapackManagerStatus, SetOnScreenKeyboard
  } from '../../wailsjs/go/internal/App.js'
  import SteamSelect from './SteamSelect.svelte'
  import { IconSearch, IconTrash, IconArrowLeft, IconDownload, IconBan } from '../lib/icons.js'
  import { consumeKey, getInputMode, setInputModeLock } from '../lib/input.js'

  export let profile
  // Effective loader of the app flow; profile.loader stays "vanilla"
  // until the profile is installed, so it can't be used for filtering.
  export let loader = 'fabric'
  export let mcInstalled = false
  export let onClose = () => {}

  const PAGE_SIZE = 20

  let query         = ''
  let sortBy        = 'downloads'
  let filterInstalled  = false
  let filterMods       = true
  let filterDatapacks  = true
  let filterResourcepacks = true
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
  let fResourcepacksEl

  $: installedSet = new Set(installedMods.map(m => m.project_id))

  // The include toggles and the query filter the installed view locally:
  // in search mode they travel to the server as facets instead.
  function installedVisible(r, mods, datapacks, resourcepacks, q) {
    const isDp = r.project_type === 'datapack' || r.categories?.includes('datapack')
    const isRp = r.project_type === 'resourcepack'
    const typeOk = isDp ? datapacks : isRp ? resourcepacks : mods
    return typeOk && (!q || (r.title ?? '').toLowerCase().includes(q))
  }

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
    }).filter(r => installedVisible(
      r, filterMods, filterDatapacks, filterResourcepacks, query.trim().toLowerCase()
    ))

  $: effectiveTotal = filterInstalled ? installedList.length : totalHits
  $: totalPages = Math.max(1, Math.ceil(effectiveTotal / PAGE_SIZE))

  // The selected version's files reveal a bundled resource pack before
  // installation (search hits alone carry no file info).
  $: selectedBundledRP = !!modVersions
    .find(v => v.id === selectedVersionId)
    ?.files?.some(f => f.file_type === 'required-resource-pack')

  // Background bundle detection for the whole page: datapack hits get
  // their newest matching version fetched (4 at a time, cached per
  // project for the screen's lifetime) so "+ Resources" shows up in the
  // list without selecting each item.
  let bundleByProject = {}

  function prefetchBundles(hits) {
    const queue = hits.filter(h =>
      h.categories?.includes('datapack') && bundleByProject[h.project_id] === undefined)
    const worker = async () => {
      while (queue.length > 0) {
        const h = queue.shift()
        let bundled = false
        try {
          const vers = await GetModVersions(h.project_id, profile.mcVersion ?? '', 'datapack', loader)
          bundled = !!vers?.[0]?.files?.some(f => f.file_type === 'required-resource-pack')
        } catch {}
        bundleByProject = { ...bundleByProject, [h.project_id]: bundled }
      }
    }
    Promise.all(Array.from({ length: 4 }, worker)).catch(() => {})
  }

  // Rows are enriched with the installed meta's bundled resource pack
  // list (or the selected version's detection), so datapacks that ship
  // textures carry a badge.
  $: displayList = (filterInstalled
    ? installedList.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)
    : results
  ).map(r => {
    const meta = installedMods.find(m => m.project_id === r.project_id)
    if (meta?.resource_packs?.length) return { ...r, resource_packs: meta.resource_packs }
    if (r.project_id === selectedMod?.project_id && selectedBundledRP) {
      return { ...r, resource_packs: ['bundled'] }
    }
    if (bundleByProject[r.project_id]) return { ...r, resource_packs: ['bundled'] }
    return r
  })

  // A shrinking list (filters, search) must not leave the cursor
  // pointing past its end.
  $: if (listIdx >= displayList.length) listIdx = Math.max(0, displayList.length - 1)

  // Version labels are display-cleaned: per-segment "v"/"mc" prefixes
  // and loader/type noise tokens (fabric/quilt/datapack/...) are
  // stripped, "+" separators join with a dash, and the profile's game
  // version is dropped as redundant (the list is already filtered to
  // it) unless it's all that remains. If cleaning collides two versions
  // into one label, the raw names are kept for both.
  const VERSION_NOISE = /^(datapack|datapacks|data|resourcepack|resourcepacks|resource|fabric|quilt|forge|neoforge|mod|dp|rp)$/i

  function cleanVersionLabel(raw, mcVersion, versionType) {
    let s = raw.trim()
    // Authors often encode the release type as a letter prefix (B0.6.2);
    // strip it only when Modrinth's structured version_type confirms it,
    // since the type is appended to the label explicitly below.
    if (versionType === 'beta')  s = s.replace(/^b(?:eta)?[ .-]?(?=\d)/i, '')
    if (versionType === 'alpha') s = s.replace(/^a(?:lpha)?[ .-]?(?=\d)/i, '')
    const segs = []
    for (const part of s.split('+')) {
      for (let seg of part.split('-')) {
        seg = seg.replace(/^v(?=\d)/i, '').replace(/^mc(?=\d)/i, '')
        if (!seg || VERSION_NOISE.test(seg)) continue
        segs.push(seg)
      }
    }
    const isGameVer = x => x.replace(/^[a-z]+(?=\d)/i, '') === mcVersion
    const withoutGame = segs.filter(x => !isGameVer(x))
    const final = withoutGame.length > 0 ? withoutGame : segs
    return final.join('-') || raw
  }

  $: versionOptions = (() => {
    const mcVer = profile.mcVersion ?? ''
    const opts = modVersions.map(v => ({
      value: v.id,
      label: cleanVersionLabel(v.version_number, mcVer, v.version_type),
      raw: v.version_number,
      tag: v.version_type === 'beta' || v.version_type === 'alpha' ? v.version_type : '',
    }))
    const counts = {}
    for (const o of opts) counts[o.label] = (counts[o.label] ?? 0) + 1
    return opts.map(o => ({
      value: o.value,
      label: counts[o.label] > 1 ? o.raw : o.label,
      tag: o.tag,
    }))
  })()

  // Vanilla runs no loader, so only datapacks make sense there.
  $: modsAllowed = loader !== 'vanilla'

  $: rightZones = buildRightZones(selectedMod, versionOptions, mcInstalled, modsAllowed)

  function buildRightZones(mod, versions, hasMC, allowMods) {
    const z = ['search']
    if (hasMC) z.push('f-installed')
    if (allowMods) z.push('f-mods')
    z.push('f-datapacks', 'f-resourcepacks', 'sort', 'pager')
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

  let worldCount = -1  // -1 = unknown, hide world hints until loaded

  // Datapack manager mod (Global Packs) status for the hint slot.
  let managerStatus = null
  function refreshManagerStatus() {
    GetDatapackManagerStatus(profile.id, loader, profile.mcVersion ?? '')
      .then(s => { managerStatus = s })
      .catch(() => {})
  }

  onMount(async () => {
    if (!modsAllowed) {
      filterMods      = false
      filterDatapacks = true
    }
    CountWorlds(profile.id).then(v => { worldCount = v }).catch(() => {})
    refreshManagerStatus()
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
    syncOsk(false)
  })

  async function doSearch(resetPage = false) {
    if (resetPage) page = 0
    listFading = true
    const params = {
      query,
      mcVersion: profile.mcVersion ?? '',
      loader,
      sortBy,
      offset:    page * PAGE_SIZE,
      filterMods,
      filterDatapacks,
      filterResourcepacks,
    }
    console.log('[mods] doSearch', params)
    try {
      const [res] = await Promise.all([
        SearchMods(
          params.query, params.mcVersion, params.loader,
          params.sortBy, params.offset,
          params.filterMods, params.filterDatapacks, params.filterResourcepacks,
        ).catch(err => { console.error('[mods] SearchMods error:', err); return { hits: [], total_hits: 0 } }),
        new Promise(r => setTimeout(r, 500)),
      ])
      console.log('[mods] raw res keys:', res ? Object.keys(res) : 'null', 'res:', res)
      console.log('[mods] results:', res?.hits?.length, 'total:', res?.total_hits)
      results   = res.hits       ?? []
      totalHits = res.total_hits ?? 0
      prefetchBundles(results)
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
      const effectiveType = mod.project_type === 'resourcepack'
        ? 'resourcepack'
        : mod.categories?.includes('datapack') ? 'datapack' : (mod.project_type ?? 'mod')
      const versions = await GetModVersions(
        mod.project_id,
        profile.mcVersion ?? '',
        effectiveType,
        loader,
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
    if (!mcInstalled || !selectedMod || !selectedVersionId || installing) return
    const ver  = modVersions.find(v => v.id === selectedVersionId)
    if (!ver) return
    const file = ver.files.find(f => f.primary) ?? ver.files[0]
    if (!file) return
    installing   = true
    installError = ''
    try {
      const effectiveType = selectedMod.project_type === 'resourcepack'
        ? 'resourcepack'
        : selectedMod.categories?.includes('datapack') ? 'datapack' : (selectedMod.project_type ?? 'mod')
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
        loader,
        profile.mcVersion ?? '',
      )
      installedMods = await ListMods(profile.id)
      refreshManagerStatus()
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
      refreshManagerStatus()
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
    // While Steam's keyboard is up, gamepad buttons arrive as our own
    // synthetic keys and would act underneath it (A used to dismiss the
    // keyboard by committing the search). Real typing from the OSK is
    // trusted events; only B/Escape passes through as the explicit
    // cancel, which also recovers if the OSK was closed by touch.
    if (oskShown && !e.isTrusted && e.key !== 'Escape') return
    // An open dropdown owns the keys: don't consume them here or the
    // dropdown's own consumeKey would reject them as duplicates.
    if (document.querySelector('.wrap.open')) return
    if (!consumeKey(e)) return

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

    // M / gamepad Y toggles the mods screen closed, mirroring how it opens.
    if (e.code === 'KeyM' || e.key === 'm' || e.key === 'M' || e.key === 'ь' || e.key === 'Ь') {
      e.preventDefault(); e.stopPropagation()
      onClose()
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

  // Steam's on-screen keyboard follows the search field's active state,
  // but only for controller and touch input: a mouse or a physical
  // keyboard means the user can already type.
  let oskShown = false
  function syncOsk(open) {
    if (open === oskShown) return
    oskShown = open
    // gamescope double-routes the OSK's trackpad pointers into the app;
    // locking the input mode keeps those moves from flipping us to
    // keyboard mode (which would unhide the cursor mid-typing).
    setInputModeLock(open)
    // The field's rect in physical pixels lets Steam anchor the
    // floating keyboard to it, mirroring SDL's invocation.
    const r = searchInputEl?.getBoundingClientRect()
    const s = window.devicePixelRatio || 1
    SetOnScreenKeyboard(
      open,
      Math.round((r?.x ?? 0) * s),
      Math.round((r?.y ?? 0) * s),
      Math.round((r?.width ?? 0) * s),
      Math.round((r?.height ?? 0) * s)
    ).catch(() => {})
  }

  $: if (!searchActive && oskShown) syncOsk(false)

  function handleRightKey(e) {
    if (focusZone === 'search') {
      if (e.key === 'Enter') {
        e.preventDefault(); e.stopPropagation()
        // The input is usually already focused (readonly) from zone
        // navigation, and a no-op focus() would not re-activate the IM.
        // Blur first (before searchActive, whose on:blur reset would
        // undo it), then focus the now-editable input fresh: that
        // raises the caret and, under gamescope, the on-screen keyboard.
        searchInputEl?.blur()
        searchActive = true
        tick().then(() => searchInputEl?.focus())
        if (getInputMode() !== 'keyboard') syncOsk(true)
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
      else if (zone === 'f-resourcepacks') fResourcepacksEl?.focus()
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
    else if (zone === 'f-resourcepacks') { filterResourcepacks = !filterResourcepacks; doSearch(true) }
    else if (zone === 'sort')         sortSelRef?.openMenu()
    else if (zone === 'version')      versionSelRef?.openMenu()
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

  // Newest three release game versions of a search hit, newest rightmost.
  // Snapshots and pre-releases are filtered out. Modrinth orders the list
  // by when support was added, not by version, so sort numerically.
  function recentVersions(mod) {
    const releases = (mod.versions ?? []).filter(v => /^\d+\.\d+(\.\d+)?$/.test(v))
    releases.sort((a, b) => {
      const pa = a.split('.').map(Number)
      const pb = b.split('.').map(Number)
      return (pa[0] - pb[0]) || ((pa[1] ?? 0) - (pb[1] ?? 0)) || ((pa[2] ?? 0) - (pb[2] ?? 0))
    })
    return releases.slice(-3)
  }

  // 16-hue palette indexed by the version components with coprime
  // weights: the same game version carries the same colour on every
  // row, while patch (+1) and minor (+13) steps land far apart on the
  // wheel, so versions that appear together stay visually distinct.
  const VER_HUES = [0, 22, 45, 67, 90, 112, 135, 157, 180, 202, 225, 247, 270, 292, 315, 337]

  function verStyle(v) {
    const [a = 0, b = 0, c = 0] = v.split('.').map(Number)
    const hue = VER_HUES[(a * 7 + b * 13 + c) % VER_HUES.length]
    return `color: hsl(${hue}, 55%, 72%); background: hsla(${hue}, 55%, 55%, 0.15)`
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
      <div class="mod-list" class:fading={listFading} class:empty={displayList.length === 0} bind:this={listEl}>
        {#if displayList.length === 0}
          <div class="list-hint">
            {#if listFading}
              <svg class="list-spinner" viewBox="0 0 40 40" aria-hidden="true">
                <circle cx="20" cy="20" r="16" />
              </svg>
            {:else}
              No results
            {/if}
          </div>
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
                {#if mod.project_type === 'resourcepack'}
                  <span class="badge badge-rp">Resources</span>
                {:else if mod.categories?.includes('datapack')}
                  {#if mod.resource_packs?.length}
                    <span class="badge badge-dp badge-mix"><span class="badge-mix-text">Datapack + Resources</span></span>
                  {:else}
                    <span class="badge badge-dp">Datapack</span>
                  {/if}
                {:else}
                  <span class="badge badge-mod">Mod</span>
                {/if}
                {#if installed}
                  <span class="badge badge-ok">Installed</span>
                {/if}
                {#if recentVersions(mod).length > 0}
                  <span class="mod-vers">
                    {#each recentVersions(mod) as v}
                      <span class="ver-badge" style={verStyle(v)}>{v}</span>
                    {/each}
                  </span>
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
          on:mousedown={() => {
            searchActive = true; focusCol = 'right'; focusZone = 'search'
            if (getInputMode() === 'touch') syncOsk(true)
          }}
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
          disabled={!mcInstalled}
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
          disabled={!modsAllowed}
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
        <button
          bind:this={fResourcepacksEl}
          class="toggle-row"
          class:zone-focused={focusZone === 'f-resourcepacks' && focusCol === 'right'}
          on:click={() => { filterResourcepacks = !filterResourcepacks; doSearch(true) }}
          on:focus={() => { focusCol = 'right'; focusZone = 'f-resourcepacks' }}
          tabindex="-1"
        >
          <span class="checkbox" class:checked={filterResourcepacks} />
          Resource packs
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
          >
            <svg width="10" height="10" viewBox="0 0 10 10" fill="none">
              <path d="M6.5 1L2.5 5l4 4" stroke="currentColor" stroke-width="1.8" stroke-linecap="square"/>
            </svg>
          </button>
          <span class="pg-info">{page + 1} / {totalPages}</span>
          <button
            class="pg-arrow"
            disabled={page + 1 >= totalPages}
            on:click={goNext}
            tabindex="-1"
          >
            <svg width="10" height="10" viewBox="0 0 10 10" fill="none">
              <path d="M3.5 1l4 4-4 4" stroke="currentColor" stroke-width="1.8" stroke-linecap="square"/>
            </svg>
          </button>
        </div>
      </div>

      <div class="hint-slot">
        {#if !mcInstalled}
          <div class="mc-hint">Install Minecraft in this profile to add mods.</div>
        {:else if selectedMod?.categories?.includes('datapack')}
          {#if managerStatus?.installed}
            <div class="mc-hint">Your datapacks are managed by {managerStatus.name}.</div>
          {:else if managerStatus?.available}
            <div class="mc-hint">Management mod available: {managerStatus.name}. It installs with your next datapack.</div>
          {:else if worldCount === 0}
            <div class="mc-hint">No worlds yet. Create one, then restart the game again to apply datapacks.</div>
          {:else if worldCount > 0}
            <div class="mc-hint">Datapacks apply to existing worlds only. Restart the game again to apply them to new ones.</div>
          {/if}
        {:else if selectedMod?.project_type === 'resourcepack'}
          {#if managerStatus?.installed}
            <div class="mc-hint">Your resource packs are managed by {managerStatus.name}.</div>
          {:else if managerStatus?.available}
            <div class="mc-hint">Management mod available: {managerStatus.name}. It installs with your next pack.</div>
          {:else}
            <div class="mc-hint">Resource packs are enabled automatically for this profile.</div>
          {/if}
        {/if}
      </div>

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
        disabled={!mcInstalled || !selectedMod || installing || (!installedSet.has(selectedMod?.project_id ?? '') && (!selectedVersionId || !versionsCompatible))}
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
  /* Stops above the footer so the nav bar stays visible with a
     context-appropriate action list. */
  .mods-screen {
    position: absolute;
    inset: 0 0 2.44rem 0;
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
    /* Viewport minus outer padding minus the footer the screen no
       longer covers. */
    height: calc(100vh - 2rem - 2.44rem);
  }

  /* ── Left column ── */
  .col-left {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    gap: 0;
  }

  /* Grid with a shared badges track: every row's badge column is sized
     by the widest one on the page (via subgrid), so type badges and
     version chips line up across rows. */
  .mod-list {
    flex: 1;
    overflow-y: auto;
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-content: start;
    gap: 0.11rem;
    scrollbar-width: thin;
    scrollbar-color: rgba(255,255,255,0.12) transparent;
    transition: opacity 0.45s ease;
  }
  .mod-list.fading {
    opacity: 0.45;
    pointer-events: none;
  }
  /* Keep the loading spinner fully visible while the list fades. */
  .mod-list.fading.empty {
    opacity: 1;
  }

  /* Empty state (no results / loading) centers in the list area. */
  .mod-list.empty {
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .list-hint {
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--text-sub);
    font-size: 0.78rem;
  }

  /* Mini version of the launcher's startup spinner. */
  .list-spinner {
    width: 2rem;
    height: 2rem;
    animation: list-spin 1s linear infinite;
  }
  .list-spinner circle {
    fill: none;
    stroke: var(--accent);
    stroke-width: 3;
    stroke-linecap: round;
    stroke-dasharray: 60 40;
  }

  @keyframes list-spin {
    to { transform: rotate(360deg); }
  }

  /* Mod row */
  .mod-row {
    display: grid;
    grid-column: 1 / -1;
    grid-template-columns: subgrid;
    align-items: center;
    gap: 0.67rem;
    padding: 0.5rem 0.78rem;
    background: var(--card);
    cursor: pointer;
    transition: background var(--t);
    /* Tall enough for name+description on the left and a three-row badge
       stack (type, Installed, versions) on the right. */
    min-height: 4.22rem;
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
    /* Type badge stretches to match the versions row below it. */
    align-items: stretch;
  }

  .mod-vers {
    display: flex;
    gap: 0.17rem;
    white-space: nowrap;
    user-select: none;
  }

  /* Chips grow from their natural width to fill the shared column, so
     the row spans the same width as the type badge above it. */
  .ver-badge {
    flex-grow: 1;
    text-align: center;
    padding: 0.11rem 0.28rem;
    border-radius: 2px;
    font-size: 0.5rem;
    font-weight: 700;
    letter-spacing: 0.03em;
  }

  .badge {
    font-size: 0.5rem;
    font-weight: 700;
    padding: 0.11rem 0.56rem;
    min-width: 4.2rem;
    text-align: center;
    box-sizing: border-box;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    white-space: nowrap;
  }

  .badge-mod { color: #8bc4e8; background: rgba(139,196,232,0.15); }
  .badge-dp  { color: #8be8a0; background: rgba(139,232,160,0.15); }
  .badge-rp  { color: #e8c98b; background: rgba(232,201,139,0.15); }

  /* Combined datapack-with-textures badge: same metrics as the other
     badges, with the pill and label blending green into amber. */
  .badge-mix {
    background: linear-gradient(90deg, rgba(139,232,160,0.15), rgba(232,201,139,0.18));
  }
  .badge-mix-text {
    background: linear-gradient(90deg, #8be8a0 25%, #e8c98b 75%);
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
    color: transparent;
  }
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
  /* Dense vertical rhythm: the column carries many controls and must
     leave room for the hint slot on small (Deck) screens. */
  .col-right {
    width: 18rem;
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    gap: 0.44rem;
  }

  .search-row {
    display: flex;
    align-items: center;
    gap: 0.56rem;
    background: var(--card);
    padding: 0 0.78rem;
    height: 2rem;
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
    gap: 0.11rem;
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
    height: 1.56rem;
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
  .toggle-row:not(:disabled):hover,
  .toggle-row.zone-focused { background: var(--card-btn-hover); color: var(--text); outline: none; }
  .toggle-row.zone-focused  { box-shadow: inset 0 0 0 2px var(--accent); }
  .toggle-row:disabled { opacity: 0.4; cursor: default; }

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
    position: relative;
    display: flex;
    align-items: center;
    background: var(--card);
    outline: none;
  }
  /* Focus ring drawn as an overlay so button hover backgrounds can't
     paint over it. */
  .pager::after {
    content: '';
    position: absolute;
    inset: 0;
    box-shadow: inset 0 0 0 2px var(--accent);
    opacity: 0;
    transition: opacity var(--t);
    pointer-events: none;
  }
  .pager.zone-focused::after { opacity: 1; }

  .pg-arrow {
    width: 1.89rem;
    height: 1.56rem;
    background: transparent;
    color: var(--text-sub);
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

  /* Flexible slot between the pager and the version row: takes all the
     free height, centers the notice, and its size drives the notice's
     font scaling via container query units. */
  .hint-slot {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    justify-content: center;
    container-type: size;
  }

  /* SteamOS-style notice: accent quote bar on a subtle accent tint.
     Font shrinks with the slot when vertical space runs out. */
  .mc-hint {
    padding: 0.44rem 0.78rem;
    font-size: clamp(0.5rem, 15cqh, 0.67rem);
    line-height: 1.5;
    color: var(--text);
    background: rgba(30, 143, 255, 0.08);
    border-left: 3px solid var(--accent);
    overflow: hidden;
    user-select: none;
  }

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

  /* Back is secondary: kept compact so the hint slot above gets the
     vertical room for its notice text. */
  .back-btn {
    height: 1.78rem;
    font-size: 0.67rem;
    gap: 0.33rem;
    background: var(--card);
    color: var(--text-sub);
  }
  .back-btn :global(svg) { width: 0.61rem; height: 0.61rem; }
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
