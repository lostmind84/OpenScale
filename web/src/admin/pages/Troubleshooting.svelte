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
  import { importResultWord } from '../lib/inventory'
  import { ReloadWatch } from '../lib/reload.svelte'
  import type { Admin } from '../lib/session.svelte'

  /**
   * La page Dépannage de §14.4 : les neuf gros boutons.
   *
   * **Sept sont libres, deux demandent le mot de passe** (ADR-033). Quiconque est derrière
   * le comptoir peut déjà débrancher l'imprimante : exiger un mot de passe pour tester la
   * balance, réimprimer ou déclarer un rouleau neuf n'ajouterait aucune sécurité et
   * supprimerait tout le dépannage, qui est le premier geste d'une mauvaise matinée.
   *
   * Les deux exceptions ne testent rien, et c'est ce qui les distingue : « basculer en
   * saisie manuelle » coupe la balance et laisse LE CLIENT taper son propre poids, et le
   * dépôt d'un CSV remplace toute la grille par un fichier apporté. L'une et l'autre
   * changent ce que le poste vend ou la façon dont il pèse, et laissent leur trace en
   * caisse. Elles portent la mention « CLÉ » avant d'être touchées.
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

  /**
   * Le bouton qui travaille en ce moment, ou une chaîne vide.
   *
   * Trois des neuf actions n'avaient AUCUN état « en cours » : les deux tests de matériel
   * et l'import passent par `admin.load`, qui — contrairement à `admin.run` — ne lève pas
   * `busy`. Un bénévole appuyait, rien ne bougeait pendant que le port s'ouvrait, et il
   * appuyait de nouveau.
   */
  let working = $state('')

  const busy = $derived(admin.busy || working !== '')

  /**
   * L'attente de l'issue d'un rechargement de catalogue.
   *
   * Le bouton répondait « Le catalogue va être relu. » — une promesse écrite avant tout
   * accès au support — puis se taisait pour de bon, parce qu'une veille qui ne trouve rien
   * revient sans un mot. L'issue n'arrive que par le sondage de trois secondes, et
   * l'identifiant de l'import en service est ce qui dit que l'attente est finie.
   */
  const reloadWatch = new ReloadWatch()

  $effect(() => {
    reloadWatch.observe(health)
  })

  /** Ce que ce poste surveille, ou ce qu'il faut dire quand il ne le publie pas. */
  const watched = $derived(
    health.catalog_source === null
      ? 'Ce poste ne publie pas la source de son catalogue.'
      : 'Catalogue surveillé : ' + health.catalog_source.label,
  )

  /**
   * Le dernier import qui n'a RIEN mis en service.
   *
   * `failed` autant que `rejected` : le premier est un import que la base n'a pas pu
   * écrire, et il était jusqu'ici invisible sur l'écran de dépannage alors que le
   * glisser-déposer de la même page traite les deux.
   */
  const setAside = $derived(
    health.catalog !== null &&
      (health.catalog.result === 'rejected' || health.catalog.result === 'failed')
      ? health.catalog
      : null,
  )

  /** Ouvre un acte : ce que le précédent a laissé à l'écran s'en va. */
  function begin(label: string): void {
    report = ''
    reloadWatch.forget()
    working = label
  }

  /** Passe une action de dépannage et garde sa phrase. */
  async function run(label: string, action: () => Promise<{ message: string }>): Promise<void> {
    begin(label)
    try {
      await admin.run(action)
    } finally {
      working = ''
    }
  }

  /**
   * Recharge le catalogue, et arme l'attente de son issue.
   *
   * L'acte a sa fonction à lui parce qu'il est le seul dont la phrase immédiate ne conclut
   * rien : elle dit ce que le poste a VU du fichier surveillé, et l'issue — appliqué,
   * identique au précédent, refusé — arrive quelques secondes plus tard par le sondage.
   */
  async function reload(): Promise<void> {
    begin('reload')
    try {
      const answer = await admin.run(api.reloadCatalog)
      if (answer !== null) reloadWatch.begin(answer)
    } finally {
      working = ''
    }
  }

  /** Passe un test de matériel, dont la réponse est une PHRASE et non une action. */
  async function probe(label: string, test: () => Promise<{ message: string }>): Promise<void> {
    begin(label)
    try {
      const answer = await admin.load(test)
      if (answer !== null) report = answer.message
    } finally {
      working = ''
    }
  }

  /**
   * Passe un acte PROTÉGÉ : la bascule en saisie manuelle et le dépôt d'un catalogue.
   *
   * Ces deux-là ne testent rien — l'une coupe la balance et laisse le client taper son
   * propre poids, l'autre remplace toute la grille — et ADR-033 les protège pour cette
   * raison. Le mot de passe est demandé au moment du geste, puis le geste est rejoué.
   */
  async function guarded(label: string, action: () => Promise<{ message: string }>): Promise<void> {
    begin(label)
    try {
      const done = await admin.protect(action)
      if (done !== null) {
        admin.notice = done.message
        await admin.refresh()
      }
    } finally {
      working = ''
    }
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
    begin('import')
    try {
      // Acte protégé (ADR-033) : il remplace toute la grille par un fichier apporté.
      const record = await admin.protect(() => api.importCatalog(file))
      if (record === null) return
      // Un fichier REFUSÉ était annoncé comme appliqué : « la veille l'appliquera dans la
      // seconde » se lisait sous un import que le service venait d'écarter, et le
      // bénévole repartait en croyant son catalogue à jour.
      report =
        record.result === 'rejected' || record.result === 'failed'
          ? `${file.name} : REFUSÉ${record.reason === '' ? '' : ' — ' + record.reason}. ` +
            'Le catalogue en service n’a pas changé.'
          : `${file.name} : ${String(record.rows_read_count)} lignes lues, ` +
            `${String(record.weighable_count)} pesables. La veille l’appliquera dans la seconde.`
      await admin.refresh()
    } finally {
      working = ''
    }
  }
