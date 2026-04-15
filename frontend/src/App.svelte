<script>
  import { onMount } from 'svelte'
  import { EventsOn } from '../wailsjs/runtime/runtime.js'
  import {
    GetProfiles, CreateProfile, SaveProfile, DeleteProfile, GetIcons,
    GetVanillaVersions, GetFabricLoaderVersions, GetFabricGameVersions,
    IsInstalled, Install, Launch, CleanGameData
  } from '../wailsjs/go/internal/App.js'

  import Carousel       from './components/Carousel.svelte'
  import VersionSelector from './components/VersionSelector.svelte'
  import ActionButton   from './components/ActionButton.svelte'
  import { GlyphA, GlyphB, GlyphDPadH, IconPlus } from './lib/icons.js'

  let profiles        = []
  let icons           = []
  let selectedIndex   = 0

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
    installed  = installedMap[profile.id] ?? false
    installing = activeInstallId === profile.id
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
    checkingInstall = true
    installed = await IsInstalled(loader, selectedMC, loader === 'fabric' ? selectedFabric : '')
    if (profile) installedMap = { ...installedMap, [profile.id]: installed }
    checkingInstall = false
  }

  async function checkAllInstalled() {
    const results = await Promise.all(profiles.map(async p => {
      if (!p.mcVersion) return [p.id, false]
      const ok = await IsInstalled(loader, p.mcVersion, loader === 'fabric' ? p.fabricLoaderVersion : '')
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
</script>

<div class="app">
  <div class="content">
    <section class="carousel-section">
      <Carousel
        {profiles}
        {icons}
        bind:selectedIndex
        checking={checkingInstall}
        {installPct}
        installProfileId={activeInstallId}
        {installedMap}
        on:create={handleCreate}
        on:delete={handleDelete}
        on:save={handleSave}
      />
    </section>

    <div class="panel-row">
      <section class="panel">
        <button class="new-profile-btn" on:click={handleCreate} tabindex="-1">
          <span class="new-profile-icon">{@html IconPlus}</span>
          <span>New Profile</span>
        </button>

        <div class="spacer" />

        <VersionSelector
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
        <span>Navigate</span>
      </span>
    </div>
    <div class="hints-right">
      <span class="hint">
        <span class="glyph">{@html GlyphA}</span>
        <span>{installed ? 'Select' : 'Download'}</span>
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
  .new-profile-btn:hover {
    background: var(--card-btn-hover);
    color: var(--text);
    font-weight: 700;
  }
  .new-profile-icon {
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
    color: inherit;
  }
  .new-profile-icon :global(svg) { width: 0.78rem; height: 0.78rem; }

  .divider {
    height: 1px;
    background: rgba(255,255,255,0.06);
  }

  .spacer {
    height: 0.89rem;
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

  .hints-left { flex: 1; }

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
