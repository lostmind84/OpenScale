# Redémarrer sans redémarrer Windows — plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Donner à un poste enfermé sous kiosque quatre gestes de reprise — relire son
`config.json`, redémarrer son service, redémarrer l'ordinateur, et la même chose en ligne
de commande — sans console d'administrateur ni coupure de courant.

**Architecture:** Trois routes neuves derrière la session administrateur, plus une action
de CLI. Aucune ne réinvente de mécanisme : la relecture rejoue le chemin de
`restoreConfig`, le redémarrage du service passe par l'arrêt ordonné de §13.4 et un code
de sortie non nul que le SCM et systemd rattrapent déjà, le redémarrage machine délègue à
`shutdown.exe` / `systemctl` derrière un compte à rebours annulable porté par l'horloge
injectée.

**Tech Stack:** Go 1.2x sans cgo · `golang.org/x/sys/windows` · Svelte 5 (runes) ·
vitest · `go test`

## Contraintes globales

- **Zéro cgo.** `CGO_ENABLED=0` sur toute la vérification. Aucune dépendance neuve.
- **Code en anglais, commentaires compris. Documentation et messages utilisateurs en
  français.** Les fichiers Svelte de `web/src/admin/` commentent en anglais (voir
  `Station.svelte`) ; les fichiers `web/src/admin/lib/api.ts` commentent en français.
  Suivre le fichier qu'on modifie.
- **Les commentaires expliquent le *pourquoi*, jamais le *quoi*.**
- **Aucun lien de session Claude nulle part** — ni commit, ni fichier, ni PR.
- **Vérification complète avant de déclarer fini :**
  `CGO_ENABLED=0 go test ./... -count=1`, `gofmt -l .`, `go vet ./...`,
  `go run ./tools/boundary`, `go run ./tools/deps`, et `npm test` dans `web/` dès qu'un
  fichier de `web/` est touché.
- **Spec de référence :**
  `docs/superpowers/specs/2026-07-31-redemarrer-sans-redemarrer-windows-design.md`.
- **Ne pas toucher aux compteurs de `SUIVI.md`** avant la tâche 9, qui les mesure.

---

## Structure des fichiers

| Fichier | Responsabilité | Tâche |
|---|---|---|
| `internal/station/hub.go` | `DowntimeGuard` (renommé) | 1 |
| `internal/station/downtime_guard_test.go` | renommé depuis `update_guard_test.go` | 1 |
| `internal/update/service.go` | la méthode de l'interface `Guard` suit le renommage | 1 |
| `internal/web/maintenance.go` | **neuf** — les trois handlers de reprise et leurs codes | 2, 3, 5 |
| `internal/web/maintenance_test.go` | **neuf** — leurs tests | 2, 3, 5 |
| `internal/web/reboot.go` | **neuf** — le compte à rebours annulable, sans HTTP | 5 |
| `internal/web/reboot_test.go` | **neuf** | 5 |
| `internal/web/server.go` | `Restarter`, `Rebooter` dans `Options`, routes protégées | 2, 3, 5 |
| `internal/platform/reboot.go` · `reboot_windows.go` · `reboot_other.go` | **neufs** — redémarrer l'ordinateur | 4 |
| `internal/platform/supervised.go` · `_windows.go` · `_other.go` | **neufs** — « suis-je sous un superviseur ? » | 3 |
| `cmd/openscale/serve.go` | le canal de redémarrage dans le `select`, le câblage | 3, 5 |
| `cmd/openscale/maintenance.go` | **neuf** — les adaptateurs `restarterFor` / `rebooterFor` | 3, 5 |
| `cmd/openscale/service.go` | l'action `restart` | 6 |
| `deploy/linux/install.sh` · `openscale-reboot.rules` | la règle polkit | 7 |
| `cmd/openscale/doctor.go` | le contrôle « peut-il redémarrer l'ordinateur ? » | 7 |
| `web/src/admin/components/Maintenance.svelte` | **neuf** — la rubrique et ses trois boutons | 8 |
| `web/src/admin/lib/api.ts` · `dto.ts` | les quatre appels et leurs types | 8 |
| `web/src/admin/pages/Station.svelte` | monte la rubrique ; son en-tête est corrigé | 8 |
| `web/test/admin-maintenance.test.ts` | **neuf** | 8 |
| `docs/02-architecture.md` · `03-glossaire.md` · `TROUBLESHOOTING.md` · `SUIVI.md` | la trace écrite | 9 |

`Maintenance.svelte` est un composant à part et non une section de plus dans
`Station.svelte` : cette page fait déjà 1 143 lignes.

---

## Task 1: Le garde change de nom

`Hub.UpdateGuard` gardera trois actes au lieu d'un. Renommage pur : aucune règle ne bouge,
tous les tests restent verts sans être réécrits.

**Files:**
- Modify: `internal/station/hub.go:259-267`
- Rename: `internal/station/update_guard_test.go` → `internal/station/downtime_guard_test.go`
- Modify: `internal/update/service.go:45-49`, `:171`
- Modify: `internal/update/service_test.go:30`
- Modify: `cmd/openscale/update.go:23-24`
- Modify: `cmd/openscale/serve.go:359`
- Modify: `internal/domain/profiles.go:145`, `internal/domain/profiles_test.go:157` (deux commentaires qui nomment la méthode)

**Interfaces:**
- Produces: `func (h *station.Hub) DowntimeGuard() (bool, string)` — vrai quand le poste
  peut être coupé, sinon la phrase française du refus.
- Produces: l'interface `update.Guard` déclare désormais `DowntimeGuard() (bool, string)`.

- [ ] **Step 1: Renommer la méthode et la fonction de règle**

Dans `internal/station/hub.go`, remplacer le bloc `UpdateGuard` par :

```go
// DowntimeGuard reports whether the station may be taken down, and says IN FRENCH
// why not when it may not.
//
// It answers for the THREE acts that stop the station: installing a new version,
// restarting the service, restarting the machine. The name says « taken down » and
// not « updated » because the rule never depended on what came after the stop --
// what it protects is the weighing in progress and the catalogue not yet in service.
//
// The rule lives here and not in the HTTP layer, for one reason: the HTTP layer
// would have to read a state in order to deduce a rule, and the rule would then
// exist in two places. It asks a question and renders the answer.
func (h *Hub) DowntimeGuard() (bool, string) {
	return downtimeGuardFor(h.State().State, h.catalogWaiting.Load())
}
```

et renommer `updateGuardFor` en `downtimeGuardFor` en laissant son corps et ses
commentaires intacts.

- [ ] **Step 2: Suivre le renommage chez les appelants**

`internal/update/service.go`, interface `Guard` :

```go
// Guard is what the service asks before taking the station down. Declared here,
// on the consumer's side; *station.Hub satisfies it.
type Guard interface {
	// DowntimeGuard reports whether the station may be taken down, and says in
	// French why not when it may not.
	DowntimeGuard() (bool, string)
}
```

et ligne 171 : `if allowed, reason := s.Guard.DowntimeGuard(); !allowed {`.

`cmd/openscale/update.go` :

```go
// DowntimeGuard answers by calling the function.
func (f guardFunc) DowntimeGuard() (bool, string) { return f() }
```

`cmd/openscale/serve.go:359` : `guardFunc(func() (bool, string) { return st.Hub().DowntimeGuard() })`.

`internal/update/service_test.go:30` : `func (g stubGuard) DowntimeGuard() (bool, string)`.

Dans `internal/station/downtime_guard_test.go`, les trois appels `b.hub.UpdateGuard()`
deviennent `b.hub.DowntimeGuard()`. Dans `internal/domain/profiles.go:145` et
`profiles_test.go:157`, les deux commentaires citent `Hub.DowntimeGuard`.

- [ ] **Step 3: Vérifier qu'il ne reste rien**

Run: `grep -rn "UpdateGuard\|updateGuardFor" --include=*.go .`
Expected: aucune sortie.

- [ ] **Step 4: Tests**

Run: `CGO_ENABLED=0 go test ./internal/... ./cmd/... -count=1`
Expected: PASS, même nombre de tests qu'avant.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(station): le garde s'appelle DowntimeGuard, parce qu'il garde trois arrêts

Il refusait de couper le poste pour installer une version. Il refusera
aussi de le couper pour redémarrer le service et pour redémarrer
l'ordinateur : la règle n'a jamais dépendu de ce qui venait après l'arrêt.
Renommage seul, aucune règle touchée."
```

---

## Task 2: Relire le fichier de configuration

**Files:**
- Create: `internal/web/maintenance.go`
- Create: `internal/web/maintenance_test.go`
- Modify: `internal/web/server.go:509-527` (table `guarded`)

**Interfaces:**
- Consumes: `ConfigStore.Read(ctx) (domain.Config, error)`, `Controller.Reload(station.ReloadRequest) (station.ReloadOutcome, error)`, `Controller.PendingConfirmation() time.Time`, `(*Server).configPayload(domain.Config, *confirmationDTO) configDTO`, `(*Server).confirmationOf([]string, time.Time) *confirmationDTO`, `writeProblem`, `writeJSON`, `unavailable`, `faultsOf`.
- Produces: `POST /admin/api/config/reload` → 200 `configDTO` · 409 · 422 `problem` · 503.

- [ ] **Step 1: Écrire les tests qui échouent**

Créer `internal/web/maintenance_test.go` :

```go
package web

