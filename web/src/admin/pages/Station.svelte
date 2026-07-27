<script lang="ts">
  import Field from '../components/Field.svelte'
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import { AdminError } from '../lib/api'
  import { differences, valueAt } from '../lib/diff'
  import type { Difference } from '../lib/diff'
  import type { Draft } from '../lib/draft.svelte'
  import type { ConfigVersionDTO, FaultDTO, HealthDTO, ProblemDTO } from '../lib/dto'
  import { frenchBytes, frenchDateTime, frenchInteger } from '../lib/format'
  import type { Admin } from '../lib/session.svelte'

  /**
   * The Station page of §14.4: identity, export and import with the field-by-field diff,
   * the five restorable versions, the binary version, the paths, the disk space.
   *
   * **There is no « restart » button.** No configuration block demands one (§11.4,
   * ADR-027): the only legitimate restart is the one the service manager triggers on its
   * own, and a button would have made it an ordinary gesture.
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
   *  7. compare `modified_at`, which no two stations can ever share (see
   *     {@link NOT_COMPARED});
   *  8. promise less than the truth. The note now names the SIX things §11.5 strips, and
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

  /** How many refused controls are drawn, out of the 45 of §11.3. */
  const FAULTS_SHOWN = 20

  /** How many versions are drawn. The station never keeps more than five (§11.4). */
  const VERSIONS_SHOWN = 5

  /**
   * The one key the comparison LEAVES OUT, and why it has to.
   *
   * `modified_at` is stamped by whoever writes the file — `writeConfig` fills it from the
   * station's own clock on every save — so the file carried from station 1 holds the
   * instant station 1 was configured and station 2 holds its own. Compared, the two ALWAYS
   * differ: the table announced « 1 champ qui change » and offered « Recopier ce champ »
   * at the exact moment §11.5 wanted somebody to read « rien ne change ». The fingerprint
   * of §11.5 clears the same field for the same reason (`Config.Fingerprint`), and copying
   * it into the draft would change nothing anyway — the next save overwrites it.
   */
  const NOT_COMPARED = new Set(['modified_at'])

  /**
   * What a hardware-free export does NOT carry, and the French name of each.
   *
   * `Config.Export(false)` clears six things — the station number and name, the three
   * option maps and the whole network block — and the import puts back only the number and
   * the two secrets. Everything else comes back EMPTY, so those rows of the diff hold
   * nothing, and « Recopier » copies emptiness as faithfully as it copies a value: the
   * WebDAV account of the catalog is in that list. This is what the screen has to name
   * before anybody presses the button.
   */
  const CLONE_STRIPS: { path: string; name: string }[] = [
    { path: 'station.name', name: 'le nom du poste' },
    { path: 'network.listen', name: 'l’adresse d’écoute' },
    { path: 'scale.options', name: 'les réglages de la balance' },
    { path: 'printer.options', name: 'les réglages de l’imprimante' },
    { path: 'catalog.options', name: 'la source du catalogue, compte compris' },
  ]

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
  /** What the 45 controls of §11.3 said of that file. */
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
  const shownFaults = $derived(candidateFaults.slice(0, FAULTS_SHOWN))
  const shownVersions = $derived(versions.slice(0, VERSIONS_SHOWN))

  const diffTally = $derived(
    tally(shownDiff.length, diff.length, 'champ qui change', 'champs qui changent'),
  )
  const faultTally = $derived(
    tally(
      shownFaults.length,
      candidateFaults.length,
      'contrôle refuse une clé',
      'contrôles refusent une clé',
    ),
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
   * What the file would change on the station, field by field.
   *
   * @param station - the configuration in service, or null when it could not be read.
   * @param file - what the station read in the imported file, or null before any import.
   * @returns the fields that differ, minus the ones {@link NOT_COMPARED} names. It is
   * EMPTY when there is nothing to compare against, which is why nothing reads « no
   * difference » out of its length alone — see {@link compared}.
   */
  function comparisonOf(
    station: Record<string, unknown> | null,
    file: Record<string, unknown> | null,
  ): Difference[] {
    if (station === null || file === null) return []
    return differences(station, file).filter((entry) => !NOT_COMPARED.has(entry.path))
  }

  /**
   * The blocks of §11.5 the file carries EMPTY while the station has a value there.
   *
   * @param station - the configuration in service.
   * @param file - what the station read in the imported file.
   */
  function strippedBlocks(
    station: Record<string, unknown> | null,
    file: Record<string, unknown> | null,
  ): { path: string; name: string }[] {
    if (station === null || file === null) return []
    const inService = station
    const inFile = file
    return CLONE_STRIPS.filter(
      (block) => isBlank(inFile, block.path) && !isBlank(inService, block.path),
    )
  }

  /**
   * True when a document carries nothing at that path: absent, null, empty text, empty map.
   *
   * The four forms all come out of one export: `Config.Export(false)` writes `""` on the
   * station name, `null` on the three option maps, and the zero value of the network block.
   *
   * @param document - the document to read.
   * @param path - the dotted path of the key.
   */
  function isBlank(document: Record<string, unknown>, path: string): boolean {
    const value = valueAt(document, path)
    if (value === undefined || value === null || value === '') return true
    if (typeof value !== 'object' || Array.isArray(value)) return false
    return Object.keys(value).length === 0
  }

  /** « le nom du poste, l’adresse d’écoute et les réglages de la balance ». */
  function frenchList(names: string[]): string {
    if (names.length < 2) return names.join('')
    return `${names.slice(0, -1).join(', ')} et ${names[names.length - 1] ?? ''}`
  }

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
      const file = await admin.protect(() => readExport(withHardware))
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

  /** One exported configuration, and the name it is saved under. */
  interface ExportedFile {
    name: string
    blob: Blob
  }

  /**
   * Fetches one export, and turns a refusal into what {@link Admin.protect} answers.
   *
   * The call lives here and not in `lib/api.ts` because it is the only one of the contract
   * that answers a FILE: everything in that module reads a body and parses it as JSON,
   * which is exactly what must not happen to a document somebody is about to save.
   *
   * @param withHardware - what the `hardware` parameter of §11.5 selects.
   */
  async function readExport(withHardware: boolean): Promise<ExportedFile> {
    const route = `/admin/api/config/export?hardware=${withHardware ? '1' : '0'}`
    const response = await fetch(route, { headers: { accept: 'application/json' } })
    if (!response.ok) {
      throw new AdminError(response.status, refusalOf(await response.text(), 'L’export'))
    }
    return { name: exportName(withHardware), blob: await response.blob() }
  }

  /**
   * The name an export is saved under, and why the screen chooses it.
   *
   * The station names every export `config-export.json`, so the clone of §11.5 — one
   * operator, one file, three other stations — ends with four identically named files in
   * one folder, two of which do not carry the hardware and must not be applied as if they
   * did. §11.5 names the file after the station it came from and the day it was taken.
   *
   * @param withHardware - whether this is the complete export.
   */
  function exportName(withHardware: boolean): string {
    const variant = withHardware ? '' : '-sans-materiel'
    return `config-poste${String(health.station)}${variant}-${isoDay()}.json`
  }

  /** Today as `2026-07-27`: the one date form that sorts right in a folder listing. */
  function isoDay(): string {
    const now = new Date()
    const month = String(now.getMonth() + 1).padStart(2, '0')
    const day = String(now.getDate()).padStart(2, '0')
    return `${String(now.getFullYear())}-${month}-${day}`
  }

  /**
   * Hands one file to the browser's own download.
   *
   * The anchor is created, clicked and dropped: it exists for the length of one gesture
   * and never sits in the page — a permanent `<a download>` is precisely what could not
   * see the station refuse.
   *
   * The object URL is released ON THE NEXT TURN of the loop, never on this one. A click
   * only QUEUES the download; a browser that reads the address afterwards finds nothing
   * and cancels it, and the sentence on screen would have announced an export nobody
   * received. One turn is enough, and it is what keeps the URL from leaking either.
   *
   * @param name - the name the file is saved under.
   * @param blob - the bytes the station answered.
   */
  function handToBrowser(name: string, blob: Blob): void {
    const address = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = address
    link.download = name
    document.body.appendChild(link)
    link.click()
    link.remove()
    setTimeout(() => {
      URL.revokeObjectURL(address)
    }, 0)
  }

  /** What `POST /admin/api/config/import` answers of the file it was given. */
  interface InspectedConfig {
    config: Record<string, unknown>
    faults: FaultDTO[]
  }

  /**
   * Has THE STATION read the file, and turns a refusal into what `protect` answers.
   *
   * `changed_blocks` travels in the same body and is deliberately not drawn: the twelve
   * block names are English tokens of `internal/web/config.go`, and the field-by-field
   * diff §14.4 asks for says strictly more than « le bloc printer a changé » — which does
   * not tell whether it is the print queue or the darkness.
   *
   * @param contents - the parsed file.
   */
  async function submitCandidate(contents: Record<string, unknown>): Promise<InspectedConfig> {
    const response = await fetch('/admin/api/config/import', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(contents),
    })
    const raw = await response.text()
    if (!response.ok) throw new AdminError(response.status, refusalOf(raw, 'L’import'))
    return JSON.parse(raw) as InspectedConfig
  }

  /**
   * Reads a configuration file and shows what it would change, field by field (§14.4).
   *
   * The file goes to the station rather than being compared in the browser, and that is a
   * PROTECTED act (ADR-033). The station strips what must not travel — the two secrets and
   * the station number (§11.5) — and passes the 45 controls of §11.3 over it, so the diff
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

  /**
   * The French sentence of a refusal: the station's own, when it wrote one.
   *
   * @param raw - the body of the refused answer.
   * @param what - the act, for the sentence of last resort.
   */
  function refusalOf(raw: string, what: string): string {
    try {
      const problem = JSON.parse(raw) as ProblemDTO | null
      if (typeof problem?.message === 'string' && problem.message !== '') return problem.message
    } catch {
      // The station answered something that is not a problem document.
    }
    return `${what} a été refusé par le poste.`
  }

  /**
   * The message of the control that refused this key, when there is one (§11.3).
   *
   * The three editable fields of this page had none: a save refused on `station.number`
   * lit the global banner « Cette configuration ne peut pas être appliquée » and left the
   * offending field looking perfectly fine.
   *
   * @param path - the dotted path of the key.
   */
  function faultOf(path: string): string {
    return draft.faults.find((fault) => fault.field === path)?.message ?? ''
  }

  /**
   * The values a control named as acceptable, when it knows them (§11.4, step 1).
   *
   * @param path - the dotted path of the key.
   */
  function allowedFor(path: string): string[] {
    return draft.faults.find((fault) => fault.field === path)?.allowed ?? []
  }

  /**
   * « 40 lignes affichées sur 137 champs qui changent. », or « 16 champs qui changent. ».
   *
   * A cap that does not say what it hides is a lie by omission, and this page had the
   * worst kind: a diff of a hundred and thirty rows with the button that acts on it below
   * the fold.
   *
   * @param shown - how many rows are drawn.
   * @param total - how many there are.
   * @param singular - what one of them is called.
   * @param plural - what several of them are called.
   */
  function tally(shown: number, total: number, singular: string, plural: string): string {
    const noun = total > 1 ? plural : singular
    if (shown >= total) return `${frenchInteger(total)} ${noun}.`
    return `${frenchInteger(shown)} lignes affichées sur ${frenchInteger(total)} ${noun}.`
  }
