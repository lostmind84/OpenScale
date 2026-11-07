# Catalogue par coopérative — plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** les deux sources de fichier du catalogue (`local_drop`, `webdav`) gagnent `file_name` (modèle de nom avec `{n}`) et `after_read` (`delete` par défaut, `keep`), et un fichier absent cesse d'être silencieux.

**Architecture:** la dérivation du nom reste dans `catalog.FileName`, qui prend désormais le modèle. Le mode `keep` s'appuie sur un repère persistant (`catalog.Watermark`) consulté à la scrutation et écrit à l'acquittement **et** au refus. La validation suit le modèle du contrôle 39 : un `OptionUse` dans le schéma de la source, un contrôle numéroté dans `internal/domain` qui lit le schéma sans nommer de source.

**Tech Stack:** Go 1.26.10 (bibliothèque standard), Svelte 5 + TypeScript 5.9 pour l'écran, vitest.

**Spec:** `docs/superpowers/specs/2026-11-07-catalogue-par-coop-design.md`

## Global Constraints

- Toutes les commandes passent par le devcontainer : `devcontainer exec --workspace-folder . <cmd>`.
- Zéro cgo, aucune dépendance nouvelle.
- Code, identifiants et commentaires en anglais ; messages utilisateur et docs en français, vocabulaire de `docs/03-glossaire.md`.
- Clés : `file_name` (défaut `flv_{n}.csv`), `after_read` (`delete` | `keep`, défaut `delete`). `{n}` est le seul remplacement.
- Les deux sources : mêmes clés, mêmes défauts, même sémantique, mêmes messages.
- `keep` ne supprime **jamais** un fichier, par aucun chemin (acquittement, refus, erreur).
- La Coope : aucune clé ajoutée à `testdata/config-lacagette.json`, comportement et empreinte inchangés.
- Docs et code dans la même PR ; **ADR-061 et les contrôles 51-52 du §11.3 dans le commit de la tâche 1**, qui applique la décision ; le reste de la doc en tâche 6, avant l'ouverture de la PR.
- **Écart avec la spec, voulu :** la spec dit « une fonction du paquet `catalog` appelée par le schéma ». `internal/domain` ne peut pas importer `internal/catalog` ; la validation est donc dans `internal/domain`, déclenchée par un `OptionUse`, exactement comme le contrôle 39 (`validate_settings.go:272`). Pas de nouvel `OptionKind`.

## Review Focus

- **Un serveur ou un disque qui ne donne pas de date** (`Stamp.Modified` nul) : en `keep`, le fichier doit être relu à chaque scrutation stable, jamais considéré « déjà vu » sur la seule taille. Test en tâche 2.
- **Un redémarrage en `keep` sur un fichier inchangé, appliqué ou refusé** : aucune relecture, aucun décompte de quarantaine en plus. Tests en tâches 3 et 4.
- **Un changement de `file_name`, d'URL ou de dossier** : le repère de l'ancien fichier ne doit pas masquer le nouveau. Test en tâche 2.
- **Un `file_name` qui sort du dossier** (`../x.csv`, `a/b.csv`, `a\b.csv`) ou vide : refusé avant l'enregistrement. Test en tâche 1.
- **Le passage de `keep` à `delete` sur un poste en service** : le premier lot suivant est supprimé comme aujourd'hui, le repère est ignoré. Test en tâche 3.

---

### Task 1: le nom dérivé du modèle, et les contrôles 51 et 52

**Files:**
- Modify: `internal/catalog/registry.go:24-33` (`FileName`, constante du défaut)
- Modify: `internal/domain/options.go:221-229` (deux `OptionUse`)
- Modify: `internal/domain/validate_settings.go` (contrôles 51, 52, dans `validateCatalogGuards` ou une fonction voisine appelée depuis `validate.go:97`)
- Modify: `internal/domain/config.go:303-308` (retirer le paragraphe `options.pattern` de `ABSENT ON PURPOSE`)
- Modify: `internal/catalog/localdrop/localdrop.go:194-210`, `internal/catalog/webdav/webdav.go:227-247` (déclarer les deux options au schéma)
- Modify: `docs/02-architecture.md` — nouvel `### ADR-061 — Le nom du fichier de catalogue et son sort après lecture se règlent par coopérative` après ADR-060, ligne Statut au format d'ADR-051 (`:5563`) avec `**Remplace** : ADR-004` ; contrôles 51 et 52 au §11.3
- Test: `internal/catalog/registry_test.go:136`, `internal/domain/validate_settings_test.go` (ou le fichier de test des contrôles 39-40), `cmd/openscale/configread_test.go`

