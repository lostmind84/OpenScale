# Gabarit d'étiquette par coopérative — plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** une coopérative déclare SON gabarit (média, zone imprimable, résolution, éléments, police par élément, hauteur de barres, bande HRI) sous `printer.custom_template`, le choisit par `printer.template`, le voit imprimé sans redémarrer, et règle un décalage négatif quand sa géométrie laisse une marge en haut ou à gauche.

**Architecture:** un seul endroit du noyau résout « le gabarit en service » : `Config.Templates(reg)` (gabarits du registre + celui de la coopérative, décalages du poste recomposés), lu par les contrôles 21/29/38, par `templatesFor`, par le Hub à chaque impression et par une route ouverte qui sert le point de départ et les bornes. Les 9 règles dures ne changent pas de nature ; elles gagnent ce qu'un gabarit **fourni** rend nécessaire (police en liste fermée, symbole dans l'étiquette, barres et HRI non nulles). La police est une chaîne côté `domain`, convertie en `printing.Font` au rendu.

**Tech Stack:** Go 1.26.10 (bibliothèque standard), Svelte 5 + TypeScript pour l'écran, vitest, mkdocs.

**Spec:** `docs/superpowers/specs/2026-11-07-gabarit-par-coop-design.md`

## Global Constraints

Copiées de la spec (§ Contraintes) :

- Zéro cgo, aucune dépendance nouvelle — en particulier aucune police système, aucune bibliothèque de mise en page.
- Docs et code dans le même commit ; un ADR dans le même commit que le code qui l'applique, pour la décision de l'emplacement du gabarit dans `config.json` et pour l'extension de `MaxOffsetDotsOn` à une borne négative.
- Les deux installeurs (`deploy/windows/install.ps1`, `deploy/linux/install.sh`) et leur table de parité (`deploy/parity_test.go`) n'ont rien à apprendre : un gabarit de coopérative est un contenu de `config.json`, pas une option d'installation.
- Le français des messages et de la documentation suit `docs/03-glossaire.md` (« gabarit », pas « template » ; « contrôle », « empreinte »…).
- TDD sur ce qui se calcule sans matériel : les 9 règles dures sur un gabarit de coopérative synthétique, un média et une résolution propres à la coopérative (accord avec la tête accepté, désaccord refusé), la copie du gabarit livré par le bouton, le recalcul des bornes de décalage avec une marge en haut, la validation de la police d'un élément, le refus d'un nom de gabarit qui recouvre un nom livré, la fusion de `templatesFor`.

Propres au dépôt : toutes les commandes passent par `devcontainer exec --workspace-folder /home/fab/Projects/OpenScale …` ; ne jamais indexer `internal/diag/archive_writer.go`, `internal/update/stager.go`, `internal/web/admin_test.go`, `internal/web/journal.go`, `.superpowers/` (`git add` par chemins explicites). `internal/web/dist` est recompilé (`make front`) et commité avec tout changement de `web/`.

**Écarts avec la spec, voulus :**

- **Le Hub résout le gabarit sur la configuration EN SERVICE**, pas seulement `templatesFor` au démarrage. Lu dans le code : `Hub.templates` est figé à `station.New` (`internal/station/hub.go:190`), `Hub.template` ne lit que le NOM dans la configuration courante (`internal/station/effects.go:67, 88-93`), et la fabrique `NewPrinter` de `serve.go:315-321` réutilise la carte du démarrage. Un gabarit de coopérative enregistré depuis l'écran (rechargement à chaud, ADR-033) imprimerait donc un `domain.Template{}` vide ; un décalage enregistré n'atteindrait pas l'étiquette avant redémarrage — à **prouver par le test rouge** de la tâche 4, pas à croire. §11.4 promet que le gabarit s'applique sans redémarrage (`reload.go:78`).
- **La route `GET /admin/api/label/geometry` est servie par `web.Server` lui-même** (registres + configuration), pas par `web.Hardware` : étendre cette interface obligerait à modifier `fakeHardware`, qui vit dans `internal/web/admin_test.go`, fichier du propriétaire.
- **Trois gardes ajoutées aux règles existantes**, sans numéro nouveau : règle 3 refuse un symbole qui commence au-dessus ou à gauche de l'étiquette ; règle 9 refuse `bar_height_um ≤ 0`, `guard_descent_um < 0`, `hri_height_um ≤ 0`. Aucune ne s'appliquait faute d'objet tant que seuls des gabarits compilés existaient ; un gabarit fourni par un document peut les violer, et ADR-051 exige la HRI imprimée.
- **Une police sur l'élément `barcode` est refusée** (règle 7) : les chiffres HRI restent dessinés en `labelFont` (`render.go:255`), la décision 2 ne donne au code-barres que barres et bande HRI.

