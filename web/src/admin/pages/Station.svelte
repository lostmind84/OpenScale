<script lang="ts">
  import Field from '../components/Field.svelte'
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import { differences, valueAt } from '../lib/diff'
  import type { Draft } from '../lib/draft.svelte'
  import type { ConfigVersionDTO, HealthDTO } from '../lib/dto'
  import { frenchBytes, frenchDateTime, frenchInteger } from '../lib/format'
  import type { Admin } from '../lib/session.svelte'

  /**
   * La page Poste de §14.4 : identité, export/import avec le diff champ par champ, les cinq
   * versions restaurables, la version du binaire, les chemins, l'espace disque.
   *
   * **Il n'y a pas de bouton « redémarrer ».** Aucun bloc de configuration ne l'exige
   * (§11.4, ADR-027) : le seul redémarrage légitime est celui que le gestionnaire de
   * services déclenche seul, et un bouton l'aurait rendu banal.
   *
   * L'import montre le diff et n'applique rien : c'est l'enregistrement qui applique, une
   * fois qu'un humain a lu ce qui change. Un fichier qui s'appliquerait tout seul serait un
   * poste reconfiguré par un double-clic.
   */
  interface Props {
    admin: Admin
    draft: Draft
    health: HealthDTO
  }

  const { admin, draft, health }: Props = $props()

  let versions = $state<ConfigVersionDTO[]>([])
  /** Le diff d'un fichier importé : une ligne par champ qui change. */
  let diff = $state<{ path: string; before: string; after: string }[]>([])
  let candidate = $state<Record<string, unknown> | null>(null)

  void loadVersions()

  /** Lit les cinq versions restaurables. */
  async function loadVersions(): Promise<void> {
    versions = (await admin.load(api.fetchVersions)) ?? []
  }

  /**
   * Lit un fichier de configuration exporté et affiche le diff champ par champ.
   *
   * La comparaison est faite ici, sur les deux documents, et non par le service : ce que
   * §14.4 demande est un APERÇU pour un humain, et l'écran a déjà les deux versions.
   */
  async function inspect(file: File | null | undefined): Promise<void> {
    if (file === null || file === undefined || draft.config === null) return
    const text = await file.text()
    let parsed: unknown
    try {
      parsed = JSON.parse(text)
    } catch {
      admin.actionError = `${file.name} n’est pas un fichier JSON lisible.`
      return
    }
    candidate = parsed as Record<string, unknown>
    diff = differences(draft.config, candidate)
    if (diff.length === 0) admin.notice = 'Ce fichier décrit exactement la configuration en service.'
  }

  /** Recopie le fichier importé dans le brouillon : l'enregistrement reste un geste à part. */
  function adopt(): void {
    if (candidate === null) return
    for (const entry of diff) draft.set(entry.path, valueAt(candidate, entry.path))
    admin.notice =
      'Le fichier est recopié dans le brouillon. Rien n’est appliqué avant « Enregistrer ».'
  }
</script>

