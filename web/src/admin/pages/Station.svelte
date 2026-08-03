<script lang="ts">
  import Act from '../components/Act.svelte'
  import ConfigDiffTable from '../components/ConfigDiffTable.svelte'
  import Field from '../components/Field.svelte'
  import ImportFaults from '../components/ImportFaults.svelte'
  import Maintenance from '../components/Maintenance.svelte'
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import { comparisonOf, frenchList, strippedBlocks } from '../lib/clone'
  import { handToBrowser, readExport, submitCandidate } from '../lib/config-file'
  import { valueAt } from '../lib/diff'
  import type { Draft } from '../lib/draft.svelte'
  import type { ConfigVersionDTO, FaultDTO, HealthDTO } from '../lib/dto'
  import { allowedFor, faultOf } from '../lib/faults'
  import { frenchBytes, frenchDateTime, frenchInteger } from '../lib/format'
  import type { Admin } from '../lib/session.svelte'
  import { tally } from '../lib/tally'

  /**
   * The Station page of §14.4: identity, export and import with the field-by-field diff,
   * the five restorable versions, the binary version, the paths, the disk space.
   *
   * **No SETTING demands a restart, and the Maintenance section is not one.** ADR-027
   * refuses a restart demanded by a configuration block — the hot reload of §11.4 covers
   * all of them, and that still holds. What the section at the bottom of this page offers
   * is a repair: on a station under kiosk no console is reachable, so rereading the file,
   * restarting the station and restarting the machine had no way in at all. The station
   * still never restarts ITSELF: it stops, and its supervisor starts it.
   *
   * An import APPLIES NOTHING. It shows what would change, and saving — a separate
   * gesture, on the bar below — is what applies it. A file that applied itself would be a
   * station reconfigured by a double-click.
   *
   * This is also the page of the CLONE of §11.5: one operator exports the reference
   * station, carries the file to the three others, and checks that the fingerprint on
   * screen is the same. Five things that workflow needed and did not have:
   *
   *  1. « En service » shows the configuration IN SERVICE. It used to render the DRAFT,
   *     so an operator who had typed a new serial port compared the file he had brought
   *     against his own unsaved edit, and read « nothing changes » on the one field that
   *     was about to;
   *  2. the diff RE-READS itself. It was computed once, at the moment of the import, and
   *     kept as a frozen array: after « Recopier », after a restored version and after a
   *     save, it still showed a « en service » column that existed nowhere;
   *  3. exporting is a PROTECTED act (ADR-033, §4.2): the export is the one payload that
   *     still carries the password hash. It was a bare `<a download>`, which has no way
   *     of seeing a 401 — the station refused, the browser saved the refusal under the
   *     name of an export, and the screen said nothing at all;
   *  4. importing and restoring are protected too, and they REPLAY once the password has
   *     been given: nobody has to pick the file again;
   *  5. the diff is BOUNDED and says what its cap hides. A configuration carries more
   *     than a hundred leaves and a clone differs on most of them: the table ran past the
   *     bottom of the screen and took « Recopier » with it.
   *
   * And what this page must never do again, each of which it did:
   *
   *  6. AFFIRM WHAT IT HAS NOT READ. An empty comparison and an impossible one are two
   *     states, and so are « no version » and « no answer »: an empty array was the value
   *     of both, and the reassuring sentence was the false one. Whatever is unknown says
   *     so — see {@link compared} and {@link versionsStanding};
   *  7. compare `modified_at`, which no two stations can ever share, or the WebDAV
   *     password, which neither document carries in clear (see {@link NOT_COMPARED});
   *  8. promise less than the truth. The note now names the SEVEN things §11.5 strips, and
   *     the screen names the ones the file came back empty on before « Recopier » copies
   *     that emptiness (see {@link CLONE_STRIPS});
   *  9. throw away half of a refused control: `allowed` carries the values that would
   *     work, and §11.4 step 1 asks for them out loud;
   * 10. claim a download it cannot witness — the browser decides whether and where, and
   *     the page only knows it handed the bytes over.
   */
  interface Props {
    admin: Admin
    draft: Draft
    health: HealthDTO
  }

  const { admin, draft, health }: Props = $props()

  /** How many rows of the diff are drawn. The file itself carries the rest. */
  const DIFF_SHOWN = 40

  /** How many versions are drawn. The station never keeps more than five (§11.4). */
  const VERSIONS_SHOWN = 5

  let versions = $state<ConfigVersionDTO[]>([])
  /**
   * What is KNOWN of the five versions: nothing yet, the answer, or a failed reading.
   *
   * Three states and not two. « Aucune version enregistrée : ce poste n'a jamais été
   * reconfiguré » is a fact about the station's past, and the page used to write it while
   * the reading was still in flight and again after a refusal — an empty list being the
   * value both of « none » and of « not read ».
   */
  let versionsStanding = $state<'reading' | 'read' | 'unreadable'>('reading')
  /**
   * The configuration IN SERVICE, read from `GET /admin/api/config`, which is open (§4.2).
   *
   * It is deliberately NOT `draft.config`: the draft is what the operator is editing, and
   * the whole point of the diff is to say what a file would change ON THE STATION.
   */
  let served = $state<Record<string, unknown> | null>(null)
  /** Why the configuration in service could not be read, in French. */
  let servedError = $state('')
  /** What the imported file would apply, as THE STATION itself read it. */
  let candidate = $state<Record<string, unknown> | null>(null)
  /** The name of that file, so the panel can say which one it is talking about. */
  let candidateName = $state('')
  /** What the 47 controls of §11.3 said of that file. */
  let candidateFaults = $state<FaultDTO[]>([])
  /** Which protected act is in flight, or an empty string. */
  let working = $state('')

  /**
   * The fingerprint the « En service » column was last read for.
   *
   * A plain field and not state, on purpose: it decides whether to read, and reading again
   * because it changed would be a loop. The file on disk and the configuration in force
   * carry DIFFERENT fingerprints while a confirmation is pending (§11.4), so the two can
   * legitimately never agree.
   */
  let readFor = ''

  const busy = $derived(admin.busy || working !== '')

  /**
   * The diff, DERIVED and never stored.
   *
   * This is what makes it re-read itself: it is a function of what the station serves and
   * of what the file says, so a restored version, a save from the bar below or a
   * confirmation that rolled back refreshes the table without anybody importing the file
   * again. Stored, it was the photograph of a moment that had passed.
   */
  const diff = $derived(comparisonOf(served, candidate))
  /**
   * Has the comparison HAPPENED?
   *
   * An empty diff and an impossible comparison are two different things and the page used
   * to draw them the same: `diff` is empty as soon as the configuration in service could
   * not be read, and the green « ce fichier décrit exactement la configuration en service »
   * appeared under the red banner saying that column could affirm nothing — the reassuring
   * one being the false one, in the very gesture where somebody decides not to copy.
   */
  const compared = $derived(served !== null && candidate !== null)
  /** Which blocks of §11.5 the file leaves empty while the station has something there. */
  const stripped = $derived(strippedBlocks(served, candidate))
  const shownDiff = $derived(diff.slice(0, DIFF_SHOWN))
  const shownVersions = $derived(versions.slice(0, VERSIONS_SHOWN))

  const diffTally = $derived(
    tally(shownDiff.length, diff.length, 'champ qui change', 'champs qui changent'),
  )
  const versionTally = $derived(
    tally(shownVersions.length, versions.length, 'version enregistrée', 'versions enregistrées'),
  )

  /** « Recopier ces 137 champs dans le brouillon », and the singular that goes with it. */
  const adoptLabel = $derived(
    diff.length > 1
      ? `Recopier ces ${frenchInteger(diff.length)} champs dans le brouillon`
      : 'Recopier ce champ dans le brouillon',
  )

  void loadVersions()

  /**
   * Re-reads the configuration in service whenever the station's fingerprint moves.
   *
   * The eight characters change on every save, every restore and every rollback, and they
   * are polled every three seconds by the dashboard: watching them is how this page learns
   * that what it is comparing against is no longer true.
   */
  $effect(() => {
    const fingerprint = health.config_fingerprint
    if (fingerprint === readFor) return
    readFor = fingerprint
    void readServed()
  })

  /**
   * Reads the five restorable versions.
   *
   * A refusal does NOT fall back on the empty list: `?? []` turned « je n'ai pas pu lire »
   * into « ce poste n'a jamais été reconfiguré », which is a statement about the station
   * nobody had the right to make out of a failed reading.
   */
  async function loadVersions(): Promise<void> {
    const read = await admin.load(api.fetchVersions)
    if (read === null) {
      versionsStanding = 'unreadable'
      return
    }
    versions = read
    versionsStanding = 'read'
  }

  /**
   * Reads the configuration in service.
   *
   * It does not go through {@link Admin.load}: that one clears the sentence of the last
   * act on success, and this read happens on its own, three seconds after anything. A
   * refusal read here would have erased the refusal an operator was still reading (§5.1).
   */
  async function readServed(): Promise<void> {
    try {
      served = (await api.fetchConfig()).config
      servedError = ''
    } catch (failure) {
      // What was read before is DROPPED. This reading is triggered by the fingerprint
      // moving, so the document still held is precisely the one that is no longer in
      // service: a « En service » column drawn from it would be an affirmation about the
      // station made out of a failed reading. Nothing compares until a reading succeeds.
      served = null
      servedError = failure instanceof Error ? failure.message : 'Le poste n’a pas répondu.'
    }
  }

  /**
   * Exports the configuration, as a PROTECTED act (ADR-033).
   *
   * `GET /admin/api/config/export` is the one read the station keeps behind the password,
   * because it is the one payload that still carries the password hash (§11.5). Two bare
   * `<a download>` fetched it, and an anchor cannot see a refusal: on an expired session
   * the browser saved a file named like an export and holding « Session expirée ».
   *
   * @param withHardware - whether the serial port, the print queue and the network travel.
   */
  async function exportConfig(withHardware: boolean): Promise<void> {
    working = withHardware ? 'export-all' : 'export-clone'
    admin.notice = ''
    admin.actionError = ''
    try {
      const file = await admin.protect(() => readExport(health.station, withHardware))
      if (file === null) return
      handToBrowser(file.name, file.blob)
      // What this page can attest, and nothing beyond: the bytes left the station and the
      // browser has them. Whether the file is written, where, and under which name is the
      // browser's own business — it may ask, it may refuse, and the page has no way of
      // learning either. « est enregistré par ce navigateur » claimed the end of a story
      // it only saw the beginning of.
      admin.notice = `${file.name} est remis au navigateur : c’est lui qui l’enregistre, voyez ses téléchargements.`
    } finally {
      working = ''
    }
  }

  /**
   * Reads a configuration file and shows what it would change, field by field (§14.4).
   *
   * The file goes to the station rather than being compared in the browser, and that is a
   * PROTECTED act (ADR-033). The station strips what must not travel — the two secrets and
   * the station number (§11.5) — and passes the 47 controls of §11.3 over it, so the diff
   * shows what would REALLY be applied and the refusals are known before anybody copies
   * anything.
   *
   * @param file - what the file chooser gave.
   */
  async function inspect(file: File | null | undefined): Promise<void> {
    if (file === null || file === undefined) return
    admin.notice = ''
    admin.actionError = ''
    let parsed: unknown
    try {
      parsed = JSON.parse(await file.text())
    } catch {
      admin.actionError = `${file.name} n’est pas un fichier JSON lisible.`
      return
    }
    working = 'import'
    try {
      const read = await admin.protect(() =>
        submitCandidate(parsed as Record<string, unknown>),
      )
      if (read === null) return
      candidate = read.config
      candidateName = file.name
      candidateFaults = read.faults
      // Against what the station serves NOW, and not against what it served when the page
      // was opened: a version may have been restored from another screen since.
      await readServed()
      admin.notice = sentenceOfImport(file.name)
    } finally {
      working = ''
    }
  }

  /**
   * What one line says of the file that was just read — the three states, not two.
   *
   * The third one is the reason this function exists: with the configuration in service
   * unread, « décrit exactement la configuration en service » was written from a diff that
   * is empty because nothing was compared, and it contradicted the red banner three lines
   * above it.
   *
   * @param name - the name of the file that was read.
   */
  function sentenceOfImport(name: string): string {
    if (!compared) {
      return `${name} est lu, mais la configuration en service ne l’est pas : rien ne peut être comparé.`
    }
    if (diff.length === 0) return `${name} décrit la même configuration que celle en service.`
    return `${name} est lu. Rien n’est appliqué : relisez le tableau.`
  }

  /**
   * Copies the imported file into the draft. Saving stays a separate gesture.
   *
   * The configuration in service is re-read afterwards, so the table never becomes the
   * snapshot of the import: what it shows is always the distance between the file and the
   * station, and that distance only closes when somebody saves.
   */
  async function adopt(): Promise<void> {
    if (candidate === null) return
    const file = candidate
    // A copy, because `draft.set` writes into the draft while the diff is derived: taking
    // the fields first keeps the loop reading one single state of the comparison.
    const fields = [...diff]
    for (const entry of fields) draft.set(entry.path, valueAt(file, entry.path))
    admin.actionError = ''
    admin.notice =
      'Le fichier est recopié dans le brouillon. Rien n’est appliqué avant « Enregistrer ».'
    await readServed()
  }

  /**
   * Puts one of the five backups back in service, as a PROTECTED act (ADR-033).
   *
   * It is the one gesture of this page that changes the station on the spot, hence its 72
   * px: everything else fills a draft that somebody still has to save.
   *
   * @param version - which backup, 1 being the most recent.
   */
  async function restore(version: number): Promise<void> {
    working = `restore-${String(version)}`
    admin.notice = ''
    admin.actionError = ''
    try {
      const done = await admin.protect(() => api.restoreVersion(version))
      if (done === null) return
      await draft.load()
      await loadVersions()
      await readServed()
      await admin.refresh()
      // Written last: `draft.load` and `admin.load` clear what an act left on screen.
      admin.notice = `La version ${String(version)} est remise en service.`
    } finally {
      working = ''
    }
  }