import (
	"context"
	"net/http"
	"testing"

	"openscale/internal/domain"
)

// TestRereadingTheFilePutsItInService: a config.json edited by hand enters service
// without the station being stopped, which is the whole point of the route.
func TestRereadingTheFilePutsItInService(t *testing.T) {
	store := &memoryConfigStore{}
	b := newBench(t, func(o *benchOptions) { o.configStore = store })
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	edited := b.hub.Config()
	edited.Station.Name = "Rayon vrac"
	store.config = edited

	served := decodeStatus[configDTO](t,
		b.post("/admin/api/config/reload", `{}`), http.StatusOK)
	if served.Fingerprint != edited.Fingerprint() {
		t.Fatalf("empreinte servie %q, attendu %q : le fichier n'est pas entré en service",
			served.Fingerprint, edited.Fingerprint())
	}
	if store.saves != 0 {
		t.Errorf("%d écriture(s) du fichier : la relecture n'écrit rien, le document EST déjà le fichier",
			store.saves)
	}
}

// TestRereadingAFaultyFileRefusesWithEveryFault: one fault at a time is a screen
// somebody gives up on (§11.3).
func TestRereadingAFaultyFileRefusesWithEveryFault(t *testing.T) {
	store := &memoryConfigStore{}
	b := newBench(t, func(o *benchOptions) { o.configStore = store })
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	broken := b.hub.Config()
	broken.Journal.MaxRows = 3
	broken.Catalog.Type = "n-existe-pas"
	store.config = broken

	answer := decodeStatus[problem](t,
		b.post("/admin/api/config/reload", `{}`), http.StatusUnprocessableEntity)
	if len(answer.Faults) < 2 {
		t.Fatalf("%d faute(s) remontée(s), attendu au moins 2 : elles doivent venir toutes ensemble",
			len(answer.Faults))
	}
	if answer.Code != "ERR-CFG-01" {
		t.Errorf("code %q, attendu ERR-CFG-01", answer.Code)
	}
}

// TestRereadingAnExportWouldEraseThePassword: Config.Export strips both hashes, so a
// config.json rebuilt from one carries none. Control 31 accepts that -- it is the
// state of a station between its installation and its first access -- and rereading
// such a file on a station that HAS a password would lock the administration out.
func TestRereadingAnExportWouldEraseThePassword(t *testing.T) {
	store := &memoryConfigStore{}
	b := newBench(t, func(o *benchOptions) { o.configStore = store })
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	fromExport := b.hub.Config()
	fromExport.Admin.PasswordHash = ""
	store.config = fromExport

	answer := decodeStatus[problem](t,
		b.post("/admin/api/config/reload", `{}`), http.StatusUnprocessableEntity)
	if len(answer.Faults) == 0 || answer.Faults[0].Field != "admin.password_hash" {
		t.Fatalf("refus = %+v, attendu une faute sur admin.password_hash", answer.Faults)
	}
}

// TestRereadingIsRefusedInsideTheConfirmationWindow: the same reason writeConfig
// refuses one -- accepting would move the target of a rollback nobody confirmed.
func TestRereadingIsRefusedInsideTheConfirmationWindow(t *testing.T) {
	store := &memoryConfigStore{}
	b := newBench(t, func(o *benchOptions) { o.configStore = store })
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	moved := b.hub.Config()
	moved.Scale.Options = moved.Scale.Options.WithText("port", "COM9")
	store.config = moved
	if first := b.post("/admin/api/config/reload", `{}`); first.StatusCode != http.StatusOK {
		t.Fatalf("première relecture = %d", first.StatusCode)
	}
	second := b.post("/admin/api/config/reload", `{}`)
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("seconde relecture = %d, attendu 409", second.StatusCode)
	}
}

// memoryConfigStore is a configuration file that lives in a field.
type memoryConfigStore struct {
	config domain.Config
	saves  int
}

func (s *memoryConfigStore) Read(context.Context) (domain.Config, error) { return s.config, nil }

func (s *memoryConfigStore) Save(_ context.Context, cfg domain.Config) error {
	s.saves++
	s.config = cfg
	return nil
}

func (s *memoryConfigStore) Versions(context.Context) ([]ConfigVersion, error) { return nil, nil }

func (s *memoryConfigStore) Restore(context.Context, int) (domain.Config, error) {
	return s.config, nil
}
```

Si `newBench` ne renseigne pas `o.configStore` par défaut, `b.hub.Config()` sert de base
au document du test : c'est voulu, la relecture doit partir d'un fichier qui ne diffère
que par ce que le test a changé.

- [ ] **Step 2: Lancer les tests, vérifier qu'ils échouent**

Run: `CGO_ENABLED=0 go test ./internal/web/ -run TestReread -count=1 -v`
Expected: FAIL — les quatre répondent 404, la route n'existe pas.

- [ ] **Step 3: Écrire le handler**

Créer `internal/web/maintenance.go` :

```go
package web

import (
	"net/http"

	"openscale/internal/domain"
	"openscale/internal/station"
)

// reloadConfigFromDisk is POST /admin/api/config/reload: the file, as somebody just
// edited it, enters service without the station being stopped.
//
// # Why it is not `restoreConfig` with another number
//
// It reads config.json and not one of the five backups, and it WRITES NOTHING: the
// document already IS the file, so saving it over itself would rotate the five
// versions for nothing and drop the oldest.
//
// # Why the rollback does not put the file back
//
// station.ReloadRequest.FileBefore stays nil, so a rollback returns the station to
// the configuration in memory and LEAVES THE FILE ALONE. writeConfig does the
// opposite, and rightly: there the document came from the screen, which holds a copy.
// Here it came from somebody's hand, and overwriting it would destroy the only one
// there is. The station and the file then differ until the next reload -- which §11.3
// already answers, with the neutral profile a faulty file starts on.
func (s *Server) reloadConfigFromDisk(w http.ResponseWriter, r *http.Request) {
	if s.configStore == nil || s.controller == nil {
		unavailable(w, "la configuration n'est pas relisible ici")
		return
	}
	if deadline := s.controller.PendingConfirmation(); !deadline.IsZero() {
		writeProblem(w, http.StatusConflict, "",
			"Une configuration attend encore d'être confirmée. Confirmez-la, ou laissez le "+
				"poste revenir tout seul à la version précédente, puis relisez de nouveau.")
		return
	}

	onDisk, err := s.configStore.Read(r.Context())
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "",
			"Le fichier de configuration n'a pas pu être lu : "+err.Error())
		return
	}
	if faults := secretsLostBy(onDisk, s.hub.Config()); len(faults) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, problem{
			Code:    "ERR-CFG-01",
			Message: "Ce fichier ferait perdre au poste un secret qu'il porte aujourd'hui.",
			Faults:  faults,
		})
		return
	}
	if faults := (&onDisk).Validate(s.registries); len(faults) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, problem{
			Code:    "ERR-CFG-01",
			Message: "Cette configuration ne peut pas être appliquée.",
			Faults:  faultsOf(faults),
		})
		return
	}

	outcome, err := s.controller.Reload(station.ReloadRequest{Next: onDisk})
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "",
			"Le fichier a été lu mais n'a pas pu être appliqué : "+err.Error())
		return
	}
	s.technical.Technical(domain.LevelInfo, "config", "",
		"Configuration relue depuis le fichier.", onDisk.Fingerprint())

	s.moveListener(s.hub.Config(), onDisk, outcome.ConfirmBefore)
	writeJSON(w, http.StatusOK, s.configPayload(onDisk,
		s.confirmationOf(outcome.Changed, outcome.ConfirmBefore)))
}

// secretsLostBy names the credentials the file would erase.
//
// A config.json rebuilt from an export carries NEITHER hash: Config.Export strips both,
// always. Control 31 accepts an empty one on purpose -- it is the state of a station
// between its installation and its first access -- so the domain will not refuse this,
// and it should not: the fault is not in the file, it is in reading THIS file onto THIS
// station. Applying it would leave the administration reachable only by the recovery
// code printed on the installation sheet, and on a station whose sheet is lost, not at
// all.
func secretsLostBy(onDisk, inForce domain.Config) []faultDTO {
	var faults []faultDTO
	for _, secret := range []struct{ field, file, running string }{
		{"admin.password_hash", onDisk.Admin.PasswordHash, inForce.Admin.PasswordHash},
		{"admin.recovery_code_hash", onDisk.Admin.RecoveryCodeHash, inForce.Admin.RecoveryCodeHash},
	} {
		if secret.file == "" && secret.running != "" {
			faults = append(faults, faultDTO{
				Field: secret.field,
				Message: "ce fichier ne porte aucune empreinte, alors que le poste en a une : " +
					"il vient probablement d'un export, qui n'en emporte jamais. Reposez le secret " +
					"avec « openscale config password », ou recopiez l'empreinte du poste dans le fichier.",
			})
		}
	}
	return faults
}
```

Si `moveListener` a une autre signature que `(before, after domain.Config, deadline
time.Time)`, l'appeler comme `writeConfig` le fait — c'est le même geste et la même
raison : `network.listen` peut avoir bougé.

- [ ] **Step 4: Déclarer la route protégée**

Dans `internal/web/server.go`, ajouter à la table `guarded`, à la suite de
`config/restore` :

```go
		"POST /admin/api/config/reload":                s.reloadConfigFromDisk,