## Review Focus

- **Un gabarit de coopérative enregistré depuis l'écran puis une pesée, sans redémarrage** : l'étiquette imprimée porte la géométrie de la coopérative et le décalage enregistré, jamais un gabarit vide ni celui du démarrage. Test en tâche 4.
- **La borne négative est exactement la marge** : un décalage égal à `MinY` passe les 9 règles, `MinY − 1` est refusé — sur la géométrie DÉCALÉE, symbole compris (anti-objectif 1). Tests en tâche 3.
- **La Coope ne bouge pas** : `weighing_identical`/`weighing_neutral_single` rendent à l'octet (goldens de `render_test.go:54` et `symbol_test.go:244` intacts), bornes `[0, max]` et message du contrôle 38 identiques au caractère près, empreinte de `testdata/config-lacagette.json` inchangée après aller-retour JSON. Tâches 1, 2, 3.
- **Un nom de coopérative qui recouvre un nom livré** ne remplace jamais le gabarit livré, même sur une configuration non validée (`Config.Templates` laisse gagner le livré), et le contrôle 53 le refuse. Tâche 1.
- **Le bouton n'écrase pas un gabarit de coopérative déjà présent dans le brouillon.** Tâche 6.

Hors de ce plan, connu et non traité : une bande HRI trop étroite pour les chiffres au plancher de lisibilité passe le contrôle 29 et fait échouer le rendu (`FitHRIFace`, `symbol.go:336-380`) — l'aperçu affiche la phrase du moteur ; le domaine ne mesure pas de glyphes. La page bénévole le dit (tâche 7).

---

### Task 1: le gabarit de coopérative dans la configuration, et le contrôle 53

