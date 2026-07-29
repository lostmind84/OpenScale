# Configuration livrée pré-remplie — plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Faire voyager dans la configuration livrée tout ce que les quatre postes partagent — décalage d'étiquette en tête — en ne retirant que les clés qui désignent un poste ou l'infrastructure d'un site.

**Architecture:** `Config.Export(includeHardware=false)` cesse de mettre à `nil` les trois cartes `DriverOptions` et retire une liste de clés déclarée une fois dans `internal/domain`. Deux helpers non exportés font le retrait, l'un à plat, l'autre dans un objet imbriqué (`printer.options.fallback`). Le reste de la chaîne — `make release`, `install.ps1`, `install.sh` — ne bouge pas : elle exporte déjà.

**Tech Stack:** Go 1.26.5, `CGO_ENABLED=0`. Aucune dépendance nouvelle.

## Global Constraints

- Le code est en **anglais** — identifiants, types, fonctions, **et commentaires**. La documentation est en français. Les messages destinés aux bénévoles sont en français.
- Les commentaires expliquent le **pourquoi**, jamais le quoi.
- **Zéro cgo.** Aucune dépendance nouvelle dans ce lot.
- **Aucune URL, aucun nom d'hôte, aucun compte, aucun secret** ne doit se retrouver dans le fichier produit par l'empaquetage. Le dépôt est public et l'archive est publiée sur GitHub.
- Le renommage `cagette` → `lacoope` est **hors périmètre** : le fichier reste `testdata/config-lacagette.json`, la fonction reste `LaCagetteRules()`, le drapeau reste `--tiers cagette`. Ne renommer aucun de ces trois.
- TDD strict : écrire le test, le **voir échouer**, écrire le minimum, le voir passer, commiter.
- Vérification finale de chaque tâche : `go test ./...` et `gofmt -l` sur les fichiers touchés.

## Structure des fichiers

| Fichier | Responsabilité | Tâche |
|---|---|---|
| `internal/domain/config.go` | Déclare `stationSpecificOptions`, `withoutKeys`, `withoutNestedKeys` ; `Export` les applique | 1 |
| `internal/domain/config_test.go` | Le contrat de l'export, clé par clé, dans les deux sens | 1 |
| `cmd/openscale/config_test.go` | Le filet indépendant : ce que le fichier livré ne contient jamais | 2 |
| `testdata/config-lacagette.json` | La source de la configuration livrée | 3 |
| `docs/02-architecture.md`, `docs/00-donnees-retirees.md`, `INSTALLATION.md` | Ce que les documents promettent | 4 |

---

### Task 1 : le retrait par clé dans `Export`

**Files:**
- Modify: `internal/domain/config.go:1687-1728` (`Export`), et ajout des helpers juste après
- Test: `internal/domain/config_test.go:1144-1171` (réécrit) et un test nouveau à sa suite

**Interfaces:**
- Consumes: `DriverOptions` (`config.go:496`, `map[string]json.RawMessage`), sa méthode `clone()` (`config.go:614`), sa méthode `Group(key string) (DriverOptions, bool)` (`config.go:573`) qui rend une copie **décodée** — la muter n'atteint pas le parent.
- Produces: `Config.Export(includeHardware bool) Config`, signature inchangée. La variable non exportée `stationSpecificOptions`, lue par les tests du même paquet.

- [ ] **Step 1: Réécrire le test du retrait**

Remplacer intégralement `TestExportWithoutHardwareDropsWhatBelongsToOneStation` (`internal/domain/config_test.go:1144-1171`) par ceci :