```

- [ ] **Step 5: Lancer les tests**

Run: `CGO_ENABLED=0 go test ./internal/web/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/maintenance.go internal/web/maintenance_test.go internal/web/server.go
git commit -m "feat(admin): un config.json édité à la main se relit sans arrêter le poste

Le _readme du fichier demandait d'arrêter le service, d'éditer, puis de
redémarrer — trois gestes qu'un poste sous kiosque ne permet pas. La
relecture rejoue le chemin de restoreConfig : validation complète,
rechargement à chaud, compte à rebours de 60 s.

Elle n'écrit rien et, sur retour arrière, elle laisse le fichier tel quel :
il n'en existe aucune copie. Et elle refuse un fichier venu d'un export,
qui n'emporte pas les empreintes et fermerait l'administration."
```

---

## Task 3: Redémarrer l'application

**Files:**
- Create: `internal/platform/supervised.go`, `supervised_windows.go`, `supervised_other.go`
- Create: `cmd/openscale/maintenance.go`
- Modify: `internal/web/maintenance.go`, `internal/web/maintenance_test.go`
- Modify: `internal/web/server.go` (`Restarter` dans `Options`, champ, route)
- Modify: `cmd/openscale/serve.go` (le `select` de la ligne 519, le câblage `web.New`)
- Modify: `cmd/openscale/serve_test.go`

**Interfaces:**
- Consumes: `station.Hub.DowntimeGuard()` (tâche 1), `platform.StartedByServiceManager() bool`.
- Produces: `platform.Supervised() bool` — vrai quand un superviseur relancera ce processus.
- Produces: interface `web.Restarter { Restart() error }`, et
  `web.ErrRestartRefused` porteur de la phrase du garde.
- Produces: `POST /admin/api/restart` → 202 · 409 · 501.

- [ ] **Step 1: Écrire le test de plateforme**

Créer `internal/platform/supervised_test.go` :

```go
package platform

import (
	"os"
	"runtime"
	"testing"
)

// TestATerminalIsNotSupervised: a binary somebody typed into a shell is relaunched by
// nobody, and the route that stops the station has to know it BEFORE it stops it.
func TestATerminalIsNotSupervised(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("le test porte sur la détection systemd")
	}
	t.Setenv("INVOCATION_ID", "")
	if Supervised() {
		t.Fatal("un processus sans INVOCATION_ID est déclaré supervisé : le bouton tuerait un poste qui ne revient pas")
	}
}

// TestSystemdIsSupervised: systemd sets INVOCATION_ID for every unit it starts, and
// the unit shipped in deploy/linux carries Restart=always.
func TestSystemdIsSupervised(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("le test porte sur la détection systemd")
	}
	t.Setenv("INVOCATION_ID", "b1e0a1e0b1e0a1e0b1e0a1e0b1e0a1e0")
	if !Supervised() {
		t.Fatal("un processus lancé par systemd est déclaré non supervisé")
	}
	_ = os.Getenv
}
```

- [ ] **Step 2: Lancer, vérifier l'échec**

Run: `CGO_ENABLED=0 go test ./internal/platform/ -run Supervised -count=1 -v`
Expected: FAIL — `undefined: Supervised`.

- [ ] **Step 3: Écrire la détection**

`internal/platform/supervised.go` :

```go
package platform

// Supervised reports whether SOMEBODY WILL RELAUNCH this process if it stops.
//
// It is asked before the station stops itself on purpose, and the answer decides
// whether the act is offered at all: on a developer's machine, `openscale serve`
// typed into a terminal is relaunched by nobody, and a button that stopped it would
// have turned a station off with no way to turn it back on.
func Supervised() bool { return supervised() }
```

`internal/platform/supervised_other.go` :

```go
//go:build !windows

package platform

import "os"

// supervised reads the marker systemd sets on every unit it starts.
//
// INVOCATION_ID and not a check on PID 1 or on the parent: a unit's main process is
// not a child of PID 1 in every cgroup arrangement, whereas systemd.exec(5) documents
// this variable as being set for each invocation. The value itself carries no meaning
// here -- only its presence does.
func supervised() bool { return os.Getenv("INVOCATION_ID") != "" }
```

`internal/platform/supervised_windows.go` :

```go
package platform

// supervised is the SCM, which the service handler already asks about.
//
// A service registered with the recovery actions of §15.2 is relaunched after a
// non-zero exit code; a binary run from a console is not.
func supervised() bool { return StartedByServiceManager() }
```

- [ ] **Step 4: Lancer, vérifier le succès**

Run: `CGO_ENABLED=0 go test ./internal/platform/ -run Supervised -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Écrire les tests HTTP**

Ajouter à `internal/web/maintenance_test.go` :

```go
// stubRestarter records the demand instead of stopping a station.
type stubRestarter struct {
	err   error
	calls int
}

func (r *stubRestarter) Restart() error {
	r.calls++
	return r.err
}

// TestRestartingAsksTheStationToStop: 202 and not 200 -- the station is about to go,
// and there will be no second answer on this connection.
func TestRestartingAsksTheStationToStop(t *testing.T) {
	restarter := &stubRestarter{}
	b := newBench(t, func(o *benchOptions) { o.restarter = restarter })
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	if response := b.post("/admin/api/restart", `{}`); response.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /admin/api/restart = %d, attendu 202", response.StatusCode)
	}
	if restarter.calls != 1 {
		t.Fatalf("%d demande(s) transmise(s), attendu 1", restarter.calls)
	}
}

// TestAnUnsupervisedStationIsNotStopped: without a service manager nobody relaunches
// it, and killing it would leave a station nothing can turn back on.
func TestAnUnsupervisedStationIsNotStopped(t *testing.T) {
	b := newBench(t)
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	answer := decodeStatus[problem](t,
		b.post("/admin/api/restart", `{}`), http.StatusNotImplemented)
	if answer.Message == "" {
		t.Fatal("le refus ne porte aucune phrase française")
	}
}

// TestRestartingIsRefusedMidWeighing: the guard of the update route answers for this
// one too, and its sentence travels verbatim.
func TestRestartingIsRefusedMidWeighing(t *testing.T) {
	restarter := &stubRestarter{err: &station.DowntimeRefused{
		Reason: "Une pesée est en cours. Réessayez dans un instant."}}
	b := newBench(t, func(o *benchOptions) { o.restarter = restarter })
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	answer := decodeStatus[problem](t,
		b.post("/admin/api/restart", `{}`), http.StatusConflict)
	if answer.Message != "Une pesée est en cours. Réessayez dans un instant." {
		t.Fatalf("phrase servie %q : le refus du garde doit voyager mot pour mot", answer.Message)
	}
}
```

Ajouter `restarter Restarter` à `benchOptions` et le passer dans `web.Options` là où
`newBench` construit le serveur.

`station.DowntimeRefused` est neuf et vit avec le garde :

```go
// DowntimeRefused carries the guard's OWN French sentence up to the screen.
//
// A type and not a formatted string, for the reason update.BusyError already gives:
// the layer above renders that sentence verbatim, because the guard knows whether a
// weighing or a catalogue is in the way and the HTTP layer does not.
type DowntimeRefused struct{ Reason string }

// Error renders the refusal for a log.
func (e *DowntimeRefused) Error() string { return "station: downtime refused: " + e.Reason }
```

- [ ] **Step 6: Lancer, vérifier l'échec**

Run: `CGO_ENABLED=0 go test ./internal/web/ -run Restart -count=1 -v`
Expected: FAIL — `o.restarter` undefined, route absente.

- [ ] **Step 7: Le collaborateur et le handler**

Dans `internal/web/server.go`, à côté de `Updater` :

```go
// Restarter stops the station so that its supervisor starts it again.
//
// Declared here, on the consumer's side. NIL MEANS « nobody would relaunch it », and
// the route then answers 501 rather than stopping a station that would stay down:
// that is the case of `openscale serve` typed into a terminal.
type Restarter interface {
	// Restart asks the station to stop. It returns as soon as the demand is
	// recorded -- what carries it out also ends this process -- and a
	// *station.DowntimeRefused when the station must not be taken down now.
	Restart() error
}
```

Ajouter `Restart Restarter` à `Options`, `restarter Restarter` au `Server`, l'affecter
dans `New`, et déclarer la route dans la table `guarded` :

```go
		"POST /admin/api/restart":                      s.restart,
```

Dans `internal/web/maintenance.go` :

