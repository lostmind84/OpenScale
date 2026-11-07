# Diagnostic pas à pas de l'imprimante — plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** page **Matériel**, bloc **Imprimante**, un diagnostic en cinq étapes, sans mot de passe — (1) le périphérique existe, (2) le poste peut y écrire, (3) l'imprimante répond à la sonde de statut, (4) une étiquette de test part en entier, (5) le bénévole dit si elle est sortie — chaque échec portant une phrase française et un renvoi de documentation, plus un bouton « Copier le rapport (pour le forum) ».

**Architecture:** les étapes 1 à 3 sont **une seule** interrogation de statut, celle que le driver fait déjà (`raster.Printer.Status` → `Transport.Query` → `interrogate`), rendue par une nouvelle méthode `printing.Service.Interrogate` ; l'erreur d'ouverture remonte intacte dans le nouveau champ `ports.PrinterStatus.Cause` et se classe par `errors.Is`/`errors.As` (absent / refusé / connexion impossible). L'étape 4 est `Service.SelfTest("label")`, qui rend désormais un `ports.PrintReceipt`. L'ordonnancement et **tous** les textes vivent dans `internal/printing/diagnosis.go` (pur, testable sans matériel) ; `cmd/openscale` (`livePrinter`) y branche le transport **en service** ; `internal/web` n'ajoute que deux routes du bloc libre ; l'écran construit le rapport copiable à partir du JSON reçu.

**Tech Stack:** Go 1.26.10 (bibliothèque standard), Svelte 5 + TypeScript, vitest + jsdom, mkdocs.

**Spec:** `docs/superpowers/specs/2026-11-07-diagnostic-imprimante-design.md`

## Global Constraints

- Toutes les commandes passent par le devcontainer : `devcontainer exec --workspace-folder . <cmd>` ; git sur l'hôte.
- Zéro cgo, aucune dépendance nouvelle.
- Le français des messages suit `docs/03-glossaire.md` — « sonde », « auto-test », « inconnu » (jamais « erreur ») pour le silence du niveau N3.
- Chaque étape qui échoue porte une phrase française avec un renvoi de documentation, jamais un message technique brut (le texte brut va dans « observé », pas dans le remède).
- Docs et code dans le même commit ; une page du handbook accompagne le code, pas plus tard.
- TDD sur ce qui se calcule sans matériel : la classification d'une erreur d'ouverture (absent / refusé), le texte produit par chaque étape, le texte du rapport copiable.
- **AUCUN mot de passe nulle part dans le diagnostic, étape 4 comprise** (décision 7) : deux routes du bloc libre de `internal/web/routes.go`, jamais `guarded` ; aucun bouton du diagnostic ne porte `protected` ni ne passe par `admin.protect`.
- **Le silence à l'étape 3 n'est PAS un échec** : inconnu, pas faute (décision 2). **L'étape 5 est une question**, jamais une mesure (décision 3).
- Aucune note réseau / port 9100 / RS232 à l'écran : handbook seulement (décision 6).
- Les boutons d'auto-test existants (page Matériel, page Dépannage) et leurs messages ne changent pas ; le nombre d'octets reste propre au diagnostic (décision 9).
- Le rapport reste copiable quand l'étape 5 n'a pas de réponse : « sans réponse » (décision 10).
- Fichiers à ne jamais toucher ni indexer : `internal/diag/archive_writer.go`, `internal/update/stager.go`, `internal/web/admin_test.go`, `internal/web/journal.go`, `.superpowers/`. Toujours `git add <chemins explicites>`.
- **Écarts avec la spec, voulus :**
  1. **`web.SelfTester` ne change pas** (la spec le fait suivre `ports.Printer.SelfTest`). Son seul consommateur, le handler `selfTest`, ignore le reçu (décision 9) : en Go l'interface se déclare côté consommateur, et elle n'a pas l'usage du reçu. `livePrinter.SelfTest` adapte (`_, err := …`). Le commentaire de `contracts.go:250-251` (« ports.Printer satisfies it ») est corrigé. Effet de bord voulu : `internal/web/admin_test.go` (`b2Printer`) compile sans être touché.
  2. **`ports.PrinterStatus` gagne `Cause error` et `printing.Service` gagne `Interrogate`** — absents de « Ce qui change, et où », mais exigés par « Tranché en relecture » : `raster.Status` réduit aujourd'hui l'erreur de `Query` à un `Detail` formaté (`status.go:47-49`), et `Service.Status` passe par `Observe`, qui la perd encore (`status.go:386`). Appeler `Transport.Query` directement contournerait le verrou du driver (`raster.go:327`, `status.go:35`) et ouvrirait le nœud en concurrence avec le superviseur — l'anti-objectif n° 2. Même ADR que `SelfTest`.
  3. **`file`, étape 2 : « sans objet », justifiée par l'étape 4** (« l'étape 4 écrit dans ce dossier : c'est elle qui le dira »), pas par une inscriptibilité supposée — la spec la marquait **non vérifiée** ; on ne l'affirme donc pas, et aucun test n'a à la confirmer.
  4. **Une ligne de journal par parcours** : par la route de réponse quand l'étape 5 est posée ; par la route des étapes 1-4 quand le parcours s'y arrête (étape 5 « sans objet »). Jamais deux.

