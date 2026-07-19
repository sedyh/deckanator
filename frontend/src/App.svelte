<script>
  import { onMount, onDestroy, tick } from 'svelte'
  import { EventsOn, ClipboardSetText } from '../wailsjs/runtime/runtime.js'
  import {
    GetProfiles, CreateProfile, SaveProfile, DeleteProfile, GetIcons,
    GetVanillaVersions, GetLoaderVersions, GetLoaderGameVersions,
    IsInstalled, Install, Launch, CleanGameData, GetVersion, AnalyzeCrash,
    InstalledLoaderVersion, GetLauncherLog, StopGame
  } from '../wailsjs/go/internal/App.js'

  import Carousel       from './components/Carousel.svelte'
  import VersionSelector from './components/VersionSelector.svelte'
  import ActionButton   from './components/ActionButton.svelte'
  import ModsScreen     from './components/ModsScreen.svelte'
  import { fade } from 'svelte/transition'
  import { GlyphA, GlyphB, GlyphY, GlyphDPadH, GlyphDPadV, IconPlus } from './lib/icons.js'
  import { setupActions } from './lib/actions.js'
  import { destroy as destroyInput, consumeKey, getInputMode, onInputModeChange } from './lib/input.js'

  let profiles        = []
  let icons           = []
  let selectedIndex   = 0

  let carouselRef
  let carouselMode  = 'nav'
  let newProfileBtnEl
  let versionSelRef
  let actionBtnRef

  let panelIdx          = -1  // -1 = carousel, 0=new-profile, 1=loader, 2=mc, 3=loader-version, 4=java, 5=run
  let lastFocus         = { mode: 'none', idx: -1 }  // { mode: 'action'|'panel'|'none', idx }
  let suppressBlur      = false
  let carouselActionIdx = 0

  let modsOpen = false

  const FABRIC_LIKE = ['fabric', 'quilt']

  let loader              = 'vanilla'
  let mcVersions          = []
  let loaderGameSets      = {}  // loader -> Set of supported game versions
  let fabricVersions      = []  // loader versions of the current fabric-like loader
  let selectedMC          = ''
  let selectedFabric      = ''  // selected loader version
  let selectedJava        = ''

  $: isFabricLike = FABRIC_LIKE.includes(loader)

  function filterMC(list, l, sets) {
    const set = FABRIC_LIKE.includes(l) ? sets[l] : null
    return set && set.size > 0 ? list.filter(v => set.has(v.id)) : list
  }

  $: filteredMCVersions = filterMC(mcVersions, loader, loaderGameSets)

  async function ensureGameSet(l) {
    if (!FABRIC_LIKE.includes(l) || loaderGameSets[l]) return
    try {
      const gv = await GetLoaderGameVersions(l)
      loaderGameSets = { ...loaderGameSets, [l]: new Set(gv) }
    } catch {
      loaderGameSets = { ...loaderGameSets, [l]: new Set() }
    }
  }

  let appReady            = false
  let appVersion          = ''

  let installed           = false
  let installing          = false
  let launching           = false
  let checkingInstall     = false
  let progress            = { stage: '', current: 0, total: 100 }
  let activeInstallId     = ''
  let savedProgress       = { stage: '', current: 0, total: 100 }
  let error               = ''
  let installedMap        = {}

  $: installPct = activeInstallId
    ? (savedProgress.total > 0 ? Math.round(savedProgress.current * 100 / savedProgress.total) : 0)
    : -1

  // Rule-based crash summaries: match the whole error text against known
  // Java failure patterns; fall back to the root-cause exception line.
  const ERROR_HINTS = [
    [/UnsupportedClassVersionError/, 'Java is too old for this loader or mod'],
    [/Unsupported class file (major )?version/, 'The loader version is too old for this Minecraft version'],
    [/OutOfMemoryError/, 'The game ran out of memory'],
    [/NoClassDefFoundError|ClassNotFoundException/, 'A required class is missing: incompatible loader or missing dependency'],
    [/NoSuchMethodError|NoSuchFieldError|IncompatibleClassChangeError|AbstractMethodError/, 'Incompatible mod or loader version'],
    [/DuplicateModsFound|duplicate mods/i, 'Two copies of the same mod are installed'],
    [/requires (any )?version|depends on|is missing|Unmet dependency|Dependency/i, 'A mod dependency problem'],
    [/AccessDeniedException|Permission denied/i, 'File permission problem'],
    [/UnknownHostException|Connection refused|SocketTimeout/i, 'Network problem during startup'],
    [/GLFW|EGL error|OpenGL|libGL/i, 'Graphics initialization failed'],
  ]

  function rootCause(text) {
    const lines = text.split('\n')
    let cause = ''
    for (const raw of lines) {
      const l = raw.trim()
      if (l.startsWith('at ')) continue
      const m = l.match(/^(?:Exception in thread "[^"]*" )?(Caused by: )?((?:[\w$]+\.)+([\w$]+(?:Exception|Error|Throwable)))(?::\s*(.*))?$/)
      if (!m) continue
      const short = m[3] + (m[4] ? ': ' + m[4] : '')
      if (m[1] || !cause) cause = short
    }
    return cause
  }

  function summarizeError(text) {
    if (!text) return ''
    const cause = rootCause(text)
    for (const [re, hint] of ERROR_HINTS) {
      if (re.test(text)) return hint
    }
    if (cause) return cause.length > 120 ? cause.slice(0, 117) + '…' : cause
    return text.split('\n')[0]?.slice(0, 120) ?? 'Unknown error'
  }

  // Display copy: capitalized first letter (error strings are lowercase
  // by Go convention).
  $: errorDisplay = error ? error[0].toUpperCase() + error.slice(1) : ''

  // Pin the trace to its end: the deepest "Caused by" is the root cause.
  // Guarded per error value: tick() inside a reactive block re-triggers
  // the flush in native WebKit, which would loop this block forever.
  let errorBodyEl
  let _scrolledFor = ''
  $: if (errorDisplay && errorDisplay !== _scrolledFor && errorBodyEl) {
    _scrolledFor = errorDisplay
    tick().then(() => {
      if (errorBodyEl) errorBodyEl.scrollTop = errorBodyEl.scrollHeight
    })
  }

  let errorCopied = false
  async function copyError() {
    // The panel shows a condensed trace; copy the full launcher log.
    let text = error
    try {
      const full = await GetLauncherLog()
      if (full) text = full
    } catch {}
    try { await navigator.clipboard.writeText(text) }
    catch { try { ClipboardSetText(text) } catch {} }
    errorCopied = true
    setTimeout(() => { errorCopied = false }, 1500)
  }

  // Online analysis via mclo.gs (stateless /analyse: the log is not
  // stored or published server-side).
  let analysis      = null
  let analyzing     = false
  let analysisError = ''
  let _prevErrorText = ''
  $: if (error !== _prevErrorText) {
    _prevErrorText = error
    analysis = null
    analyzing = false
    analysisError = ''
  }

  async function analyzeError() {
    if (analyzing) return
    analyzing = true
    analysisError = ''
    try { analysis = await AnalyzeCrash(profile?.id ?? '') }
    catch (e) { analysis = null; analysisError = String(e) }
    analyzing = false
  }

  $: profile = profiles[selectedIndex] ?? null

  let _prevProfileId = ''
  $: if (profile && profile.id !== _prevProfileId) {
    _prevProfileId = profile.id
    installing = activeInstallId === profile.id
    installed  = installing ? false : (installedMap[profile.id] ?? false)
    progress   = installing ? savedProgress : { stage: '', current: 0, total: 100 }
    syncProfile(profile)
  }

  async function syncProfile(p) {
    loader = p.loader || 'vanilla'
    await ensureGameSet(loader)
    const mc  = p.mcVersion || filterMC(mcVersions, loader, loaderGameSets)[0]?.id || ''
    let fab = p.fabricLoaderVersion || ''
    if (!fab && FABRIC_LIKE.includes(loader) && mc) {
      // Saved loader version lost: recover it from the versions dir.
      try { fab = await InstalledLoaderVersion(loader, mc) } catch { fab = '' }
    }
    selectedMC = mc
    if (mc) await loadLoaderVersions(mc, fab)
    if (!installing && p.mcVersion) await checkInstalled()
  }

  setupActions()

  let inputMode = getInputMode()
  const unsubInputMode = onInputModeChange(m => { inputMode = m })
  onDestroy(() => { unsubInputMode(); destroyInput() })

  onMount(async () => {
    EventsOn('install:progress', d => { progress = d; savedProgress = d })

    GetVersion().then(v => { appVersion = v }).catch(() => {})

    icons    = await GetIcons()
    profiles = await GetProfiles()
    if (profiles.length === 0) {
      profiles = [await CreateProfile()]
    }

    await loadVersions()
    await checkAllInstalled()
    _prevProfileId = ''

    appReady = true
  })

  async function loadVersions() {
    error = ''
    try {
      mcVersions = await GetVanillaVersions()
      await ensureGameSet(loader)

      if (!selectedMC) {
        selectedMC = filterMC(mcVersions, loader, loaderGameSets)[0]?.id ?? ''
      }

      if (isFabricLike) await loadLoaderVersions(selectedMC)
      await checkInstalled()
    } catch (e) { error = String(e) }
  }

  async function loadLoaderVersions(mcVersion, preferred = '') {
    // Direct check instead of the reactive isFabricLike: this runs right
    // after `loader` was assigned, before Svelte re-derives it.
    if (!FABRIC_LIKE.includes(loader) || !mcVersion) {
      fabricVersions = []
      selectedFabric = ''
      return
    }
    let versions = []
    try { versions = await GetLoaderVersions(loader, mcVersion) } catch { versions = [] }
    fabricVersions = versions
    // The profile's saved version always wins: it's what is actually on
    // disk, even if the meta list is unavailable or no longer lists it.
    selectedFabric = preferred || versions[0]?.version || ''
  }

  async function checkInstalled() {
    if (!selectedMC) return
    if (!profile?.mcVersion) {
      installed = false
      if (profile) installedMap = { ...installedMap, [profile.id]: false }
      return
    }
    checkingInstall = true
    installed = await IsInstalled(loader, selectedMC, isFabricLike ? selectedFabric : '')
    if (profile) installedMap = { ...installedMap, [profile.id]: installed }
    checkingInstall = false
  }

  async function checkAllInstalled() {
    const results = await Promise.all(profiles.map(async p => {
      if (!p.mcVersion) return [p.id, false]
      const ok = await IsInstalled(p.loader, p.mcVersion, p.loader === 'fabric' ? p.fabricLoaderVersion : '')
      return [p.id, ok]
    }))
    installedMap = Object.fromEntries(results)
  }

  async function onVersionChange(e) {
    const field = e?.detail?.field
    if (field === 'loader') {
      await ensureGameSet(loader)
      const list = filterMC(mcVersions, loader, loaderGameSets)
      if (!list.find(v => v.id === selectedMC)) selectedMC = list[0]?.id ?? ''
      await loadLoaderVersions(selectedMC)
    } else if (field === 'mc' && isFabricLike) {
      await loadLoaderVersions(selectedMC)
    }
    await checkInstalled()
  }

  async function handleInstall() {
    if (!profile) return
    const saved = {
      ...profile,
      loader,
      mcVersion: selectedMC,
      fabricLoaderVersion: isFabricLike ? selectedFabric : ''
    }
    await SaveProfile(saved)
    profiles = profiles.map(p => p.id === saved.id ? saved : p)

    const installId = profile.id
    activeInstallId = installId
    error      = ''
    installing = true
    progress   = { stage: 'Preparing...', current: 0, total: 100 }
    savedProgress = progress
    try {
      await Install(loader, selectedMC, isFabricLike ? selectedFabric : '', selectedJava)
      if (profile?.id === installId) installed = true
      await checkAllInstalled()
    } catch (e) {
      if (profile?.id === installId) error = String(e)
    } finally {
      if (activeInstallId === installId) {
        installing = false
        activeInstallId = ''
        savedProgress = { stage: '', current: 0, total: 100 }
      }
    }
  }

  let stopRequested = false

  async function handleStop() {
    stopRequested = true
    try { await StopGame() } catch {}
  }

  async function handleLaunch() {
    if (!profile) return
    error = ''
    launching = true
    await SaveProfile({
      ...profile,
      loader,
      mcVersion: selectedMC,
      fabricLoaderVersion: isFabricLike ? selectedFabric : ''
    })
    try { await Launch(profile.id) }
    catch (e) { if (!stopRequested) error = String(e) }
    finally { launching = false; stopRequested = false }
  }

  async function handleCreate() {
    const p = await CreateProfile()
    profiles      = [...profiles, p]
    selectedIndex = profiles.length - 1
    installedMap  = { ...installedMap, [p.id]: false }
    carouselMode  = 'nav'
  }

  async function handleDelete(e) {
    await DeleteProfile(e.detail)
    profiles = profiles.filter(p => p.id !== e.detail)
    if (selectedIndex >= profiles.length) selectedIndex = Math.max(0, profiles.length - 1)
    installed = false

    if (profiles.length === 0) {
      await CleanGameData()
      const mc = filteredMCVersions[0]?.id ?? ''
      selectedMC = mc
      if (mc) await loadLoaderVersions(mc)
    } else {
      _prevProfileId = ''
    }
  }

  async function handleSave(e) {
    await SaveProfile(e.detail)
    profiles = profiles.map(p => p.id === e.detail.id ? e.detail : p)
  }

  $: locked = installed || installing

  $: focusableItems = buildFocusableItems(locked, loader, !!profile)

  $: inActionMode = carouselMode === 'action'

  $: if (inActionMode) lastFocus = { mode: 'action', idx: carouselActionIdx }

  $: if (focusableItems && panelIdx >= 0) {
    const item = focusableItems.find(i => i.idx === panelIdx)
    if (item) {
      item.focus()
    } else {
      const last = focusableItems[focusableItems.length - 1]
      panelIdx = last?.idx ?? -1
      last?.focus()
    }
  }

  function buildFocusableItems(locked, loader, hasProfile) {
    const items = [{ idx: 0, focus: () => newProfileBtnEl?.focus() }]
    // Without a profile the selectors and the action button are disabled
    // (and unfocusable), so navigation only offers New Profile.
    if (!hasProfile) return items
    if (!locked) {
      items.push({ idx: 1, focus: () => versionSelRef?.focusLoader() })
      items.push({ idx: 2, focus: () => versionSelRef?.focusMC() })
      if (FABRIC_LIKE.includes(loader)) items.push({ idx: 3, focus: () => versionSelRef?.focusFabric() })
      items.push({ idx: 4, focus: () => versionSelRef?.focusJava() })
    }
    items.push({ idx: 5, focus: () => actionBtnRef?.focus() })
    return items
  }

  function navigatePanelBy(delta) {
    const pos = focusableItems.findIndex(i => i.idx === panelIdx)
    const next = pos + delta
    if (next < 0) {
      panelIdx = -1
      carouselRef?.enterAction()
      return
    }
    if (next >= focusableItems.length) return
    panelIdx = focusableItems[next].idx
    lastFocus = { mode: 'panel', idx: panelIdx }
    focusableItems[next].focus()
  }

  function handleEnterPanel() {
    if (focusableItems.length > 0) {
      panelIdx = focusableItems[0].idx
      lastFocus = { mode: 'panel', idx: panelIdx }
      focusableItems[0].focus()
    }
  }

  // Keep panelIdx in sync when focus lands in the panel by mouse (clicking
  // a select focuses its trigger programmatically), so keyboard navigation
  // continues from the clicked element instead of a stale position.
  function handlePanelFocusIn(e) {
    const t = e.target
    let idx = -1
    if (newProfileBtnEl && newProfileBtnEl.contains(t)) idx = 0
    else if (versionSelRef) {
      const field = versionSelRef.fieldOfNode(t)
      if (field === 'loader') idx = 1
      else if (field === 'mc') idx = 2
      else if (field === 'fabric') idx = 3
      else if (field === 'java') idx = 4
    }
    if (idx === -1 && actionBtnRef?.containsNode(t)) idx = 5
    if (idx >= 0 && idx !== panelIdx) {
      panelIdx = idx
      lastFocus = { mode: 'panel', idx }
    }
  }

  function handleGlobalKey(e) {
    // Ownership checks come first: consumeKey must only run when this
    // handler will actually route the event, otherwise it poisons the
    // debounce map for the handler that owns it.
    if (modsOpen) return
    if (document.querySelector('.wrap.open')) return
    if (!consumeKey(e)) return

    if (e.code === 'KeyM' || e.key === 'm' || e.key === 'M' || e.key === 'ь' || e.key === 'Ь') {
      if (profile && !modsOpen && carouselMode !== 'edit') {
        e.preventDefault()
        modsOpen = true
        return
      }
    }

    // Edit mode: route special keys through Carousel. Letter keys and other
    // unhandled keys fall through the native input for normal typing.
    if (carouselMode === 'edit') {
      if (e.key === 'Enter')     { e.preventDefault(); carouselRef?.editCommit();    return }
      if (e.key === 'Escape')    { e.preventDefault(); carouselRef?.editCancel();    return }
      if (e.key === 'ArrowDown') { e.preventDefault(); carouselRef?.editNextField(); return }
      if (e.key === 'ArrowUp')   { e.preventDefault(); carouselRef?.editPrevField(); return }
      return
    }

    // Route action-mode navigation through Carousel methods, since synthetic
    // events dispatched to window don't bubble down to Carousel's element
    // keydown handler.
    if (inActionMode) {
      if (e.key === 'ArrowUp')   { e.preventDefault(); carouselRef?.actionUp(); return }
      if (e.key === 'ArrowDown') { e.preventDefault(); carouselRef?.actionDown(); return }
      if (e.key === 'Enter')     { e.preventDefault(); carouselRef?.actionConfirm(); return }
      if (e.key === 'Escape')    { e.preventDefault(); carouselRef?.actionCancel(); return }
    }

    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault()
      const keepAction = lastFocus.mode === 'action' || inActionMode
      if (e.key === 'ArrowLeft') carouselRef?.navigateLeft(keepAction)
      else carouselRef?.navigateRight(keepAction)
      if (!keepAction && lastFocus.mode === 'panel') {
        const restoreIdx = lastFocus.idx
        suppressBlur = true
        tick().then(() => {
          const item = focusableItems.find(i => i.idx === restoreIdx)
          if (item) { panelIdx = restoreIdx; item.focus() }
          suppressBlur = false
        })
      } else if (!keepAction && lastFocus.mode === 'none' && panelIdx === -1) {
        tick().then(() => carouselRef?.enterAction())
      }
      return
    }

    if (e.key === 'Enter' && carouselMode === 'nav' && profile && panelIdx === -1) {
      e.preventDefault()
      carouselRef?.enterAction()
      return
    }

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (panelIdx === -1) {
        if (lastFocus.mode === 'panel') {
          const item = focusableItems.find(i => i.idx === lastFocus.idx)
            ?? focusableItems[focusableItems.length - 1]
          if (item) { panelIdx = item.idx; item.focus() }
        } else if (lastFocus.mode === 'action' && profiles.length > 0) {
          carouselRef?.enterAction()
        } else {
          if (focusableItems.length > 0) { panelIdx = focusableItems[0].idx; focusableItems[0].focus() }
        }
      } else {
        navigatePanelBy(1)
      }
      return
    }

    if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (panelIdx >= 0) {
        navigatePanelBy(-1)
      } else if (lastFocus.mode === 'panel') {
        const item = focusableItems.find(i => i.idx === lastFocus.idx)
          ?? focusableItems[focusableItems.length - 1]
        if (item) { panelIdx = item.idx; item.focus() }
      } else if (profiles.length > 0) {
        carouselRef?.enterAction()
      } else {
        if (focusableItems.length > 0) { panelIdx = focusableItems[0].idx; focusableItems[0].focus() }
      }
      return
    }

    if (e.key === 'Escape' && panelIdx >= 0) {
      e.preventDefault()
      panelIdx = -1
      carouselRef?.focusCarousel()
    }
  }