```go
// codeRestartUnsupervised is what a station nobody would relaunch answers.
//
// It is NOT the code of the restart itself (ERR-SYS-09, in cmd/openscale): one says
// « the station is going down on purpose », the other « it would not come back ». Two
// pieces of news, two codes -- a volunteer looking either up in TROUBLESHOOTING.md must
// not land on the other.
const codeRestartUnsupervised = "ERR-SYS-10"

// restart is POST /admin/api/restart: the station stops, its supervisor starts it.
//
// ADR-027 removed a route of this name, and this is not that route. What it refused is
// a restart DEMANDED BY A SETTING -- no configuration block may ask for one, and none
// does. This one is a repair, for the cases nobody foresaw, and it goes through the one
// restart that ADR itself calls legitimate: the one the SCM or systemd triggers.
func (s *Server) restart(w http.ResponseWriter, _ *http.Request) {
	if s.restarter == nil {
		writeProblem(w, http.StatusNotImplemented, codeRestartUnsupervised,
			"Ce poste n'est pas lancé par un service : personne ne le redémarrerait. "+
				"Installez-le en service avec « openscale service install ».")
		return
	}
	if err := s.restarter.Restart(); err != nil {
		var refused *station.DowntimeRefused
		if errors.As(err, &refused) {
			// The guard WROTE the sentence and this layer does not paraphrase it:
			// paraphrasing would lose the only thing the volunteer can act on.
			writeProblem(w, http.StatusConflict, "", refused.Reason)
			return
		}
		writeProblem(w, http.StatusInternalServerError, "",
			"Le redémarrage n'a pas pu être demandé : "+err.Error())
		return
	}
	// 202: the station is about to stop, and there will be no second answer on this
	// connection. The screen now polls /healthz until somebody answers again.
	writeJSON(w, http.StatusAccepted, actionDTO{
		Done: true, Message: "Le poste redémarre. L'écran revient tout seul."})
}
```

- [ ] **Step 8: Le canal, côté `cmd/openscale`**

Créer `cmd/openscale/maintenance.go` :

```go
package main

import (
	"sync"

	"openscale/internal/platform"
	"openscale/internal/station"
	"openscale/internal/web"
)

// stationRestarter turns the button into the one thing serve() waits on.
//
// It closes a channel ONCE: two volunteers touching the button at the same second must
// produce one stop, not a panic on a channel closed twice.
type stationRestarter struct {
	once  sync.Once
	asked chan struct{}
	guard func() (bool, string)
}

// newStationRestarter builds the restarter over the running station.
func newStationRestarter(guard func() (bool, string)) *stationRestarter {
	return &stationRestarter{asked: make(chan struct{}), guard: guard}
}

// Restart records the demand, unless the station must not be taken down now.
func (r *stationRestarter) Restart() error {
	if allowed, reason := r.guard(); !allowed {
		return &station.DowntimeRefused{Reason: reason}
	}
	r.once.Do(func() { close(r.asked) })
	return nil
}

// Asked is what serve() selects on.
func (r *stationRestarter) Asked() <-chan struct{} { return r.asked }

// restarterFor returns what the HTTP layer should be given.
//
// ★ IT RETURNS A NIL INTERFACE, NEVER A TYPED NIL — the same trap updaterFor documents:
// a typed nil in an interface is not nil, the handler's guard reads false, and the call
// panics on a nil receiver.
//
// It returns nil on a station nobody supervises, which is a developer's terminal: the
// route then says so instead of stopping a process that would stay stopped.
func restarterFor(r *stationRestarter) web.Restarter {
	if !platform.Supervised() {
		return nil
	}
	return r
}
```

- [ ] **Step 9: Le troisième cas du `select`**

Dans `cmd/openscale/serve.go`, ajouter le code technique à côté de `codeServerStopped` :

```go
	// codeRestartAsked is ERR-SYS-09: a volunteer asked for a restart from the
	// administration screen. It is written to the technical journal BEFORE the stop,
	// because nothing written afterwards would ever be written -- and because the
	// Windows event log will call this stop « inattendu », which it is not.
	codeRestartAsked = "ERR-SYS-09"
```

et l'exit code, à côté de `exitFatal` :

```go
	// exitRestart is a stop somebody ASKED FOR. It is non-zero on purpose: that is
	// what makes the SCM apply the recovery actions of §15.2 and systemd its
	// Restart=always. A clean 0 would be recorded as a stop nobody undoes.
	exitRestart = 4
```

Construire le restarter avant `web.New` :

```go
	restarter := newStationRestarter(func() (bool, string) { return st.Hub().DowntimeGuard() })
```

le passer dans `web.Options` : `Restart: restarterFor(restarter),`

puis le troisième cas du `select` de la ligne 519 :

```go
	var fatal error
	select {
	case <-ctx.Done():
	case <-restarter.Asked():
		fatal = &serviceFailure{Code: codeRestartAsked, Exit: exitRestart, Message: "" +
			"Redémarrage demandé depuis l'écran d'administration. Le gestionnaire de " +
			"services relance le poste."}
		recordFailure(db, clock, fatal)
	case err := <-served:
		// ... inchangé
	}
```

L'arrêt qui suit — `cancelRoot()`, `st.Stop()`, l'attente — n'est pas touché : c'est
exactement le même arrêt ordonné, et c'est pour ça qu'on passe par là.

- [ ] **Step 10: Test de bout en bout côté `cmd`**

Ajouter à `cmd/openscale/serve_test.go`, en suivant le montage des tests voisins (qui
lancent `serve` avec `o.serving` pour connaître l'adresse) :

```go
// TestARestartAskedStopsTheStationWithANonZeroCode: the code is what the SCM reads to
// decide whether it relaunches. A zero here is a station that never comes back.
func TestARestartAskedStopsTheStationWithANonZeroCode(t *testing.T) {
	// … monter serve() comme les tests voisins, appeler POST /admin/api/restart,
	// puis :
	var failure *serviceFailure
	if !errors.As(err, &failure) {
		t.Fatalf("serve a rendu %v, attendu un serviceFailure", err)
	}
	if failure.Exit == 0 {
		t.Fatal("code de sortie 0 : le SCM enregistrerait un arrêt propre et ne relancerait rien")
	}
	if failure.Code != codeRestartAsked {
		t.Errorf("code %q, attendu %q", failure.Code, codeRestartAsked)
	}
}
```

- [ ] **Step 11: Tests**

Run: `CGO_ENABLED=0 go test ./internal/... ./cmd/... -count=1`
Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add -A
git commit -m "feat(admin): le poste se redémarre depuis l'écran, par son superviseur

Un poste sous kiosque n'a aucune sortie quand quelque chose est figé. Le
bouton refait l'arrêt ordonné de §13.4 et rend un code non nul : le SCM
applique ses reprises, systemd son Restart=always. Aucun script neuf,
aucun second chemin d'arrêt.

Sans superviseur — « openscale serve » dans un terminal — la route répond
501 au lieu d'arrêter un processus que personne ne relèverait."
```

---

## Task 4: `platform.Reboot`

**Files:**
- Create: `internal/platform/reboot.go`, `reboot_windows.go`, `reboot_other.go`, `reboot_test.go`

**Interfaces:**
- Produces: `platform.Reboot() error` · `platform.ErrRebootUnsupported`.

- [ ] **Step 1: Écrire le test**

`internal/platform/reboot_test.go` :

```go
package platform

import (
	"errors"
	"runtime"
	"testing"
)

// TestRebootIsHonestAboutPlatformsThatCannot: a station whose platform has no reboot
// must say so to the screen, and the screen must be able to tell that case from a
// refusal -- a sentinel, not a formatted string, for the reason ErrServiceUnsupported
// gives.
func TestRebootIsHonestAboutPlatformsThatCannot(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		t.Skip("cette plateforme sait redémarrer : le test porte sur les autres")
	}
	if err := Reboot(); !errors.Is(err, ErrRebootUnsupported) {
		t.Fatalf("Reboot() = %v, attendu ErrRebootUnsupported", err)
	}
}

// TestTheUnsupportedSentenceNamesARemedy: every refusal of this package says what to
// do about it, in French, because a volunteer reads it.
func TestTheUnsupportedSentenceNamesARemedy(t *testing.T) {
	if ErrRebootUnsupported.Error() == "" {
		t.Fatal("le sentinel ne porte aucune phrase")
	}
}
```

- [ ] **Step 2: Lancer, vérifier l'échec**

Run: `CGO_ENABLED=0 go test ./internal/platform/ -run Reboot -count=1 -v`
Expected: FAIL — `undefined: Reboot`.

- [ ] **Step 3: Écrire les trois fichiers**

`internal/platform/reboot.go` :

```go
package platform

import "errors"

// ErrRebootUnsupported is what Reboot returns on a platform with no reboot of its own.
//
// A sentinel and not a formatted string, for the reason ErrServiceUnsupported gives:
// the caller tells this case apart from a refusal, and answers 501 rather than 500.
var ErrRebootUnsupported = errors.New(
	"le redémarrage de l'ordinateur depuis l'écran n'existe que sous Windows et Linux")