<div class="pages">
  <Panel title="Identité du poste">
    <Field
      label="Numéro du poste"
      path="station.number"
      kind="number"
      value={draft.text('station.number')}
      hint="C’est de lui que dérive le nom du fichier de catalogue attendu, flv_<n>.csv."
      onchange={(value) => draft.set('station.number', Number(value))}
    />
    <Field
      label="Nom du poste"
      path="station.name"
      value={draft.text('station.name')}
      hint="Ce que lit un bénévole : « Poste 2 — fruits »."
      onchange={(value) => draft.set('station.name', value)}
    />
    <Field
      label="Coopérative"
      path="station.coop"
      value={draft.text('station.coop')}
      onchange={(value) => draft.set('station.coop', value)}
    />
    <dl class="identity">
      <dt>Empreinte de la configuration en service</dt>
      <dd data-fingerprint>{health.config_fingerprint}</dd>
      <dt>Version du binaire</dt>
      <dd>{health.version}</dd>
      <dt>Répertoire de données</dt>
      <dd>{health.disk === null ? 'non publié par ce poste' : health.disk.path}</dd>
      <dt>Espace disque</dt>
      <dd>
        {#if health.disk === null}
          non mesuré
        {:else}
          {frenchBytes(health.disk.free_bytes)} libres sur
          {frenchBytes(health.disk.total_bytes)} — seuil d’alerte
          {frenchInteger(health.disk.alert_mb)} Mo
        {/if}
      </dd>
    </dl>
  </Panel>

  <Panel
    title="Exporter, importer"
    note="L’export sans le matériel est ce qui sert à cloner un poste (§11.5) : ni mot de passe, ni numéro de poste ne voyagent."
  >
    <div class="actions">
      <a class="action touch-target" href="/admin/api/config/export?hardware=1" download>
        Exporter tout
      </a>
      <a class="action touch-target" href="/admin/api/config/export?hardware=0" download>
        Exporter sans le matériel
      </a>
      <label class="action touch-target">
        Importer un fichier
        <input
          type="file"
          accept=".json,application/json"
          onchange={(event) => void inspect(event.currentTarget.files?.item(0))}
        />
      </label>
    </div>

    {#if diff.length > 0}
      <div class="scroll">
        <table data-diff>
          <thead>
            <tr>
              <th>Champ</th>
              <th>En service</th>
              <th>Dans le fichier</th>
            </tr>
          </thead>
          <tbody>
            {#each diff as entry (entry.path)}
              <tr>
                <td><code>{entry.path}</code></td>
                <td>{entry.before}</td>
                <td>{entry.after}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      <button type="button" class="action touch-target" onclick={adopt}>
        Recopier ces {frenchInteger(diff.length)} champs dans le brouillon
      </button>
    {/if}
  </Panel>

  <Panel
    title="Cinq versions restaurables"
    note="Chaque enregistrement fait tourner les versions : la plus récente est la 1."
  >
    {#if versions.length === 0}
      <p class="fact">Aucune version enregistrée : ce poste n’a jamais été reconfiguré.</p>
    {:else}
      <ul class="rows">
        {#each versions as version (version.version)}
          <li>
            <span class="what">version {frenchInteger(version.version)}</span>
            <span class="detail">{frenchDateTime(version.modified_at)}</span>
            <span class="detail">{version.config_fingerprint}</span>
            <button
              type="button"
              class="pick touch-target"
              disabled={admin.busy}
              onclick={() => void admin.run(async () => {
                await api.restoreVersion(version.version)
                await draft.load()
                await loadVersions()
                return { message: `La version ${String(version.version)} est remise en service.` }
              })}
            >
              remettre en service
            </button>
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
    margin: 0.5rem 0;
    font-size: 1.125rem;
  }

  .identity {
    margin: 1rem 0 0;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.25rem 1rem;
    font-size: 1.0625rem;
  }

  .identity dt {
    color: var(--ink-muted);
  }

  .identity dd {
    margin: 0;
    font-weight: 700;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--touch-gap);
    margin-bottom: 0.75rem;
  }

  .action {
    display: inline-flex;
    align-items: center;
    padding: 0 1rem;
    font-size: 1.0625rem;
    font-weight: 700;
    text-decoration: none;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    cursor: pointer;
  }

  .action input {
    display: none;
  }

  .scroll {
    overflow-x: auto;
  }

  table {
    border-collapse: collapse;
    width: 100%;
    font-size: 1.0625rem;
  }

  th,
  td {
    padding: 0.375rem 0.5rem;
    text-align: left;
    border-bottom: 1px solid var(--border);
  }

  th {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  .rows {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .rows li {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: center;
    padding: 0.375rem 0;
    border-top: 1px solid var(--border);
    font-size: 1.0625rem;
  }

  .what {
    font-weight: 700;
  }

  .detail {
    color: var(--ink-muted);
  }

  .pick {
    padding: 0 0.5rem;
    text-decoration: underline;
  }
</style>
