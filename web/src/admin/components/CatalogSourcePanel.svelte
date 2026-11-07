<script lang="ts">
  import Field from './Field.svelte'
  import Panel from './Panel.svelte'
  import type { Draft } from '../lib/draft.svelte'
  import { expectedCatalogFile, sharesOneFile } from '../lib/catalog-file'
  import { faultOf } from '../lib/faults'
  import { labelOf } from '../lib/fields'

  /**
   * Where the station goes looking for its catalog, and the settings that source owns.
   *
   * It sits at the top of the Catalogue page because where a catalog comes from is read
   * before what it delivered. The directory field only appears under the source that
   * watches one, and the address only under the source that queries one — an empty field
   * under a source that ignores it is an invitation to fill it in, and the station would
   * refuse the save (§11.3, controls 39 and 47). The file name and what happens to the
   * file once read belong to BOTH sources, so they show under either.
   */
  interface Props {
    /** The configuration document: this panel edits the `catalog` block of it. */
    draft: Draft
    /** The number of this station: the file it watches for is named after it. */
    station: number
  }

  const { draft, station }: Props = $props()

  /**
   * The settings each source owns OUTRIGHT.
   *
   * Switching source wipes them, because the station refuses their mere PRESENCE under
   * the other one: no account and no password to read a directory one owns (control 39),
   * no directory of this machine behind an address (control 47). Without that clean-up,
   * the one gesture this panel exists to offer came back as three refusals over fields
   * nobody had filled in. What holds for both sources — the watch cadence, the separator,
   * the ceilings — is not listed here and therefore never moves.
   */
  const OWN_OPTIONS: Record<string, string[]> = {
    local_drop: ['catalog.options.directory'],
    webdav: ['catalog.options.url', 'catalog.options.username', 'catalog.options.password'],
  }

  /** What `after_read` means when the document does not carry it: the file is deleted. */
  const AFTER_READ_DEFAULT = 'delete'

  /** The one `after_read` value that leaves the file where it is. */
  const AFTER_READ_KEEP = 'keep'

  /**
   * The help under the file name, kept out of the markup because Svelte reads `{n}` in a
   * template as an expression.
   */
  const FILE_NAME_HINT = '{n} est remplacé par le numéro du poste.'

  /**
   * The source the document declares, empty for as long as it has not been read.
   *
   * No fallback on « dépôt local »: a document with no source is refused by the station
   * (control 5), and ticking a radio in its place would have the screen read out a choice
   * the file does not carry — then answer « aucune source de catalogue n'est déclarée » to
   * a save that nothing had announced.
   */
  const source = $derived(draft.text('catalog.type'))

  /** The file-name pattern the document carries, undefined while it carries none. */
  const pattern = $derived(draft.text('catalog.options.file_name') || undefined)

  /** The name this station waits for, from the pattern and its own number. */
  const expected = $derived(expectedCatalogFile(pattern, station))

  /** What the station does with a file once read: `keep`, or the default deletion. */
  const keeping = $derived(draft.text('catalog.options.after_read') === AFTER_READ_KEEP)

  /**
   * Where the station looks on a WebDAV server: the address and the name, with exactly
   * one slash between them. Just the name while no address has been typed.
   */
  const remoteFile = $derived.by(() => {
    const url = draft.text('catalog.options.url').replace(/\/+$/u, '')
    return url === '' ? expected : `${url}/${expected}`
  })

  /**
   * Writes the file-name pattern, or removes it when the field is emptied.
   *
   * The key goes away rather than staying empty: control 51 refuses a PRESENT but empty
   * `file_name` as a blocking fault, where an absent one just means the default.
   *
   * @param value - what was typed.
   */
  function setFileName(value: string): void {
    if (value === '') draft.unset('catalog.options.file_name')
    else draft.set('catalog.options.file_name', value)
  }

  /**
   * Chooses what happens to a file once read.
   *
   * Deletion is the default and leaves no key behind, so that the file of a station
   * which never asked for anything else stays what it was.
   *
   * @param chosen - `delete` or `keep`.
   */
  function chooseAfterRead(chosen: string): void {
    if (chosen === AFTER_READ_DEFAULT) draft.unset('catalog.options.after_read')
    else draft.set('catalog.options.after_read', chosen)
  }

  /**
   * Chooses the catalog source, and wipes the settings of the other one.
   *
   * @param chosen - the source, as the service names it.
   */
  function chooseSource(chosen: string): void {
    draft.set('catalog.type', chosen)
    for (const [type, paths] of Object.entries(OWN_OPTIONS)) {
      if (type === chosen) continue
      for (const path of paths) draft.unset(path)
    }
  }
</script>