// Reboot restarts the MACHINE, not the station.
//
// It returns as soon as the demand is accepted: what carries it out ends this process.
func Reboot() error { return reboot() }
```

`internal/platform/reboot_windows.go` :

```go
package platform

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// reboot asks Windows to restart, now.
//
// # Why shutdown.exe and not InitiateSystemShutdownEx
//
// The API would mean enabling SeShutdownPrivilege on our own token first, which is
// three calls that each fail differently and none of which can be exercised on a build
// machine. shutdown.exe does exactly that, it has done it since Windows 2000, and its
// exit code says whether it was refused. The service runs as LocalSystem, which holds
// the privilege.
//
// /t 0 and not a delay: the countdown lives in the application, on the injected clock,
// so that it is the same on both platforms and provable without restarting a machine.
//
// CREATE_NO_WINDOW for the reason ApplyUpdate documents at length -- and never
// DETACHED_PROCESS, which silently runs nothing.
func reboot() error {
	command := exec.Command("shutdown.exe", "/r", "/t", "0",
		"/c", "Redemarrage demande depuis l'ecran d'administration OpenScale")
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("l'ordinateur n'a pas pu être redémarré : %w (%s)", err, output)
	}
	return nil
}
```

`internal/platform/reboot_other.go` :

```go
//go:build !windows

package platform

import (
	"fmt"
	"os/exec"
	"runtime"
)

// reboot asks logind to restart the machine.
//
// It goes through systemctl and not through a D-Bus call written here: the call has to
// pass polkit either way, systemctl reports the refusal in a sentence, and a D-Bus
// client would be a dependency for one call.
//
// THE SERVICE RUNS AS `openscale`, NOT AS ROOT. Without the polkit rule installed by
// deploy/linux/install.sh, this is refused -- and that refusal is the nominal state of
// a station installed before the rule existed. The message carries the remedy.
func reboot() error {
	if runtime.GOOS != "linux" {
		return ErrRebootUnsupported
	}
	output, err := exec.Command("systemctl", "reboot").CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"l'ordinateur n'a pas pu être redémarré : %w (%s). Si le message parle "+
				"d'autorisation, la règle polkit du poste manque : relancez "+
				"« sudo ./install.sh » depuis deploy/linux",
			err, output)
	}
	return nil
}
```

- [ ] **Step 4: Lancer les tests**

Run: `CGO_ENABLED=0 go test ./internal/platform/ -count=1` puis
`CGO_ENABLED=0 GOOS=linux go build ./... && CGO_ENABLED=0 GOOS=windows go build ./...`
Expected: PASS, et les deux compilations passent.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/reboot*.go
git commit -m "feat(platform): redémarrer l'ordinateur, sous Windows et sous Linux

shutdown.exe /r /t 0 et systemctl reboot. Le délai n'est pas ici : il vit
dans l'application, sur l'horloge injectée, pour être le même des deux
côtés et se prouver sans redémarrer une machine.

Sous Linux le service tourne en « openscale » et non en root : sans la
règle polkit, l'appel est refusé, et le message le dit avec son remède."
```

---

## Task 5: Le compte à rebours du redémarrage machine

**Files:**
- Create: `internal/web/reboot.go`, `internal/web/reboot_test.go`
- Modify: `internal/web/maintenance.go` (les deux handlers), `internal/web/server.go`
- Modify: `cmd/openscale/maintenance.go` (`rebooterFor`), `cmd/openscale/serve.go` (câblage)

**Interfaces:**
- Consumes: `ports.Clock.After(d) <-chan time.Time`, `station.DowntimeRefused` (tâche 3).
- Produces: `web.Rebooter { Reboot() error }` ; `rebootPlan` avec
  `Arm(now time.Time) (time.Time, error)`, `Cancel() bool`, `Deadline() time.Time`.
- Produces: `POST /admin/api/reboot` → 202 `rebootDTO` · 409 · 501 ;
  `DELETE /admin/api/reboot` → 200 `actionDTO` · 409.

- [ ] **Step 1: Écrire les tests du compte à rebours**

`internal/web/reboot_test.go` :

```go
package web

import (
	"errors"
	"testing"
	"time"

	"openscale/internal/fake"
)

// TestTheCountdownFiresWhenItElapses: the whole reason the delay lives here and not in
// shutdown.exe -- it is provable without restarting a machine.
func TestTheCountdownFiresWhenItElapses(t *testing.T) {
	clock := fake.NewClock(epoch)
	fired := make(chan struct{}, 1)
	plan := newRebootPlan(clock, func() error { fired <- struct{}{}; return nil })

	if _, err := plan.Arm(); err != nil {
		t.Fatalf("Arm : %v", err)
	}
	clock.Advance(rebootDelay)
	select {
	case <-fired:
	case <-time.After(hang):
		t.Fatal("l'échéance est passée sans que l'ordinateur soit redémarré")
	}
}

// TestCancellingBeforeTheDeadlineStopsIt: thirty seconds is what somebody who touched
// the wrong button has, and it is the only thing that makes this button survivable.
func TestCancellingBeforeTheDeadlineStopsIt(t *testing.T) {
	clock := fake.NewClock(epoch)
	fired := make(chan struct{}, 1)
	plan := newRebootPlan(clock, func() error { fired <- struct{}{}; return nil })

	if _, err := plan.Arm(); err != nil {
		t.Fatalf("Arm : %v", err)
	}
	if !plan.Cancel() {
		t.Fatal("l'annulation a été refusée alors que l'échéance n'était pas passée")
	}
	clock.Advance(2 * rebootDelay)
	select {
	case <-fired:
		t.Fatal("l'ordinateur a redémarré après une annulation")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestArmingTwiceIsRefused: a second click must not start a second countdown, which
// would be a machine restarting while somebody believes they cancelled.
func TestArmingTwiceIsRefused(t *testing.T) {
	clock := fake.NewClock(epoch)
	plan := newRebootPlan(clock, func() error { return nil })

	if _, err := plan.Arm(); err != nil {
		t.Fatalf("premier armement : %v", err)
	}
	if _, err := plan.Arm(); !errors.Is(err, errRebootArmed) {
		t.Fatalf("second armement = %v, attendu errRebootArmed", err)
	}
}

// TestCancellingNothingSaysSo: the screen has to tell « I stopped it » from « there
// was nothing to stop », because the second means the machine is already going.
func TestCancellingNothingSaysSo(t *testing.T) {
	plan := newRebootPlan(fake.NewClock(epoch), func() error { return nil })
	if plan.Cancel() {
		t.Fatal("annuler sans rien d'armé a répondu « annulé »")
	}
}
```

Si le constructeur de `fake.Clock` ne s'appelle pas `NewClock(epoch)`, employer celui que
`harness_test.go` utilise déjà.

- [ ] **Step 2: Lancer, vérifier l'échec**

Run: `CGO_ENABLED=0 go test ./internal/web/ -run Countdown -count=1 -v`
Expected: FAIL — `undefined: newRebootPlan`.

- [ ] **Step 3: Écrire le compte à rebours**

`internal/web/reboot.go` :

```go
package web

import (
	"errors"
	"sync"
	"time"

	"openscale/internal/station/ports"
)

// rebootDelay is how long somebody who touched the wrong button has to say so.
//
// Thirty seconds, and the number is the whole safety of this act: it is long enough to
// read the sentence and reach the « Annuler » button, short enough that a volunteer who
// meant it does not go looking for the power switch.
const rebootDelay = 30 * time.Second

// errRebootArmed reports a countdown already running.
var errRebootArmed = errors.New("web: a reboot is already armed")

// rebootPlan is the countdown before the machine restarts.
//
// # Why the delay is here and not in shutdown.exe
//
// `shutdown /r /t 30` would offer the same thing to Windows and nothing at all to
// Linux, where `systemctl reboot` is immediate: one button would then behave two ways.
// And a delay held by the operating system cannot be tested without restarting a
// machine, whereas this one runs on the injected clock -- arming, cancelling and
// elapsing are all provable in microseconds.
type rebootPlan struct {
	clock  ports.Clock
	reboot func() error

	mu       sync.Mutex
	deadline time.Time
	// cancelled is closed to call the countdown off. A channel and not a flag: the
	// goroutine is asleep on the clock, and only a channel wakes it.
	cancelled chan struct{}
}

// newRebootPlan builds a plan that calls reboot when its countdown elapses.
func newRebootPlan(clock ports.Clock, reboot func() error) *rebootPlan {
	return &rebootPlan{clock: clock, reboot: reboot}
}

// Arm starts the countdown and reports when it will fire.
func (p *rebootPlan) Arm() (time.Time, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancelled != nil {
		return time.Time{}, errRebootArmed
	}
	p.deadline = p.clock.Now().Add(rebootDelay)
	p.cancelled = make(chan struct{})

	elapsed := p.clock.After(rebootDelay)
	cancelled := p.cancelled
	// A goroutine bounded by the two things that can end it, and by nothing else
	// (§13.1): the deadline, or the cancellation.
	go func() {
		select {
		case <-elapsed:
			_ = p.reboot()
		case <-cancelled:
		}
	}()
	return p.deadline, nil
}

// Cancel calls the countdown off, and reports whether there was one.
//
// The two answers are two sentences on the screen: « c'est annulé » and « il est trop
// tard, l'ordinateur redémarre » are not the same news.
func (p *rebootPlan) Cancel() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancelled == nil {
		return false
	}
	close(p.cancelled)
	p.cancelled = nil
	p.deadline = time.Time{}
	return true
}

// Deadline reports when the machine restarts, or the zero time when nothing is armed.
func (p *rebootPlan) Deadline() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.deadline
}
```