```go
func TestExportWithoutHardwareDropsWhatBelongsToOneStation(t *testing.T) {
	config := loadDelivered(t)
	// A local drop names a directory; the delivered file is on webdav, so the key
	// has to be put there for the test to have anything to assert on.
	setOption(t, config.Catalog.Options, "directory", `C:\ProgramData\OpenScale\data\catalog\incoming`)
	setOption(t, config.Printer.Options, "address", "192.168.0.43:9100")
	exported := config.Export(false)

	if exported.Station.Number != 0 || exported.Station.Name != "" {
		t.Errorf("station = %+v, le numéro et le nom ne s'exportent pas", exported.Station)
	}
	if exported.Network != (NetworkConfig{}) {
		t.Errorf("network = %+v, il ne s'exporte pas", exported.Network)
	}
	if exported.Admin.PasswordHash != "" || exported.Admin.RecoveryCodeHash != "" {
		t.Error("les empreintes admin ne s'exportent pas")
	}

	gone := []struct {
		path    string
		key     string
		options DriverOptions
	}{
		{"scale.options.port", "port", exported.Scale.Options},
		{"printer.options.queue", "queue", exported.Printer.Options},
		{"printer.options.address", "address", exported.Printer.Options},
		{"printer.options.path", "path", exported.Printer.Options},
		{"catalog.options.url", "url", exported.Catalog.Options},
		{"catalog.options.username", "username", exported.Catalog.Options},
		{"catalog.options.password", "password", exported.Catalog.Options},
		{"catalog.options.directory", "directory", exported.Catalog.Options},
	}
	for _, option := range gone {
		if _, present := option.options[option.key]; present {
			t.Errorf("%s s'exporte, alors qu'il désigne un poste ou un site", option.path)
		}
	}
	fallback, ok := exported.Printer.Options.Group("fallback")
	if !ok {
		t.Fatal("printer.options.fallback a disparu de l'export : seules ses clés de repli partent")
	}
	for _, key := range []string{"queue", "address", "path"} {
		if _, present := fallback[key]; present {
			t.Errorf("printer.options.fallback.%s s'exporte", key)
		}
	}

	// The original is untouched: an export is a copy, not a stripping.
	if config.Station.Number != 2 {
		t.Error("l'export ne doit rien retirer à la configuration en service")
	}
	if port, _ := config.Scale.Options.Text("port"); port != "COM8" {
		t.Error("l'export a retiré le port de la configuration en service")
	}
	if fallback, ok := config.Printer.Options.Group("fallback"); !ok {
		t.Error("l'export a retiré le repli de la configuration en service")
	} else if queue, _ := fallback.Text("queue"); queue != "SATO WS408_3" {
		t.Error("l'export a retiré la file de repli de la configuration en service")
	}
}
```

- [ ] **Step 2: Ajouter le test de ce qui DOIT voyager**

À la suite du précédent, dans le même fichier :

```go
// TestExportWithoutHardwareKeepsWhatTheFleetShares is the reason this lot exists.
//
// INSTALLATION.md promises the next stations that the label offset « voyage avec la
// configuration clonée ». It lives in printer.options, which the export used to drop
// whole, so the promise was false.
func TestExportWithoutHardwareKeepsWhatTheFleetShares(t *testing.T) {
	config := loadDelivered(t)
	exported := config.Export(false)

	kept := []struct {
		path    string
		key     string
		options DriverOptions
	}{
		{"printer.options.offset_x", "offset_x", exported.Printer.Options},
		{"printer.options.offset_y", "offset_y", exported.Printer.Options},
		{"printer.options.darkness", "darkness", exported.Printer.Options},
		{"printer.options.speed", "speed", exported.Printer.Options},
		{"printer.options.transport", "transport", exported.Printer.Options},
		{"scale.options.baud", "baud", exported.Scale.Options},
		{"scale.options.parity", "parity", exported.Scale.Options},
		{"catalog.options.separator", "separator", exported.Catalog.Options},
		{"catalog.options.poll_interval_s", "poll_interval_s", exported.Catalog.Options},
		{"catalog.options.max_weighable_drop", "max_weighable_drop", exported.Catalog.Options},
	}
	for _, option := range kept {
		if _, present := option.options[option.key]; !present {
			t.Errorf("%s ne voyage pas, alors que les quatre postes le partagent", option.path)
		}
	}
	// The grid, the template and the coop name were already travelling: they must
	// keep doing so.
	if len(exported.Pricing.Tiers) != 2 || exported.Printer.Template != DefaultTemplateName {
		t.Error("la grille de tarifs et le gabarit doivent voyager")
	}
	if exported.Station.Coop != config.Station.Coop {
		t.Error("le nom de la coopérative doit voyager : il est partagé par les quatre postes")
	}
}
```

