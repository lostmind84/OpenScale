<script lang="ts" module>
  /** ERR-SCL-09 : « saisie manuelle demandée depuis l'écran de dépannage » (§11.4). */
  const MANUAL_ENTRY_CODE = 'ERR-SCL-09'
</script>

<script lang="ts">
  import BigButton from '../components/BigButton.svelte'
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import type { HealthDTO } from '../lib/dto'
  import { frenchDateTime } from '../lib/format'
  import type { Admin } from '../lib/session.svelte'

  /**
   * La page Dépannage de §14.4 : les neuf gros boutons, sans mot de passe (ADR-018).
   *
   * **Pourquoi ces actions ne sont pas protégées.** Quiconque est derrière le comptoir peut
   * déjà débrancher l'imprimante : le mot de passe n'ajouterait là aucune sécurité et
   * supprimerait tout le dépannage. Le mot de passe reste exigé pour tout ce qui ÉCRIT la
   * configuration, et aucun de ces boutons ne l'écrit — ils lisent un port, interrogent un
   * statut, sortent une étiquette de démonstration, ou font entrer le poste dans un ÉTAT.
   *
   * Le dixième bouton, « Imprimer sur l'imprimante du poste voisin », n'apparaît que si une
   * imprimante de secours est configurée : un bouton qui répondrait « fonction non
   * disponible sur ce poste » à quelqu'un déjà en difficulté serait pire que son absence.
   */
  interface Props {
    admin: Admin
    health: HealthDTO
  }

  const { admin, health }: Props = $props()

  /** Ce que le dernier test de matériel a répondu, en français. */
  let report = $state('')

  /**
   * Le poste est-il en saisie manuelle du poids ?
   *
   * La question se pose au CODE de la dégradation et non à sa présence : ERR-SCL-09 est
   * « quelqu'un l'a demandé », ERR-SCL-03 est « le port ne s'ouvre pas », et un bouton qui
   * confondrait les deux proposerait « Revenir à la balance » à un poste dont le câble est
   * débranché (§11.4).
   */
  const manual = $derived(health.state.degraded?.code === MANUAL_ENTRY_CODE)
  const routing = $derived(health.printing)
  const onFallback = $derived(routing?.on_fallback === true)

  /** Le fichier déposé sur la zone de glisser-déposer. */
  let dropping = $state(false)

  /** Passe une action de dépannage et garde sa phrase. */
  async function run(action: () => Promise<{ message: string }>): Promise<void> {
    report = ''
    await admin.run(action)
  }

  /** Passe un test de matériel, dont la réponse est une PHRASE et non une action. */
  async function probe(test: () => Promise<{ message: string }>): Promise<void> {
    report = ''
    const answer = await admin.load(test)
    if (answer !== null) report = answer.message
  }

  /**
   * Importe un CSV déposé sur l'écran (A4, ADR-011).
   *
   * Le fichier est analysé puis écrit là où la veille ordinaire le trouvera : même
   * parseur, même qualification, même acquittement qu'un fichier déposé par le producteur.
   * Il n'y a donc pas de second chemin d'import à maintenir.
   */
  async function importFile(file: File | null | undefined): Promise<void> {
    dropping = false
    if (file === null || file === undefined) return
    report = ''
    const record = await admin.load(() => api.importCatalog(file))
    if (record === null) return
    report =
      `${file.name} : ${String(record.rows_read_count)} lignes lues, ` +
      `${String(record.weighable_count)} pesables. La veille l’appliquera dans la seconde.`
    await admin.refresh()
  }
</script>