<Panel title="Où le poste va chercher le catalogue">
  <div class="choice" role="radiogroup" aria-label="Source du catalogue">
    <label>
      <input
        type="radio"
        name="catalog-source"
        value="local_drop"
        checked={source === 'local_drop'}
        onchange={() => chooseSource('local_drop')}
      />
      Un répertoire de ce poste ou du réseau
    </label>
    <label>
      <input
        type="radio"
        name="catalog-source"
        value="webdav"
        checked={source === 'webdav'}
        onchange={() => chooseSource('webdav')}
      />
      Un serveur WebDAV
    </label>
  </div>

  {#if draft.config === null}
    <p class="fact muted">Lecture des réglages du poste…</p>
  {:else if source === 'local_drop'}
    <Field
      label={labelOf('catalog.options.directory')}
      path="catalog.options.directory"
      value={draft.text('catalog.options.directory')}
      hint="Laissez vide pour le répertoire du poste, celui que le service crée lui-même. Un répertoire nommé ici doit exister : le poste ne le crée pas."
      fault={faultOf(draft, 'catalog.options.directory')}
      onchange={(value) => draft.set('catalog.options.directory', value)}
    />
    {@render fileSettings()}
    <p class="fact muted" data-expected-file>
      Le poste y cherche le fichier <code>{expected}</code>{@render afterReadClause()}
    </p>
    {@render sharedFileNote()}
  {:else if source === 'webdav'}
    <Field
      label={labelOf('catalog.options.url')}
      path="catalog.options.url"
      value={draft.text('catalog.options.url')}
      fault={faultOf(draft, 'catalog.options.url')}
      onchange={(value) => draft.set('catalog.options.url', value)}
    />
    <Field
      label={labelOf('catalog.options.username')}
      path="catalog.options.username"
      value={draft.text('catalog.options.username')}
      fault={faultOf(draft, 'catalog.options.username')}
      onchange={(value) => draft.set('catalog.options.username', value)}
    />
    <!--
      The field opens EMPTY and not on the value in service: the station no longer serves
      the password to the browser. Left empty, it changes nothing.
    -->
    <Field
      label={labelOf('catalog.options.password')}
      path="catalog.options.password"
      kind="password"
      value=""
      hint="Laissez vide : le mot de passe actuel est conservé."
      fault={faultOf(draft, 'catalog.options.password')}
      onchange={(value) => draft.set('catalog.options.password', value)}
    />
    {@render fileSettings()}
    <p class="fact muted" data-expected-file>
      Le poste cherche <code>{remoteFile}</code>{@render afterReadClause()}
    </p>
    {@render sharedFileNote()}
    <p class="fact muted" data-webdav-warning>
      Sur un serveur WebDAV, le dépôt d’un fichier CSV depuis cet écran n’est plus
      possible : le poste n’a plus de répertoire local où l’écrire. C’est le seul recours
      du jour de la mise en service.
    </p>
  {:else}
    <p class="fact">
      Ce poste ne déclare aucune source : choisissez-en une ci-dessus, sinon il n’ira
      chercher aucun catalogue.
    </p>
  {/if}
</Panel>

{#snippet fileSettings()}
  <Field
    label={labelOf('catalog.options.file_name')}
    path="catalog.options.file_name"
    value={draft.text('catalog.options.file_name')}
    hint={FILE_NAME_HINT}
    fault={faultOf(draft, 'catalog.options.file_name')}
    onchange={setFileName}
  />
  <p class="group-label" id="catalog-after-read-label">{labelOf('catalog.options.after_read')}</p>
  <div class="choice" role="radiogroup" aria-labelledby="catalog-after-read-label">
    <label>
      <input
        type="radio"
        name="catalog-after-read"
        value="delete"
        checked={!keeping}
        onchange={() => chooseAfterRead('delete')}
      />
      Supprimer le fichier après lecture
    </label>
    <label>
      <input
        type="radio"
        name="catalog-after-read"
        value="keep"
        checked={keeping}
        onchange={() => chooseAfterRead('keep')}
      />
      Laisser le fichier en place
    </label>
  </div>
  {#if faultOf(draft, 'catalog.options.after_read') !== ''}
    <p class="fault">{faultOf(draft, 'catalog.options.after_read')}</p>
  {/if}
{/snippet}

<!--
  What the station does with the file, said in the sentence about where it looks. The
  deletion is what tells the producer the delivery was taken, so it is only claimed in
  the mode that does it.
-->
{#snippet afterReadClause()}
  {#if keeping}
    . Il ne supprime jamais le fichier : il le relit quand il change.
  {:else}
    , et le supprime une fois lu : c’est ce qui dit au producteur que la livraison est
    prise.
  {/if}
{/snippet}

<!--
  Not a refusal: one file shared by several stations is legitimate when it is kept. It is
  only a warning where the first reader takes the file away from the others.
-->
{#snippet sharedFileNote()}
  {#if sharesOneFile(pattern) && !keeping}
    <p class="fact" data-shared-file-note>
      Ce fichier sera supprimé par le premier poste qui le lit : ne le faites lire qu’à un
      seul poste, ou choisissez « Laisser le fichier en place ».
    </p>
  {/if}
{/snippet}

<style>
  .fact {
    margin: 0.5rem 0;
    font-size: 1.125rem;
  }

  .group-label {
    margin: 0.5rem 0 0;
    font-size: 1.125rem;
    font-weight: 700;
  }

  .fault {
    margin: 0.25rem 0;
    padding-left: 0.5rem;
    font-size: 1rem;
    color: var(--ink-muted);
    border-left: 0.25rem solid var(--fault);
  }

  .muted {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  /*
   * The two sources, one under the other: they rule each other out, and two lines compare
   * better than two boxes side by side on a screen held to a reading width.
   */
  .choice {
    display: flex;
    flex-direction: column;
  }

  label {
    display: block;
    margin-top: 0.5rem;
    font-size: 1.0625rem;
    font-weight: 700;
  }

  .choice label {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    /* 44 px: the density of the administration's form controls. */
    min-height: 2.75rem;
    margin: 0;
    font-weight: 400;
  }

  input {
    min-height: 2.75rem;
    width: 100%;
    max-width: 34rem;
    padding: 0 0.75rem;
    font: inherit;
    font-variant-numeric: inherit;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  /*
   * A radio button is not a text field, and the `input` rule above would hand it the
   * width, the height, the border and the background of one. Everything is taken back
   * here so that the browser draws it the way it draws a radio.
   */
  .choice input {
    width: 1.5rem;
    height: 1.5rem;
    min-height: 0;
    flex: 0 0 auto;
    padding: 0;
    background: none;
    border: none;
    border-radius: 0;
  }
</style>