- [ ] **Step 3: Lancer les deux tests et les voir échouer**

Run: `go test ./internal/domain/ -run "TestExportWithoutHardware" -count=1 -v`

Expected: les deux ÉCHOUENT. Le premier sur `printer.options.fallback a disparu de l'export` (les cartes sont mises à `nil`), le second sur les dix `ne voyage pas`. Si l'un des deux passe, l'implémentation est déjà là — arrêter et comprendre pourquoi.

- [ ] **Step 4: Déclarer la liste de retrait**

Dans `internal/domain/config.go`, juste **avant** `func (c *Config) Export` (ligne 1702) :

```go
// stationSpecificOptions names the driver option keys an export must not carry when
// it is meant to seed ANOTHER station.
//
// Everything else in the three option maps travels, and that default is deliberate: a
// driver option is a setting the fleet SHARES until somebody proves otherwise, and the
// proof is written here. Dropping the maps whole was the opposite default, and it made
// INSTALLATION.md lie — it promises the label offset travels with the cloned
// configuration, and printer.options went out with it.
//
// Two kinds of key are named, and only those two: what designates ONE station (a
// serial port, a Windows queue), and what designates ONE SITE's infrastructure (a
// host, an account, a path). A value that is neither belongs to the fleet.
var stationSpecificOptions = struct {
	scale    []string
	printer  []string
	fallback []string
	catalog  []string
}{
	// COM7 on this station, something else on the next one.
	scale: []string{"port"},
	// A Windows queue name differs per machine: the « _2 » of « SATO WS408_2 » is a
	// duplicate suffix Windows added, measured on PC-RECEPTION. And `address` is a
	// HOST — 192.168.0.43:9100 on the bench — which this repository never ships
	// (docs/00-donnees-retirees.md).
	printer:  []string{"queue", "address", "path"},
	fallback: []string{"queue", "address", "path"},
	// The share and the account belong to one site. The password leaves in NO mode,
	// and that is handled before this list, unconditionally.
	catalog: []string{"url", "username", "directory"},
}

// withoutKeys returns the options minus the named keys.
//
// An absent block stays absent: returning an empty map where there was none would
// turn « ce poste ne déclare pas d'imprimante » into « ce poste déclare une
// imprimante sans rien dedans », which validates differently.
func withoutKeys(options DriverOptions, keys []string) DriverOptions {
	if options == nil {
		return nil
	}
	out := options.clone()
	for _, key := range keys {
		delete(out, key)
	}
	return out
}

// withoutNestedKeys does the same inside one nested option object, such as
// printer.options.fallback.
//
// A group that cannot be read is left ALONE rather than dropped: hiding a malformed
// value would send the operator looking for a key the file still carries.
func withoutNestedKeys(options DriverOptions, group string, keys []string) DriverOptions {
	nested, ok := options.Group(group)
	if !ok {
		return options
	}
	for _, key := range keys {
		delete(nested, key)
	}
	raw, err := json.Marshal(nested)
	if err != nil {
		return options
	}
	out := options.clone()
	out[group] = raw
	return out
}
```

- [ ] **Step 5: Appliquer la liste dans `Export`**

Dans `internal/domain/config.go`, remplacer le bloc `if includeHardware { … }` de `Export` (lignes 1718-1727) par :

```go
	if includeHardware {
		return out
	}
	out.Station.Number, out.Station.Name = 0, ""
	out.Network = NetworkConfig{}
	out.Admin.RecoveryCodeHash = ""
	out.Scale.Options = withoutKeys(out.Scale.Options, stationSpecificOptions.scale)
	out.Printer.Options = withoutNestedKeys(
		withoutKeys(out.Printer.Options, stationSpecificOptions.printer),
		"fallback", stationSpecificOptions.fallback)
	out.Catalog.Options = withoutKeys(out.Catalog.Options, stationSpecificOptions.catalog)
	return out
```