</script>

<div class="pages">
  <Panel title="Identité du poste">
    <!--
      The three fields carry their own refusal: the 47 controls of §11.3 name a KEY, and a
      page that shows only the global banner leaves somebody hunting for which one.
    -->
    <Field
      label="Numéro du poste"
      path="station.number"
      kind="number"
      value={draft.text('station.number')}
      fault={faultOf(draft, 'station.number')}
      allowed={allowedFor(draft, 'station.number')}
      hint="C’est de lui que dérive le nom du fichier de catalogue attendu, flv_<n>.csv."
      onchange={(value) => draft.set('station.number', Number(value))}
    />
    <Field
      label="Nom du poste"
      path="station.name"
      value={draft.text('station.name')}
      fault={faultOf(draft, 'station.name')}
      allowed={allowedFor(draft, 'station.name')}
      hint="Ce que lit un bénévole : « Poste 2 — fruits »."
      onchange={(value) => draft.set('station.name', value)}
    />
    <Field
      label="Coopérative"
      path="station.coop"
      value={draft.text('station.coop')}
      fault={faultOf(draft, 'station.coop')}
      allowed={allowedFor(draft, 'station.coop')}
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
    note="Pour installer un autre poste : ce fichier emporte les tarifs, les garde-fous, l’étiquette, les catégories, et les réglages du matériel que les quatre postes partagent — le décalage d’étiquette, le noircissement, la vitesse, le débit de la balance. Reste ici ce qui désigne ce poste-ci ou ce magasin — le mot de passe, le code de secours, le numéro et le nom du poste, le port de la balance, la file d’impression, l’adresse du partage et son compte, le chemin des images et le réseau."
  >
    <div class="actions">
      <Act
        label="Exporter tout"
        protected
        busy={working === 'export-all'}
        disabled={busy}
        onrun={() => void exportConfig(true)}
      />
      <Act
        label="Exporter sans le matériel"
        protected
        busy={working === 'export-clone'}
        disabled={busy}
        onrun={() => void exportConfig(false)}
      />
      <!--
        A LABEL and not a button — turning it into one would break the file picker it
        wraps — so it copies a family of `Act` by hand. It gets the press feedback of a
        command all the same: one that answers nothing under the finger reads as a dead
        page, whatever element it happens to be made of (§3.2).

        The family is the NEUTRAL one, and it took a red to see why. Red means « this does
        not undo itself in one click », and `POST /admin/api/config/import` applies
        strictly nothing: it validates, and answers the diff a human then reads. The button
        that DOES write — « Recopier » below — is blue, so the two were painted the wrong
        way round from each other. An unearned red wears out the one that is earned, and
        this page carries a real one ten lines further down, on the restore.
      -->
      <label class="choose" class:working={working === 'import'} class:off={busy}>
        {working === 'import' ? 'Lecture du fichier…' : 'Importer un fichier'}
        <span class="key" title="Demande le mot de passe">clé</span>
        <input
          type="file"
          accept=".json,application/json"
          disabled={busy}
          onchange={(event) => void inspect(event.currentTarget.files?.item(0))}
        />
      </label>
    </div>
    <p class="fact muted">
      L’export emporte encore l’empreinte du mot de passe : c’est la seule lecture que le
      poste garde derrière la clé. L’import, lui, est lu PAR LE POSTE, qui écarte les deux
      secrets et le numéro de poste avant de dire ce qui changerait.
    </p>

    {#if servedError !== ''}
      <p class="fact warned" data-served-failure>
        La configuration en service n’a pas pu être lue : {servedError} La colonne
        « En service » ne peut donc rien affirmer.
      </p>
    {/if}

    {#if candidate !== null}
      <p class="fact filename" data-filename>Fichier lu : {candidateName}</p>

      {#if candidateFaults.length > 0}
        <ImportFaults faults={candidateFaults} />
      {/if}

      {#if !compared}
        <p class="fact warned" data-uncompared>
          Ce fichier n’a été comparé à rien : la configuration en service n’a pas pu être
          lue. Ni « identique », ni « n champs changent » — ce que ce fichier changerait
          sur ce poste reste inconnu.
        </p>
      {:else if diff.length === 0}
        <p class="fact same" data-same>
          Ce fichier décrit la même configuration que celle en service : il n’y a rien à
          recopier. C’est ce qu’on veut lire à la fin d’un clonage. Deux champs ne sont pas
          comparés : la date du dernier enregistrement, que chaque poste écrit lui-même, et
          le mot de passe du catalogue, qu’aucun des deux ne porte en clair.
        </p>
      {:else}
        <p class="fact muted" data-tally="diff">
          {diffTally}
          {#if diff.length > shownDiff.length}
            Les autres sont dans le fichier, et « Recopier » les prend tous.
          {/if}
        </p>
        <ConfigDiffTable rows={shownDiff} />
        {#if stripped.length > 0}
          <p class="fact warned" data-stripped>
            Ce fichier ne porte pas {frenchList(stripped.map((block) => block.name))} :
            l’export sans le matériel les retire, et l’import ne remet que le numéro du
            poste. Les lignes correspondantes sont VIDES ci-dessus, et
            « Recopier » recopie ce vide dans le brouillon.
          </p>
        {/if}
        <div class="actions">
          <!-- Blue and not red: what is copied over goes into the DRAFT, and nothing is
               in service until « Enregistrer » has been touched. -->
          <Act kind="write" label={adoptLabel} disabled={busy} onrun={() => void adopt()} />
        </div>
        <p class="fact muted">
          Recopier n’applique rien : les valeurs entrent dans le brouillon, et c’est
          « Enregistrer » qui les met en service.
        </p>
      {/if}
    {/if}
  </Panel>

  <Panel
    title="Cinq versions restaurables"
    note="Chaque enregistrement fait tourner les versions : la plus récente est la 1."
  >
    {#if versionsStanding === 'reading'}
      <p class="fact muted" data-versions="reading">Lecture des versions enregistrées…</p>
    {:else if versionsStanding === 'unreadable'}
      <p class="fact warned" data-versions="unreadable">
        Les versions enregistrées n’ont pas pu être lues : cette liste ne dit donc rien de
        ce que ce poste garde. Ce n’est pas « aucune version ».
      </p>
    {:else if versions.length === 0}
      <p class="fact" data-versions="none">
        Aucune version enregistrée : ce poste n’a jamais été reconfiguré.
      </p>
    {:else}
      <p class="fact muted" data-tally="versions">
        {versionTally} Le poste n’en garde jamais plus de cinq.
      </p>
      <div class="scroll">
        <ul class="rows">
          {#each shownVersions as version (version.version)}
            <li>
              <span class="what">version {frenchInteger(version.version)}</span>
              <span class="detail">{frenchDateTime(version.modified_at)}</span>
              <span class="detail">{version.config_fingerprint}</span>
              <Act
                kind="destructive"
                label="Remettre cette version en service"
                protected
                busy={working === `restore-${String(version.version)}`}
                disabled={busy}
                onrun={() => void restore(version.version)}
              />
            </li>
          {/each}
        </ul>
      </div>
      <p class="fact muted">
        Remettre une version en service remplace la configuration du poste sur-le-champ, et
        ce qui n’a pas été enregistré est perdu : c’est le seul geste de cette page qui
        change le poste tout de suite, et le seul qui garde ses 72 px.
      </p>
    {/if}
  </Panel>

  <Maintenance {admin} />
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

  .muted {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  .filename {
    margin-top: 1rem;
    font-weight: 700;
  }

  .identity {
    margin: 1rem 0 0;
    padding: 0.75rem 1rem;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.25rem 1rem;
    font-size: 1.0625rem;
    background: var(--bg);
    border-radius: var(--radius);
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
    margin: 0.75rem 0 0;
  }

  /*
   * The file chooser, and the only control of this page that is not an `<Act>`: it is a
   * `<label>` wrapping an `<input type="file">`, which a button cannot replace. It copies
   * the NEUTRAL family of `Act` by hand — the tokens and the 44 px both, because in that
   * component the family carries the height as much as the colour: 72 px belong to
   * `destructive`, and reading a file into a diff is not one.
   */
  .choose {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    /* The 44 px of the administration's form controls (ADR-033), spelled the way `.act`
       spells them: this label is a command of the same page and the same density. */
    min-height: 2.75rem;
    padding: 0 1rem;
    font-size: 1.0625rem;
    font-weight: 700;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-1);
    cursor: pointer;
    transition:
      transform var(--tap) var(--ease),
      background-color var(--tap) var(--ease),
      border-color var(--tap) var(--ease),
      box-shadow var(--slide) var(--ease);
  }

  /*
   * `button:active` of app.css does not reach a label.
   *
   * It repeats that rule's `:not(:disabled)` AND adds `.off`, because a `<label>` has no
   * disabled state of its own: what is disabled is the `<input>` inside it. Without the
   * second guard the label went on shrinking under the finger while the click opened
   * nothing at all — an answer to a dead gesture, which is the exact opposite of what §3.2
   * asks this feedback for.
   */
  .choose:active:not(.off) {
    transform: scale(0.975);
  }

  /* The neutral family answers the pointer with its BORDER, exactly like `.read` of
     `Act`: there is no solid background here to darken. */
  @media (hover: hover) {
    .choose:hover:not(.off) {
      border-color: var(--ink-muted);
      box-shadow: var(--shadow-2);
    }
  }

  .choose.off {
    opacity: 0.5;
    box-shadow: none;
    cursor: default;
  }

  /* The chooser that is reading stays FULLY legible: it is the one being watched. */
  .choose.working {
    opacity: 1;
    border-color: var(--waiting);
  }

  .choose input {
    display: none;
  }

  /* A key, not a red padlock: the act is possible, it only asks who you are. The word is
     written out — an icon alone teaches nothing to whoever does not know it (§14.4).
     The acts carry their own; this one belongs to the chooser, which is a label.

     It takes the tokens `Act` gives a NEUTRAL button, and not the inverted pair: the
     inversion exists so the badge does not dissolve into a solid fill, and there is no
     longer a solid fill to dissolve into. */
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

  /* The good news of a clone: the file and the station say the same thing (§11.5). */
  .same {
    padding: 0.5rem 0.75rem;
    border-left: 0.375rem solid var(--ready);
    border-radius: var(--radius-sm);
    background: var(--ready-wash);
  }

  .warned {
    padding: 0.5rem 0.75rem;
    border-left: 0.375rem solid var(--fault);
    border-radius: var(--radius-sm);
    background: var(--fault-wash);
  }

  /*
   * Every list of this page is BOUNDED and scrolls inside its own box.
   *
   * A configuration carries more than a hundred and thirty leaves, and a station cloned
   * from another differs on most of them: the table pushed « Recopier » a full screen
   * below the fold, and the body of the page never scrolls sideways for it either.
   */
  .scroll {
    max-height: 24rem;
    overflow: auto;
    border: 1px solid var(--border-soft);
    border-radius: var(--radius-lg);
    background: var(--bg);
  }

  .rows {
    margin: 0;
    padding: 0 0.75rem;
    list-style: none;
  }

  .rows li {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: center;
    padding: 0.5rem 0;
    border-top: 1px solid var(--border);
    font-size: 1.0625rem;
  }

  .rows li:first-child {
    border-top: none;
  }

  .what {
    font-weight: 700;
  }

  .detail {
    color: var(--ink-muted);
  }
</style>