**Files:**
- Modify: `internal/domain/config.go:277-287` (`PrinterConfig.CustomTemplate`)
- Create: `internal/domain/custom_template.go` (résolution et contrôle 53)
- Modify: `internal/domain/validate.go:81-105` (appel `// 53` en fin de liste), `:126-132` (`labelGeometry` lit `SelectedTemplate`), `:296` (`addChoice` avec `c.TemplateNames(reg)`)
- Modify: `internal/domain/redact.go:212-226` (copie profonde du gabarit de coopérative)
- Modify: `docs/02-architecture.md` — `### ADR-062 — Le gabarit d'étiquette se déclare par coopérative, sous printer.custom_template` après ADR-061 (Statut au format d'ADR-061, Portée §7, §11.2, §11.3) ; §11.2 (clé commentée) ; §11.3 (contrôle 53)
- Test: `internal/domain/custom_template_test.go` (nouveau), `internal/domain/redact_test.go`, `cmd/openscale/configread_test.go` (à côté du test d'aller-retour de la ligne ~405)

**Interfaces:**
- Produces:
  - `PrinterConfig.CustomTemplate *Template \`json:"custom_template,omitempty"\``
  - `const domain.StarterTemplateName = "cooperative"` ; `func domain.StarterTemplate() Template` — `IdenticalTemplate()` renommé `StarterTemplateName`, décalages à zéro.
  - `func (c *Config) Templates(reg Registries) map[string]Template` — `reg.templates()` plus `*CustomTemplate` sous son `Name` **s'il ne recouvre aucun nom du registre** ; chaque gabarit porte `OffsetXDots`/`OffsetYDots` = `intOption(c.Printer.Options, "offset_x"/"offset_y")`.
  - `func (c *Config) TemplateNames(reg Registries) []string` (triés) ; `func (c *Config) SelectedTemplate(reg Registries) (Template, bool)`.
  - `func (c *Config) validateCustomTemplate(reg Registries) []Fault` — contrôle 53.
- Consumes: `Registries.templates` (`options.go:407-411`), `intOption` (`validate.go:378`), `IdenticalTemplate` (`templates.go:168`).

- [ ] **Step 1: tests de résolution** — `custom := StarterTemplate(); custom.Name = "graoucoop"`, config valide (fixture du paquet) avec `Printer.CustomTemplate = &custom`, `Printer.Template = "graoucoop"`, `offset_y = 2` :
  - `TemplateNames(Registries{})` = `["graoucoop", "weighing_identical", "weighing_neutral_single"]` ;
  - `SelectedTemplate` rend `Name == "graoucoop"`, `OffsetYDots == 2` ;
  - `custom.Name = "weighing_identical"` → `SelectedTemplate` rend `IdenticalTemplate()` (éléments identiques), pas la copie modifiée (`Elements[0].FontSizeUM = 2_500` sur la copie) ;
  - sans `CustomTemplate` : `Templates` égale `ShippedTemplates()` décalages compris.
- [ ] **Step 2: tests des contrôles** (`Validate(Registries{})`) :
  - nom `""`/`"  "` → une faute `printer.custom_template.name`, message contenant `"doit porter un nom"` ;
  - nom `"weighing_identical"` → une faute `printer.custom_template.name`, message contenant `"nom d'un gabarit livré"`, `Values` = noms livrés ;
  - `offset_y_dots = 1` dans le gabarit → une faute `printer.custom_template.offset_y_dots` dont le message cite `"printer.options.offset_y"` ;
  - `printer.template = "graoucoop"`, `media.dots_per_mm = 12` → faute `printer.template.media.dots_per_mm` (contrôle 29, accord avec la tête) ; `dots_per_mm = 0` → faute `template.media.dots_per_mm` (contrôle 21) ;
  - `media.width_um = 30_000` (zone imprimable 35 000) → faute `printer.template.printable_area` ;
  - `printer.template = "inconnu"` → `Values` contient `"graoucoop"` ;
  - copie intacte de `StarterTemplate()` nommée `"graoucoop"` → **aucune** faute.
- [ ] **Step 3: tests d'`Export` et d'empreinte** — `redact_test.go` : modifier `Export(true).Printer.CustomTemplate.Elements[0]` ne modifie pas l'original. `configread_test.go` : `deliveredConfig(t)` aller-retour `json.Marshal`/`Unmarshal` → le JSON ne contient pas `"custom_template"`, `Fingerprint()` identique.
- [ ] **Step 4: rouge** — `devcontainer exec --workspace-folder /home/fab/Projects/OpenScale go test ./internal/domain/ ./cmd/openscale/` → `undefined: StarterTemplate`, puis assertions.
- [ ] **Step 5: implémenter** — le champ ; `custom_template.go` ; `labelGeometry` appelle `c.SelectedTemplate(reg)` (le reste de la structure inchangé) ; `Export` : `if c.Printer.CustomTemplate != nil` → nouvelle valeur avec `Elements` copiés. Messages du contrôle 53 : « le gabarit de la coopérative doit porter un nom : c'est lui que printer.template désigne » ; « %q est le nom d'un gabarit livré : le gabarit de la coopérative en porte un autre, pour ne jamais changer ce qu'imprime un poste qui le nommait » ; « le décalage se règle dans printer.options.offset_y, pas dans le gabarit ».
- [ ] **Step 6: vert** — même commande, puis `go test ./...`.
- [ ] **Step 7: ADR-062, §11.2, §11.3** — contexte (Graoucoop, rouleau « ETD TD 35x25 ADH RENFORCE », décision du 07/11/2026) ; options (gabarits compilés seulement ; carte `templates` à la racine ; un bloc `printer.custom_template`) ; décision (décisions 11, 13, 14, 15 de la spec) ; conséquences (un livré gagne toujours sur un homonyme ; empreinte de La Coope inchangée par `omitempty` ; changer le gabarit change le bloc `printer` et arme donc le compte à rebours de 60 s de §11.4).
- [ ] **Step 8: commit** — `feat(domain): le gabarit de la coopérative se déclare sous printer.custom_template` (code, tests, ADR-062, §11.2, §11.3).

### Task 2: la police d'un élément, et les gardes du symbole

**Files:**
- Modify: `internal/domain/template.go:12-50` (constantes de police, `knownFonts`), `:244-264` (`Element.Font`)
- Modify: `internal/domain/template_validate.go:82-108` (règle 9), `:110-134` (règle 7)
- Modify: `internal/printing/layout.go:54-62` (godoc de `labelFont` : repli, plus police de chaque champ ; `elementFont`), `internal/printing/field.go:88`
- Modify: `docs/02-architecture.md` §7.5 (règles 7 et 9) ; `docs/03-glossaire.md` (« police » ↔ `font`, si absent)
- Test: `internal/domain/template_validate_test.go`, `internal/printing/fonts_test.go` (ou le fichier de test des polices existant), `internal/printing/render_test.go`

**Interfaces:**
- Produces:
  - `const domain.FontCarlito = "carlito"`, `domain.FontDejaVuSansCondensed = "dejavu-sans-condensed"` ; `func domain.KnownFonts() []string` (ces deux, dans cet ordre).
  - `Element.Font string \`json:"font,omitempty"\`` — vide = police par défaut du moteur.
  - `func printing.elementFont(e domain.Element) Font` — `""` → `labelFont`, sinon `Font(e.Font)`.

- [ ] **Step 1: tests du domaine** — sur `IdenticalTemplate()` modifié : `Elements[0].Font = "comic-sans"` → une faute `elements[0].font`, message `police inconnue "comic-sans"`, `Values` = `KnownFonts()` ; `"dejavu-sans-condensed"` → aucune ; `Font` posé sur l'élément `barcode` → une faute `elements[5].font` ; `Symbol.BarHeightUM = 0` → faute `symbol.bar_height_um` ; `Symbol.HRIHeightUM = 0` → faute `symbol.hri_height_um` ; `Symbol.GuardDescentUM = -1` → faute `symbol.guard_descent_um` ; `IdenticalTemplate().Validate(2)` et `NeutralSingleTemplate().Validate(1)` restent vides.
- [ ] **Step 2: tests du rendu** — chaque valeur de `domain.KnownFonts()` a une entrée dans `fontFileNames` et `len` égaux (garde contre la dérive des deux listes) ; un gabarit dont `product_name` déclare `"dejavu-sans-condensed"` rend un bitmap **différent** de celui de `IdenticalTemplate()` sur le même `weighing(...)` ; `TestTheLabelMatchesItsGoldens` passe sans régénérer aucun golden.
- [ ] **Step 3: rouge** — `devcontainer exec --workspace-folder /home/fab/Projects/OpenScale go test ./internal/domain/ ./internal/printing/`.
- [ ] **Step 4: implémenter** — règle 7 : `if e.Field == FieldBarcode && e.Font != ""` → « le symbole ne prend pas de police : ses chiffres sont dessinés par le moteur, bornés par la bande HRI » ; sinon `e.Font != "" && !known(knownFonts, e.Font)` → `police inconnue %q` avec `Values`. Règle 9 avant les tailles de police. `compose` (`field.go:88`) appelle `r.fonts.Face(elementFont(e), …)` ; `render.go:255` inchangé.
- [ ] **Step 5: vert** — même commande, puis `go test -race ./internal/printing/...` et `go test ./...`.
- [ ] **Step 6: §7.5 et glossaire**, puis **commit** — `feat(printing): la police se choisit par élément du gabarit`.

### Task 3: les bornes du décalage sur les quatre côtés

**Files:**
- Modify: `internal/domain/template.go:300-326` (`OffsetBounds`, `OffsetBoundsOn` ; `MaxOffsetDots`/`MaxOffsetDotsOn` deviennent des enveloppes qui rendent `MaxX, MaxY`), `:350-375` (`inkedOrigin` à côté d'`inkedExtent`)
- Modify: `internal/domain/template_validate.go:167-185` (règle 3 : origine du symbole)
- Modify: `internal/domain/validate.go:335-352` (contrôle 38 lit les bornes) ; ajout de `Config.OffsetBounds`
- Modify: `docs/02-architecture.md` — `### ADR-063 — Les bornes du décalage se calculent sur les quatre côtés du gabarit en service` (Portée §7.5, §11.3 contrôle 38, ADR-045) ; §7.5 règles 3 et 6 ; §11.3 contrôle 38
- Test: `internal/domain/template_test.go`, `internal/domain/validate_corpus_test.go` (ou le test existant du contrôle 38 : `grep -n "hors bornes" internal/domain/*_test.go`)

**Interfaces:**
- Produces:
  - `type domain.OffsetBounds struct { MinX, MaxX, MinY, MaxY int }` (tags JSON `min_x`, `max_x`, `min_y`, `max_y`).
  - `func (t *Template) OffsetBoundsOn(head PrinterCapabilities, tierCount int) OffsetBounds` — `MinY = -(top/1000)`, `MinX = -(left/1000)`, avec `top`/`left` en milli-dots le plus petit `y`/`x` d'un élément actif non `barcode` **ou** de `symbolBox()` ; `Max*` = calcul actuel.
  - `func (c *Config) OffsetBounds(reg Registries) (OffsetBounds, bool)` — `false` quand le contrôle 38 se tait aujourd'hui (gabarit inconnu, résolution inutilisable, pas en désaccord).

- [ ] **Step 1: tests des bornes** — gabarit `topMargin` = `IdenticalTemplate()` avec chaque `Element.YUM += 2_000`, `HeightUM` de l'élément `barcode` `-= 2_000`, `Symbol.YUM += 2_000`, `Symbol.BarHeightUM = 9_375` :
  - `topMargin.Validate(2)` vide (le fixture lui-même est légal) ;
  - `OffsetBoundsOn(ReferenceHead(), 2)` → `MinY == -16`, `MinX == 0`, `MaxY` égal à celui d'`IdenticalTemplate()` ;
  - `OffsetYDots = -16` → `Validate(2)` vide ; `-17` → au moins une faute ;
  - `IdenticalTemplate()` et `NeutralSingleTemplate()` : `MinX == MinY == 0`, `MaxX, MaxY` égaux à `MaxOffsetDots(2)` ;
  - règle 3 : `Symbol.YUM = -125` (un dot) → faute `inked_content` dont le message contient `"au-dessus"`.
- [ ] **Step 2: tests du contrôle 38** — config avec `custom_template = topMargin` sélectionné : `offset_y = -16` → aucune faute ; `-17` → faute `printer.options.offset_y`, message `-17 dots hors bornes [-16, <maxY>] pour le gabarit "graoucoop"` ; config livrée avec `offset_x = -1` → message **identique au caractère près** à l'actuel (`-1 dots hors bornes [0, <maxX>] pour le gabarit "weighing_identical" : …`).
- [ ] **Step 3: rouge** — `devcontainer exec --workspace-folder /home/fab/Projects/OpenScale go test ./internal/domain/`.
- [ ] **Step 4: implémenter** — `validateLabelOffsets` prend `bounds, ok := c.OffsetBounds(reg)` (la signature passe à `(reg Registries, label labelGeometry)` si nécessaire) et teste `offset < bounds.MinX || offset > bounds.MaxX`, format `[%d, %d]`. Règle 3 : « le symbole commence en (%s ; %s) dots, au-dessus ou à gauche du bord de l'étiquette » quand le haut ou la gauche de `symbolBox()` est négatif.
- [ ] **Step 5: vert** — même commande, puis `go test ./...` (`internal/printing/raster/bounds_test.go` doit rester vert sans modification).
- [ ] **Step 6: ADR-063, §7.5, §11.3** — contexte (témoignage « ±1 dot au mieux, jamais négatif » = exactement `[0, max]`) ; options (garder `(x, y int)` + une paire ; type `OffsetBounds`) ; décision ; conséquences (`FLOOR_DOTS` de l'écran disparaît en tâche 6 ; La Coope garde `[0, max]` parce que ses deux gabarits commencent en (0 ; 0)).
- [ ] **Step 7: commit** — `feat(domain): le décalage devient négatif quand le gabarit laisse une marge`.

### Task 4: le poste imprime le gabarit de la configuration en service

**Files:**
- Modify: `cmd/openscale/wiring.go:21-47` (`templatesFor` = `cfg.Templates(reg)` ; message avec `cfg.TemplateNames(reg)`)
- Modify: `cmd/openscale/serve.go:315-321` (la fabrique `NewPrinter` appelle `templatesFor(next, registries)` et rend son erreur)
- Modify: `cmd/openscale/preview.go:36-40` (message avec `cfg.TemplateNames`)
- Modify: `internal/station/effects.go:62-93` (`Hub.template` prend la configuration), `internal/station/options.go:109-110` (godoc de `Templates` : le registre des gabarits nommés, le gabarit de coopérative et les décalages venant de la configuration en service)
- Modify: `docs/02-architecture.md` §11.4 (la phrase « le gabarit s'applique sans redémarrage » dit désormais d'où il vient)
- Test: `cmd/openscale/drivers_test.go` (à côté de `TestTheOffsetIsCarriedByTheTemplateAndNotByTheHead`, ligne 183), `internal/station/reload_test.go`

**Interfaces:**
- Consumes: `Config.Templates`, `Config.TemplateNames`, `Config.SelectedTemplate` (tâche 1) ; `fake.Printer.Jobs()` (`internal/fake/printer.go:135`) ; `b.station.Reload(ReloadRequest{Next: next})`, `b.weigh`, `b.awaitPrint` (`internal/station/harness_test.go`).
- Produces: `func (h *Hub) template(cfg *domain.Config) domain.Template` — `cfg.SelectedTemplate(domain.Registries{Templates: h.templates})`, gabarit nul si absent (comportement actuel).

- [ ] **Step 1: tests** —
  - `TestTemplatesForCarriesTheCooperativeTemplate` : config livrée + `custom_template` `"graoucoop"` sélectionné, `offset_x = 1` → la carte a 3 entrées, `["graoucoop"].OffsetXDots == 1`, `["weighing_identical"]` présent ; `printer.template = "inconnu"` → erreur dont le texte contient `graoucoop`.
  - `TestAReloadedCooperativeTemplateIsWhatPrints` (station) : banc par défaut, `Reload` d'une config portant `custom_template` `"graoucoop"` (`Elements[0].FontSizeUM = 2_500`) sélectionné, pesée → `Jobs()[last].Template.Name == "graoucoop"` et `Elements[0].FontSizeUM == 2_500`.
  - `TestAReloadedOffsetIsWhatPrints` : `Reload` avec `offset_y = 1` sur `weighing_identical`, pesée → `Template.OffsetYDots == 1`.
- [ ] **Step 2: rouge** — `devcontainer exec --workspace-folder /home/fab/Projects/OpenScale go test ./cmd/openscale/ ./internal/station/`. Les deux tests de station doivent échouer sur l'assertion (gabarit vide, décalage 0). **S'ils passent, s'arrêter et le signaler** : le premier écart de ce plan reposerait sur une lecture fausse.
- [ ] **Step 3: implémenter** — `templatesFor`, la fabrique, `Hub.print` passe `cfg` à `h.template`.
- [ ] **Step 4: vert** — même commande, puis `go test -race ./internal/station/ ./cmd/openscale/` et `go test ./...`.
- [ ] **Step 5: commit** — `fix(station): le gabarit imprimé est celui de la configuration en service` (code, tests, §11.4).

### Task 5: la route qui sert le point de départ et les bornes

**Files:**
- Create: `internal/web/labelgeometry.go`
- Modify: `internal/web/routes.go:62` (route ouverte, à côté de `preview.png` : c'est une lecture, ADR-033)
- Test: `internal/web/labelgeometry_test.go` (nouveau ; **jamais** `admin_test.go`), avec `newBench`, `b.get`, `decode[T]` de `harness_test.go`

**Interfaces:**
- Produces: `GET /admin/api/label/geometry` → `200` `{"starter": <domain.StarterTemplate()>, "offset_bounds": {"min_x", "max_x", "min_y", "max_y"} | null}` ; bornes de `s.hub.Config().OffsetBounds(s.registries)`, `null` quand `ok` est faux ; `Cache-Control: no-store`.

- [ ] **Step 1: test** — banc par défaut : `starter.name == "cooperative"`, `len(starter.elements) == 6`, `offset_bounds.min_x == 0 && offset_bounds.min_y == 0`, `max_y > 0` ; banc dont la config porte `topMargin` sélectionné (fixture recopiée de la tâche 3) → `min_y == -16`.
- [ ] **Step 2: rouge** — `devcontainer exec --workspace-folder /home/fab/Projects/OpenScale go test -run LabelGeometry ./internal/web/` → 404.
- [ ] **Step 3: implémenter** — le handler (godoc : pourquoi une source Go unique plutôt qu'une copie TypeScript qui dériverait).
- [ ] **Step 4: vert** — même commande, puis `go test -race ./internal/web/`.
- [ ] **Step 5: commit** — `feat(web): la route qui sert le gabarit de départ et les bornes du décalage`.

### Task 6: l'écran Étiquette — « Partir du gabarit livré » et le plancher calculé

**Files:**
- Modify: `web/src/admin/lib/api.ts` (à côté de `previewURL`, ligne 338)
- Modify: `web/src/admin/pages/Label.svelte:58-65` (`FLOOR_DOTS` remplacé), `:203-207` (`nudge`), `:332-366` (flèches et paragraphe `.bound`), `:402-412` (bouton, aide du champ gabarit)
- Modify: `docs/02-architecture.md` §14.4 (écran Étiquette)
- Test: `web/test/admin-label.test.ts` (branche `/admin/api/label/geometry` dans `fakeFetch`, ligne ~87 ; le test de la ligne 233 devient celui du plancher servi)
- Rebuild: `internal/web/dist` par `make front`

**Interfaces:**
- Consumes: la route de la tâche 5.
- Produces: `export interface LabelGeometryDTO { starter: Record<string, unknown> & { name: string }; offset_bounds: { min_x: number; max_x: number; min_y: number; max_y: number } | null }` ; `export async function fetchLabelGeometry(): Promise<LabelGeometryDTO>`.

- [ ] **Step 1: tests vitest** —
  - bornes `{min_x: 0, min_y: 0, …}` : `← 1 dot` et `↑ 1 dot` désarmés, deux appuis après `1 dot →` laissent `offset_x` à `0` (le test actuel, alimenté par la route) ;
  - bornes `{min_y: -16, …}` : `↑ 1 dot` armé, un appui écrit `offset_y = -1` ; le paragraphe dit `16 dots` ;
  - route en échec : plancher `0` (le plus prudent), aucune erreur affichée ;
  - bouton « Partir du gabarit livré » : écrit `printer.custom_template` = `starter` (égalité profonde) et `printer.template = "cooperative"`, `draft.dirty === true`, aucun `PUT` émis ;
  - brouillon portant déjà `printer.custom_template` : bouton désarmé, la phrase dit qu'il n'écrase pas le gabarit existant.
- [ ] **Step 2: rouge** — `devcontainer exec --workspace-folder /home/fab/Projects/OpenScale sh -c 'cd web && npx vitest run test/admin-label.test.ts'`.
- [ ] **Step 3: implémenter** — `bounds` lus à l'ouverture et après chaque enregistrement (même moment que l'image, mêmes raisons : ils décrivent la configuration en service) ; `floorX = bounds?.min_x ?? 0`. Textes : paragraphe `.bound` « Le décalage descend sous zéro seulement si le gabarit laisse de la marge en haut ou à gauche : ici jusqu'à {n} en horizontal, {m} en vertical. Le maximum est annoncé si l'enregistrement le dépasse. » ; aide du champ « weighing_identical est le gabarit de production ; le gabarit de la coopérative porte le nom écrit dans printer.custom_template. » ; sous le bouton « Copie le gabarit livré dans la configuration, sous le nom cooperative, et le choisit. Ajustez-le ensuite dans le document, puis enregistrez : l'aperçu le montre après l'enregistrement. » Docstrings TSDoc sur ce qui change.
- [ ] **Step 4: vert** — `make front-check`, `sh -c 'cd web && npm run lint && npm run format:check'`, puis `make front` et `git status internal/web/dist`.
- [ ] **Step 5: commit** — `feat(web): l'écran Étiquette part du gabarit livré et suit les bornes du gabarit` (avec `internal/web/dist`, §14.4).

### Task 7: la page du bénévole, le glossaire et le suivi

**Files:**
- Create: `handbook/gabarit.md` (modèle : `handbook/catalogue.md`)
- Modify: `mkdocs.yml:57-60` (« Gabarit d'étiquette: gabarit.md » après « Catalogue »)
- Modify: `docs/03-glossaire.md` (« gabarit de coopérative » ↔ `custom_template` / `CustomTemplate`), `handbook/odr/template.md` si elle affirme qu'un seul gabarit existe (vérifier par lecture)
- Modify: `SUIVI.md` (entrée datée du journal, en tête du tableau de `## Journal`)

- [ ] **Step 1: `handbook/gabarit.md`** — les cinq points de la spec (§ « Documentation bénévole à produire »), dans cet ordre : où se règle le gabarit (`printer.custom_template`, un seul par poste, partir du bouton) et le média (la résolution est celle de la tête) ; les deux polices par leur nom exact et pourquoi pas d'autre ; ce qui ne se règle jamais, avec les messages exacts des tâches 1 à 3 recopiés depuis le code ; la lecture de l'aperçu (même moteur, montré **après** enregistrement, compte à rebours de 60 s à confirmer) et la bande HRI trop étroite qu'il révèle ; le calibrage du rouleau en RAW (renvoi absolu GitHub vers §8.4). Un exemple JSON complet tiré de `StarterTemplate()` (sérialisé par un test ou `go run`, jamais recopié de mémoire).
- [ ] **Step 2: glossaire, `odr/template.md`, `SUIVI.md`** — l'entrée de journal : besoin de la Graoucoop, cause mesurée (contrôle 38 et `FLOOR_DOTS` bornés à zéro), ce qui est livré (ADR-062, ADR-063, contrôle 53), le défaut trouvé en route (tâche 4) et ce qui reste hors périmètre (diagnostic USB, éditeur visuel, gabarit à une ligne refusé).
- [ ] **Step 3: vérifier** — `devcontainer exec --workspace-folder /home/fab/Projects/OpenScale mkdocs build --strict` vert ; `grep -n "jamais négatif\|ne descend pas sous zéro\|FLOOR_DOTS" docs/02-architecture.md handbook/ web/src` ne rend plus que des passages historiques assumés.
- [ ] **Step 4: commit** — `docs: le gabarit d'étiquette se règle par coopérative`.