- [ ] **Step 6: Mettre le commentaire de `Export` d'accord avec le code**

Remplacer les lignes 1689-1692 du commentaire de `Export` par :

```go
// With includeHardware false it drops station.number, station.name, network, the
// admin fingerprints, and the option keys of stationSpecificOptions -- a serial
// port, a print queue, a host, an account, a path. What is left is what four
// stations of one fleet share, and it is what "clone a station" copies (§11.5).
```

- [ ] **Step 7: Lancer les deux tests et les voir passer**

Run: `go test ./internal/domain/ -run "TestExportWithoutHardware" -count=1 -v`
Expected: PASS tous les deux.

- [ ] **Step 8: Lancer le paquet entier, puis la suite**

Run: `go test ./internal/domain/ -count=1`
Expected: ok. `TestExportNeverCarriesAPassword` et `TestExportWithHardwareKeepsTheRecoveryCode` doivent passer **sans avoir été touchés** — si l'un des deux tombe, l'implémentation déborde de son périmètre.

Run: `go test ./... -count=1`
Expected: ok partout. `cmd/openscale` porte `TestCloningAStationShowsTheSAMEEightCharacters`, qui doit rester vert : l'empreinte ne dépend pas des clés déplacées.

- [ ] **Step 9: Format et vet**

Run: `gofmt -l internal/domain/config.go internal/domain/config_test.go`
Expected: aucune sortie.

Run: `go vet ./internal/domain/`
Expected: aucune sortie.

- [ ] **Step 10: Commit**

```bash
git add internal/domain/config.go internal/domain/config_test.go
git commit -m "feat(config): l'export ne retire que ce qui designe un poste ou un site"
```

---

### Task 2 : le filet indépendant sur le fichier livré

Ce test ne lit pas `stationSpecificOptions`. Il cherche les **valeurs** qui ne doivent jamais partir. Si quelqu'un ajoute demain une clé de driver portant un hôte sans la classer, la tâche 1 ne le verra pas et celle-ci si.

**Files:**
- Test: `cmd/openscale/config_test.go` — ajouter à la fin du fichier

**Interfaces:**
- Consumes: `deliveredConfig(t *testing.T) string` (`cmd/openscale/config_test.go:16`), qui rend le chemin de `testdata/config-lacagette.json`. `domain.Config`, `Config.Export`.
- Produces: rien. C'est un test terminal.

- [ ] **Step 1: Écrire le test**

Ajouter à la fin de `cmd/openscale/config_test.go` :

```go
// TestTheDeliveredExportShipsNoHostNoAccountNoQueue is the net under the strip list.
//
// It asserts on VALUES and not on keys, so it still bites the day somebody adds a
// driver option carrying a host without classing it in stationSpecificOptions. The
// archive is published on GitHub: what leaves here leaves for good.
func TestTheDeliveredExportShipsNoHostNoAccountNoQueue(t *testing.T) {
	raw, err := os.ReadFile(deliveredConfig(t))
	if err != nil {
		t.Fatalf("lecture de la configuration livrée : %v", err)
	}
	var delivered domain.Config
	if err := json.Unmarshal(raw, &delivered); err != nil {
		t.Fatalf("décodage de la configuration livrée : %v", err)
	}

	shipped, err := json.Marshal(delivered.Export(false))
	if err != nil {
		t.Fatalf("encodage de l'export : %v", err)
	}

	forbidden := map[string]string{
		"dav.example.org": "un nom d'hôte",
		"balance":         "un compte",
		"SATO WS408_2":    "une file d'impression",
		"SATO WS408_3":    "une file d'impression de repli",
		"COM8":            "un port série",
	}
	for value, what := range forbidden {
		if bytes.Contains(shipped, []byte(value)) {
			t.Errorf("le fichier livré porte %s (%q) : il est publié sur GitHub et installé sur quatre postes",
				what, value)
		}
	}
}
```