</script>

<div class="troubleshooting">
  {#if report !== ''}
    <p class="report" data-report>{report}</p>
  {/if}

  <!--
    The outcome of a reload, once the polling has seen the station change import. It is a
    line of its own and not the one above: what a hardware test answers is over the
    instant it is read, while this one arrives seconds after the button was released.
  -->
  {#if reloadWatch.sentence !== ''}
    <p class="report" data-reload>{reloadWatch.sentence}</p>
  {/if}

  {#if routing !== null && routing.banner !== ''}
    <p class="report" data-routing>{routing.banner}</p>
  {/if}

  <!--
    Permanently, and without anybody having to press anything: this is the screen a
    volunteer opens in front of a station that is not showing the catalog they expect, and
    « where is it even looking? » is the first question. The dashboard and the Catalogue
    page said it; the page carrying « Recharger le catalogue » did not.
  -->
  <p class="fact muted" data-source>{watched}</p>

  <section class="buttons" aria-label="Actions de dépannage">
    <BigButton
      label="Tester la balance"
      hint="Ce que le poste a déjà observé — le port n’est pas rouvert."
      busy={working === 'scale'}
      disabled={busy}
      onrun={() => void probe('scale', api.testScale)}
    />
    <BigButton
      label="Tester l’imprimante"
      hint="Ce que le superviseur a vu il y a moins d’une seconde."
      busy={working === 'printer'}
      disabled={busy}
      onrun={() => void probe('printer', api.testPrinter)}
    />
    <BigButton
      label="Imprimer une étiquette de test"
      hint="Une étiquette de démonstration sort de l’imprimante du poste."
      busy={working === 'label'}
      disabled={busy}
      onrun={() => void run('label', api.testLabel)}
    />
    <BigButton
      label="Réimprimer la dernière"
      hint="La dernière étiquette imprimée sort une seconde fois."
      busy={working === 'reprint'}
      disabled={busy}
      onrun={() => void run('reprint', api.reprintLast)}
    />
    <BigButton
      label="Recharger le catalogue"
      kind="write"
      hint="La veille refait tout de suite le contrôle qu’elle fait toutes les cinq secondes."
      busy={working === 'reload'}
      disabled={busy}
      onrun={() => void reload()}
    />
    <!--
      Red in both directions: cutting the scale out AND putting it back both change the way
      the station weighs, and the volunteer coming back has to read it as plainly as the
      one switching over.
    -->
    <BigButton
      label={manual ? 'Revenir à la balance' : 'Basculer en saisie manuelle'}
      kind="destructive"
      hint={manual
        ? 'Le poids sera de nouveau lu sur la balance.'
        : 'Le poids se tape à la main : le poste continue de servir sans balance.'}
      engaged={manual}
      protected
      busy={working === 'manual'}
      disabled={busy}
      onrun={() => void guarded('manual', () => api.setManualEntry(!manual))}
    />
    <BigButton
      label="J’ai changé le rouleau"
      kind="write"
      hint="Le compteur d’étiquettes repart à zéro. C’est le seul geste qui dise quelque chose de vrai du papier."
      busy={working === 'roll'}
      disabled={busy}
      onrun={() => void run('roll', api.rollChanged)}
    />
    {#if routing !== null && routing.fallback_available}
      <BigButton
        label={onFallback ? 'Revenir à l’imprimante du poste' : 'Imprimer sur l’imprimante du poste voisin'}
        kind="write"
        hint={onFallback
          ? 'Les étiquettes repartiront sur l’imprimante de ce poste.'
          : 'Les étiquettes sortiront sur l’imprimante voisine, pour cette session seulement.'}
        engaged={onFallback}
        busy={working === 'fallback'}
        disabled={busy}
        onrun={() => void run('fallback', () => api.useFallbackPrinter(!onFallback))}
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
      <p>
        Déposez ici le fichier <code>flv_{health.station}.csv</code>
        <span class="key" title="Demande le mot de passe">clé</span>
      </p>
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

  {#if setAside !== null}
    <Panel title="Le dernier fichier n’a pas pris service">
      <p class="fact">
        {importResultWord(setAside.result)} —
        {setAside.reason === '' ? 'aucun motif enregistré.' : setAside.reason}
        Le catalogue en service n’a pas changé.
      </p>
      <p class="fact muted">
        Dernier essai : {frenchDateTime(setAside.occurred_at)}.
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

  /* A key, not a red padlock: the act is possible, it only asks who you are. The word is
     written out — an icon alone teaches nothing to whoever does not know it. The badge is
     the same one the Catalogue page puts on the SAME drop zone: `POST
     /admin/api/catalog/import` is a guarded route, and an act cannot announce itself
     differently depending on the screen it is reached from. */
  .key {
    padding: 0.0625rem 0.375rem;
    border-radius: var(--radius-pill);
    background: var(--bg);
    color: var(--ink-muted);
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  /* The IRREVERSIBLE red of `Act`, without being one: a drop replaces the WHOLE grid by
     the file brought in. It stays a `<label>` — turning it into a button would break the
     file picker it wraps. */
  .choose {
    display: inline-flex;
    align-items: center;
    padding: 0 1rem;
    font-size: 1.125rem;
    font-weight: 700;
    color: var(--surface);
    background: var(--danger);
    border: 1px solid var(--danger);
    border-radius: var(--radius);
    cursor: pointer;
  }

  /* A solid fill DARKENS on hover: lightening it would drop the white ink below the 7:1
     the hue was chosen for. */
  @media (hover: hover) {
    .choose:hover {
      filter: brightness(0.92);
    }
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