## Review Focus

- **Une `Cause` qui n'est pas une ouverture ratée** (`transport.ErrClosed` pendant un rechargement, contexte échu, driver fermé) rangée en « périphérique présent / droit d'écrire ». Les étapes 1-2 ne sont « réussi » que sur preuve positive : `Cause == nil`, ou une erreur **après** ouverture (`*fs.PathError` d'`Op` ≠ `"open"`, `*net.OpError` d'`Op` ≠ `"dial"`) ; sinon « inconnu ». Tests en tâche 3.
- **Le silence de l'étape 3 qui fuit en échec** quelque part : état Go, état JSON, libellé du rapport, ou étape 4 sautée après un silence. Tests en tâches 3 et 6.
- **Une deuxième ouverture** : `Diagnose` qui appelle `Interrogate` deux fois, ou un handler qui ajoute un `Status`/`Observe`. Preuve structurelle en tâche 3 : sur `transport.Devfile` à ouvreur compteur, **2** ouvertures quand tout passe, **1** quand l'ouverture échoue.
- **« Copier » sur une page servie en HTTP depuis un téléphone** : `navigator.clipboard` n'existe pas hors contexte sécurisé ; le bouton ne doit ni rien faire en silence ni se désactiver. Repli : texte sélectionné dans une zone visible et consigne « Ctrl+C ». Test en tâche 6.
- **Le journal technique** : une seule ligne par parcours (jamais une par étape), et le texte libre du bénévole (`seen`) borné à 200 caractères sur **une** ligne — un `\n` y fabriquerait une fausse ligne de journal. Tests en tâche 5.

---

### Task 1: l'auto-test rend son reçu (contrat des drivers, ADR-065)

