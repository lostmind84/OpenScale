<script lang="ts">
  import Field from '../components/Field.svelte'
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import type { Draft } from '../lib/draft.svelte'
  import type { DetectionDTO, HealthDTO, PortDTO, PrinterDeviceDTO } from '../lib/dto'
  import { frenchDateTime, frenchDuration, frenchInteger } from '../lib/format'
  import type { Admin } from '../lib/session.svelte'

  /**
   * La page Matériel de §14.4.
   *
   * Ce qu'elle change par rapport à un écran de réglages ordinaire tient en une phrase :
   * **c'est la détection qui répond à « y a-t-il une balance ? », pas l'exploitant.**
   * « Détecter automatiquement » ouvre chaque port, applique les parseurs et annonce
   * « COM8 : 12 trames valides, GRAM XFOC » — un exploitant n'a pas à savoir ce qu'est un
   * port série pour répondre à cette question.
   */
  interface Props {
    admin: Admin
    draft: Draft
    health: HealthDTO
  }

  const { admin, draft, health }: Props = $props()

  let ports = $state<PortDTO[]>([])
  let printers = $state<PrinterDeviceDTO[]>([])
  let detections = $state<DetectionDTO[]>([])
  let frames = $state<string[]>([])
  let searching = $state(false)

  const scale = $derived(health.state.scale)
  const printer = $derived(health.state.printer)
  const present = $derived(draft.flag('scale.present'))

  /** Énumère les ports série, avec la description USB qui nomme un câble visible. */
  async function listPorts(): Promise<void> {
    ports = (await admin.load(api.fetchPorts)) ?? []
  }

  /** « Lister les files » d'impression que la plateforme connaît. */
  async function listPrinters(): Promise<void> {
    printers = (await admin.load(api.fetchPrinters)) ?? []
  }

  /** « Rechercher l'imprimante » : le balayage, qui prend des secondes. */
  async function discover(): Promise<void> {
    searching = true
    printers = (await admin.load(api.discoverPrinters)) ?? printers
    searching = false
  }

  /**
   * « Détecter automatiquement » : chaque port ouvert trois secondes, les parseurs
   * appliqués, et le verdict annoncé port par port.
   */
  async function detect(): Promise<void> {
    detections = []
    if (ports.length === 0) await listPorts()
    for (const port of ports) {
      const report = await admin.load(() => api.detectScale(port.name))
      if (report !== null) detections = [...detections, report]
    }
  }

  /** Le visualiseur des dernières trames brutes (§14.4) : trois secondes d'écoute. */
  async function capture(): Promise<void> {
    const port = draft.text('scale.options.port')
    frames = (await admin.load(() => api.captureFrames(port, 3))) ?? []
  }

  /** Le message du contrôle qui a refusé cette clé, quand il y en a un (§11.3). */
  function faultOf(path: string): string {
    return draft.faults.find((fault) => fault.field === path)?.message ?? ''
  }

  /** Les valeurs qu'un contrôle a nommées comme acceptables. */
  function allowedFor(path: string): string[] {
    return draft.faults.find((fault) => fault.field === path)?.allowed ?? []
  }
</script>

