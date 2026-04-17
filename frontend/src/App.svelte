<script>
  import { onMount, onDestroy, tick } from 'svelte'
  import { EventsOn } from '../wailsjs/runtime/runtime.js'
  import {
    GetProfiles, CreateProfile, SaveProfile, DeleteProfile, GetIcons,
    GetVanillaVersions, GetFabricLoaderVersions, GetFabricGameVersions,
    IsInstalled, Install, Launch, CleanGameData
  } from '../wailsjs/go/internal/App.js'

  import Carousel       from './components/Carousel.svelte'
  import VersionSelector from './components/VersionSelector.svelte'
  import ActionButton   from './components/ActionButton.svelte'
  import ModsScreen     from './components/ModsScreen.svelte'
  import { GlyphA, GlyphB, GlyphY, GlyphDPadH, GlyphDPadV, IconPlus } from './lib/icons.js'
  import { tryActivate, release } from './lib/gamepad.js'

  let profiles        = []
  let icons           = []
  let selectedIndex   = 0

  let carouselRef
  let carouselMode  = 'nav'
  let newProfileBtnEl
  let versionSelRef
  let actionBtnRef

  let panelIdx          = -1  // -1 = carousel, 0=new-profile, 1=mc, 2=fabric, 3=java, 4=run
  let lastFocus         = { mode: 'none', idx: -1 }  // { mode: 'action'|'panel'|'none', idx }
  let suppressBlur      = false
  let carouselActionIdx = 0

  let modsOpen = false

  const loader = 'fabric'
  let mcVersions          = []
  let fabricGameVersions  = new Set()
  let fabricVersions      = []
  let selectedMC          = ''
  let selectedFabric      = ''
  let selectedJava        = ''

  $: filteredMCVersions = fabricGameVersions.size > 0
    ? mcVersions.filter(v => fabricGameVersions.has(v.id))
    : mcVersions

  let appReady            = false

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
    const mc  = p.mcVersion           || filteredMCVersions[0]?.id  || ''
    const fab = p.fabricLoaderVersion || ''
    selectedMC = mc
    if (mc) await loadFabricVersions(mc, fab)
    if (!installing && p.mcVersion) await checkInstalled()
  }

  // ── Gamepad polling ──────────────────────────────────────────────────────
  const BUTTON_MAP = {
    0:  'Enter',
    1:  'Escape',
    12: 'ArrowUp',
    13: 'ArrowDown',
    14: 'ArrowLeft',
    15: 'ArrowRight',
  }
  const STICK_THRESHOLD = 0.5
  const POLL_INTERVAL_MS = 8

  // aggregate state across ALL gamepads to avoid duplicates from
  // multiple virtual controllers (Steam Deck reports physical + Steam Input)
  const btnState  = {}  // key → bool: any gamepad pressing this?
  const axisState = {}  // 'h'|'v' → current direction key or null

  let pollTimer    = null
  let gamepadCount = 0

  function fireKey(key) {
    if (!tryActivate(key)) return
    window.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
  }

  function pollGamepads() {
    const next = {}  // key → bool

    for (const gp of navigator.getGamepads()) {
      if (!gp) continue

      for (const [idx, key] of Object.entries(BUTTON_MAP)) {
        if (gp.buttons[idx]?.pressed) next[key] = true
      }

      // Y button (index 3) → open Mods
      if (gp.buttons[3]?.pressed) next['__y'] = true

      // analog stick — take highest magnitude across all gamepads
      const lx = gp.axes[0] ?? 0
      const ly = gp.axes[1] ?? 0
      if (Math.abs(lx) > STICK_THRESHOLD) {
        const k = lx < 0 ? 'ArrowLeft' : 'ArrowRight'
        if (!next['__haxis'] || Math.abs(lx) > (next['__hval'] ?? 0)) {
          next['__haxis'] = k; next['__hval'] = Math.abs(lx)
        }
      }
      if (Math.abs(ly) > STICK_THRESHOLD) {
        const k = ly < 0 ? 'ArrowUp' : 'ArrowDown'
        if (!next['__vaxis'] || Math.abs(ly) > (next['__vval'] ?? 0)) {
          next['__vaxis'] = k; next['__vval'] = Math.abs(ly)
        }
      }
    }

    // edge detection: fire once per press, release action when button is let go
    for (const [idx, key] of Object.entries(BUTTON_MAP)) {
      const was = btnState[key] ?? false
      const is  = next[key] ?? false
      if (is && !was) fireKey(key)
      if (!is && was) release(key)
      btnState[key] = is
    }

    // Y button
    const yWas = btnState['__y'] ?? false
    const yIs  = next['__y'] ?? false
    if (yIs && !yWas && profile && !modsOpen) modsOpen = true
    if (!yIs && yWas) release('__y')
    btnState['__y'] = yIs

    // analog stick (fire on direction change, not on every frame)
    const hKey = next['__haxis'] ?? null
    const vKey = next['__vaxis'] ?? null
    if (hKey !== axisState.h) { axisState.h = hKey; if (hKey) fireKey(hKey) }
    if (vKey !== axisState.v) { axisState.v = vKey; if (vKey) fireKey(vKey) }
  }

  function startPolling() {
    if (!pollTimer) pollTimer = setInterval(pollGamepads, POLL_INTERVAL_MS)
  }

  function stopPolling() {
    if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  }

  window.addEventListener('gamepadconnected', () => { gamepadCount++; startPolling() })
  window.addEventListener('gamepaddisconnected', () => {
    gamepadCount = Math.max(0, gamepadCount - 1)
    if (gamepadCount === 0) stopPolling()
  })

  onDestroy(stopPolling)

  // ─────────────────────────────────────────────────────────────────────────

  onMount(async () => {
    EventsOn('install:progress', d => { progress = d; savedProgress = d })

    icons    = await GetIcons()
    profiles = await GetProfiles()
    if (profiles.length === 0) {
      profiles = [await CreateProfile()]
    }

    await loadVersions()
    await checkAllInstalled()
    _prevProfileId = ''

    appReady = true
    startPolling()
  })

  async function loadVersions() {
    error = ''
    try {
      const [allMC, fgv] = await Promise.all([GetVanillaVersions(), GetFabricGameVersions()])
      mcVersions         = allMC
      fabricGameVersions = new Set(fgv)

      if (!selectedMC) {
        const list = loader === 'fabric' ? allMC.filter(v => fabricGameVersions.has(v.id)) : allMC
        selectedMC = list[0]?.id ?? ''
      }

      if (loader === 'fabric') await loadFabricVersions(selectedMC)
      await checkInstalled()
    } catch (e) { error = String(e) }
  }

  async function loadFabricVersions(mcVersion, preferred = '') {
    const versions = await GetFabricLoaderVersions(mcVersion)
    const target = preferred && versions.find(v => v.version === preferred)
      ? preferred
      : versions[0]?.version ?? ''
    fabricVersions = versions
    selectedFabric = target
  }

  async function checkInstalled() {
    if (!selectedMC) return
    if (!profile?.mcVersion) {
      installed = false
      if (profile) installedMap = { ...installedMap, [profile.id]: false }
      return
    }
    checkingInstall = true
    installed = await IsInstalled(loader, selectedMC, loader === 'fabric' ? selectedFabric : '')
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
    if (e?.detail?.field === 'mc' && loader === 'fabric') {
      await loadFabricVersions(selectedMC)
    }
    await checkInstalled()
  }

  async function handleInstall() {
    if (!profile) return
    const saved = {
      ...profile,
      loader,
      mcVersion: selectedMC,
      fabricLoaderVersion: loader === 'fabric' ? selectedFabric : ''
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
      await Install(loader, selectedMC, loader === 'fabric' ? selectedFabric : '', selectedJava)
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

  async function handleLaunch() {
    if (!profile) return
    error = ''
    launching = true
    await SaveProfile({
      ...profile,
      loader,
      mcVersion: selectedMC,
      fabricLoaderVersion: loader === 'fabric' ? selectedFabric : ''
    })
    try { await Launch(profile.id) }
    catch (e) { error = String(e) }
    finally { launching = false }
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
      if (mc) await loadFabricVersions(mc)
    } else {
      _prevProfileId = ''
    }
  }

  async function handleSave(e) {
    await SaveProfile(e.detail)
    profiles = profiles.map(p => p.id === e.detail.id ? e.detail : p)
  }

  $: locked = installed || installing

  $: focusableItems = buildFocusableItems(locked, loader)

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

  function buildFocusableItems(locked, loader) {
    const items = [{ idx: 0, focus: () => newProfileBtnEl?.focus() }]
    if (!locked) {
      items.push({ idx: 1, focus: () => versionSelRef?.focusMC() })
      if (loader === 'fabric') items.push({ idx: 2, focus: () => versionSelRef?.focusFabric() })
      items.push({ idx: 3, focus: () => versionSelRef?.focusJava() })
    }
    items.push({ idx: 4, focus: () => actionBtnRef?.focus() })
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

  function handleGlobalKey(e) {
    // Deduplicate: native events from Steam Input and our synthetic events
    // map to the same action - only the first activation wins
    if (!tryActivate(e.key)) return
    // For native events, release action on keyup (gamepad polling handles its own releases)
    if (e.isTrusted) {
      window.addEventListener('keyup', ev => { if (ev.key === e.key) release(e.key) }, { once: true })
    }
    if (modsOpen) { release(e.key); return }
    if (document.querySelector('.wrap.open')) return

    if (e.code === 'KeyM') {
      console.log('[app] m pressed profile:', profile?.id, 'modsOpen:', modsOpen, 'carouselMode:', carouselMode)
      if (profile && !modsOpen && carouselMode !== 'edit') {
        modsOpen = true
        return
      }
    }

    if (carouselMode === 'edit') return

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

    if (inActionMode) return

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
    <ModsScreen {profile} onClose={() => {
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
      <section class="panel" on:focusout={(e) => {
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
          {loader}
          mcVersions={filteredMCVersions}
          bind:selectedMC
          {fabricVersions}
          bind:selectedFabric
          bind:selectedJava
          locked={installed || installing}
          on:change={onVersionChange}
        />

        <div class="spacer" />

        <ActionButton
          bind:this={actionBtnRef}
          {installed}
          {installing}
          {launching}
          {progress}
          disabled={!profile || !selectedMC || (!!activeInstallId && !installing)}
          on:install={handleInstall}
          on:launch={handleLaunch}
        />
      </section>

      <div class="error-side" class:visible={!!error}>
        <div class="error-content">{error}</div>
      </div>
    </div>
  </div>

  <footer class="footer">
    <div class="hints-left">
      <span class="hint">
        <span class="glyph">{@html GlyphDPadH}</span>
        <span>Profiles</span>
      </span>
      <span class="hint">
        <span class="glyph">{@html GlyphDPadV}</span>
        <span>Navigate</span>
      </span>
    </div>
    <div class="hints-right">
      {#if profile && !modsOpen}
        <span class="hint">
          <span class="glyph">{@html GlyphY}</span>
          <span>Mods</span>
        </span>
      {/if}
      <span class="hint">
        <span class="glyph">{@html GlyphA}</span>
        <span>Select</span>
      </span>
      <span class="hint">
        <span class="glyph">{@html GlyphB}</span>
        <span>Back</span>
      </span>
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
    width: 0;
    overflow: hidden;
    transition: width 300ms cubic-bezier(.25,.46,.45,.94);
    flex-shrink: 0;
  }
  .error-side.visible {
    width: 16rem;
  }

  .error-content {
    width: 16rem;
    height: 100%;
    padding: 0.56rem 0.78rem;
    font-size: 0.67rem;
    color: var(--red);
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-word;
    background: rgba(215, 95, 95, 0.07);
    opacity: 0;
    transform: translateX(0.5rem);
    transition: opacity 200ms ease 150ms, transform 200ms ease 150ms;
  }
  .error-side.visible .error-content {
    opacity: 1;
    transform: translateX(0);
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
    padding: 0 1.56rem;
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