**Files:**
- Modify: `internal/station/ports/ports.go:231-232` (`Printer.SelfTest`)
- Modify: `internal/printing/raster/selftest.go:25-67` (`SelfTest`, `printDemoLabel`, `printPattern` rendent le reçu de `p.Print` / `p.send`)
- Modify: `internal/printing/service.go:256-276`, `internal/printing/preview/driver.go:440-470`, `internal/printing/example/printer.go:316-340`, `internal/fake/printer.go:155-160` (reçu nul : un faux n'envoie rien), `cmd/openscale/wiring.go:243` (`unbuiltPrinter`), `cmd/openscale/admin.go:270-283` (`livePrinter.SelfTest` garde `error`, jette le reçu)
- Modify: `internal/printing/conformance/harness.go:187-191` (`selfTestQuietly` rend aussi le reçu), `check_selftest.go:38,110,154`, `check_invariant.go:122,161`
- Modify: `internal/web/contracts.go:250-255` (commentaire seul : l'interface reste à `error`, écart 1)
- Test (fakes et appels) : `internal/printing/harness_test.go:264`, `internal/printing/conformance/stub_test.go:354`, `internal/printing/conformance/conformance_test.go:84-86`, `internal/station/units_test.go:249`, `internal/printing/raster/selftest_test.go`, `raster/hardware_test.go:146,167,187`, `preview/driver_test.go:196,215`, `printing/service_test.go:415-468`
- Docs : `docs/02-architecture.md:386-387` (citation de l'interface) ; nouvel `### ADR-065 — L'auto-test rend son reçu, et le statut garde la cause de son échec` après ADR-064 (`:6062`), ligne Statut au format d'ADR-064 (`**Portée** : §8.5, §8.6, ports.Printer, ports.PrinterStatus`) ; `docs/07-ajouter-un-materiel.md:145` (une phrase : `SelfTest` rend le reçu de ce qu'il a remis au transport)

**Interfaces:**
- Produces: `SelfTest(ctx context.Context, what string) (ports.PrintReceipt, error)` sur `ports.Printer`, `*printing.Service`, `*raster.Printer`, `*preview.Printer`, `*example.Printer`, `*fake.Printer`, `unbuiltPrinter`.
- Unchanged: `web.SelfTester.SelfTest(ctx, what) error`, `livePrinter.SelfTest(ctx, what) error`.

- [ ] **Step 1: tests** —
  - `raster/selftest_test.go` : `TestTheLabelSelfTestReportsTheBytesItHandedOver` — `newPrinter` avec `DemoLabel`, `SelfTest(ctx, "label")` → `receipt.Bytes == len(transport.last(t))` et `> 0` ; idem `alignment` et `ruler`.
  - `service_test.go` : `TestTheServiceSelfTestPassesTheReceiptOn` — `stubPrinter` → `receipt.Bytes == 16310` ; sur échec, `receipt == ports.PrintReceipt{}`.
  - `conformance_test.go:84-86` : nouvelle ligne `{"a self-test that succeeds and reports no byte", "EverySelfTestAnswersAsDeclared", brokenSubject(func(s *stubPrinter) { s.selfTestReportsNoBytes = true })}`.
  - Les appels existants passent à `_, err := ….SelfTest(…)`.
- [ ] **Step 2: rouge** — `devcontainer exec --workspace-folder . go test ./internal/printing/... ./internal/station/... ./cmd/openscale/` → échecs de compilation, puis d'assertion.
- [ ] **Step 3: implémenter** — signatures ; `checkADeclaredSelfTestPrinted` signale aussi un succès avec `receipt.Bytes == 0` quand la destination a reçu quelque chose (même contrôle, le compte « dix-huit contrôles » de `docs/03-glossaire.md:890` ne bouge pas).
- [ ] **Step 4: vert** — même commande, puis `go test ./...` et `make boundary`.
- [ ] **Step 5: ADR-065** — contexte (le diagnostic doit dire « n octets sur N » et classer l'erreur d'ouverture ; le nombre était calculé puis jeté, `raster/selftest.go:51`) ; options (méthode nouvelle sur `ports.Transport` — refusée par la spec ; `Transport.Query` appelé par le diagnostic — contourne le verrou du driver, seconde façon de parler à l'imprimante ; élargir `SelfTest` et ajouter `PrinterStatus.Cause` — retenue) ; décision ; conséquences (tout driver rend son reçu, le banc l'exige ; `web.SelfTester` reste à `error`, décision 9). `Cause` y est décidé ici, codé en tâche 2.
- [ ] **Step 6: commit** — `feat(printing): l'auto-test rend le reçu de ce qu'il a remis` (code, ADR-065, `docs/07`).

### Task 2: le statut garde sa cause, et `Service.Interrogate`

**Files:**
- Modify: `internal/station/ports/ports.go:95-104` (champ `Cause`)
- Modify: `internal/printing/raster/status.go:33-72` (`Cause: err` dans les cas `ErrUnsupported` et `err != nil` ; cas `p.closed` : `Cause` = un `*ports.PrintError{Kind: ports.KindInternal, Op: "raster.Status", …}` construit sur place — **pas** `refuseWhenClosed`, qui reprend `p.mu`)
- Modify: `internal/printing/service.go` (après `Status`, `:279`)
- Test: `internal/printing/raster/raster_test.go:290` (`TestStatusIsHonestAboutWhatItCannotKnow`), `internal/printing/service_test.go`, `internal/printing/transport/transport_test.go` (ou `hardware_test.go` si l'ouverture réelle y vit déjà)
- Docs : `docs/03-glossaire.md:847` (`PrinterStatus` : ajouter `Cause`)

**Interfaces:**
- Produces:
  - `ports.PrinterStatus.Cause error` — « the transport failure behind this status, unchanged so a caller can `errors.Is` it ; nil when the probe completed ».
  - `func (s *printing.Service) Interrogate(ctx context.Context) ports.PrinterStatus` — **un** appel à `s.target().Status(ctx)`, rendu tel quel ; n'écrit ni `s.seen`, ni le journal, ni le compteur de rouleau.

- [ ] **Step 1: tests** —
  - `raster_test.go:290` : chaque cas vérifie `Cause` — `ErrUnsupported` → `errors.Is(status.Cause, ports.ErrUnsupported)` ; muette → `Cause == nil` ; nouveau cas `queryErr = &fs.PathError{Op: "open", Path: "/dev/usb/lp0", Err: fs.ErrNotExist}` → `PrinterFaulted` et `errors.Is(status.Cause, fs.ErrNotExist)` ; driver fermé → `Cause != nil`.
  - `service_test.go` : `TestInterrogateAsksTheDriverOnceAndKeepsTheCause` — stub `status = {Health: PrinterFaulted, Cause: errBoom}` → `Interrogate` rend `errors.Is(Cause, errBoom)`, `statusCalls == 1`, et `s.Report()` inchangé.
  - Ouvertures réelles (vérification des hypothèses de la spec) : `OpenSystemNode(filepath.Join(t.TempDir(), "lp0"))` → `errors.As(err, &pe)`, `pe.Op == "open"`, `errors.Is(err, fs.ErrNotExist)` ; fichier `chmod 0o000` → `fs.ErrPermission` (`t.Skip` si `os.Geteuid() == 0` : root ignore les droits) ; `DialSystemTCP` vers l'adresse d'un `net.Listen("tcp", "127.0.0.1:0")` aussitôt fermé → `errors.As(err, &oe)` et `oe.Op == "dial"`. Si ce dernier rougit, **ne pas** inventer de classement : relire `$(go env GOROOT)/src/net/dial.go` et rapporter.
- [ ] **Step 2: rouge** — `devcontainer exec --workspace-folder . go test ./internal/printing/... ./internal/station/ports/`.
- [ ] **Step 3: implémenter.**
- [ ] **Step 4: vert** — même commande, puis `go test -race ./internal/printing/...`.
- [ ] **Step 5: commit** — `feat(printing): le statut garde la cause de son échec`.

### Task 3: les cinq étapes, leurs textes, et la preuve d'une seule ouverture

**Files:**
- Create: `internal/printing/diagnosis.go`
- Test: `internal/printing/diagnosis_test.go` ; `internal/printing/raster/diagnosis_test.go` (preuve structurelle)
- Create: `handbook/imprimante.md` — squelette : une section par étape, ancre explicite `{#etape-1}` … `{#etape-5}` (`attr_list` est actif, `mkdocs.yml:72` ; le slug par défaut perdrait le « é »), ce que l'étape observe et quoi changer ; complété en tâche 7
- Docs : `docs/03-glossaire.md:876` (§ `internal/printing` : une ligne pour `Diagnose` · `DiagnosisStep` · `StepState` · `Subject` · `Prober` · `AnswerStep` · `DiagnosisGuide`)

**Interfaces:**
- Consumes: `Service.Interrogate`, `Service.SelfTest` (tâches 1-2) ; `domain.TransportDevfile|TCP|Winspool|File` (`internal/domain/config.go:42-45`).
- Produces:
  - `type StepState string` ; `StepPass = "pass"`, `StepFail = "fail"`, `StepUnknown = "unknown"`, `StepNotApplicable = "not_applicable"` (clés de `diag.Status.String()`, réécrites, **sans** importer `internal/diag`), `StepUnanswered = "unanswered"`.
  - `type DiagnosisStep struct { Number int; Title string; State StepState; Observed string; Remedy string; Doc string }` — `Remedy` et `Doc` vides sauf `fail`/`unknown`.
  - `type Subject struct { Transport string; Device string; Target string }` — `Transport` = `printer.options.transport` **en service** (`""` pour un driver sans transport) ; `Device` = la file, le nœud, l'adresse ou le dossier réellement écrit ; `Target` = `Describe()` du transport en service.
  - `type Prober interface { Interrogate(ctx context.Context) ports.PrinterStatus; SelfTest(ctx context.Context, what string) (ports.PrintReceipt, error) }` — `*Service` la satisfait (assertion `var _ Prober = (*Service)(nil)`).
  - `type DiagnosisProbes struct { Printer Prober; Queues func(context.Context) ([]string, error); Stat func(string) (fs.FileInfo, error) }` — `Stat` nil vaut `os.Stat`.
  - `func Diagnose(ctx context.Context, s Subject, p DiagnosisProbes) []DiagnosisStep` — toujours **cinq** étapes ; **exactement un** `Interrogate` quand `Transport` ∈ {devfile, tcp, winspool, file} ; `SelfTest(ctx, "label")` sauf si l'étape 1 ou 2 a échoué.
  - `func AnswerStep(printed bool, seen string) DiagnosisStep` — l'étape 5 répondue.
  - `const DiagnosisGuide = "https://lostmind84.github.io/OpenScale/imprimante/"` (`site_url` de `mkdocs.yml:14`) ; `Doc` = `DiagnosisGuide + "#etape-N"`.

Titres (maquette validée, constants) : 1 « Le périphérique existe » · 2 « Le poste a le droit d'y écrire » · 3 « L'imprimante répond quand on lui demande son état » · 4 « Une étiquette de test est envoyée en entier » · 5 « L'étiquette est-elle sortie ? ». Observés et remèdes : rédigés par le constructeur et **lus à voix haute** (spec, « Laissé au constructeur ») ; les tests n'en figent que les fragments ci-dessous.

- [ ] **Step 1: tests de table** (`diagnosis_test.go`, faux `Prober` qui compte ses appels, `Queues`/`Stat` factices) :
  - devfile, `Cause = &fs.PathError{Op: "open", Path: "/dev/usb/lp0", Err: fs.ErrNotExist}` → états `[fail, not_applicable, not_applicable, not_applicable, not_applicable]` ; étape 1 `Observed` contient `/dev/usb/lp0`, `Doc` finit par `#etape-1` ; **0** `SelfTest`.
  - devfile, `fs.ErrPermission` → `[pass, fail, n/a, n/a, n/a]` ; remède 2 contient `groupe lp`.
  - devfile, silence (`Health: PrinterUnknown`, `Cause: nil`, `Raw: nil`), `SelfTest` rend `{Bytes: 16214}` → `[pass, pass, unknown, pass, unanswered]` ; étape 3 : ni `Observed` ni `Remedy` ne contiennent `erreur`, `Remedy` contient `On continue` ; étape 4 `Observed` = `16 214 octets sur 16 214` (espace insécable fine U+202F entre milliers — le test l'écrit avec ` `).
  - devfile, `Cause = transport.ErrClosed`-like (`errors.New("fermé")`) → étapes 1 et 2 `unknown`, **jamais** `pass`.
  - devfile, `Cause = &fs.PathError{Op: "read", …}` → `[pass, pass, fail, …]`.
  - tcp, `Cause = &net.OpError{Op: "dial", Err: errors.New("connection refused")}` → `[fail, n/a, n/a, n/a, n/a]`, `Observed` contient `connexion impossible` **et** `connection refused` (texte brut, décision 8).
  - tcp, silence → étape 1 `pass` (`connexion établie`), étape 2 `not_applicable`.
  - winspool, `Queues` → `["SATO WS408_2"]`, `Device = "SATO WS408_2"`, `Cause = ports.ErrUnsupported` → `[pass, not_applicable, not_applicable, pass, unanswered]` ; avec `Device = "SATO WS408_1"` → étape 1 `fail`, `Observed` nomme les files vues, `SelfTest` **non** appelé ; `Queues` en erreur → étape 1 `unknown` et `SelfTest` appelé.
  - file, `Stat` → `fs.ErrNotExist` → étape 1 `fail` ; dossier présent → `[pass, not_applicable, not_applicable, …]`.
  - `Transport = ""` → étapes 1-3 `not_applicable`, **0** `Interrogate`, étape 4 jouée.
  - étape 4 en échec (`SelfTest` → `errors.New("x")`) → étape 4 `fail`, étape 5 `not_applicable`.
  - `SelfTest` → `{Bytes: 0}, nil` → étape 4 `unknown`.
  - `AnswerStep(true, "")` → `pass` ; `AnswerStep(false, "le voyant 1 clignote")` → `fail`, `Observed` contient `le voyant 1 clignote`, `Doc` finit par `#etape-5`.
  - Pour chaque cas : `Interrogate` appelé **≤ 1** fois ; tout `Doc` non vide pointe une ancre `{#etape-N}` présente dans `../../handbook/imprimante.md` (lu par le test).
- [ ] **Step 2: preuve structurelle** (`raster/diagnosis_test.go`) — `transport.NewDevfile` avec `Open` compteur rendant un `transport.Duplex` en mémoire (`Read` → `0, io.EOF`, `Write` accepte tout), sous `newPrinter(t, …)` avec `DemoLabel`, sous `printing.NewService` ; `printing.Diagnose(…, Subject{Transport: domain.TransportDevfile, Device: "/dev/usb/lp0"}, …)` → **2** ouvertures et `[pass, pass, unknown, pass, unanswered]` ; ouvreur rendant `&fs.PathError{Op: "open", Err: fs.ErrNotExist}` → **1** ouverture. Mutation à faire et défaire : un second `Interrogate` dans `Diagnose` doit faire rougir ce test.
- [ ] **Step 3: rouge** — `devcontainer exec --workspace-folder . go test ./internal/printing/ ./internal/printing/raster/`.
- [ ] **Step 4: implémenter** `diagnosis.go` et le squelette `handbook/imprimante.md`.
- [ ] **Step 5: vert** — même commande, `go test -race ./internal/printing/...`, `make boundary`, `mkdocs build --strict` (la page peut rester hors `nav` jusqu'à la tâche 7 ; si `--strict` refuse une page hors navigation, l'y ajouter dès maintenant).
- [ ] **Step 6: commit** — `feat(printing): le diagnostic de l'imprimante en cinq étapes`.

### Task 4: le diagnostic tourne sur l'imprimante EN SERVICE

**Files:**
- Modify: `cmd/openscale/admin.go:243-283` (`livePrinter` : champ `subject printing.Subject`, `hold(printer, subject)`, méthodes ci-dessous)
- Modify: `cmd/openscale/serve.go:237`, `cmd/openscale/wiring.go:62-76` (`printerRebuilder` passe le sujet de `next`)
- Modify: `cmd/openscale/wiring.go` (fonction `diagnosisSubject`, à côté de `declaresTransport` `:195`)
- Modify: `internal/web/contracts.go` (après `SelfTester`, `:250-255` : les deux types et l'interface ci-dessous, que la tâche 5 consomme)
- Test: `cmd/openscale/drivers_test.go` (motif de `:429`, `printerRebuilder` + `livePrinter`)

**Interfaces:**
- Consumes: `printing.Diagnose`, `printing.AnswerStep`, `printing.Subject`, `printing.DiagnosisProbes` (tâche 3) ; `platform.PrintQueues` (`internal/platform/printqueues.go:8`), `domain.DeviceKeyQueue` ; `(*printing.Service).Routing()` (`internal/printing/routing.go:33`) ; `labelsDir(dataDir)` (comme `newPrinter`).
- Produces:
  - Dans `internal/web` (qui n'importe pas `internal/printing`) : `type DiagnosisStep struct { Number int; Title, State, Observed, Remedy, Doc string }` ; `type PrinterDiagnosis struct { Target string; Steps []DiagnosisStep }` ; `type PrinterDiagnostician interface { DiagnosePrinter(ctx context.Context) (PrinterDiagnosis, error); AnswerStep(printed bool, seen string) DiagnosisStep }`, et l'assertion `var _ web.PrinterDiagnostician = (*livePrinter)(nil)` côté `cmd/openscale`.
  - `func diagnosisSubject(cfg domain.Config, reg *printing.Registry, dataDir string) printing.Subject` — `Transport` vide si `!declaresTransport(reg, cfg.Printer.Type)` ; `Device` : `queue` / `path` / `address`, et pour `file` `path` sinon `labelsDir(dataDir)` (même règle que `transport.New`, `transport.go:179-183`).
  - `func (l *livePrinter) hold(printer ports.Printer, subject printing.Subject)` — imprimante et sujet posés **sous le même verrou**.
  - `func (l *livePrinter) DiagnosePrinter(ctx context.Context) (web.PrinterDiagnosis, error)` — refuse en français quand `wrapper == nil` (même phrase que `printService`, `admin.go:299-301`) ou quand `wrapper.Routing().Fallback` (« les étiquettes partent sur l'imprimante de secours : repassez sur l'imprimante du poste, page Dépannage, avant de la diagnostiquer ») ; sinon `Subject.Target = wrapper.Routing().Name`, `Queues` = `platform.PrintQueues` filtré sur `Key == domain.DeviceKeyQueue`, puis conversion champ à champ.
  - `func (l *livePrinter) AnswerStep(printed bool, seen string) web.DiagnosisStep`.

- [ ] **Step 1: tests** —
  - `TestTheDiagnosisRunsOnThePrinterInService` — `shippedConfig(t)`, `Printer.Options = Options.WithText("transport", "file").WithText("path", dir)` (dir = `t.TempDir()`), `printerRebuilder(registries(), printerRegistry(), fake.NewClock(captureStart), nopLog{}, t.TempDir(), live)` → `live.DiagnosePrinter` : étapes `[pass, not_applicable, not_applicable, pass, unanswered]`, étape 4 `Observed` contient `octets sur`, et **un** fichier est apparu dans `dir`.
  - `TestTheDiagnosisFollowsAReload` — reconstruire avec `path` = un second dossier → l'étape 4 écrit dans le second, pas le premier.
  - `TestTheDiagnosisRefusesAnUnbuiltPrinter` — `live.hold(unbuiltPrinter{reason: "x"}, printing.Subject{})` → erreur contenant `page Matériel`.
  - `diagnosisSubject` : `preview` → `Transport == ""` ; `config-lacagette.json` → `{winspool, "SATO WS408_2"}`.
- [ ] **Step 2: rouge** — `devcontainer exec --workspace-folder . go test ./cmd/openscale/`.
- [ ] **Step 3: implémenter.**
- [ ] **Step 4: vert** — même commande, puis `go test ./...`.
- [ ] **Step 5: commit** — `feat(cmd): le diagnostic lit le transport que le service a ouvert`.

### Task 5: deux routes libres, et une ligne de journal par parcours

**Files:**
- Modify: `internal/web/server.go:46-92,98-130` (`Options.PrinterDiagnosis`, champ du `Server`, copie dans `New`)
- Modify: `internal/web/routes.go:38-67` (bloc **libre**, à côté de `test-label` `:54`)
- Create: `internal/web/diagnosis.go` (handlers + DTO)
- Modify: `cmd/openscale/serve.go:422` (`PrinterDiagnosis: live`)
- Test: `internal/web/diagnosis_test.go` (nouveau) ; `internal/web/harness_test.go:85-110,213-220` (`benchOptions.printerDiagnosis`, passé à `Options`)
- Docs : `docs/02-architecture.md:4363` (§14.5 : les deux routes, avec « libre, ADR-033 : tester ne change ni ce que le poste vend ni la façon dont il pèse ») ; le commentaire du bloc libre de `routes.go` les nomme

**Interfaces:**
- Consumes: `web.PrinterDiagnostician`, `web.PrinterDiagnosis`, `web.DiagnosisStep` (tâche 4).
- Produces :
  - `Options.PrinterDiagnosis PrinterDiagnostician` (nil = 501 sur les deux routes).
  - `POST /admin/api/printer/diagnosis` → 200 `{"target": "…", "steps": [{"number","title","state","observed","remedy","doc"} ×5]}` ; budget `ports.WithBudget(r.Context(), s.clock, deviceBudget)` (`server.go:41`) ; collaborateur nil → `unavailable` (501) ; erreur → 502 `ERR-PRN-01`. Quand l'étape 5 vaut `not_applicable`, **une** ligne `s.technical.Technical(…, "printer", "", …)`.
  - `POST /admin/api/printer/diagnosis/answer`, corps `{"steps": ["pass","pass","unknown","pass"], "printed": false, "seen": "…"}` (`decodeJSON`, `answers.go:71`) → 200 `{"step": {…étape 5…}}` et **une** ligne : niveau `domain.LevelInfo` si `printed`, `domain.LevelWarn` sinon ; message français ; détail stable `1=pass 2=pass 3=unknown 4=pass 5=fail vu=« … »`.
  - 400 si `steps` n'a pas 4 valeurs parmi `pass|fail|unknown|not_applicable`, si `seen` dépasse 200 caractères (runes), ou s'il contient un caractère de contrôle (`unicode.IsControl`).

- [ ] **Step 1: tests** (`newBench` **sans** `login`, faux diagnosticien) :
  - `TestTheDiagnosisAnswersWithoutAPassword` — mot de passe posé (`b.setPassword`, `harness_test.go:450`), **aucun** `b.login` : les deux routes répondent 200, jamais 401.
  - `TestTheDiagnosisSaysWhatIsMissing` — collaborateur nil → 501, corps contenant `pas disponible` (motif de `troubleshooting_test.go:189`).
  - `TestTheDiagnosisCarriesFiveSteps` — JSON décodé : 5 étapes, clés exactes ci-dessus, `state` de l'étape 3 = `unknown`.
  - `TestAnAnswerIsJournalledOnce` — une réponse `printed:false, seen:"le voyant 1 clignote"` → exactement **1** ligne nouvelle de `b.technical`, niveau `warn`, détail contenant `3=unknown` et `le voyant 1 clignote` ; un diagnostic dont l'étape 5 est `unanswered` → **0** ligne ; dont l'étape 5 est `not_applicable` → **1** ligne.
  - `TestAnAnswerIsRefusedWhenMalformed` — `steps` de 3 valeurs, valeur `"ok"`, `seen` de 201 runes, `seen` contenant `"\n"` → 400 chacun, aucune ligne de journal.
  - `TestTheDiagnosisIsBounded` — faux diagnosticien qui attend `ctx.Done()` ; avancer l'horloge du banc de `deviceBudget` → réponse 502 (si le banc ne sait pas avancer son horloge pendant une requête, le noter et s'en tenir à vérifier que `ctx` porte une échéance).
- [ ] **Step 2: rouge** — `devcontainer exec --workspace-folder . go test ./internal/web/`.
- [ ] **Step 3: implémenter** ; `routes.go` enregistre les deux routes **hors** de la carte `guarded`.
- [ ] **Step 4: vert** — même commande, `go test -race ./internal/web/ ./cmd/openscale/`, `go test ./...`.
- [ ] **Step 5: commit** — `feat(web): deux routes libres pour le diagnostic de l'imprimante` (code, §14.5).

### Task 6: l'écran — le déroulé, la question, et « Copier le rapport »

**Files:**
- Modify: `web/src/admin/lib/dto.ts` (`DiagnosisStepDTO`, `PrinterDiagnosisDTO`), `web/src/admin/lib/api.ts` (près de `testLabel`, `:186`)
- Create: `web/src/admin/lib/printer-report.ts`, `web/src/admin/components/PrinterDiagnosis.svelte`
- Modify: `web/src/admin/pages/Hardware.svelte:92` (`ActName` gagne `'diagnosis'`), `:793-829` (le composant sous les `<Act>` du bloc Imprimante, lancé par `act('diagnosis', …)`, `:398`), `:627-636` (commentaire : le diagnostic est la troisième exception, raison ADR-033)
- Test: `web/test/printer-report.test.ts`, `web/test/admin-printer-diagnosis.test.ts` (motif `mount` de `admin-hardware.test.ts`), `web/test/admin-families.test.ts:111-116`
- Docs : `docs/02-architecture.md` §14.4 — ligne **Matériel** du tableau (`:4288`) et un sous-paragraphe « Le diagnostic pas à pas de l'imprimante » après le tableau (cinq étapes, sans mot de passe, silence = inconnu, question au bénévole, rapport copiable construit par l'écran)
- Rebuild: `internal/web/dist` par `make front`

**Interfaces:**
- Produces:
  - `export function diagnosePrinter(): Promise<PrinterDiagnosisDTO>` ; `export function answerPrinterDiagnosis(body: { steps: StepState[]; printed: boolean; seen: string }): Promise<{ step: DiagnosisStepDTO }>`.
  - `export type StepState = 'pass' | 'fail' | 'unknown' | 'not_applicable' | 'unanswered'` ; `export const STATE_WORDS: Record<StepState, string>` = `réussi` / `échoué` / `inconnu` / `sans objet` / `sans réponse`.
  - `export function diagnosisReport(d: PrinterDiagnosisDTO): string`.
  - `PrinterDiagnosis.svelte`, props `{ disabled: boolean; busy: boolean; onrun: (job: () => Promise<void>) => void }`.

- [ ] **Step 1: tests vitest** —
  - `printer-report.test.ts` : un diagnostic `[pass, pass, unknown, pass, unanswered]` → le texte commence par `Diagnostic de l'imprimante — OpenScale`, porte la ligne `Imprimante : nœud d'impression /dev/usb/lp0`, une ligne `3. L'imprimante répond quand on lui demande son état : inconnu`, la ligne `5. L'étiquette est-elle sortie ? : sans réponse`, et **ne contient pas** `échoué` ; une étape `fail` ajoute `   À faire : …` et `   Documentation : https://…#etape-N` ; texte brut (aucun `<`, aucun `*`).
  - `admin-printer-diagnosis.test.ts` (`api` simulé) : « Lancer le diagnostic » → cinq étapes rendues, l'observé dans un élément à chasse fixe ; l'étape 3 `unknown` n'a pas la classe d'échec ; la question n'apparaît que si l'étape 5 vaut `unanswered` ; « Non » + texte → `answerPrinterDiagnosis` reçoit `{steps: ['pass','pass','unknown','pass'], printed: false, seen: 'le voyant 1 clignote'}` et l'étape 5 est remplacée ; « Copier le rapport » actif **avant** toute réponse ; avec `navigator.clipboard.writeText` simulé → appelé une fois avec `diagnosisReport(…)` ; **sans** `navigator.clipboard` → une zone de texte visible contient le rapport, sélectionné, et la consigne `Ctrl+C` s'affiche.
  - `admin-families.test.ts` : nouveau cas — la source de `PrinterDiagnosis.svelte` (lue comme `hardware`) : `guards(actSaying(…, 'Lancer le diagnostic')) === false`, et ni `protected` ni `admin.protect` n'y figurent.
- [ ] **Step 2: rouge** — `devcontainer exec --workspace-folder . sh -c 'cd web && npx vitest run test/printer-report.test.ts test/admin-printer-diagnosis.test.ts test/admin-families.test.ts'`.
- [ ] **Step 3: implémenter** — libellés de la maquette : titre de zone « Matériel › Imprimante › Diagnostic », boutons « Lancer le diagnostic » et « Copier le rapport (pour le forum) », question « L'étiquette est-elle sortie ? » avec « Oui » / « Non » et un champ facultatif « Ce que vous voyez sur l'imprimante » (200 caractères, `maxlength`). Aucune note réseau, port 9100 ou RS232 (décision 6).
- [ ] **Step 4: vert** — `devcontainer exec --workspace-folder . make front-check`, `sh -c 'cd web && npm run lint && npm run format:check'`, puis `make front` et `git status internal/web/dist`.
- [ ] **Step 5: commit** — `feat(web): le diagnostic pas à pas sur la page Matériel` (avec `internal/web/dist`, §14.4).

### Task 7: la documentation du bénévole seul

**Files:**
- Modify: `handbook/imprimante.md` (compléter le squelette de la tâche 3), `mkdocs.yml:57-62` (entrée « Imprimante » après « Gabarit d'étiquette »), `handbook/index.md:18-24` (une ligne du tableau « Par où commencer »)
- Modify: `SUIVI.md` (entrée datée en tête du tableau `## Journal`, `:1735`)

- [ ] **Step 1: `handbook/imprimante.md`** — dans la forme de `handbook/catalogue.md` : où est le bouton, ce que chaque étape observe et quoi changer (ancres inchangées), pourquoi l'étape 3 « inconnu » n'est pas une panne, pourquoi l'étape 5 est une question, le rapport à coller sur le forum des coopératives et le fichier de diagnostic qui reste l'outil d'un ticket ; **l'USB et le réseau** (décision 6) : une imprimante en USB est isolée du réseau du magasin — les autres postes ne peuvent ni l'ajouter automatiquement ni lui envoyer de travaux ; le RS232 est refusé par construction (contrôle 42, 16 ko par étiquette) ; renvoi vers `docs/07-ajouter-un-materiel.md` si l'imprimante déclarée ne répond vraiment jamais. Renvois vers `docs/` par URL GitHub absolues (ODR-0002).
- [ ] **Step 2: `SUIVI.md`** — ce qui est livré (cinq étapes, deux routes libres, ADR-065, le rapport copiable), l'écart 1 (`web.SelfTester` inchangé), la sortie des vérifications **recopiée telle quelle**, et le **non vérifié** : aucun essai sur la SATO WS408 de la Graoucoop, ni sur une file Windows réelle, ni de copie depuis un téléphone en HTTP.
- [ ] **Step 3: vérifier** — `devcontainer exec --workspace-folder . mkdocs build --strict` vert ; `go test ./internal/printing/` (le test des ancres relit la page) ; `grep -n "9100\|RS232" web/src/admin/components/PrinterDiagnosis.svelte` ne rend rien.
- [ ] **Step 4: commit** — `docs: diagnostiquer l'imprimante seul, étape par étape`.