<div class="pages">
  <Panel
    title="Balance"
    note="La cadence et l’état viennent de ce que le poste observe vraiment, jamais d’un réglage."
  >
    <p class="fact">
      {#if !health.scale_present}
        Ce poste est déclaré sans balance : le feu est éteint et le poids se saisit à la main.
      {:else}
        {scale.connected ? 'Elle répond' : 'Elle ne répond plus'} — une mesure toutes les
        {frenchDuration(scale.median_ms)} sur {frenchInteger(scale.observations_count)}
        intervalles{scale.provisional ? ', cadence encore provisoire' : ''}.
      {/if}
    </p>

    <label class="check">
      <input
        type="checkbox"
        checked={!present}
        onchange={(event) => draft.set('scale.present', !event.currentTarget.checked)}
      />
      <span>
        Ce poste n’a pas de balance
        <small>Le feu s’ÉTEINT au lieu de rester rouge, et la saisie manuelle devient le mode normal.</small>
      </span>
    </label>

    <Field
      label="Protocole"
      path="scale.type"
      value={draft.text('scale.type')}
      hint="Les deux entrées de registre de §9.3. Les valeurs acceptées apparaissent ici si l’enregistrement est refusé."
      fault={faultOf('scale.type')}
      allowed={allowedFor('scale.type')}
      onchange={(value) => draft.set('scale.type', value)}
    />
    <Field
      label="Port série"
      path="scale.options.port"
      value={draft.text('scale.options.port')}
      hint="Choisissez-le dans la liste détectée ci-dessous plutôt que de le taper."
      fault={faultOf('scale.options.port')}
      allowed={ports.map((port) => port.name)}
      onchange={(value) => draft.set('scale.options.port', value)}
    />

    <div class="actions">
      <button type="button" class="action touch-target" onclick={() => void listPorts()}>
        Lister les ports
      </button>
      <button type="button" class="action touch-target" onclick={() => void detect()}>
        Détecter automatiquement
      </button>
      <button type="button" class="action touch-target" onclick={() => void capture()}>
        Voir les trames brutes
      </button>
    </div>

    {#if ports.length > 0}
      <ul class="list">
        {#each ports as port (port.name)}
          <li>
            <button
              type="button"
              class="pick touch-target"
              onclick={() => draft.set('scale.options.port', port.name)}
            >
              {port.name}
            </button>
            <span class="detail">
              {port.description === '' ? 'aucune description USB' : port.description}
              {port.vid === '' ? '' : ` — VID ${port.vid} PID ${port.pid}`}
            </span>
          </li>
        {/each}
      </ul>
    {/if}

    {#if detections.length > 0}
      <ul class="list" data-detections>
        {#each detections as detection (detection.port)}
          <li>
            <span class="what">{detection.port}</span>
            <span class="detail">{detection.message}</span>
          </li>
        {/each}
      </ul>
    {/if}

    {#if frames.length > 0}
      <ol class="frames">
        {#each frames.slice(-20) as frame, index (index)}
          <li><code>{frame}</code></li>
        {/each}
      </ol>
    {/if}
  </Panel>

  <Panel title="Imprimante">
    <p class="fact">
      {printer.detail === '' ? printer.health : printer.detail} — observé
      {printer.observed_at === '' ? 'jamais' : frenchDateTime(printer.observed_at)},
      {frenchInteger(printer.pending_jobs_count)} travaux en attente.
    </p>

    <Field
      label="Driver"
      path="printer.type"
      value={draft.text('printer.type')}
      hint="Le driver raster est le chemin de production (ADR-002)."
      fault={faultOf('printer.type')}
      allowed={allowedFor('printer.type')}
      onchange={(value) => draft.set('printer.type', value)}
    />
    <Field
      label="Transport"
      path="printer.options.transport"
      value={draft.text('printer.options.transport')}
      hint="Local par défaut : file Windows ou fichier de périphérique (ADR-007)."
      fault={faultOf('printer.options.transport')}
      allowed={allowedFor('printer.options.transport')}
      onchange={(value) => draft.set('printer.options.transport', value)}
    />
    <Field
      label="File ou chemin"
      path="printer.options.queue"
      value={draft.text('printer.options.queue')}
      hint="Choisissez-la dans la liste ci-dessous : une file mal orthographiée ne s’imprime pas."
      fault={faultOf('printer.options.queue')}
      allowed={printers.map((device) => device.name)}
      onchange={(value) => draft.set('printer.options.queue', value)}
    />

    <div class="actions">
      <button type="button" class="action touch-target" onclick={() => void listPrinters()}>
        Lister les files
      </button>
      <button
        type="button"
        class="action touch-target"
        disabled={searching}
        onclick={() => void discover()}
      >
        {searching ? 'Recherche en cours…' : 'Rechercher l’imprimante'}
      </button>
      <button
        type="button"
        class="action touch-target"
        onclick={() => void admin.run(() => api.printerSelfTest('label'))}
      >
        Auto-test : étiquette
      </button>
      <button
        type="button"
        class="action touch-target"
        onclick={() => void admin.run(() => api.printerSelfTest('alignment'))}
      >
        Auto-test : alignement
      </button>
      <button
        type="button"
        class="action touch-target"
        onclick={() => void admin.run(() => api.printerSelfTest('ruler'))}
      >
        Auto-test : réglette
      </button>
    </div>

    {#if printers.length > 0}
      <ul class="list">
        {#each printers as device (device.name)}
          <li>
            <button
              type="button"
              class="pick touch-target"
              onclick={() => draft.set('printer.options.queue', device.name)}
            >
              {device.name}
            </button>
            <span class="detail">
              {device.detail}{device.default ? ' — file par défaut du système' : ''}
            </span>
          </li>
        {/each}
      </ul>
    {/if}
  </Panel>
</div>

<style>
  .pages {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .fact {
    margin: 0 0 0.75rem;
    font-size: 1.125rem;
  }

  .check {
    display: flex;
    gap: 0.75rem;
    align-items: flex-start;
    padding: 0.5rem 0;
    font-size: 1.125rem;
  }

  .check input {
    width: 1.5rem;
    height: 1.5rem;
    margin-top: 0.125rem;
  }

  .check small {
    display: block;
    font-size: 1rem;
    color: var(--ink-muted);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--touch-gap);
    margin: 0.75rem 0;
  }

  .action {
    padding: 0 1rem;
    font-size: 1.125rem;
    font-weight: 700;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .list,
  .frames {
    margin: 0;
    padding: 0 0 0 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .list li,
  .frames li {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: center;
    padding: 0.25rem 0;
    border-top: 1px solid var(--border);
    font-size: 1.0625rem;
  }

  .pick {
    padding: 0 0.75rem;
    font-weight: 700;
    text-decoration: underline;
  }

  .what {
    font-weight: 700;
  }

  .detail {
    color: var(--ink-muted);
  }
</style>