- [ ] **Step 4: Lancer, vérifier le succès**

Run: `CGO_ENABLED=0 go test ./internal/web/ -run "Countdown|Cancelling|Arming" -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Écrire les tests HTTP**

Ajouter à `internal/web/maintenance_test.go` :

```go
// stubRebooter records the demand instead of restarting a machine.
type stubRebooter struct{ calls int }

func (r *stubRebooter) Reboot() error { r.calls++; return nil }

// TestTheMachineRestartsAfterTheCountdown, from the route down.
func TestTheMachineRestartsAfterTheCountdown(t *testing.T) {
	rebooter := &stubRebooter{}
	b := newBench(t, func(o *benchOptions) { o.rebooter = rebooter })
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	armed := decodeStatus[rebootDTO](t, b.post("/admin/api/reboot", `{}`), http.StatusAccepted)
	if armed.SecondsLeft != 30 {
		t.Fatalf("compte à rebours servi à %d s, attendu 30", armed.SecondsLeft)
	}
	if rebooter.calls != 0 {
		t.Fatal("l'ordinateur a redémarré avant l'échéance : le délai ne sert à rien")
	}
	b.clock.Advance(31 * time.Second)
	// … attendre que la goroutine ait appelé le rebooter, avec le budget `hang`.
}

// TestTheRebootIsCancellable: the button that makes this act survivable.
func TestTheRebootIsCancellable(t *testing.T) {
	rebooter := &stubRebooter{}
	b := newBench(t, func(o *benchOptions) { o.rebooter = rebooter })
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	b.post("/admin/api/reboot", `{}`)
	if response := b.do(http.MethodDelete, "/admin/api/reboot", "", nil); response.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /admin/api/reboot = %d, attendu 200", response.StatusCode)
	}
	b.clock.Advance(2 * time.Minute)
	if rebooter.calls != 0 {
		t.Fatal("l'ordinateur a redémarré après une annulation")
	}
}

// TestASecondRebootIsRefused, and 409 rather than a second countdown.
func TestASecondRebootIsRefused(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.rebooter = &stubRebooter{} })
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	b.post("/admin/api/reboot", `{}`)
	if second := b.post("/admin/api/reboot", `{}`); second.StatusCode != http.StatusConflict {
		t.Fatalf("second armement = %d, attendu 409", second.StatusCode)
	}
}

// TestAPlatformWithNoRebootSaysSo, rather than offering a button that fails.
func TestAPlatformWithNoRebootSaysSo(t *testing.T) {
	b := newBench(t)
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	if response := b.post("/admin/api/reboot", `{}`); response.StatusCode != http.StatusNotImplemented {
		t.Fatalf("POST /admin/api/reboot sans rebooter = %d, attendu 501", response.StatusCode)
	}
}
```

Ajouter `rebooter Rebooter` à `benchOptions`.

- [ ] **Step 6: Écrire les handlers**

Dans `internal/web/server.go`, à côté de `Restarter` :

```go
// Rebooter restarts the MACHINE. Declared here, on the consumer's side;
// platform.Reboot satisfies it once adapted. Nil answers 501: a station whose platform
// has no reboot must say so rather than offer a button that fails at the last click.
type Rebooter interface {
	// Reboot restarts the machine. It returns as soon as the demand is accepted.
	Reboot() error
}
```

Ajouter `Reboot Rebooter` à `Options`, `rebootPlan *rebootPlan` au `Server`, le construire
dans `New` quand `Options.Reboot != nil`, et déclarer les deux routes dans `guarded` :

```go
		"POST /admin/api/reboot":                       s.armReboot,
		"DELETE /admin/api/reboot":                     s.cancelReboot,
```

Dans `internal/web/maintenance.go` :

```go
// codeRebootUnsupported is what a platform with no reboot answers.
const codeRebootUnsupported = "ERR-SYS-11"

// rebootDTO is the countdown, as the screen reads it.
type rebootDTO struct {
	At          string `json:"at"`
	SecondsLeft int    `json:"seconds_left"`
}

// armReboot is POST /admin/api/reboot: the machine restarts in thirty seconds.
//
// Thirty seconds and not none: this is the one act of the administration that nothing
// undoes afterwards, and the button that cancels it is what makes it offerable at all.
func (s *Server) armReboot(w http.ResponseWriter, _ *http.Request) {
	if s.rebootPlan == nil {
		writeProblem(w, http.StatusNotImplemented, codeRebootUnsupported,
			"Ce poste ne sait pas redémarrer l'ordinateur depuis l'écran.")
		return
	}
	if allowed, reason := s.hub.DowntimeGuard(); !allowed {
		writeProblem(w, http.StatusConflict, "", reason)
		return
	}
	deadline, err := s.rebootPlan.Arm()
	if err != nil {
		writeProblem(w, http.StatusConflict, "",
			"L'ordinateur redémarre déjà. Touchez « Annuler » si ce n'est pas ce que vous vouliez.")
		return
	}
	s.technical.Technical(domain.LevelWarn, "system", "",
		"Redémarrage de l'ordinateur demandé depuis l'écran d'administration.", "")
	writeJSON(w, http.StatusAccepted, rebootDTO{
		At:          deadline.Format(time.RFC3339),
		SecondsLeft: int(rebootDelay / time.Second),
	})
}

// cancelReboot is DELETE /admin/api/reboot.
func (s *Server) cancelReboot(w http.ResponseWriter, _ *http.Request) {
	if s.rebootPlan == nil {
		writeProblem(w, http.StatusNotImplemented, codeRebootUnsupported,
			"Ce poste ne sait pas redémarrer l'ordinateur depuis l'écran.")
		return
	}
	if !s.rebootPlan.Cancel() {
		// 409 and not 404: nothing was armed, which on this screen means either that
		// somebody else cancelled it or that it is already too late. Both deserve the
		// sentence rather than a bare « not found ».
		writeProblem(w, http.StatusConflict, "",
			"Aucun redémarrage n'est en attente.")
		return
	}
	s.technical.Technical(domain.LevelInfo, "system", "",
		"Redémarrage de l'ordinateur annulé.", "")
	writeJSON(w, http.StatusOK, actionDTO{
		Done: true, Message: "Le redémarrage est annulé."})
}
```

`Hub` doit exposer `DowntimeGuard()` à `internal/web` : ajouter la méthode à l'interface
`Hub` de `server.go` si elle n'y est pas, avec le commentaire qui dit que la règle
appartient à la station.

- [ ] **Step 7: Câbler côté `cmd/openscale`**

Dans `cmd/openscale/maintenance.go` :

```go
// machineRebooter is platform.Reboot, as the HTTP layer asks for it.
type machineRebooter struct{}

// Reboot restarts the machine.
func (machineRebooter) Reboot() error { return platform.Reboot() }

// rebooterFor returns what the HTTP layer should be given, or a NIL INTERFACE on a
// platform that cannot restart -- never a typed nil, for the reason restarterFor gives.
func rebooterFor() web.Rebooter {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return nil
	}
	return machineRebooter{}
}
```

et dans `web.Options` de `serve.go` : `Reboot: rebooterFor(),`.

- [ ] **Step 8: Tests**

Run: `CGO_ENABLED=0 go test ./internal/... ./cmd/... -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "feat(admin): redémarrer l'ordinateur, avec trente secondes pour se raviser

Sous cage et sous Shell Launcher, l'écran client n'a aucune sortie : le
seul recours était de couper le courant.

Le délai vit dans l'application, sur l'horloge injectée, et non dans
shutdown.exe : c'est ce qui lui donne le même comportement sous Linux, où
systemctl reboot est immédiat, et ce qui rend l'échéance prouvable sans
redémarrer une machine."
```

---

## Task 6: `openscale service restart`

**Files:**
- Modify: `cmd/openscale/service.go:41-113`
- Modify: `cmd/openscale/service_test.go`

**Interfaces:**
- Consumes: `platform.StopService(clk, name, budget) error`, `platform.StartService(name) error`.

- [ ] **Step 1: Écrire le test**

Ajouter à `cmd/openscale/service_test.go`, en suivant le montage des tests voisins :

```go
// TestRestartDoesNotStartWhatItCouldNotStop: chaining would hide the real fault behind
// a second message about a service that was never stopped.
func TestRestartDoesNotStartWhatItCouldNotStop(t *testing.T) {
	// … monter runService avec un gestionnaire de service de test dont Stop échoue,
	// puis :
	if err := runService([]string{"restart"}, &out); err == nil {
		t.Fatal("un arrêt en échec a été suivi d'un démarrage")
	}
	if started {
		t.Fatal("start a été appelé alors que stop avait échoué")
	}
}