**Interfaces:**
- Produces:
  - `const catalog.DefaultFileName = "flv_{n}.csv"`
  - `func catalog.FileName(pattern string, stationNumber int) string` — `pattern == ""` vaut `DefaultFileName` ; remplace chaque `{n}` par le numéro.
  - `func catalog.FileNameOf(options domain.DriverOptions, stationNumber int) string` — lit `file_name` et appelle `FileName`.
  - `const catalog.AfterReadDelete = "delete"`, `catalog.AfterReadKeep = "keep"`, `func catalog.AfterReadOf(options domain.DriverOptions) string` (défaut `delete`).
  - `domain.UseCatalogFileName`, `domain.UseCatalogFolderURL` (nouvelles valeurs d'`OptionUse`).
  - Schéma des deux sources : `{Key: "file_name", Kind: domain.OptionText, Use: domain.UseCatalogFileName}`, `{Key: "after_read", Kind: domain.OptionEnum, Values: []string{"delete", "keep"}}` ; l'option `url` de WebDAV prend `Use: domain.UseCatalogFolderURL`.

- [ ] **Step 1: tests de dérivation** — dans `registry_test.go`, table : `("", 2) → "flv_2.csv"`, `("flv_{n}.csv", 3) → "flv_3.csv"`, `("flv.csv", 1) → "flv.csv"`, `("produits_poste{n}.csv", 4) → "produits_poste4.csv"`. Le test existant de la ligne 136 passe sur `FileName("", n)`.
- [ ] **Step 2: tests des contrôles** — config valide avec `catalog.type = local_drop` puis `webdav` :
  - `file_name` = `"../x.csv"`, `"a/b.csv"`, `"a\\b.csv"`, `"  "` → une faute sur `catalog.options.file_name`, message contenant `"un nom de fichier, pas un chemin"` ;
  - `file_name` = `"flv.csv"`, `"flv_{n}.csv"` → aucune faute ;
  - `url` = `"https://dav.example.org:8001/catalogue/flv.csv"` et `".../FLV.CSV"` → une faute sur `catalog.options.url` dont le message contient `"file_name"` ;
  - `url` = `"https://dav.example.org:8001/catalogue"` et `".../catalogue/"` → aucune faute.
- [ ] **Step 3: test d'empreinte** — dans `cmd/openscale/configread_test.go` : charger `deliveredConfig(t)`, `json.Marshal` puis `json.Unmarshal` du `domain.Config` (le chemin de `PUT /admin/api/config`), vérifier que `catalog.options` n'a ni `file_name` ni `after_read` (`Options.Has`) et que `Fingerprint()` est identique avant et après.
- [ ] **Step 4: lancer, voir rouge** — `devcontainer exec --workspace-folder . go test ./internal/catalog/ ./internal/domain/ ./cmd/openscale/` → échecs de compilation (`FileName` à un argument) puis d'assertion.
- [ ] **Step 5: implémenter** — `FileName`/`FileNameOf`/`AfterReadOf` dans `registry.go` ; les deux `OptionUse` ; les contrôles 51 (`file_name` : vide après `TrimSpace`, ou contient `/`, `\`, `..`) et 52 (dernier segment du chemin de l'URL se termine par `.csv`, casse ignorée), écrits avec `optionsUsedAs(reg.CatalogSources, c.Catalog.Type, Use…)` comme le contrôle 39 ; les schémas ; les deux appels de production (`localdrop.go:149`, `webdav.go:122`) passent à `catalog.FileNameOf(c.Catalog.Options, c.StationNumber)`. Messages : contrôle 51 « %q n'est pas un nom de fichier valide : c'est un nom de fichier, pas un chemin — le dossier se règle à part » ; contrôle 52 « %q désigne un fichier : l'adresse est celle du dossier, et le nom du fichier se règle dans file_name ».
- [ ] **Step 6: vert** — même commande, `ok` pour les trois paquets ; puis `go test ./...` vert.
- [ ] **Step 7: écrire ADR-061 et les contrôles 51-52 du §11.3** — contexte (retour Graoucoop, décision du 07/11/2026 « ce qui est propre à une coopérative devient un réglage »), options envisagées (statu quo + lien symbolique chez l'hébergeur ; nom libre sans mode ; nom + deux modes), décision, conséquences (ADR-004 devient le défaut ; `keep` autorise plusieurs postes sur un fichier ; repère `<data>/catalog/watermark.json`).
- [ ] **Step 8: commit** — `feat(catalog): nom du fichier de catalogue réglable par modèle {n}` (code, ADR-061, §11.3).

### Task 2: le repère du mode `keep`

**Files:**
- Create: `internal/catalog/watermark.go`
- Test: `internal/catalog/watermark_test.go`

**Interfaces:**
- Consumes: `catalog.Stamp{Size int64; Modified time.Time}` (`stability.go:12`).
- Produces:
  - `func catalog.OpenWatermark(dataDir, location string) (*Watermark, error)` — fichier `<dataDir>/catalog/watermark.json` ; un fichier absent, illisible ou portant une autre `location` donne un repère **vide** (jamais une erreur pour ces trois cas).
  - `func (w *Watermark) Seen(s Stamp) bool` — vrai seulement si un tampon est retenu, que `s.Modified` n'est **pas** nul, et que taille et date (`Equal`) coïncident.
  - `func (w *Watermark) Remember(s Stamp) error` — écrit `{"location","size","modified"}` par fichier temporaire du même dossier, `Sync`, `os.Rename`.

- [ ] **Step 1: tests** — `TestAWatermarkRemembersAcrossReopening` (Remember puis OpenWatermark même location → `Seen` vrai) ; `TestAWatermarkOfAnotherLocationIsEmpty` (Remember sur `"https://a/flv.csv"`, réouverture sur `"https://a/flv_1.csv"` → `Seen` faux) ; `TestAStampWithoutDateIsNeverSeen` (Remember `{Size: 10}`, `Seen({Size: 10})` faux) ; `TestAChangedSizeOrDateIsNotSeen` ; `TestAnUnreadableWatermarkIsEmpty` (fichier contenant `"{"` → repère vide, pas d'erreur).
- [ ] **Step 2: rouge** — `devcontainer exec --workspace-folder . go test -run Watermark ./internal/catalog/` → `undefined: OpenWatermark`.
- [ ] **Step 3: implémenter** `internal/catalog/watermark.go` (le dossier `<dataDir>/catalog` est créé avec `os.MkdirAll` à `Remember`, pas à l'ouverture).
- [ ] **Step 4: vert** — même commande.
- [ ] **Step 5: commit** — `feat(catalog): repère persistant pour le mode keep`.

### Task 3: `local_drop` — `keep`, et le fichier absent dit

**Files:**
- Modify: `internal/catalog/localdrop/localdrop.go:74-165` (champs `afterRead string`, `watermark *catalog.Watermark`, `observed catalog.Stamp`, `missing bool`)
- Modify: `internal/catalog/localdrop/source.go:31-36` (`Describe`), `:80-107` (`poll`), `:109-142` (`read`), `:179-203` (`refuse`), `:213-239` (`Acknowledge`)
- Test: `internal/catalog/localdrop/acknowledge_test.go`, `localdrop_test.go`, `quarantine_test.go` (helper `station(t, options string)`, `fake.NewClock(t0)`)

**Interfaces:**
- Consumes: `catalog.FileNameOf`, `catalog.AfterReadOf`, `catalog.AfterReadKeep` (tâche 1) ; `catalog.OpenWatermark`, `Seen`, `Remember` (tâche 2).
- Produces: aucun symbole exporté nouveau ; `Describe()` ajoute « — en attente de <nom> » quand le fichier manque.

- [ ] **Step 1: tests** :
  - `TestKeepNeverDeletesAnAppliedFile` — `after_read: keep`, fichier valide, `Next` puis `Acknowledge(ImportApplied)` → le fichier existe toujours, une archive existe.
  - `TestKeepNeverDeletesARefusedFile` — CSV illisible en `keep` → `Next` rend l'erreur, le fichier existe toujours, la quarantaine compte **1**.
  - `TestKeepDoesNotRereadAnUnchangedFile` — après acquittement, avancer l'horloge de 10 scrutations → aucun lot (contexte à échéance, `Next` rend `ctx.Err()`).
  - `TestKeepRereadsAChangedFile` — réécrire le fichier avec une autre taille/date → un nouveau lot.
  - `TestKeepSurvivesARestart` — acquitter, `Close`, reconstruire la source sur le même `DataDir` → aucun lot sur le fichier inchangé ; idem pour un fichier **refusé** : la quarantaine reste à **1**.
  - `TestSwitchingBackToDeleteDeletesTheNextFile` — repère écrit en `keep`, source reconstruite en `delete` → le fichier est lu puis supprimé.
  - `TestAMissingFileIsSaidOnceAndThenItsReturn` — journal technique de test : une ligne à l'absence (niveau Info en `delete`, Warn en `keep`), aucune à la scrutation suivante, une ligne au retour ; `Describe()` contient `"en attente de flv_2.csv"` pendant l'absence.
  - Les tests existants de `delete` passent sans modification.
- [ ] **Step 2: rouge** — `devcontainer exec --workspace-folder . go test ./internal/catalog/localdrop/`.
- [ ] **Step 3: implémenter** — `New` lit `after_read` et ouvre le repère sur `Path()` en `keep` ; `poll` : sur absence, transition `missing` journalisée une fois ; sur présence, en `keep`, `watermark.Seen(stamp)` → `return nil, nil` ; garder le tampon observé stable dans `observed` ; `read` inchangé ; `refuse` et `Acknowledge` : en `keep`, `watermark.Remember(observed)` à la place de `s.remove(...)` (archive, motif, quarantaine conservés) ; message d'acquittement « Catalogue acquitté, fichier laissé en place. ». Une erreur de `Remember` est journalisée `ERR-CAT-05` et rend `catalog.ErrNotAcknowledged` depuis `Acknowledge`.
- [ ] **Step 4: vert** — même commande, puis `go test -race ./internal/catalog/...`.
- [ ] **Step 5: commit** — `feat(catalog): local_drop sait laisser le fichier en place`.

### Task 4: `webdav` — `keep`, l'adresse, et le fichier absent dit

**Files:**
- Modify: `internal/catalog/webdav/webdav.go:67-160` (mêmes champs qu'en tâche 3 ; `New` refuse une URL dont le dernier segment finit par `.csv`, message du contrôle 52)
- Modify: `internal/catalog/webdav/source.go:25-30` (`Describe`), `:60-85` (`poll`), `:140-162` (`refuse`), `Acknowledge`
- Test: `internal/catalog/webdav/source_test.go`, `webdav_test.go` (helper `station(t, remote *share, options map[string]any)`, `share` faux serveur)

**Interfaces:**
- Consumes: tâches 1 et 2, comme la tâche 3. Le repère est ouvert sur `s.file.String()`.
- Produces: rien d'exporté.

- [ ] **Step 1: tests** — les sept tests de la tâche 3, transposés : « le fichier existe toujours » = le faux serveur `share` n'a reçu **aucun** `DELETE` ; plus `TestAFolderURLEndingInCSVIsRefused` (`New` avec `url = srv.URL + "/flv.csv"` → erreur dont le texte contient `"file_name"`) ; plus `TestAServerWithoutDateIsRereadInKeep` (le `share` ne rend pas `getlastmodified` → deux lots successifs sur le même contenu, aucun `DELETE`).
- [ ] **Step 2: rouge** — `devcontainer exec --workspace-folder . go test ./internal/catalog/webdav/`.
- [ ] **Step 3: implémenter** — miroir de la tâche 3 ; `delete(ctx)` n'est plus appelé en `keep`, ni dans `refuse` ni dans `Acknowledge`. Message d'acquittement « Catalogue acquitté, fichier laissé sur le partage. ».
- [ ] **Step 4: vert** — même commande, puis `go test -race ./internal/catalog/...` et `go test ./...`.
- [ ] **Step 5: commit** — `feat(catalog): webdav sait laisser le fichier en place`.

### Task 5: l'écran et `doctor` disent le nom attendu

**Files:**
- Create: `web/src/admin/lib/catalog-file.ts`
- Test: `web/test/catalog-file.test.ts`, test existant du panneau s'il y en a un (`grep -rl CatalogSourcePanel web/test`)
- Modify: `web/src/admin/components/CatalogSourcePanel.svelte:36-39` (`OWN_OPTIONS` des deux sources : `file_name`, `after_read`), `:100-136`
- Modify: `web/src/admin/lib/fields.ts:16` (`FIELD_LABELS`)
- Modify: `web/src/admin/pages/Station.svelte:366`, `web/src/admin/pages/Catalog.svelte:966-967`
- Modify: `internal/diag/doctor_config.go:299-307` (`catalogArrivalRemedy` appelle `catalog.FileNameOf`)
- Rebuild: `internal/web/dist` par `make front`

**Interfaces:**
- Produces: `export function expectedCatalogFile(pattern: string | undefined, station: number): string` (défaut `flv_{n}.csv`, remplace chaque `{n}`) ; `export function sharesOneFile(pattern: string | undefined): boolean` (vrai si le modèle n'a pas de `{n}`).

- [ ] **Step 1: tests vitest** — `expectedCatalogFile(undefined, 2) === 'flv_2.csv'`, `('flv.csv', 1) === 'flv.csv'`, `('p{n}_{n}.csv', 3) === 'p3_3.csv'` ; `sharesOneFile('flv.csv') === true`, `sharesOneFile(undefined) === false`.
- [ ] **Step 2: rouge** — `devcontainer exec --workspace-folder . sh -c 'cd web && npx vitest run test/catalog-file.test.ts'`.
- [ ] **Step 3: implémenter** — le module ; dans le panneau, pour les deux sources : champ `file_name` (libellé « Nom du fichier », aide « {n} est remplacé par le numéro du poste »), choix `after_read` (« Supprimer le fichier après lecture » / « Laisser le fichier en place »), la ligne « Le poste cherche `<nom attendu>` » — pour WebDAV, l'adresse complète `url` + `/` + nom ; la note, **non bloquante**, quand `sharesOneFile` et `after_read` ≠ `keep` : « Ce fichier sera supprimé par le premier poste qui le lit : ne le faites lire qu'à un seul poste, ou choisissez « Laisser le fichier en place ». » La phrase actuelle « …et le supprime une fois lu… » ne s'affiche qu'en `delete`. `Station.svelte:366` : « C'est de lui que dérive le nom du fichier de catalogue attendu, quand ce nom contient {n} (page Catalogue). » `Catalog.svelte:967` utilise `expectedCatalogFile`. `catalogArrivalRemedy` cite `catalog.FileNameOf(...)`, le texte `"flv_<numéro de poste>.csv"` reste pour un numéro absent.
- [ ] **Step 4: vert** — `devcontainer exec --workspace-folder . make front-check`, `sh -c 'cd web && npm run lint && npm run format:check'`, `go test ./internal/diag/`, puis `make front` et vérifier `git status internal/web/dist`.
- [ ] **Step 5: commit** — `feat(web): l'écran dit le fichier de catalogue attendu` (avec `internal/web/dist`).

### Task 6: la documentation

**Files:**
- Modify: `docs/02-architecture.md` — §10.1 (étape 1 « Le nom est dérivé de station.number et de rien d'autre ») ; §11.2 (exemple de configuration : `pattern` retiré → `file_name`, `after_read`) ; le commentaire `station.number … flv_<n>.csv` du bloc `Reload` (§11.4).
- Modify: `docs/08-ajouter-une-source-de-catalogue.md` (ligne ~239 : la suppression est l'acquittement **en `delete`**).
- Modify: `CLAUDE.md` — la puce « Le CSV par poste reste le mode livré… » devient : par défaut, un fichier par poste supprimé après lecture ; `file_name` et `after_read` le règlent par coopérative (ADR-061).
- Create: `handbook/catalogue.md` ; Modify: `mkdocs.yml:57-66` (entrée « Catalogue » après « Démarrer »), `handbook/getting-started.md:203-205, 286`.
- Modify: `SUIVI.md` (entrée datée du journal).

- [ ] **Step 1: §10.1, §11.2, `docs/08`, `CLAUDE.md`, `SUIVI.md`** selon la liste ci-dessus.
- [ ] **Step 2: `handbook/catalogue.md`** — pour le bénévole seul : les deux réglages, quand choisir `keep`, les trois symptômes (fichier absent → « en attente de … » au tableau de bord ; URL finissant par `.csv` → contrôle 52 ; nom sans `{n}` en `delete` → la note de l'écran), et le cas du serveur sans date (relecture à chaque scrutation). Renvois par URL GitHub absolues vers `docs/` (ODR-0002).
- [ ] **Step 3: vérifier** — `devcontainer exec --workspace-folder . mkdocs build --strict` vert ; `grep -rn 'flv_<n>.csv\|de rien d.autre' docs/02-architecture.md handbook/ CLAUDE.md` ne rend plus que des passages historiques assumés.
- [ ] **Step 4: commit** — `docs: le catalogue se règle par coopérative`.
