<script>
  import { onMount, onDestroy, tick } from 'svelte'
  import { EventsOn, ClipboardSetText } from '../wailsjs/runtime/runtime.js'
  import {
    GetProfiles, CreateProfile, SaveProfile, DeleteProfile, GetIcons,
    GetVanillaVersions, GetLoaderVersions, GetLoaderGameVersions,
    IsInstalled, Install, Launch, CleanGameData, GetVersion, AnalyzeCrash, IsDeckDesktop,
    InstalledLoaderVersion, GetLauncherLog, StopGame, GetSettings, SaveSettings
  } from '../wailsjs/go/internal/App.js'

  import Carousel       from './components/Carousel.svelte'
  import VersionSelector from './components/VersionSelector.svelte'
  import ActionButton   from './components/ActionButton.svelte'
  import ModsScreen     from './components/ModsScreen.svelte'
  import SettingsPanel  from './components/SettingsPanel.svelte'
  import { fade } from 'svelte/transition'
  import { GlyphA, GlyphB, GlyphX, GlyphY, GlyphDPadH, GlyphDPadV, IconPlus } from './lib/icons.js'
  import { setupActions } from './lib/actions.js'
  import { startUpdateCheck } from './lib/update.js'
  import { destroy as destroyInput, consumeKey, getInputMode, onInputModeChange, getMirrorState, onMirrorState } from './lib/input.js'

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

  // Deck desktop sessions: while Steam's desktop action set is active
  // (X pops the keyboard, buttons are mirrored as keys), a modal notice
  // teaches the remedy. It shows itself when input.js detects mirrored
  // presses and disappears on its own the moment the set is switched.
  let deckDesktop = false
  let mirrors = getMirrorState()
  const unsubMirror = onMirrorState(m => { mirrors = m })

  $: deckNoticeOpen = deckDesktop && mirrors

  // Settings panel: slides in from the right, dims the rest, and
  // returns focus to wherever it was on close.
  let settingsOpen   = false
  let appSettings    = { closeAfterLaunch: true, memoryMinMb: 0, memoryMaxMb: 0, fullscreen: false }
  let settingsReturnEl = null

  function openSettings() {
    settingsReturnEl = document.activeElement
    settingsOpen = true
  }

  function closeSettings() {
    settingsOpen = false
    tick().then(() => {
      if (settingsReturnEl?.isConnected && settingsReturnEl !== document.body) {
        settingsReturnEl.focus()
      } else {
        carouselRef?.focusCarousel()
      }
      settingsReturnEl = null
    })
  }

  function onSettingsChange(e) {
    appSettings = e.detail
    SaveSettings(appSettings).catch(() => {})
  }

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

  // Exception classes that only wrap another failure: their name says
  // nothing, so we skip them and fall back to a generic summary.
  const OPAQUE_EXCEPTIONS = /^(FormattedException|RuntimeException|Exception|Error|Throwable|InvocationTargetException|CompletionException|ExecutionException)$/

  function rootCause(text) {
    const lines = text.split('\n')
    let cause = ''
    for (const raw of lines) {
      const l = raw.trim()
      if (l.startsWith('at ')) continue
      const m = l.match(/^(?:Exception in thread "[^"]*" )?(Caused by: )?((?:[\w$]+\.)+([\w$]+(?:Exception|Error|Throwable)))(?::\s*(.*))?$/)
      if (!m) continue
      // A bare opaque wrapper with no message carries no information.
      if (OPAQUE_EXCEPTIONS.test(m[3]) && !m[4]) continue
      const short = m[4] ? m[4] : m[3]
      if (m[1] || !cause) cause = short
    }
    return cause
  }

  function summarizeError(text) {
    if (!text) return ''
    for (const [re, hint] of ERROR_HINTS) {
      if (re.test(text)) return hint
    }
    const cause = rootCause(text)
    if (cause) return cause.length > 120 ? cause.slice(0, 117) + '…' : cause
    return 'Minecraft crashed'
  }

  // Display copy: the near-universal "minecraft crashed (...)" prefix
  // line is dropped (the summary above carries the message) and the
  // first letter is capitalized (error strings are lowercase in Go).
  function formatErrorText(text) {
    if (!text) return ''
    const stripped = text.replace(/^minecraft (crashed|exited|hung)[^\n]*\n+/i, '')
    const s = stripped || text
    return s[0].toUpperCase() + s.slice(1)
  }

  $: errorDisplay = formatErrorText(error)

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

  // Keyboard/gamepad focus for the Copy bar: reached with ArrowRight
  // from the settings panel while an error is shown.
  let errorCopyEl
  let errorFocused = false
  $: if (!error && errorFocused) {
    errorFocused = false
    carouselRef?.focusCarousel()
  }

  // Shrinks the node's font until its content fits its box; re-runs on
  // text change. Used by the error summary inside the fixed-height head.
  function fitText(node, _text) {
    const fit = () => {
      node.style.fontSize = ''
      let size = parseFloat(getComputedStyle(node).fontSize)
      while (node.scrollHeight > node.clientHeight + 1 && size > 8) {
        size -= 0.5
        node.style.fontSize = size + 'px'
      }
    }
    fit()
    return { update: fit }
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
  // stored or published server-side). Fired automatically on a new
  // error with a 3s budget; findings silently enrich the panel, no
  // findings or a failed request add nothing.
  let analysis = null
  let errorTimer = null
  let _prevErrorText = ''
  $: if (error !== _prevErrorText) {
    _prevErrorText = error
    analysis = null
    clearTimeout(errorTimer)
    if (error) {
      autoAnalyze(error)
      // The panel dismisses itself after 30s (mirrored by the countdown
      // strip on the Copy bar), landing focus on the action button.
      errorTimer = setTimeout(dismissError, 30000)
    }
  }

  function dismissError() {
    errorFocused = false
    error = ''
    tick().then(() => {
      const item = focusableItems.find(i => i.idx === 5)
        ?? focusableItems[focusableItems.length - 1]
      if (item) { panelIdx = item.idx; item.focus() }
    })
  }

  async function autoAnalyze(forError) {
    try {
      const res = await AnalyzeCrash(profile?.id ?? '')
      if (error === forError && res?.problems?.length > 0) analysis = res
    } catch {}
  }

  $: profile = profiles[selectedIndex] ?? null

  let _prevProfileId = ''
  $: if (profile && profile.id !== _prevProfileId) {
    _prevProfileId = profile.id
    installing = activeInstallId === profile.id
    installed  = installing ? false : (installedMap[profile.id] ?? false)
    progress   = installing ? savedProgress : { stage: '', current: 0, total: 100 }
    // A crash belongs to the profile it was launched from: switching
    // profiles (keys, trackpad, touch or mouse) dismisses the panel.
    error = ''
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

  // The pointer only makes sense while the mouse is in use: gamepad and
  // touch sessions hide it (it comes back on the first real mousemove).
  $: document.body.classList.toggle('cursor-hidden', inputMode !== 'keyboard')
  onDestroy(() => { unsubInputMode(); unsubMirror(); destroyInput() })

  onMount(async () => {
    EventsOn('install:progress', d => { progress = d; savedProgress = d })

    GetVersion().then(v => { appVersion = v }).catch(() => {})
    startUpdateCheck()
    IsDeckDesktop().then(v => {
      deckDesktop = v
      console.log('[deck-notice] deckDesktop =', v)
    }).catch(() => {})
    GetSettings().then(s => { appSettings = s }).catch(() => {})

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
    // The action-set notice is modal and not dismissible from the app:
    // it clears itself when the user switches the set.
    if (deckNoticeOpen) {
      e.preventDefault()
      return
    }
    if (settingsOpen) return
    if (modsOpen) return
    if (document.querySelector('.wrap.open')) return
    if (!consumeKey(e)) return

    if (e.code === 'KeyO' || e.key === 'o' || e.key === 'O' || e.key === 'щ' || e.key === 'Щ') {
      if (carouselMode !== 'edit') {
        e.preventDefault()
        openSettings()
        return
      }
    }

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

    // Copy bar focus: Left/Escape return to the panel, Enter copies
    // (explicitly, so gamepad A works and native Enter doesn't double).
    if (errorFocused) {
      if (e.key === 'ArrowLeft' || e.key === 'Escape') {
        e.preventDefault()
        const item = focusableItems.find(i => i.idx === lastFocus.idx)
          ?? focusableItems[focusableItems.length - 1]
        if (item) { panelIdx = item.idx; item.focus() }
        return
      }
      if (e.key === 'Enter') { e.preventDefault(); copyError(); return }
      if (e.key === 'ArrowRight') {
        // Continuing right past the Copy bar dismisses the error panel
        // and resumes carousel navigation.
        e.preventDefault()
        error = ''
        carouselRef?.navigateRight(false)
        return
      }
      if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
        e.preventDefault()
        return
      }
    }

    // From the lower settings panel, Right moves into the error panel's
    // Copy bar instead of scrolling the carousel.
    if (e.key === 'ArrowRight' && error && panelIdx >= 0) {
      e.preventDefault()
      errorCopyEl?.focus()
      return
    }

    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault()
      // Navigating left out of the lower panel dismisses the error panel.
      if (e.key === 'ArrowLeft' && error && panelIdx >= 0) error = ''
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

  {#if deckNoticeOpen}
    <div class="deck-notice-overlay" transition:fade={{ duration: 150 }}>
      <div class="deck-notice">
        <div class="deck-notice-text">
          <div>Steam's <b>desktop controls</b> are active:</div>
          <div class="deck-notice-bullet">
            &bull; Buttons act as keyboard keys, so presses double up
            and navigation misbehaves.
          </div>
          <div>
            Hold the <span class="deck-notice-key">&#9776;</span> button
            until this notification appears:
          </div>
        </div>
        <div class="deck-toast">
          <svg class="deck-toast-logo" viewBox="0 0 65 65" xmlns="http://www.w3.org/2000/svg">
            <defs><mask id="steamCut"><rect width="65" height="65" fill="white"/><path d="M30.31 23.985l.003.158-7.83 11.375c-1.268-.058-2.54.165-3.748.662a8.14 8.14 0 0 0-1.498.8L.042 29.893s-.398 6.546 1.26 11.424l12.156 5.016c.6 2.728 2.48 5.12 5.242 6.27a8.88 8.88 0 0 0 11.603-4.782 8.89 8.89 0 0 0 .684-3.656L42.18 36.16l.275.005c6.705 0 12.155-5.466 12.155-12.18s-5.44-12.16-12.155-12.174c-6.702 0-12.155 5.46-12.155 12.174zm-1.88 23.05c-1.454 3.5-5.466 5.147-8.953 3.694a6.84 6.84 0 0 1-3.524-3.362l3.957 1.64a5.04 5.04 0 0 0 6.591-2.719 5.05 5.05 0 0 0-2.715-6.601l-4.1-1.695c1.578-.6 3.372-.62 5.05.077 1.7.703 3 2.027 3.696 3.72s.692 3.56-.01 5.246M42.466 32.1a8.12 8.12 0 0 1-8.098-8.113 8.12 8.12 0 0 1 8.098-8.111 8.12 8.12 0 0 1 8.1 8.111 8.12 8.12 0 0 1-8.1 8.113m-6.068-8.126a6.09 6.09 0 0 1 6.08-6.095c3.355 0 6.084 2.73 6.084 6.095a6.09 6.09 0 0 1-6.084 6.093 6.09 6.09 0 0 1-6.081-6.093z" fill="black"/></mask></defs><path d="M1.305 41.202C5.259 54.386 17.488 64 31.959 64c17.673 0 32-14.327 32-32s-14.327-32-32-32C15.001 0 1.124 13.193.028 29.874c2.074 3.477 2.879 5.628 1.275 11.328z" fill="currentColor" mask="url(#steamCut)"/>
          </svg>
          <svg class="deck-toast-icon" viewBox="0 0 40 22" xmlns="http://www.w3.org/2000/svg">
            <rect x="0.5" y="0.5" width="39" height="21" rx="3.5" fill="currentColor"/>
            <rect x="11" y="4.5" width="18" height="13" fill="#343a46"/>
            <circle cx="5.6" cy="7" r="1.8" fill="#343a46"/>
            <circle cx="5.6" cy="15" r="1.8" fill="#343a46"/>
            <circle cx="34.4" cy="7" r="1.8" fill="#343a46"/>
            <circle cx="34.4" cy="15" r="1.8" fill="#343a46"/>
          </svg>
          <div class="deck-toast-lines">
            <div><span class="deck-toast-info">i</span> Action Set Activated</div>
            <div>Gamepad</div>
          </div>
        </div>
      </div>
    </div>
  {/if}

  {#if settingsOpen}
    <SettingsPanel settings={appSettings} version={appVersion} on:change={onSettingsChange} on:close={closeSettings} />
  {/if}

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
            <span class="error-summary" use:fitText={errorDisplay}>{summarizeError(errorDisplay)}</span>
          </div>
          {#if analysis}
            <div class="error-analysis">
              {#each analysis.problems as p}
                <div class="an-problem">{p.message}</div>
                {#each p.solutions ?? [] as s}
                  <div class="an-solution">&bull; {s.message}</div>
                {/each}
              {/each}
            </div>
          {/if}
          <pre class="error-body" bind:this={errorBodyEl}>{errorDisplay}</pre>
          <button
            bind:this={errorCopyEl}
            class="error-copy"
            class:copied={errorCopied}
            on:click={copyError}
            on:focus={() => { errorFocused = true }}
            on:blur={() => { errorFocused = false }}
            tabindex="-1"
          >
            {#key error}
              <span class="copy-timer" />
            {/key}
            <span class="copy-label">{errorCopied ? 'Copied' : 'Copy'}</span>
          </button>
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
              {#if !deckNoticeOpen}
                {#if !settingsOpen && !modsOpen}
                  <span class="hint">
                    {#if inputMode === 'gamepad'}
                      <span class="glyph">{@html GlyphDPadH}</span>
                    {:else}
                      <span class="keycap">←</span><span class="keycap">→</span>
                    {/if}
                    <span>Profiles</span>
                  </span>
                {/if}
                <span class="hint">
                  {#if inputMode === 'gamepad'}
                    <span class="glyph">{@html GlyphDPadV}</span>
                  {:else}
                    <span class="keycap">↑</span><span class="keycap">↓</span>
                  {/if}
                  <span>Navigate</span>
                </span>
              {/if}
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
              {#if deckNoticeOpen}
                <span class="hint">
                  <span class="deck-notice-key">&#9776;</span>
                  <span>Change action set</span>
                </span>
              {:else}
              {#if profile && !settingsOpen && !modsOpen}
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
                  {#if modsOpen}<span class="hint-slash">/</span><span class="glyph">{@html GlyphY}</span>{/if}
                {:else}
                  <span class="keycap">Esc</span>
                  {#if modsOpen}<span class="hint-slash">/</span><span class="keycap">M</span>{/if}
                {/if}
                <span>Back</span>
              </span>
              {#if !modsOpen}
                <button
                  class="hint hint-btn"
                  on:click={() => settingsOpen ? closeSettings() : openSettings()}
                  tabindex="-1"
                >
                  {#if inputMode === 'gamepad'}
                    <span class="glyph">{@html GlyphX}</span>
                  {:else}
                    <span class="keycap">O</span>
                  {/if}
                  <span>Settings</span>
                </button>
              {/if}
              {/if}
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

  /* Deck desktop hint: a centered SteamOS-style notice above the UI,
     below the footer so the nav bar stays readable. */
  .deck-notice-overlay {
    position: fixed;
    inset: 0 0 2.44rem 0;
    z-index: 300;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.8);
  }

  .deck-notice {
    width: 18rem;
    padding: 0.78rem 0.89rem;
    background: var(--bg);
    border-left: 3px solid var(--accent);
    box-shadow: 0 0 2rem rgba(0, 0, 0, 0.6);
    display: flex;
    flex-direction: column;
    gap: 0.67rem;
  }

  .deck-notice-text {
    display: flex;
    flex-direction: column;
    gap: 0.33rem;
    font-size: 0.67rem;
    line-height: 1.5;
    color: var(--text);
  }

  .deck-notice-bullet {
    padding-left: 0.5rem;
    color: var(--text-sub);
  }

  /* A replica of Steam's own "Action Set Activated" toast, so the user
     knows exactly what to look for. */
  .deck-toast {
    position: relative;
    overflow: hidden;
    display: flex;
    align-items: center;
    gap: 0.61rem;
    padding: 0.5rem 0.67rem;
    background: #343a46;
    color: #fff;
  }

  /* The Steam mark (PD shape from Wikimedia Commons) as a faint
     watermark bleeding off the toast's right edge. */
  .deck-toast-logo {
    position: absolute;
    right: -1.1rem;
    top: 50%;
    transform: translateY(-50%);
    height: 4.4rem;
    width: auto;
    color: #fff;
    opacity: 0.12;
    pointer-events: none;
  }

  .deck-toast-icon {
    width: 2.1rem;
    height: auto;
    flex-shrink: 0;
  }

  .deck-toast-lines {
    display: flex;
    flex-direction: column;
    gap: 0.11rem;
    font-size: 0.67rem;
    font-weight: 700;
  }

  .deck-toast-info {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 0.72rem;
    height: 0.72rem;
    border-radius: 50%;
    background: #fff;
    color: #343a46;
    font-size: 0.5rem;
    font-weight: 700;
    vertical-align: middle;
  }

  /* Same white pill treatment as the version badge. */
  .deck-notice-key {
    display: inline-block;
    padding: 0.06rem 0.39rem;
    background: #fff;
    border-radius: 999px;
    color: #161920;
    font-weight: 700;
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
    opacity: 0;
    transform: translateX(0.5rem);
    transition: opacity 200ms ease 150ms, transform 200ms ease 150ms;
  }
  /* Panel border drawn as an overlay that stops above the Copy bar, so
     the bar spans the full width with no red edges flanking it. */
  .error-content::before {
    content: '';
    position: absolute;
    inset: 0 0 2.67rem 0;
    pointer-events: none;
    border: 1px solid rgba(215, 95, 95, 0.4);
    border-bottom: none;
  }
  .error-side.visible .error-content {
    opacity: 1;
    transform: translateX(0);
  }

  /* Matches the New Profile button's height across the row; the summary
     font shrinks (fitText) instead of overflowing the fixed box. */
  .error-head {
    display: flex;
    align-items: center;
    height: 1.9rem;
    gap: 0.44rem;
    padding: 0.22rem 0.78rem;
    border-bottom: 1px solid rgba(215, 95, 95, 0.25);
    flex-shrink: 0;
    box-sizing: border-box;
  }

  .error-summary {
    flex: 1;
    max-height: 100%;
    overflow: hidden;
    font-size: 0.61rem;
    font-weight: 700;
    line-height: 1.35;
    color: var(--red);
    word-break: break-word;
  }

  /* Full-width action bar at the bottom of the error panel, sized like
     the main action (Play/Install) button. The separator and the focus
     ring are drawn on an ::after overlay so the countdown fill (a
     positioned child) can't paint over them and thin the ring. */
  .error-copy {
    position: relative;
    flex-shrink: 0;
    width: 100%;
    height: 2.67rem;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
    font-size: 0.83rem;
    font-weight: 700;
    letter-spacing: 0.03em;
    color: var(--red);
    background: rgba(215, 95, 95, 0.15);
    cursor: pointer;
    transition: background var(--t), color var(--t);
  }
  .error-copy::after {
    content: '';
    position: absolute;
    inset: 0;
    pointer-events: none;
    box-shadow: inset 0 1px 0 rgba(215, 95, 95, 0.25);
  }
  .error-copy:focus { outline: none; }
  .error-copy:focus::after {
    box-shadow: inset 0 0 0 2px var(--accent);
  }

  .copy-label { position: relative; }

  /* Countdown fill mirroring the 30s auto-dismiss, drawn inside the
     button behind the label like the install progress bar. */
  .copy-timer {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 100%;
    background: rgba(215, 95, 95, 0.2);
    animation: copy-countdown 30s linear forwards;
    pointer-events: none;
  }

  @keyframes copy-countdown {
    from { width: 100%; }
    to   { width: 0; }
  }
  .error-copy:hover:not(.copied) { background: rgba(215, 95, 95, 0.3); }
  .error-copy.copied {
    color: #8be8a0;
    background: rgba(139, 232, 160, 0.15);
  }
  .error-copy.copied .copy-timer { background: rgba(139, 232, 160, 0.2); }

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

  .hint-btn {
    background: none;
    padding: 0;
    cursor: pointer;
    transition: color var(--t);
  }
  .hint-btn:hover { color: var(--text); }

  .hint-slash {
    color: var(--text-sub);
    opacity: 0.6;
    margin: 0 -0.11rem;
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