// TestTheUsageNamesRestart: a volunteer reading « openscale service » must find the
// action, or it does not exist for them.
func TestTheUsageNamesRestart(t *testing.T) {
	if !strings.Contains(serviceUsage, "restart") {
		t.Fatal("« restart » ne figure pas dans l'aide de « openscale service »")
	}
}
```

Si `cmd/openscale` n'a pas de gestionnaire de service injectable, ne garder que le second
test et vérifier le premier point par lecture : le `case "restart"` retourne sur erreur.
**Ne pas introduire une injection pour ce seul test** — `platform.StopService` parle au
SCM et n'a pas de seam, et en fabriquer une pour trois lignes coûterait plus qu'elle ne
prouve.

- [ ] **Step 2: Lancer, vérifier l'échec**

Run: `CGO_ENABLED=0 go test ./cmd/openscale/ -run Restart -count=1 -v`
Expected: FAIL.

- [ ] **Step 3: Écrire l'action**

Dans `cmd/openscale/service.go`, après le `case "stop"` :

```go
	case "restart":
		// The error of the stop is returned WITHOUT trying the start: a service
		// nobody managed to stop is not restarted, and chaining would answer a
		// second message about a failure the first one already named.
		if err := platform.StopService(clock, platform.ServiceName, serviceStopBudget()); err != nil {
			return err
		}
		if err := platform.StartService(platform.ServiceName); err != nil {
			return err
		}
		fmt.Fprintf(out, "service %s redémarré.\n", platform.ServiceName)
		return nil
```

et les trois phrases qui nomment l'action :

- le message d'erreur : `"service prend une action : install, uninstall, start, stop, restart ou status"` ;
- le `fmt.Errorf` final : `"action inconnue %q : install, uninstall, start, stop, restart ou status"` ;
- `serviceUsage` : la ligne d'en-tête `<install|uninstall|start|stop|restart|status>` et,
  sous `stop`, la ligne :

```
  restart     arrête le service, attend son arrêt, puis le redémarre
```

- [ ] **Step 4: Lancer les tests**

Run: `CGO_ENABLED=0 go test ./cmd/openscale/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/openscale/service.go cmd/openscale/service_test.go
git commit -m "feat(cli): openscale service restart

Les deux moitiés existaient. L'erreur de l'arrêt est rendue sans tenter le
démarrage : un service qu'on n'a pas su arrêter ne se redémarre pas, et
enchaîner masquerait la vraie faute."
```

---

## Task 7: La règle polkit et le contrôle de `doctor`

Sans elle, le bouton de la tâche 5 est refusé sur tout poste Linux.

**Files:**
- Create: `deploy/linux/openscale-reboot.rules`
- Modify: `deploy/linux/install.sh`, `deploy/linux/uninstall.sh`
- Modify: `cmd/openscale/doctor.go`, `cmd/openscale/doctor_test.go`
- Modify: `deploy/deploy_test.go`

- [ ] **Step 1: Écrire la règle**

`deploy/linux/openscale-reboot.rules` :

```javascript
// Autorisation de redémarrage du poste — docs/02-architecture.md §15.3.
//
// POURQUOI ELLE EXISTE : le service tourne en « openscale », shell nologin, et l'écran
// client tourne sous cage, d'où « il n'y a littéralement rien vers quoi s'échapper ».
// Sans cette règle, le bouton « Redémarrer l'ordinateur » est refusé par polkit et le
// seul recours d'un bénévole redevient la coupure de courant.
//
// LA PORTÉE EST LA PLUS ÉTROITE POSSIBLE : un seul utilisateur, une seule action, et
// PAS org.freedesktop.login1.power-off — un poste éteint à distance ne se rallume pas à
// distance, et personne n'en a besoin.
//
// Installée par install.sh dans /etc/polkit-1/rules.d/49-openscale-reboot.rules.

polkit.addRule(function (action, subject) {
  if (action.id === 'org.freedesktop.login1.reboot' && subject.user === 'openscale') {
    return polkit.Result.YES
  }
})
```

- [ ] **Step 2: L'installer**

Dans `deploy/linux/install.sh`, à côté de la pose des règles udev, ajouter la copie vers
`/etc/polkit-1/rules.d/49-openscale-reboot.rules` avec le même style de trace que les
autres étapes, et dans `uninstall.sh` sa suppression.

- [ ] **Step 3: Le test de déploiement**

Dans `deploy/deploy_test.go`, à côté des tests qui lisent les unités :

```go
// TestTheRebootRuleIsNarrow: the station may restart the machine and nothing else. A
// rule that granted more would be a privilege escalation shipped in an installer.
func TestTheRebootRuleIsNarrow(t *testing.T) {
	rule := read(t, "linux/openscale-reboot.rules")
	if !strings.Contains(rule, "org.freedesktop.login1.reboot") {
		t.Fatal("la règle n'autorise pas le redémarrage : le bouton sera refusé")
	}
	if strings.Contains(rule, "power-off") {
		t.Error("la règle autorise l'extinction : un poste éteint à distance ne se rallume pas")
	}
	if !strings.Contains(rule, "subject.user === 'openscale'") {
		t.Error("la règle ne se limite pas à l'utilisateur du poste")
	}
}

// TestInstallPosesTheRebootRule.
func TestInstallPosesTheRebootRule(t *testing.T) {
	script := read(t, "linux/install.sh")
	if !strings.Contains(script, "49-openscale-reboot.rules") {
		t.Fatal("install.sh ne pose pas la règle polkit : le bouton sera refusé sur tout poste Linux")
	}
}
```

Employer le helper de lecture que `deploy_test.go` utilise déjà plutôt que `read`.

- [ ] **Step 4: Le contrôle de `doctor`**

Ajouter un contrôle, dans le style des quinze autres : sous Linux, il vérifie la présence
de `/etc/polkit-1/rules.d/49-openscale-reboot.rules` ; sous Windows il passe en indiquant
que `LocalSystem` porte le privilège ; ailleurs il est écarté. Son remède nomme
`sudo ./install.sh`. Ajouter le test correspondant dans `doctor_test.go`, sur le modèle
des contrôles voisins.

- [ ] **Step 5: Tests**

Run: `CGO_ENABLED=0 go test ./deploy/ ./cmd/openscale/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add deploy/linux/openscale-reboot.rules deploy/linux/install.sh deploy/linux/uninstall.sh deploy/deploy_test.go cmd/openscale/doctor.go cmd/openscale/doctor_test.go
git commit -m "feat(installation): le poste Linux a le droit de redémarrer l'ordinateur

Le service tourne en « openscale », pas en root : sans règle polkit,
systemctl reboot est refusé et le bouton ne sert à rien. La règle est la
plus étroite possible — un utilisateur, une action, et pas l'extinction.

doctor le contrôle, pour que le défaut se voie avant la panne."
```

---

## Task 8: L'écran

**Files:**
- Create: `web/src/admin/components/Maintenance.svelte`
- Create: `web/test/admin-maintenance.test.ts`
- Modify: `web/src/admin/lib/api.ts`, `web/src/admin/lib/dto.ts`
- Modify: `web/src/admin/pages/Station.svelte` (montage + en-tête à corriger)

**Interfaces:**
- Consumes: `POST /admin/api/config/reload` (tâche 2), `POST /admin/api/restart` (3),
  `POST` et `DELETE /admin/api/reboot` (5).
- Consumes: `Act.svelte` (`kind`, `label`, `busy`, `act`, `protected`, `onrun`),
  `Panel.svelte`, `admin.protect()`, `api.AdminError`.

- [ ] **Step 1: Les types et les appels**

Dans `web/src/admin/lib/dto.ts` :

```ts
/** Le compte à rebours avant le redémarrage de l'ordinateur. */
export interface RebootDTO {
  at: string
  seconds_left: number
}
```

Dans `web/src/admin/lib/api.ts`, dans la famille des routes protégées :

```ts
/**
 * Relit `config.json` tel qu'il est sur le disque et le met en service.
 *
 * Elle n'écrit rien : le document EST le fichier. Un refus 422 porte toutes les fautes,
 * comme un enregistrement, et le fichier reste tel qu'il est — c'est le seul exemplaire.
 */
export function reloadConfigFromDisk(): Promise<ConfigDTO> {
  return postJSON<ConfigDTO>('/admin/api/config/reload', {})
}

/**
 * Demande au poste de redémarrer.
 *
 * 202 : le poste va s'arrêter et son superviseur le relancera. Il n'y aura pas de
 * seconde réponse sur cette connexion — l'écran sonde `/healthz` jusqu'au retour, comme
 * après une mise à jour. Un 501 dit que personne ne le relancerait.
 */
export function restartStation(): Promise<ActionDTO> {
  return postJSON<ActionDTO>('/admin/api/restart', {})
}

/** Arme le redémarrage de l'ordinateur, dans trente secondes. */
export function armReboot(): Promise<RebootDTO> {
  return postJSON<RebootDTO>('/admin/api/reboot', {})
}