</script>

<svelte:window on:keydown={handleGlobalKey} />

<div class="app">
  <div class="splash" class:splash-gone={appReady} aria-hidden="true">
    <svg class="splash-spinner" viewBox="0 0 40 40">
      <circle cx="20" cy="20" r="16" />
    </svg>
  </div>

  {#if modsOpen && profile}
    <ModsScreen {profile} {loader} mcInstalled={installed} onClose={() => {
      modsOpen = false
      tick().then(() => {
        if (lastFocus.mode === 'action') {
          carouselRef?.enterAction()
        } else {
          carouselRef?.focusCarousel()
        }
      })
    }} />
  {/if}
  <div class="content">
    <section class="carousel-section">
      <Carousel
        bind:this={carouselRef}
        bind:mode={carouselMode}
        bind:actionIdx={carouselActionIdx}
        {profiles}
        {icons}
        bind:selectedIndex
        checking={checkingInstall}
        on:mods={() => { if (profile) modsOpen = true }}
        {installPct}
        installProfileId={activeInstallId}
        {installedMap}
        on:create={handleCreate}
        on:delete={handleDelete}
        on:save={handleSave}
        on:enterPanel={handleEnterPanel}
      />
    </section>

    <div class="panel-row">
      <section class="panel" on:focusin={handlePanelFocusIn} on:focusout={(e) => {
        if (!suppressBlur && panelIdx >= 0 && !e.currentTarget.contains(/** @type {Node} */ (e.relatedTarget))) {
          lastFocus = { mode: 'panel', idx: panelIdx }
          panelIdx = -1
        }
      }}>
        <button
          bind:this={newProfileBtnEl}
          class="new-profile-btn"
          class:panel-focused={panelIdx === 0}
          on:click={handleCreate}
          tabindex="-1"
        >
          <span class="new-profile-icon">{@html IconPlus}</span>
          <span>New Profile</span>
        </button>

        <div class="spacer" />

        <VersionSelector
          bind:this={versionSelRef}
          bind:loader
          mcVersions={filteredMCVersions}
          bind:selectedMC
          {fabricVersions}
          bind:selectedFabric
          bind:selectedJava
          locked={installed || installing}
          disabled={!profile}
          on:change={onVersionChange}
        />

        <div class="spacer" />

        <ActionButton
          bind:this={actionBtnRef}
          {installed}
          {installing}
          {launching}
          {progress}
          disabled={!profile || !selectedMC || (isFabricLike && !selectedFabric) || (!!activeInstallId && !installing)}
          on:install={handleInstall}
          on:launch={handleLaunch}
          on:stop={handleStop}
        />
      </section>

      <div class="error-side" class:visible={!!error}>
        <div class="error-content">
          <div class="error-head">
            <span class="error-summary">{summarizeError(errorDisplay)}</span>
            <button class="error-copy" on:click={analyzeError} tabindex="-1" disabled={analyzing}>
              {analyzing ? '…' : 'Analyze'}
            </button>
            <button class="error-copy" class:copied={errorCopied} on:click={copyError} tabindex="-1">
              Copy
            </button>
          </div>
          {#if analysis || analysisError}
            <div class="error-analysis">
              {#if analysisError}
                <div class="an-none">Analysis failed: {analysisError}</div>
              {:else if analysis.problems?.length > 0}
                {#each analysis.problems as p}
                  <div class="an-problem">{p.message}</div>
                  {#each p.solutions ?? [] as s}
                    <div class="an-solution">&bull; {s.message}</div>
                  {/each}
                {/each}
              {:else}
                <div class="an-none">No known problems detected by mclo.gs</div>
              {/if}
            </div>
          {/if}
          <pre class="error-body" bind:this={errorBodyEl}>{errorDisplay}</pre>
        </div>
      </div>
    </div>
  </div>

  <footer class="footer">
    <div class="hints-left">
      {#if appVersion}
        <span class="app-version">{appVersion}</span>
      {/if}
      <div class="hint-swap">
        {#if inputMode !== 'touch'}
          {#key inputMode}
            <div class="hint-group" in:fade={{ duration: 180 }} out:fade={{ duration: 180 }}>
              <span class="hint">
                {#if inputMode === 'gamepad'}
                  <span class="glyph">{@html GlyphDPadH}</span>
                {:else}
                  <span class="keycap">←</span><span class="keycap">→</span>
                {/if}
                <span>Profiles</span>
              </span>
              <span class="hint">
                {#if inputMode === 'gamepad'}
                  <span class="glyph">{@html GlyphDPadV}</span>
                {:else}
                  <span class="keycap">↑</span><span class="keycap">↓</span>
                {/if}
                <span>Navigate</span>
              </span>
            </div>
          {/key}
        {/if}
      </div>
    </div>
    <div class="hints-right">
      <div class="hint-swap swap-right">
        {#if inputMode !== 'touch'}
          {#key inputMode}
            <div class="hint-group" in:fade={{ duration: 180 }} out:fade={{ duration: 180 }}>
              {#if profile && !modsOpen}
                <span class="hint">
                  {#if inputMode === 'gamepad'}
                    <span class="glyph">{@html GlyphY}</span>
                  {:else}
                    <span class="keycap">M</span>
                  {/if}
                  <span>Mods</span>
                </span>
              {/if}
              <span class="hint">
                {#if inputMode === 'gamepad'}
                  <span class="glyph">{@html GlyphA}</span>
                {:else}
                  <span class="keycap">Enter</span>
                {/if}
                <span>Select</span>
              </span>
              <span class="hint">
                {#if inputMode === 'gamepad'}
                  <span class="glyph">{@html GlyphB}</span>
                {:else}
                  <span class="keycap">Esc</span>
                {/if}
                <span>Back</span>
              </span>
            </div>
          {/key}
        {/if}
      </div>
    </div>
  </footer>
</div>

<style>
  .app {
    position: relative;
    width: 100vw;
    height: 100vh;
    display: flex;
    flex-direction: column;
    background: var(--bg);
  }

  .content {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.89rem;
    padding: 0.67rem 1.11rem 0;
    overflow: hidden;
  }

  .carousel-section {
    width: 100%;
    display: flex;
    justify-content: center;
    overflow: visible;
    isolation: isolate;
  }

  .panel-row {
    display: flex;
    align-items: stretch;
    gap: 0.75rem;
  }

  .panel {
    width: 16rem;
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    gap: 0;
  }

  .error-side {
    position: relative;
    width: 0;
    overflow: hidden;
    transition: width 300ms cubic-bezier(.25,.46,.45,.94);
    flex-shrink: 0;
  }
  .error-side.visible {
    width: 16rem;
  }

  /* Absolutely positioned so the error panel never grows the row: its
     height is dictated by the settings panel, and the trace body (the
     least important part) is clipped and scrolls inside. */
  .error-content {
    position: absolute;
    top: 0;
    left: 0;
    bottom: 0;
    width: 16rem;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    background: rgba(215, 95, 95, 0.07);
    border: 1px solid rgba(215, 95, 95, 0.4);
    opacity: 0;
    transform: translateX(0.5rem);
    transition: opacity 200ms ease 150ms, transform 200ms ease 150ms;
  }
  .error-side.visible .error-content {
    opacity: 1;
    transform: translateX(0);
  }

  .error-head {
    display: flex;
    align-items: flex-start;
    gap: 0.44rem;
    padding: 0.44rem 0.56rem;
    border-bottom: 1px solid rgba(215, 95, 95, 0.25);
    flex-shrink: 0;
  }

  .error-summary {
    flex: 1;
    font-size: 0.61rem;
    font-weight: 700;
    line-height: 1.4;
    color: var(--red);
    word-break: break-word;
  }

  .error-copy {
    flex-shrink: 0;
    padding: 0.17rem 0.44rem;
    font-size: 0.56rem;
    font-weight: 700;
    color: var(--red);
    background: rgba(215, 95, 95, 0.15);
    border-radius: 2px;
    cursor: pointer;
    transition: background var(--t);
  }
  .error-copy:hover:not(:disabled) { background: rgba(215, 95, 95, 0.3); }
  .error-copy:disabled { opacity: 0.6; cursor: default; }
  .error-copy.copied {
    color: #8be8a0;
    background: rgba(139, 232, 160, 0.15);
  }

  .error-analysis {
    flex-shrink: 0;
    max-height: 45%;
    overflow-y: auto;
    padding: 0.44rem 0.56rem;
    border-bottom: 1px solid rgba(215, 95, 95, 0.25);
    font-size: 0.56rem;
    line-height: 1.5;
    scrollbar-width: thin;
  }
  .an-problem {
    font-weight: 700;
    color: var(--red);
    word-break: break-word;
  }
  .an-problem:not(:first-child) { margin-top: 0.33rem; }
  .an-solution {
    color: var(--text);
    opacity: 0.9;
    word-break: break-word;
  }
  .an-none { color: var(--text-sub); }

  .error-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    margin: 0;
    padding: 0.44rem 0.56rem;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.5rem;
    line-height: 1.5;
    color: var(--red);
    white-space: pre-wrap;
    word-break: break-word;
    scrollbar-width: thin;
    scrollbar-color: rgba(215, 95, 95, 0.3) transparent;
  }

  .new-profile-btn {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 0.56rem;
    padding: 0.44rem 0.78rem;
    background: var(--card-btn);
    color: var(--text-sub);
    font-size: 0.72rem;
    font-weight: 400;
    transition: background var(--t), color var(--t), font-weight var(--t);
  }
  .new-profile-btn:hover,
  .new-profile-btn:focus,
  .new-profile-btn.panel-focused {
    background: var(--card-btn-hover);
    color: var(--text);
    font-weight: 700;
  }
  .new-profile-btn:focus {
    outline: none;
    box-shadow: inset 0 0 0 2px var(--accent);
  }
  .new-profile-icon {
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
    color: inherit;
  }
  .new-profile-icon :global(svg) { width: 0.78rem; height: 0.78rem; }

  .spacer {
    height: 0.89rem;
  }

  /* ── Splash ── */
  .splash {
    position: fixed;
    inset: 0;
    z-index: 999;
    background: var(--bg);
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 1;
    transition: opacity 350ms ease;
    pointer-events: all;
  }
  .splash-gone {
    opacity: 0;
    pointer-events: none;
  }

  .splash-spinner {
    width: 3rem;
    height: 3rem;
    animation: spin 1s linear infinite;
  }
  .splash-spinner circle {
    fill: none;
    stroke: var(--accent);
    stroke-width: 3;
    stroke-linecap: round;
    stroke-dasharray: 60 40;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  /* ── Footer ── */
  .footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 0.62rem;
    height: 2.44rem;
    background: #161920;
    flex-shrink: 0;
  }

  .hints-left { flex: 1; display: flex; align-items: center; gap: 1.11rem; }

  .hints-right {
    display: flex;
    align-items: center;
    gap: 1.11rem;
  }

  .hint {
    display: flex;
    align-items: center;
    gap: 0.39rem;
    font-size: 0.67rem;
    color: var(--text-sub);
  }

  /* Crossfade container: outgoing and incoming hint groups share one
     grid cell so they overlap during the transition and the bar never
     looks empty or shifts layout. */
  .hint-swap {
    display: grid;
    align-items: center;
  }
  .hint-swap > .hint-group {
    grid-area: 1 / 1;
    justify-self: start;
  }
  .hint-swap.swap-right > .hint-group {
    justify-self: end;
  }

  .hint-group {
    display: flex;
    align-items: center;
    gap: 1.11rem;
  }

  /* Filled white like the controller glyphs; label shows the footer
     colour through, matching the cut-out letters of A/B/Y. */
  .keycap {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 1.2rem;
    height: 1.2rem;
    padding: 0 0.28rem;
    background: #fff;
    border-radius: 3px;
    font-size: 0.61rem;
    font-weight: 900;
    color: #161920;
    user-select: none;
  }
  .keycap + .keycap { margin-left: 0.17rem; }

  /* Build badge styled after the SteamOS "STEAM" logo pill. */
  .app-version {
    padding: 0.22rem 0.61rem;
    background: #fff;
    border-radius: 999px;
    font-size: 0.56rem;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: #161920;
    user-select: none;
  }

  .glyph {
    display: inline-flex;
    align-items: center;
    height: 1.2rem;
    flex-shrink: 0;
  }
  .glyph :global(svg) {
    height: 1.2rem;
    width: auto;
  }
</style>