- [ ] **Step 2: Ajouter l'import manquant**

`cmd/openscale/config_test.go` importe déjà `encoding/json`, `os`, `path/filepath`, `strings`, `testing`, `openscale/internal/domain` et `openscale/internal/web`. Il manque **`bytes`** : l'ajouter au premier groupe d'imports, en gardant l'ordre alphabétique.

- [ ] **Step 3: Lancer le test**

Run: `go test ./cmd/openscale/ -run TestTheDeliveredExportShipsNoHostNoAccountNoQueue -count=1 -v`
Expected: PASS, parce que la tâche 1 est déjà faite.

- [ ] **Step 4: Vérifier que le test MORD**

C'est l'étape qui donne sa valeur au filet. Retirer temporairement `"queue"` de `stationSpecificOptions.printer` dans `internal/domain/config.go`, relancer :

Run: `go test ./cmd/openscale/ -run TestTheDeliveredExportShipsNoHostNoAccountNoQueue -count=1`
Expected: FAIL, `le fichier livré porte une file d'impression ("SATO WS408_2")`.

Remettre `"queue"`, relancer, attendre PASS. **Ne pas commiter la version amputée.**

- [ ] **Step 5: Commit**

```bash
git add cmd/openscale/config_test.go
git commit -m "test(config): le fichier livre ne porte ni hote, ni compte, ni file, ni port"
```

---

### Task 3 : `station.coop` devient « La Coope »

**Files:**
- Modify: `testdata/config-lacagette.json:6`

**Interfaces:**
- Consumes: rien.
- Produces: la valeur `"La Coope"` dans `station.coop`, lue par tout ce qui charge la configuration livrée.

- [ ] **Step 1: Changer la valeur**

Dans `testdata/config-lacagette.json`, ligne 6, remplacer :

```json
  "station": { "number": 2, "name": "Poste 2 — fruits", "coop": "Les Amis de la Coopé" },
```

par :

```json
  "station": { "number": 2, "name": "Poste 2 — fruits", "coop": "La Coope" },
```

Ne rien changer d'autre dans ce fichier. En particulier, **ne pas** renommer le fichier.

- [ ] **Step 2: Lancer la suite complète**

Run: `go test ./... -count=1`
Expected: ok partout. `cmd/openscale/serve_test.go:294` **pose** cette chaîne sur une configuration de test, il ne l'affirme pas sur celle qui est livrée : il n'a pas à bouger. Si un test tombe, lire son assertion avant de le corriger — une assertion sur le nom d'une coopérative est probablement une assertion qui n'aurait jamais dû exister.

- [ ] **Step 3: Lancer la suite front**

Run: `npm --prefix web test`
Expected: ok. `web/test/fixtures/odoo.ts` reprend les catégories et la remise de la configuration livrée, pas le nom de la coopérative.

- [ ] **Step 4: Commit**

```bash
git add testdata/config-lacagette.json
git commit -m "fix(config): la configuration livree nomme La Coope, pas une autre cooperative"
```

---

### Task 4 : les documents disent ce que le code fait

**Files:**
- Modify: `docs/02-architecture.md` §11.5, le nœud `FILE` du diagramme
- Modify: `docs/00-donnees-retirees.md`, section « Ce que cela change pour la mise en service »
- Modify: `INSTALLATION.md`, tableau « Les postes suivants »

**Interfaces:**
- Consumes: la liste de la tâche 1.
- Produces: rien.

- [ ] **Step 1: Corriger le diagramme de §11.5**

Dans `docs/02-architecture.md`, le nœud du diagramme Mermaid dit aujourd'hui :

```
FILE["config-station2-2026-07-24.json<br/>SANS station.number, station.name,<br/>scale.options, printer.options, catalog.options,<br/>network et les empreintes admin"]
```