/** Annule le redémarrage armé, tant que l'échéance n'est pas passée. */
export function cancelReboot(): Promise<ActionDTO> {
  return sendJSON<ActionDTO>('DELETE', '/admin/api/reboot', {})
}
```

- [ ] **Step 2: Écrire les tests de l'écran**

`web/test/admin-maintenance.test.ts`, sur le modèle de `admin-update.test.ts` :

```ts
// Le compte à rebours s'affiche et « Annuler » l'arrête.
// Le bouton de redémarrage de l'ordinateur est `kind="destructive"`.
// Un 501 sur /admin/api/restart affiche la phrase du service et n'affiche pas le bouton
//   comme s'il avait marché.
// « Relire le fichier » affiche les fautes d'un 422, toutes.
```

Écrire ces quatre tests avec le montage exact des tests voisins (`render`, `screen`,
`fetch` bouchonné). Les intitulés ci-dessus sont la liste des cas, pas le contenu.

- [ ] **Step 3: Lancer, vérifier l'échec**

Run: `cd web && npm test -- admin-maintenance`
Expected: FAIL — le composant n'existe pas.

- [ ] **Step 4: Écrire le composant**

`web/src/admin/components/Maintenance.svelte` : un `Panel` intitulé **Maintenance**, trois
`Act`, dans l'ordre de brutalité croissante :

| Bouton | `kind` | Ce qu'il dit après |
|---|---|---|
| Relire le fichier de configuration | `write` | l'empreinte servie, ou toutes les fautes du 422 |
| Redémarrer le poste | `write` | « Le poste redémarre. L'écran revient tout seul. » puis sonde `/healthz` |
| Redémarrer l'ordinateur | `destructive` | le compte à rebours, et un bouton **Annuler** tant qu'il court |

Trois règles que le composant doit tenir :

1. **Une confirmation à deux temps sur le troisième**, comme un acte qui ne se défait pas.
2. **Le sondage de `/healthz` est celui d'`Update.svelte`** — une erreur réseau y est le
   cas nominal, puisque c'est le geste demandé qui tue le serveur. Extraire la fonction
   plutôt que la recopier, si les deux pages peuvent la partager.
3. **Un 501 affiche la phrase du service**, jamais un bouton grisé sans explication.

- [ ] **Step 5: Monter la rubrique et corriger l'en-tête**

Dans `web/src/admin/pages/Station.svelte`, monter `<Maintenance {admin} />` en bas de
page, et **corriger le paragraphe de l'en-tête** qui dit aujourd'hui :

> **There is no « restart » button.** No configuration block demands one (§11.4,
> ADR-027) […]

par :

```
 * **The restart buttons are repairs, not settings.** No configuration block demands a
 * restart, and none may (§11.4, ADR-027): what the Maintenance section offers is the
 * way out of a station under kiosk, where no console can be reached. Rereading the file
 * changes nothing about that rule -- it is the hot reload of §11.4, applied to a
 * document somebody edited by hand.
```

- [ ] **Step 6: Tests front**

Run: `cd web && npm test && npx svelte-check`
Expected: PASS, aucune erreur `svelte-check`.

- [ ] **Step 7: Reconstruire le bundle embarqué**

Run: `cd web && npm run build`
Expected: `internal/web/dist/` mis à jour — il est embarqué par `//go:embed`, un oubli
livrerait un binaire dont l'écran ignore les nouveaux boutons.

- [ ] **Step 8: Commit**

```bash
git add web/ internal/web/dist/
git commit -m "feat(admin): la page Poste porte les trois gestes de reprise

Trois boutons dans l'ordre de brutalité croissante, et le troisième est
rouge avec trente secondes pour se raviser.

L'en-tête de la page disait « il n'y a pas de bouton redémarrer, ADR-027 » :
il dit maintenant ce que l'ADR refuse vraiment — un redémarrage exigé par
un réglage — et ce que ces boutons sont, des réparations."
```

---

## Task 9: La trace écrite

**Files:**
- Modify: `docs/02-architecture.md` (§14.5 la liste des routes, §14.4 la page Poste,
  §15.3 la règle polkit, un ADR neuf)
- Modify: `docs/03-glossaire.md` (« ordinateur » ≠ « poste », `ERR-SYS-09` à `ERR-SYS-11`)
- Modify: `TROUBLESHOOTING.md` (les deux codes neufs)
- Modify: `SUIVI.md` (les compteurs, **mesurés**)

- [ ] **Step 1: L'ADR**

Écrire l'ADR au prochain numéro libre — vérifier avec
`grep -c "^### ADR-" docs/02-architecture.md` et prendre la suite du dernier.

Titre : **« Redémarrer est une réparation, jamais une conséquence d'un réglage »**.
Il doit dire, dans le style des voisins : le contexte (un poste sous kiosque n'a aucune
sortie), la décision (quatre gestes, le mécanisme de l'arrêt propre, le délai applicatif),
et les conséquences — dont **« ADR-027 n'est pas rouvert »**, avec la phrase qui distingue
un redémarrage exigé par un bloc de configuration d'un redémarrage de dépannage.
Portée : `internal/web`, `internal/platform`, `cmd/openscale`, `deploy/linux`, §11.4,
§14.4, §14.5, §15.3.

- [ ] **Step 2: Corriger le § qui dit le contraire**

Dans `docs/02-architecture.md`, la liste des routes de §14.5 porte aujourd'hui :

> (pas de POST /admin/api/restart : aucun bloc de configuration n'exige un
> redémarrage du processus — §11.4. Le seul redémarrage légitime est celui que
> le SCM ou systemd déclenche seul.)

Le remplacer par les quatre routes neuves et la phrase qui les cadre : aucun bloc de
configuration n'exige toujours de redémarrage, et le redémarrage offert reste celui que
le SCM ou systemd déclenche — la station se contente de s'arrêter.

De même, la ligne « Poste » du tableau de §14.4 porte *« (Pas de bouton « redémarrer » :
aucun bloc de configuration ne l'exige — §11.4.) »* : la remplacer par la rubrique
Maintenance et ses trois gestes.

- [ ] **Step 3: Les deux codes**

Ajouter `ERR-SYS-09` (redémarrage demandé), `ERR-SYS-10` (poste non supervisé, le
redémarrage ne reviendrait pas) et `ERR-SYS-11` (plateforme sans redémarrage de
l'ordinateur) à la table des codes du glossaire et à `TROUBLESHOOTING.md`, avec la phrase
que le poste affiche et ce qu'un bénévole doit faire.

**Avant de poser ces trois numéros**, vérifier pourquoi `ERR-SYS-06` manque : le dépôt
laisse des numéros en trou quand un code est retiré (ADR-044 le fait pour les contrôles
37 et 47). Si c'en est un, ne pas le combler ; si c'est un oubli, le dire dans le commit.

Ajouter au § « Vocabulaire de prose » du glossaire : **« ordinateur » désigne la machine,
« poste » l'application** — deux sens du même mot sur un même écran est le défaut qu'on
évite ici.

- [ ] **Step 4: Mesurer, puis écrire**

Run:
```bash
CGO_ENABLED=0 go test ./... -count=1
gofmt -l . && go vet ./... && go run ./tools/boundary && go run ./tools/deps
cd web && npm test && npx svelte-check
```
Expected: tout vert. **Relever les nombres réels** — tests Go, paquets, tests front — et
n'écrire dans `SUIVI.md` que ce qui a été lu dans cette sortie.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "docs: quatre gestes de reprise, et ce qu'ADR-027 refuse vraiment

L'architecture disait « pas de POST /admin/api/restart » sans distinguer
un redémarrage exigé par un réglage — toujours refusé — d'un redémarrage de
dépannage, qu'aucun écran n'offrait. L'ADR neuf pose la distinction et la
liste des routes la reflète.

Compteurs de SUIVI.md mesurés, pas recopiés."
```

---

## Auto-revue

**Couverture du spec.** Les quatre gestes ont chacun leur tâche (2, 3, 5, 6) ; le piège de
l'export est le troisième test de la tâche 2 ; le retour arrière qui ne réécrit pas le
fichier est dans le commentaire et l'implémentation de `reloadConfigFromDisk` (`FileBefore`
laissé nil) ; le 501 hors superviseur est tâche 3 ; le délai applicatif et son annulation
sont tâche 5 ; la règle polkit et le contrôle `doctor` sont tâche 7 ; le renommage du garde
est tâche 1 ; le libellé « ordinateur » est tâche 8 et 9 ; le kiosque reste hors périmètre,
et rien ne l'implémente.

**Cohérence des noms.** `DowntimeGuard` (tâche 1) est appelé par `stationRestarter.Restart`
(3) et par `armReboot` (5). `station.DowntimeRefused` est produit en 3 et consommé en 3.
`rebootDelay`, `errRebootArmed`, `newRebootPlan`, `Arm`, `Cancel`, `Deadline` (5) ne sont
employés que dans `internal/web`. `platform.Supervised` (3) est consommé par
`restarterFor` (3). `platform.Reboot` (4) est consommé par `machineRebooter` (5).

**Deux points que l'implémenteur devra trancher sur pièce**, et qui sont nommés là où ils
tombent : la signature exacte de `moveListener` (tâche 2, étape 3) et l'existence d'un
gestionnaire de service injectable dans `cmd/openscale` (tâche 6, étape 1). Aucun des deux
ne change la conception.
