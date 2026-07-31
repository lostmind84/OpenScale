# Contribuer

Tout ce qui suit est **constaté dans le dépôt** — historique git, intégration continue,
cibles de construction. Ce qui n'est écrit nulle part est signalé comme tel, en fin de
page.

## Avant d'ouvrir une PR : une commande

=== "Linux · macOS"

    ```bash
    make test
    ```

=== "Windows"

    ```powershell
    pwsh -File ./make.ps1 test
    ```

Elle enchaîne `go vet`, **deux passes** de `go test` — une avec `-race`, une sans —, puis
`make boundary` et `make deps`. C'est plus large que ce que joue l'intégration continue,
qui écarte quelques tests sensibles à l'ordonnanceur.

Sans `gcc`, la passe `-race` est sautée avec un avertissement. Elle trouve de vraies
courses : installez-le si vous touchez au Hub.

## Les branches

Un préfixe, une barre, un sujet en français avec des tirets. Les préfixes réellement
utilisés :

`feat/` · `fix/` · `docs/` · `chore/` · `ci/` · `test/` · `design/` · `security/`

Par exemple : `feat/sources-de-catalogue-enfichables`,
`fix/date-du-dernier-import-du-catalogue`.

## Les commits

**Conventional Commits, sujet en français.** Sur les 209 commits hors fusion, 208 suivent
la forme `type(portée): sujet`.

| Type | Ce qu'il couvre |
|---|---|
| `feat` · `fix` | l'essentiel de l'historique |
| `docs` | très employé — la documentation compte autant que le code ici |
| `test` · `refactor` · `ci` · `build` · `style` · `chore` | le reste |

Les portées suivent le découpage des paquets et des écrans : `web`, `admin`, `station`,
`config`, `domain`, `printing`, `catalogue`, `scale`, `deps`…

**Le sujet dit pourquoi, pas quoi.** C'est la caractéristique la plus nette de cet
historique, et elle va à l'encontre de la règle habituelle des 50 caractères : le sujet
médian en fait **73**. Deux exemples réels :

```
fix(catalogue): la date en barre basse est celle du dernier import appliqué
test(catalogue): l'assembleur partage etait le seul heritier sans tests
```

Le corps est long et argumenté quand la décision mérite d'être défendue. Ne le sautez
pas.

> **TODO(dev)** : les accents du sujet sont irréguliers — 81 commits accentués contre
> 128 sans, alors que toute la documentation du dépôt est accentuée. Trancher, puis
> l'écrire ici.

## La pull request

L'intégration se fait par PR mergée vers `main` ; l'historique n'a pas de *fast-forward*.
La CI doit être verte : quatre jobs, décrits ci-dessous.

> **TODO(dev)** : le dépôt n'a ni gabarit de PR, ni `CODEOWNERS`, ni règle de relecture
> écrite. Les protections de branche de `main` ne sont pas lisibles depuis le dépôt.

## Ce que l'intégration continue refuse

Rien de tout cela ne s'attrape à la relecture. Autant le savoir avant de pousser.

| Job | Ce qu'il vérifie |
|---|---|
| **test** | `go vet` · les deux passes de `go test` · `boundary` · `deps` · les planchers de couverture · `gofmt -l` doit ne rien renvoyer |
| **build** | compilation croisée en `CGO_ENABLED=0` vers Windows amd64, Linux amd64 et Linux arm64 |
| **scripts** | les scripts d'installation, exécutés par **Windows PowerShell 5.1** — le seul PowerShell d'un poste réel, et pas celui de votre machine |
| **front** | types, tests, construction, budget de poids, et `internal/web/dist` à jour |

Les planchers de couverture par paquet : `domain` 95 %, `scale` 90 %, `catalog` 85 %,
`store` 85 %, `printing` 80 %.

Les actions GitHub sont épinglées **sur un SHA de commit**, jamais sur un tag. Si vous en
ajoutez une, faites de même : un tag est un pointeur que son propriétaire déplace quand
il veut.

## Toucher au front

```bash
cd web
npm ci          # jamais npm install : les versions sont gelées
npm run dev
npm test        # vitest
npm run check   # svelte-check
```

!!! danger "Le piège qui a mordu deux fois"

    `internal/web/dist` est **commité**, pour que `go build` fonctionne sans Node. C'est
    donc l'écran réellement livré — et la suite de tests ne le regarde jamais : elle lit
    `web/src`.

    Un `dist` en retard laisse donc tout au vert pendant que le binaire sert un écran
    périmé. Après toute modification de `web/` :

    ```bash
    npm --prefix web run build
    git add internal/web/dist
    ```

    La CI compare les octets et refuse la PR si vous l'oubliez.

## Ajouter une balance ou une imprimante

C'est **un paquet et une ligne** dans `cmd/openscale/drivers.go`. Une cible dédiée
vérifie qu'un driver est complet, sans matériel ni réseau :

```bash
make driver
```

Elle couvre les bancs de conformité, les tests de registre et la coupe architecturale.
Elle ne couvre **ni** `-race`, **ni** le front, **ni** `make deps` : lancez `make test`
avant de dire que c'est fini.

Lisez [le guide des matériels](https://github.com/lostmind84/OpenScale/blob/main/docs/07-ajouter-un-materiel.md)
**avant** d'écrire quoi que ce soit — les paquets `internal/scale/example/` et
`internal/printing/example/` sont faits pour être copiés. Pour une source de catalogue,
[le guide correspondant](https://github.com/lostmind84/OpenScale/blob/main/docs/08-ajouter-une-source-de-catalogue.md)
et `internal/catalog/example/`.

## Publier une version

Poser et pousser un tag suffit : un workflow construit les trois archives et les attache
à la page *Releases*.

```bash
git tag -a 2.0.0 -m "Version 2.0.0"
git push origin 2.0.0
```

Un tag qui ne ressemble pas à une version ne déclenche **rien du tout** — pas même une
exécution en échec. Si une Release reste sans archives, regardez la page *Actions* : une
liste vide veut dire que le tag n'a pas été reconnu. Le détail est dans
[`docs/06-developpement.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/06-developpement.md).

## Deux conventions non négociables

**Le code est en anglais** — identifiants, paquets, colonnes SQL, clés de configuration,
routes, **et les commentaires**. La documentation est en français. Les messages destinés
aux bénévoles et aux clients sont en français.

**Le nommage fait autorité** dans
[`docs/03-glossaire.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/03-glossaire.md).
On ne s'en écarte pas, et on n'invente pas un second mot pour une chose qui en a déjà un.

Le reste des conventions — Clean Code, priorité au Go idiomatique en cas de conflit,
`godoc` et `TSDoc` — est dans
[`CLAUDE.md`](https://github.com/lostmind84/OpenScale/blob/main/CLAUDE.md).

## Ce que le dépôt ne dit nulle part

> **TODO(dev)** : il n'existe pas de `CONTRIBUTING.md` à la racine — cette page en tient
> lieu pour l'instant. Pas de hook git installé, pas de linter au-delà de `gofmt` et
> `go vet`, et aucune mention d'un DCO ou d'un accord de contribution. Le projet est sous
> AGPL-3.0-or-later : les contributions le sont aussi, mais rien ne le formalise.
