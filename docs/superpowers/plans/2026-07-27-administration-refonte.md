# Reprise de l'écran d'administration — plan d'implémentation

> **Pour les agents :** ce plan s'exécute tâche par tâche. Les étapes sont des cases à
> cocher. La spécification est `docs/superpowers/specs/2026-07-27-administration-refonte-design.md`.

**But** : rendre l'administration utilisable et cohérente avec l'écran client refait —
réparer ce qui plante, protéger l'acte plutôt que la porte, et redessiner les 9 pages.

**Approche** : quatre lots livrables séparément. Le lot A ne contient aucune conception,
seulement des réparations, pour qu'un poste redevienne utilisable immédiatement. Le lot B
pose le socle dont C et D dépendent.

**Pile** : Go 1.23 sans cgo · Svelte 5 (runes) + TypeScript · Vite · Vitest · Playwright
pour la mesure dans le navigateur.

## Contraintes globales

Copiées de `CLAUDE.md` et de la spécification. Elles s'appliquent à **toutes** les tâches.

- **Langue** : code, identifiants, commentaires en **anglais** ; documentation en
  **français** ; messages destinés aux bénévoles en **français**.
- **Zéro cgo.** Aucune dépendance nouvelle sans vérification qu'elle est pur Go.
- **Style** : Clean Code, mais **le Go idiomatique gagne** en cas de conflit. `godoc` en Go
  (commentaire commençant par le nom de l'élément), `TSDoc` en TypeScript et Svelte. Les
  commentaires expliquent le **pourquoi**, jamais le quoi.
- **Test-driven** pour tout ce qui est calculable.
- **Avant de déclarer une tâche terminée, exécuter la vérification et montrer sa sortie.**
- L'écran **client** ne régresse pas : `web/test/tokens.test.ts` continue de garantir ses
  cibles de 72 px, et `npm run budget` reste sous 110 ko gzip.
- Commandes : `cd web && npx vitest run` · `npm run check` · `npm run build` ·
  `npm run budget` · `go test ./...` · `go vet ./...`

## Fichiers touchés

| Fichier | Responsabilité après le chantier |
|---|---|
| `internal/web/config.go` | charge utile de configuration — **aucune liste servie `null`** |
| `internal/web/server.go` | table des routes — **deux camps**, ouvert et protégé |
| `internal/web/guard.go` | `authenticated()` — le 401 renvoie vers le chemin qui existe |
| `internal/domain/config.go` | contrôle 31 — refuse une empreinte au corps imprimable |
| `testdata/config-lacagette.json` | configuration livrée — **aucun secret** |
| `deploy/windows/install.ps1` | tire un code de secours quand le champ est absent **ou inutilisable** |
| `web/src/App.svelte` | le garde d'ouverture de l'administration se relâche |
| `web/src/admin/App.svelte` | **le rail** : ossature, navigation, colonne de lecture |
| `web/src/admin/lib/session.svelte.ts` | état : deux champs d'erreur distincts, et `protect()` |
| `web/src/admin/lib/api.ts` | `429` et `Retry-After` lus |
| `web/src/admin/components/PasswordPanel.svelte` | **créé** — la demande de mot de passe d'un acte |
| `web/src/admin/pages/*.svelte` | les 9 pages, forme et structure |
| `web/test/tokens.test.ts` | restreint à l'écran client |
| `web/test/admin-*.test.ts` | les tests de l'administration |

---

# LOT A — Réparer

Aucune conception. Ce lot rend le poste utilisable.

## Tâche A1 — Aucune liste de la charge utile d'administration n'est servie `null`

**Fichiers**
- Modifier : `internal/web/config.go` (le `configDTO`, autour de la ligne 53)
- Test : `internal/web/config_test.go`

**Interfaces**
- Produit : rien de nouveau ; les champs `retired_keys`, `pending_confirmation` et
  `config.catalog.options` cessent de valoir `null` dans la réponse JSON.

- [ ] **A1.1 — Écrire le test qui échoue**

Dans `internal/web/config_test.go`, sur le modèle de
`internal/web/catalog_test.go:192` (`TestNoListOfThisPayloadIsEverNull`) :

```go
// TestNoListOfTheAdminPayloadIsEverNull.
//
// Une slice nil se sérialise en `null`, et `null.length` est une TypeError. C'est le
// défaut EXACT que l'écran client a eu sur `categories` ; il est arrivé ici sur
// `retired_keys`, et l'écran d'administration est tombé dans la surcouche ERR-UI-01
// avec son rechargement automatique à 5 s — « une page d'erreur sans détails ».
func TestNoListOfTheAdminPayloadIsEverNull(t *testing.T) {
	b := newBench(t)
	b.openSession(t)

	raw := b.getBody(t, "/admin/api/config")

	for _, path := range []string{`"retired_keys":null`, `"options":null`} {
		if strings.Contains(raw, path) {
			t.Fatalf("la charge utile porte %s : le front lève sur .length", path)
		}
	}
}
```

- [ ] **A1.2 — Lancer le test, vérifier qu'il échoue**

`go test ./internal/web/ -run TestNoListOfTheAdminPayloadIsEverNull -v`
Attendu : FAIL sur `"retired_keys":null`.

- [ ] **A1.3 — Corriger la charge utile**

Dans `internal/web/config.go`, là où le `configDTO` est construit : allouer les listes et
la carte plutôt que de les laisser nil.

```go
// Alloué et non laissé nil : une slice nil se sérialise en `null`, et l'écran
// d'administration lit `retired_keys.length` sans filet. C'est le défaut qui a fait
// tomber l'écran client sur `categories`, et il est revenu ici.
out := configDTO{Retired: make([]string, 0)}
```

Faire de même pour la carte `catalog.options` du document servi.

- [ ] **A1.4 — Lancer le test, vérifier qu'il passe**

`go test ./internal/web/ -run TestNoListOfTheAdminPayloadIsEverNull -v` → PASS
puis `go test ./internal/...` → tout vert.

- [ ] **A1.5 — Filet côté front**

Dans `web/src/admin/lib/draft.svelte.ts:42` et `:116`, ne pas dépendre du service :

```ts
this.retired = body.retired_keys ?? []
```

Le service ne sert plus `null` ; ce filet existe pour le poste qui tourne encore un
binaire plus ancien.

- [ ] **A1.6 — Commit**

```bash
git add internal/web/config.go internal/web/config_test.go web/src/admin/lib/draft.svelte.ts
git commit -m "fix(web): trois champs de la charge utile d'administration partaient en null"
```

## Tâche A2 — Le refus atteint l'écran

**Fichiers**
- Modifier : `web/src/admin/lib/session.svelte.ts`
- Modifier : `web/src/admin/App.svelte` (l'affichage des deux erreurs)
- Test : `web/test/admin-errors.test.ts` (créé)

**Interfaces**
- Produit : `Admin.linkError` (l'erreur du sondage) et `Admin.actionError` (l'erreur du
  dernier acte). `Admin.error` disparaît. Les pages qui la lisaient lisent `actionError`.

- [ ] **A2.1 — Écrire le test qui échoue**

```ts
// web/test/admin-errors.test.ts
import { describe, expect, it, vi } from 'vitest'
import { Admin } from '../src/admin/lib/session.svelte'

describe('l’erreur d’un acte survit au sondage', () => {
  it('n’est pas effacée par le rafraîchissement du tableau de bord', async () => {
    const admin = new Admin(60_000)
    vi.stubGlobal('fetch', async (route: string) => {
      if (route.includes('/health')) return new Response('{}', { status: 200 })
      return new Response(JSON.stringify({ message: 'Mot de passe incorrect.' }), { status: 401 })
    })

    await admin.login('faux')
    expect(admin.actionError).toBe('Mot de passe incorrect.')

    // Le sondage tourne toutes les 3 s et lit /health avec succès.
    await admin.refresh()
    expect(admin.actionError).toBe('Mot de passe incorrect.')
    vi.unstubAllGlobals()
  })
})
```

- [ ] **A2.2 — Lancer, vérifier l'échec**

`cd web && npx vitest run test/admin-errors.test.ts`
Attendu : FAIL — `actionError` n'existe pas.

- [ ] **A2.3 — Scinder le champ**

Dans `session.svelte.ts` :

```ts
  /**
   * Ce que le SONDAGE a à dire, et lui seul.
   *
   * Il tourne toutes les trois secondes ; s'il partageait son champ avec les actes, il
   * effacerait « Mot de passe incorrect. » avant qu'on ait fini de le lire — ce qu'il a
   * fait pendant tout le temps où l'administration a existé.
   */
  linkError = $state('')
  /** Ce que le dernier ACTE a à dire. Rien ne l'efface qu'un autre acte. */
  actionError = $state('')
```

`refresh()` n'écrit que `linkError`. `run()`, `login()`, `recover()` et `load()` n'écrivent
que `actionError`. `open()` remet `actionError` et `notice` à vide — changer de page est un
acte neuf.

`notice` est remis à vide par tout acte qui commence, y compris ceux qui échouent, pour que
la phrase de succès d'une action ne survive pas à la suivante.

- [ ] **A2.4 — Suivre les appelants**

`grep -rn "admin.error" web/src/admin/` et remplacer par `admin.actionError`, sauf dans
`App.svelte` où les deux s'affichent — le lien en tête d'écran, l'acte près de l'action.

- [ ] **A2.5 — Vérifier**

`cd web && npx vitest run` → tout vert · `npm run check` → 0 erreur

- [ ] **A2.6 — Commit**

```bash
git add web/src/admin web/test/admin-errors.test.ts
git commit -m "fix(admin): le refus d'un acte était effacé toutes les trois secondes par le sondage"
```

## Tâche A3 — La touche Réglages répond de nouveau

**Fichiers**
- Modifier : `web/src/App.svelte:169-175`
- Modifier : `web/src/admin/mount.ts` (rendre le démontage observable)
- Test : `web/test/screen.test.ts`

**Interfaces**
- Consomme : `mountAdmin(host: HTMLElement, onclose?: () => void)`
- Produit : rien.

- [ ] **A3.1 — Écrire le test qui échoue**

```ts
  it('rouvre l’administration après un retour à l’écran client', async () => {
    await open()
    const key = [...host.querySelectorAll<HTMLElement>('.filters button')].find((b) =>
      b.textContent?.includes('Réglages'),
    )
    key?.click()
    await vi.waitUntil(() => document.querySelector('[data-admin]') !== null)

    // Le bénévole revient à la grille…
    document.querySelector<HTMLElement>('[data-admin] .back')?.click()
    await vi.waitUntil(() => document.querySelector('[data-admin]') === null)

    // …et doit pouvoir y retourner.
    key?.click()
    await vi.waitUntil(() => document.querySelector('[data-admin]') !== null)
  })
```

- [ ] **A3.2 — Lancer, vérifier l'échec** — le second clic ne remonte rien.

- [ ] **A3.3 — Relâcher le garde**

```ts
  let adminMounted = false
  async function openAdmin(): Promise<void> {
    if (adminMounted) return
    adminMounted = true
    const module = await import('./admin/mount')
    // Relâché au démontage, sans quoi la touche ne répond plus jamais après un retour
    // à la grille — ce qu'elle a fait dès le jour où le garde est apparu (ADR-032).
    module.mountAdmin(document.body, () => {
      adminMounted = false
    })
  }
```

`mount.ts` appelle ce rappel après `unmount()`.

- [ ] **A3.4 — Vérifier** — `npx vitest run test/screen.test.ts` → PASS

- [ ] **A3.5 — Commit**

```bash
git add web/src/App.svelte web/src/admin/mount.ts web/test/screen.test.ts
git commit -m "fix(web): la touche Réglages ne répondait plus après un retour à la grille"
```

## Tâche A4 — La configuration livrée ne porte aucun secret

**Fichiers**
- Modifier : `testdata/config-lacagette.json` (bloc `admin`)
- Modifier : `internal/domain/config.go` (contrôle 31)
- Modifier : `deploy/windows/install.ps1` (autour de la ligne 184)
- Test : `internal/domain/config_test.go`

**Interfaces**
- Produit : `usableArgon2id(encoded string) bool` dans `internal/domain/config.go`.

- [ ] **A4.1 — Écrire les deux tests qui échouent**

```go
// TestTheDeliveredConfigurationCarriesNoSecret — §14.4 le dit d'elle : « la configuration
// livrée est l'export de §11.5, qui ne porte aucun secret ». Le fichier portait un
// remplissage tapé à la main, que VerifySecret refuse pour TOUT mot de passe, et que le
// contrôle 31 déclarait sain parce qu'il ne vérifiait que la forme.
func TestTheDeliveredConfigurationCarriesNoSecret(t *testing.T) {
	cfg := loadDelivered(t)
	if cfg.Admin.PasswordHash != "" || cfg.Admin.RecoveryCodeHash != "" {
		t.Fatalf("la configuration livrée porte un secret : %q / %q",
			cfg.Admin.PasswordHash, cfg.Admin.RecoveryCodeHash)
	}
}

// TestAHandTypedHashIsRefused — la longueur ne suffit pas :
// « for-the-delivered-configurationg » fait EXACTEMENT les 32 octets d'argon2id. Ce qui
// le trahit est que ses 32 octets sont du texte imprimable, ce que 32 octets tirés au
// sort ne sont qu'une fois sur 10^14.
func TestAHandTypedHashIsRefused(t *testing.T) {
	const placeholder = "$argon2id$v=19$m=65536,t=3,p=2$b3BlbnNjYWxlLXNhbHQxMg$Zm9yLXRoZS1kZWxpdmVyZWQtY29uZmlndXJhdGlvbmc"
	config := loadDelivered(t)
	config.Admin.PasswordHash = placeholder
	if findFault(config.Validate(testRegistries()), "admin.password_hash") == nil {
		t.Fatal("une empreinte tapée à la main est déclarée saine")
	}
}
```

- [ ] **A4.2 — Lancer, vérifier l'échec**

`go test ./internal/domain/ -run "Delivered|HandTyped" -v`

- [ ] **A4.3 — Vider le bloc admin du fichier livré**

```json
  "admin": {
    "password_hash": "",
    "recovery_code_hash": "",
    "session_minutes": 30,
    "attempts_per_minute": 5
  },
```

- [ ] **A4.4 — Durcir le contrôle 31**

```go
// usableArgon2id reports whether a stored hash could have been produced by argon2id.
//
// Well-formed is not enough, and the delivered configuration proved it: it carried
// « for-the-delivered-configurationg », which parses, and whose payload is EXACTLY the 32
// bytes argon2id produces. What gives it away is that those 32 bytes are printable text —
// 32 random bytes are, once in 10^14. Refusing that is not what repairs the defect; it is
// what stops the same gesture from coming back without a sound.
func usableArgon2id(encoded string) bool {
	// …parse, then: false when every byte of the key is printable ASCII.
}
```

Le contrôle 31 appelle `usableArgon2id` en plus de `wellFormedArgon2id`, et nomme le champ
fautif — `admin.password_hash` ou `admin.recovery_code_hash`.

- [ ] **A4.5 — Corriger l'installeur**

`deploy/windows/install.ps1:184` — la condition « le champ est vide » devient « le champ
est vide **ou** le binaire le refuse ». Le binaire est l'autorité : `openscale config
validate` nomme le champ.

- [ ] **A4.6 — Vérifier**

`go test ./internal/...` → tout vert · `go run ./cmd/openscale config validate testdata/config-lacagette.json`

- [ ] **A4.7 — Commit**

```bash
git add testdata/config-lacagette.json internal/domain/config.go internal/domain/config_test.go deploy/windows/install.ps1
git commit -m "fix(config): la configuration livrée portait une fausse empreinte, et doctor la déclarait saine"
```

## Tâche A5 — Les trois refus que le front ne savait pas lire

**Fichiers**
- Modifier : `web/src/admin/lib/api.ts` (lecture du `Retry-After`)
- Modifier : `internal/web/guard.go:127` (la phrase du 401 sans mot de passe)
- Test : `web/test/admin-errors.test.ts`

- [ ] **A5.1 — Écrire le test**

```ts
  it('dit combien de temps dure le verrouillage', async () => {
    vi.stubGlobal('fetch', async () =>
      new Response(JSON.stringify({ message: 'Trop d’essais.' }), {
        status: 429,
        headers: { 'Retry-After': '240' },
      }),
    )
    const admin = new Admin(60_000)
    await admin.login('faux')
    expect(admin.actionError).toContain('4 minutes')
    vi.unstubAllGlobals()
  })
```

- [ ] **A5.2 — Lire l'en-tête**

Dans `read()` de `api.ts`, sur un `429`, composer la phrase avec le `Retry-After` quand il
est là.

- [ ] **A5.3 — Corriger la phrase du service**

`guard.go:127` renvoie aujourd'hui vers « l'assistant de premier démarrage », **qui n'existe
pas dans le code**. La phrase devient :

```go
writeProblem(w, http.StatusConflict, "",
    "Ce poste n'a pas encore de mot de passe. Saisissez le code de secours de la fiche d'installation pour en poser un.")
```

Et le code passe de `401` à `409` : « pas de mot de passe » n'est pas « mauvais mot de
passe », et l'écran doit pouvoir les distinguer.

- [ ] **A5.4 — Vérifier** — `go test ./internal/web/` · `npx vitest run`

- [ ] **A5.5 — Commit**

```bash
git commit -am "fix(admin): le verrouillage et l'absence de mot de passe ne se disaient pas"
```

---

# LOT B — Le socle et la protection de l'acte

## Tâche B1 — Les routes changent de camp

**Fichiers**
- Modifier : `internal/web/server.go:441-465` (la table `guarded`)
- Test : `internal/web/session_test.go`

**Interfaces**
- Produit : deux tables — `readable` (montée sans `authenticated`) et `guarded`.

- [ ] **B1.1 — Écrire le test qui échoue**

```go
// TestWhatOpensAndWhatIsProtected — ADR-033 : la protection porte sur l'ACTE.
// On voit tout, on n'écrit pas tout.
func TestWhatOpensAndWhatIsProtected(t *testing.T) {
	b := newBench(t) // sans session

	open := []string{"/admin/api/config", "/admin/api/ports", "/admin/api/printers",
		"/admin/api/config/versions", "/admin/api/journal", "/admin/api/journal/export.csv"}
	for _, route := range open {
		if got := b.get(route).StatusCode; got == http.StatusUnauthorized {
			t.Errorf("%s demande un mot de passe pour LIRE", route)
		}
	}

	protected := []string{"/admin/api/config/export"}
	for _, route := range protected {
		if got := b.get(route).StatusCode; got != http.StatusUnauthorized {
			t.Errorf("%s = %d, attendu 401 : il emporte l'empreinte du mot de passe", route, got)
		}
	}

	// Les deux nouvelles : plus lourdes de conséquences que ce que le mot de passe gardait.
	for _, route := range []string{
		"/admin/api/troubleshooting/manual-entry", "/admin/api/catalog/import",
	} {
		if got := b.post(route, "{}").StatusCode; got != http.StatusUnauthorized {
			t.Errorf("%s = %d, attendu 401", route, got)
		}
	}
}
```

- [ ] **B1.2 — Lancer, vérifier l'échec**

- [ ] **B1.3 — Scinder la table**

Sortir de `guarded` : `GET /admin/api/config`, `/ports`, `/printers`,
`/label/preview.png`, `/config/versions`, `/journal`, `/journal/export.csv`,
`/technical`, `/imports`. Les monter directement.

Entrer dans `guarded` : `POST /admin/api/troubleshooting/manual-entry` et
`POST /admin/api/catalog/import`, aujourd'hui montées en clair lignes 427 et 433.

Un commentaire au-dessus de chaque table dit le critère d'ADR-033 : *ce qui change ce que
le poste vend, ou la façon dont il pèse.*

- [ ] **B1.4 — Vérifier** — `go test ./internal/web/` → tout vert

- [ ] **B1.5 — Commit**

```bash
git commit -am "feat(web): la protection porte sur l'acte et non sur la porte (ADR-033)"
```

## Tâche B2 — Le mécanisme de l'acte protégé

**Fichiers**
- Créer : `web/src/admin/components/PasswordPanel.svelte`
- Modifier : `web/src/admin/lib/session.svelte.ts` (ajout de `protect`)
- Test : `web/test/admin-protect.test.ts` (créé)

**Interfaces**
- Produit : `Admin.protect<T>(action: () => Promise<T>): Promise<T | null>` — tente
  l'acte ; sur `401`/`409`, ouvre le panneau ; à la réponse, **rejoue l'acte**.
- Produit : `Admin.pending` — l'acte en attente de mot de passe, ou `null`.

- [ ] **B2.1 — Écrire le test qui échoue**

```ts
describe('un acte protégé demande le mot de passe puis se rejoue', () => {
  it('n’oblige pas à ressaisir ce qu’on venait de faire', async () => {
    const admin = new Admin(60_000)
    let attempts = 0
    const action = async (): Promise<string> => {
      attempts += 1
      if (attempts === 1) throw new AdminError(401, 'Session expirée.')
      return 'enregistré'
    }

    const promise = admin.protect(action)
    await vi.waitUntil(() => admin.pending !== null)
    await admin.answerPassword('openscale')

    expect(await promise).toBe('enregistré')
    expect(attempts).toBe(2)
  })
})
```

- [ ] **B2.2 — Lancer, vérifier l'échec**

- [ ] **B2.3 — Écrire `protect`**

```ts
  /**
   * Exécute un acte protégé, en demandant le mot de passe SEULEMENT s'il le faut.
   *
   * ADR-033 : on peut tout voir, on ne peut pas tout écrire. Le mot de passe n'est donc
   * plus une porte franchie avant de regarder, mais une question posée au moment d'agir —
   * et l'acte est REJOUÉ derrière, sans que l'exploitant ait à refaire sa saisie.
   */
  async protect<T>(action: () => Promise<T>): Promise<T | null> { … }
```

Sur `401` : le panneau demande le mot de passe (`openSession`). Sur `409` : le panneau
demande le **code de secours** et un nouveau mot de passe (`recoverSession`) — c'est le
premier démarrage de §5.5 de la spécification.

- [ ] **B2.4 — Écrire `PasswordPanel.svelte`**

Un panneau, pas une page : titre, la phrase du refus, le champ, le nombre d'essais restants
quand le service le dit, le chemin du code de secours, deux boutons. Il emploie les jetons
de `app.css`.

- [ ] **B2.5 — Vérifier** — `npx vitest run` · `npm run check`

- [ ] **B2.6 — Commit**

```bash
git commit -am "feat(admin): le mot de passe est demandé à l'acte, et l'acte est rejoué"
```

## Tâche B3 — Le rail

**Fichiers**
- Modifier : `web/src/admin/App.svelte` (ossature complète)
- Modifier : `web/test/tokens.test.ts` (restreint à l'écran client)
- Test : `web/test/admin-shell.test.ts` (créé)

- [ ] **B3.1 — Écrire les tests qui échouent**

```ts
  it('montre les neuf pages dans deux groupes, sans porte', () => {
    // « Réglages avancés » n'est plus un onglet : les six pages sont là.
    const labels = [...host.querySelectorAll('.rail button')].map((b) => b.textContent?.trim())
    expect(labels).toContain('Matériel')
    expect(labels).not.toContain('Réglages avancés')
  })
```

- [ ] **B3.2 — Restreindre le test de jetons**

Dans `web/test/tokens.test.ts`, `sources()` ne parcourt plus `src/**` mais
`src/components/`, `src/App.svelte` et `src/app.css`. Le commentaire dit **pourquoi**, sans
quoi quelqu'un le rétablira :

```ts
/**
 * … La règle des 72 px vient d'une contrainte PHYSIQUE — 20 mm sous un doigt — et
 * l'administration ne s'y trouve pas : elle se conduit à la souris, sur des pages de
 * réglages à 45 champs (ADR-033). Ce test garde donc l'écran CLIENT, où la contrainte
 * s'applique, et les gros boutons du mode bénévole ont leur propre test.
 */
```

- [ ] **B3.3 — Écrire le rail**

Ossature : `.admin` en grille `16rem 1fr`. Le rail porte le titre, les deux groupes
(`Au quotidien`, `Réglages`), et en pied l'identité du poste, l'empreinte et le retour à
l'écran client. Le corps porte un en-tête de page et une colonne `max-width: 68rem`.

- [ ] **B3.4 — Vérifier dans le navigateur**

Mesurer les 9 pages : largeur du rail constante, colonne bornée, `scrollWidth ≤
clientWidth`, aucune erreur console, à 1366 / 1920 / 2560.

- [ ] **B3.5 — Commit**

```bash
git commit -am "feat(admin): un rail, deux groupes, et une colonne de lecture bornée"
```

---

# LOT C — Les deux pages du quotidien

## Tâche C1 — Tableau de bord

**Fichiers** — `web/src/admin/pages/Dashboard.svelte`, test `web/test/admin-dashboard.test.ts`

- [ ] **C1.1** Six feux en tuiles de hauteur unique, mesurée dans le navigateur.
- [ ] **C1.2** La cadence lit `scale.connected` et cesse d'écrire « 0 ms » sous huit
  intervalles ; test sur un poste dont la balance ne répond pas.
- [ ] **C1.3** `NON CONFIGURÉ` devient orange (règle CSS sur `data-configured`) et l'état
  « je ne sais pas » se distingue ; test sur les trois états.
- [ ] **C1.4** Les décisions locales reçoivent un plafond, et passent **après** la ligne du
  redémarrage sans intervention.
- [ ] **C1.5** Le lien « voir les 16 lignes » transporte son argument.
- [ ] **C1.6** Vérifier, commiter.

## Tâche C2 — Dépannage

**Fichiers** — `web/src/admin/pages/Troubleshooting.svelte`, test `web/test/admin-troubleshooting.test.ts`

- [ ] **C2.1** Les neuf actions gardent 72 px ; test dédié qui remplace la garantie perdue
  en B3.2.
- [ ] **C2.2** Les trois actions sans état « en cours » en reçoivent un.
- [ ] **C2.3** La réponse d'une action **remplace** la précédente.
- [ ] **C2.4** Un import refusé est annoncé refusé.
- [ ] **C2.5** Les deux actes protégés portent leur marque avant le clic, et passent par
  `admin.protect`.
- [ ] **C2.6** Vérifier, commiter.

---

# LOT D — Les six pages de réglages

Chaque page est indépendante des cinq autres : elles peuvent être traitées en parallèle.

## Tâche D1 — Matériel
Panneaux à en-tête d'état, réglages série repliés · trames **toujours affichées**, hexa et
décodé · détection qui annonce son avancement et désarme son bouton · `printer.health`
traduit · aucun état inventé pendant le chargement.

## Tâche D2 — Règles
Les 14 garde-fous de §6.4 avec leurs seuils · bloc code-barres · la note cesse d'annoncer
un message modifiable · `Number('')` n'écrit plus 0 · dérogations nommant le produit ·
clé d'itération non ambiguë · libellé du verdict au repos corrigé.

## Tâche D3 — Étiquette
L'aperçu reçoit le décalage X/Y · `onerror` sur l'`<img>` · bandeau chiffré de `Diagnose()`.

## Tâche D4 — Catalogue
Listes plafonnées et défilantes, total annoncé · « Le proposer de nouveau » atteignable ·
dérogation dissociée du retrait · dépôt de CSV et produits retirés présents.

## Tâche D5 — Journal
Tableau à en-tête figé dans son conteneur · détail ouvert à l'endroit du clic · filtre
inexistant retiré · jetons traduits.

## Tâche D6 — Poste
Diff relu après adoption · colonne « En service » qui montre le service · `<a download>`
qui ne meurt plus en silence.

---

# Vérification finale

- [ ] `go build ./... && go vet ./... && go test ./...`
- [ ] `cd web && npx vitest run && npm run check && npm run build && npm run budget`
- [ ] Mesure navigateur des 9 pages à 1366 / 1920 / 2560 : rail constant, colonne bornée,
  aucun défilement horizontal, aucune erreur console
- [ ] Captures avant/après des 9 pages
- [ ] Documentation : ADR-033, §14.4, §14.5, `SUIVI.md`