<div class="troubleshooting">
  {#if report !== ''}
    <p class="report" data-report>{report}</p>
  {/if}

  {#if routing !== null && routing.banner !== ''}
    <p class="report" data-routing>{routing.banner}</p>
  {/if}

  <section class="buttons" aria-label="Actions de dépannage">
    <BigButton
      label="Tester la balance"
      hint="Ce que le poste a déjà observé — le port n’est pas rouvert."
      disabled={admin.busy}
      onrun={() => void probe(api.testScale)}
    />
    <BigButton
      label="Tester l’imprimante"
      hint="Ce que le superviseur a vu il y a moins d’une seconde."
      disabled={admin.busy}
      onrun={() => void probe(api.testPrinter)}
    />
    <BigButton
      label="Imprimer une étiquette de test"
      hint="Une étiquette de démonstration sort de l’imprimante du poste."
      disabled={admin.busy}
      onrun={() => void run(api.testLabel)}
    />
    <BigButton
      label="Réimprimer la dernière"
      hint="La dernière étiquette imprimée sort une seconde fois."
      disabled={admin.busy}
      onrun={() => void run(api.reprintLast)}
    />
    <BigButton
      label="Recharger le catalogue"
      hint="La veille refait tout de suite le contrôle qu’elle fait toutes les cinq secondes."
      disabled={admin.busy}
      onrun={() => void run(api.reloadCatalog)}
    />
    <BigButton
      label={manual ? 'Revenir à la balance' : 'Basculer en saisie manuelle'}
      hint={manual
        ? 'Le poids sera de nouveau lu sur la balance.'
        : 'Le poids se tape à la main : le poste continue de servir sans balance.'}
      engaged={manual}
      disabled={admin.busy}
      onrun={() => void run(() => api.setManualEntry(!manual))}
    />
    <BigButton
      label="J’ai changé le rouleau"
      hint="Le compteur d’étiquettes repart à zéro. C’est le seul geste qui dise quelque chose de vrai du papier."
      disabled={admin.busy}
      onrun={() => void run(api.rollChanged)}
    />
    {#if routing !== null && routing.fallback_available}
      <BigButton
        label={onFallback ? 'Revenir à l’imprimante du poste' : 'Imprimer sur l’imprimante du poste voisin'}
        hint={onFallback
          ? 'Les étiquettes repartiront sur l’imprimante de ce poste.'
          : 'Les étiquettes sortiront sur l’imprimante voisine, pour cette session seulement.'}
        engaged={onFallback}
        disabled={admin.busy}
        onrun={() => void run(() => api.useFallbackPrinter(!onFallback))}
      />
    {/if}
    <a class="big touch-target" href={api.DIAGNOSTIC_URL} download>
      <span class="label">Télécharger le fichier de diagnostic</span>
      <span class="hint">Un seul fichier à envoyer au support. Il ne contient aucun mot de passe.</span>
    </a>
  </section>

  <Panel
    title="Importer un catalogue"
    note="Glissez le fichier CSV ici, ou choisissez-le. Il passe par le même chemin que le fichier du producteur."
  >
    <div
      class="drop"
      class:dropping
      role="group"
      aria-label="Zone de dépôt du catalogue"
      ondragover={(event) => {
        event.preventDefault()
        dropping = true
      }}
      ondragleave={() => (dropping = false)}
      ondrop={(event) => {
        event.preventDefault()
        void importFile(event.dataTransfer?.files.item(0))
      }}
    >
      <p>Déposez ici le fichier <code>flv_{health.station}.csv</code></p>
      <label class="choose touch-target">
        Choisir un fichier
        <input
          type="file"
          accept=".csv,text/csv"
          onchange={(event) => void importFile(event.currentTarget.files?.item(0))}
        />
      </label>
    </div>
  </Panel>

  {#if health.catalog !== null && health.catalog.result === 'rejected'}
    <Panel title="Le dernier fichier a été refusé">
      <p class="fact">
        {health.catalog.reason === '' ? 'Aucun motif enregistré.' : health.catalog.reason}
        Le catalogue en service n’a pas changé.
      </p>
      <p class="fact muted">
        Dernier essai : {frenchDateTime(health.catalog.occurred_at)}.
      </p>
    </Panel>
  {/if}
</div>

<style>
  .troubleshooting {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .buttons {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(22rem, 1fr));
    gap: var(--touch-gap);
  }

  /* Le téléchargement est une ANCRE et non un bouton : c'est le navigateur qui doit
     enregistrer le fichier, et un `fetch` obligerait à reconstruire le nom du fichier et
     la boîte d'enregistrement à la main. Elle porte l'apparence et la cible des autres. */
  .big {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 0.25rem;
    min-height: 6rem;
    padding: 1rem 1.25rem;
    text-align: left;
    text-decoration: none;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .label {
    font-size: 1.375rem;
    font-weight: 700;
  }

  .hint {
    font-size: 1rem;
    color: var(--ink-muted);
  }

  .report {
    margin: 0;
    padding: 0.75rem 1rem;
    font-size: 1.125rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-left: 0.5rem solid var(--ready);
    border-radius: var(--radius);
  }

  .drop {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 1rem;
    padding: 1rem;
    border: 2px dashed var(--border);
    border-radius: var(--radius);
  }

  .drop.dropping {
    border-color: var(--focus);
  }

  .drop p {
    margin: 0;
    font-size: 1.125rem;
  }

  .choose {
    display: inline-flex;
    align-items: center;
    padding: 0 1rem;
    font-size: 1.125rem;
    font-weight: 700;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    cursor: pointer;
  }

  .choose input {
    display: none;
  }

  .fact {
    margin: 0 0 0.5rem;
    font-size: 1.125rem;
  }

  .muted {
    color: var(--ink-muted);
  }
</style>