</script>

<div class="pages">
  <Panel title="Identité du poste">
    <!--
      The three fields carry their own refusal: the 45 controls of §11.3 name a KEY, and a
      page that shows only the global banner leaves somebody hunting for which one.
    -->
    <Field
      label="Numéro du poste"
      path="station.number"
      kind="number"
      value={draft.text('station.number')}
      fault={faultOf('station.number')}
      allowed={allowedFor('station.number')}
      hint="C’est de lui que dérive le nom du fichier de catalogue attendu, flv_<n>.csv."
      onchange={(value) => draft.set('station.number', Number(value))}
    />
    <Field
      label="Nom du poste"
      path="station.name"
      value={draft.text('station.name')}
      fault={faultOf('station.name')}
      allowed={allowedFor('station.name')}
      hint="Ce que lit un bénévole : « Poste 2 — fruits »."
      onchange={(value) => draft.set('station.name', value)}
    />
    <Field
      label="Coopérative"
      path="station.coop"
      value={draft.text('station.coop')}
      fault={faultOf('station.coop')}
      allowed={allowedFor('station.coop')}
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
    note="L’export sans le matériel est ce qui sert à cloner un poste (§11.5). Restent sur place : le mot de passe, le code de secours, le numéro et le nom du poste, les réglages de la balance, ceux de l’imprimante, la source du catalogue et le réseau. Voyage ce que les quatre postes doivent avoir en commun : tarifs, garde-fous, étiquette, catégories."
  >
    <div class="actions">
      <button type="button" class="act" disabled={busy} onclick={() => void exportConfig(true)}>
        {working === 'export-all' ? 'Export en cours…' : 'Exporter tout'}
        <span class="key" title="Demande le mot de passe">clé</span>
      </button>
      <button type="button" class="act" disabled={busy} onclick={() => void exportConfig(false)}>
        {working === 'export-clone' ? 'Export en cours…' : 'Exporter sans le matériel'}
        <span class="key" title="Demande le mot de passe">clé</span>
      </button>
      <!--
        A LABEL and not a button, and it gets the same press feedback all the same: a
        command that answers nothing under the finger reads as a dead page, whatever
        element it happens to be made of (§3.2).
      -->
      <label class="act" class:working={working === 'import'} class:off={busy}>
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
      poste garde derrière la clé (§11.5, ADR-033). L’import, lui, est lu PAR LE POSTE, qui
      écarte les deux secrets et le numéro de poste avant de dire ce qui changerait.
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
        <div class="faults" data-faults>
          <p class="fact">Ce fichier serait refusé en l’état : {faultTally}</p>
          <!--
            Deliberately UNKEYED: one field carries as many controls as it breaks, so a
            `each` keyed by `field` would throw `each_key_duplicate` on the first file that
            breaks two rules on the same key, and take the whole screen with it.
          -->
          <ul>
            {#each shownFaults as fault}
              <li>
                <code>{fault.field}</code>
                {fault.message}
                <!--
                  Half the control was being thrown away: `allowed` carries the values that
                  WOULD work, and §11.4 step 1 asks for them in so many words. « Ce port
                  n'existe pas sur ce poste » without the list of the ports that do exist
                  is a refusal nobody can act on — least of all here, where the file is not
                  correctable field by field.
                -->
                {#if fault.allowed !== undefined && fault.allowed.length > 0}
                  <span class="allowed">Valeurs acceptées : {fault.allowed.join(', ')}.</span>
                {/if}
              </li>
            {/each}
          </ul>
          <p class="fact muted">
            Recopier reste possible : les valeurs entrent dans le brouillon, où elles se
            corrigent champ par champ avant l’enregistrement.
          </p>
        </div>
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
          recopier. C’est ce qu’on veut lire à la fin d’un clonage (§11.5). La date du
          dernier enregistrement n’est pas comparée — chaque poste écrit la sienne, et
          l’empreinte de §11.5 l’ignore pour la même raison.
        </p>
      {:else}
        <p class="fact muted" data-tally="diff">
          {diffTally}
          {#if diff.length > shownDiff.length}
            Les autres sont dans le fichier, et « Recopier » les prend tous.
          {/if}
        </p>
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
              {#each shownDiff as entry (entry.path)}
                <tr>
                  <td><code>{entry.path}</code></td>
                  <td>{entry.before}</td>
                  <td>{entry.after}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
        {#if stripped.length > 0}
          <p class="fact warned" data-stripped>
            Ce fichier ne porte pas {frenchList(stripped.map((block) => block.name))} :
            l’export sans le matériel les retire (§11.5), et l’import ne remet que le
            numéro du poste. Les lignes correspondantes sont VIDES ci-dessus, et
            « Recopier » recopie ce vide dans le brouillon.
          </p>
        {/if}
        <div class="actions">
          <button type="button" class="act" disabled={busy} onclick={() => void adopt()}>
            {adoptLabel}
          </button>
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
              <button
                type="button"
                class="act danger touch-target"
                disabled={busy}
                onclick={() => void restore(version.version)}
              >
                {working === `restore-${String(version.version)}`
                  ? 'En cours…'
                  : 'Remettre cette version en service'}
                <span class="key" title="Demande le mot de passe">clé</span>
              </button>
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
   * A form control of the administration is 44 px and not the 72 px of the customer grid:
   * this page is driven with a mouse (ADR-033). What keeps its 72 px is what cannot be
   * undone in one click — putting a backup back in service.
   */
  .act {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    height: 2.75rem;
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
   * The file chooser is a label: `button:active` of app.css does not reach it.
   *
   * It repeats that rule's `:not(:disabled)` AND adds `.off`, because a `<label>` has no
   * disabled state of its own: what is disabled is the `<input>` inside it. Without the
   * second guard the label went on shrinking under the finger while the click opened
   * nothing at all — an answer to a dead gesture, which is the exact opposite of what §3.2
   * asks this feedback for.
   */
  .act:active:not(:disabled):not(.off) {
    transform: scale(0.975);
  }

  @media (hover: hover) {
    .act:hover:not(:disabled) {
      border-color: var(--ink-muted);
      box-shadow: var(--shadow-2);
    }
  }

  .act:disabled,
  .act.off {
    opacity: 0.5;
    box-shadow: none;
    cursor: default;
  }

  .act.working {
    border-color: var(--waiting);
    background: var(--waiting-wash);
  }

  .act.danger {
    /* Height comes from `.touch-target`, which imposes 72 px on this one alone. */
    height: auto;
    border-color: var(--fault);
    background: var(--fault-wash);
  }

  .act input {
    display: none;
  }

  /* A key, not a red padlock: the act is possible, it only asks who you are. The word is
     written out — an icon alone teaches nothing to whoever does not know it (§14.4). */
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

  /* A file that would be refused is not a failure of the station: it is something to fix
     in the draft before saving, hence the warning wash and not the fault one. */
  .faults {
    margin-top: 0.75rem;
    padding: 0.25rem 1rem 0.5rem;
    border-left: 0.375rem solid var(--warning);
    border-radius: var(--radius);
    background: var(--warning-wash);
  }

  .faults ul {
    margin: 0;
    padding-left: 1.25rem;
    font-size: 1.0625rem;
  }

  /* The values that WOULD work, on their own line: they are what somebody types next. */
  .allowed {
    display: block;
    color: var(--ink-muted);
    font-size: 1rem;
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

  table {
    border-collapse: collapse;
    width: 100%;
    font-size: 1.0625rem;
  }

  th,
  td {
    padding: 0.375rem 0.75rem;
    text-align: left;
    border-bottom: 1px solid var(--border);
  }

  th {
    position: sticky;
    top: 0;
    color: var(--ink-muted);
    font-size: 1rem;
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