Le remplacer par :

```
FILE["config-station2-2026-07-24.json<br/>SANS station.number, station.name, network,<br/>les empreintes admin, et les clés qui désignent<br/>un poste ou un site : port série, file d'impression,<br/>adresse, chemin, URL, compte, mot de passe"]
```

- [ ] **Step 2: Ajouter la phrase qui dit le défaut par défaut**

Dans `docs/02-architecture.md`, juste sous le diagramme de §11.5, ajouter un paragraphe :

```markdown
**Le reste des options de driver voyage**, et ce défaut est délibéré : une option de
réglage est partagée par les quatre postes jusqu'à preuve du contraire, et la preuve
s'écrit dans `stationSpecificOptions` (`internal/domain/config.go`). Le décalage
d'étiquette est le cas qui a tranché — la notice promet depuis toujours qu'il voyage
avec la configuration clonée, et il partait avec `printer.options`.
```

- [ ] **Step 3: Resserrer `00-donnees-retirees.md`**

Dans `docs/00-donnees-retirees.md`, section « Ce que cela change pour la mise en service », remplacer :

```
Il y a donc un seul endroit à renseigner le jour de l'installation, et c'est déjà
l'inconnue n° 9 de `docs/02-architecture.md` §21 : le bloc `catalog.options` de
`config-lacagette.json`.
```

par :

```
Il y a donc un seul endroit à renseigner le jour de l'installation, et c'est déjà
l'inconnue n° 9 de `docs/02-architecture.md` §21 : **trois clés** du bloc
`catalog.options` de `config-lacagette.json` — `url`, `username` et `password`. Le
reste du bloc — séparateur, seuils, cadence — voyage avec la configuration livrée
depuis le 29/07/2026, et un test refuse qu'un hôte, un compte ou une file
d'impression s'y glisse (`cmd/openscale/config_test.go`).
```

- [ ] **Step 4: `INSTALLATION.md` peut enfin dire vrai**

Dans `INSTALLATION.md`, la ligne du tableau « Les postes suivants » qui dit
« Étapes 5 et 6 : balance et imprimante (le décalage est déjà bon) » est désormais
exacte pour un poste installé avec la nouvelle archive. Ajouter juste sous le tableau :

```markdown
> **Le décalage voyage vraiment** : il est dans la configuration livrée, avec le
> noircissement, la vitesse et les réglages série de la balance. Vérifiez-le sur la
> première étiquette du poste cloné plutôt que de le régler à nouveau.

**N'écris aucun numéro de version dans cette phrase**, ni « v0.6 », ni « depuis la
version … ». Le lot ne décide pas dans quelle version il sortira, et une notice qui
promet une version inexistante est un défaut.
```

- [ ] **Step 5: Vérifier que rien n'a cassé**

Run: `go test ./... -count=1`
Expected: ok. `deploy/deploy_test.go` lit des documents ; une phrase ajoutée ne doit rien casser, mais il faut le vérifier plutôt que le supposer.

- [ ] **Step 6: Commit**

```bash
git add docs/02-architecture.md docs/00-donnees-retirees.md INSTALLATION.md
git commit -m "docs: l'export retire des cles, plus des blocs entiers"
```

---

## Vérification finale du lot

- [ ] **Suite complète avec le détecteur de concurrence**

Run: `go test -race ./... -count=1`
Expected: aucun `FAIL`.

- [ ] **Format et vet sur tout**

Run: `go vet ./...`
Expected: aucune sortie.

- [ ] **Mesurer ce que le poste neuf demande encore**

La spécification fait du nombre de fautes un critère de recette, pas une prévision. Le mesurer pour de vrai :

```
go run ./cmd/openscale config validate testdata/config-lacagette.json
```

puis, sur l'export livré, compter les fautes que `doctor` énumérerait. Écrire le chiffre obtenu dans `SUIVI.md`, à côté des neuf mesurées le 29/07/2026 sur la v0.5. Un chiffre mesuré, jamais un chiffre attendu.
