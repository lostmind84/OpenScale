# `web/` — l'écran client

Le front Svelte du poste de pesée. Il est compilé vers `internal/web/dist`, que
`//go:embed` fait voyager dans le binaire : **un seul fichier à copier pour installer
un poste**.

## Les trois commandes

```
npm ci            # versions exactes, gelées par package-lock.json
npm run build     # -> ../internal/web/dist  (COMMITÉ)
npm run check     # svelte-check : types TypeScript et Svelte
npm test          # vitest
npm run budget    # < 110 ko gzip pour l'écran client (§14.1)
```

Depuis la racine : `make front` construit, `make front-check` construit puis vérifie.

## Ce qu'il faut savoir avant d'y toucher

**`internal/web/dist` est commité.** `go build` doit fonctionner sur une machine sans
Node : dans trois ans, un mainteneur qui corrige une règle métier n'installe pas de
chaîne JS. Après toute modification de `src/`, reconstruire et committer le `dist`.

**Le catalogue entier est dans le navigateur.** `GET /api/v1/catalog` sert les ~60 ko de
JSON une fois ; le filtrage et la recherche n'appellent plus rien. C'est ce qui les rend
instantanés **et** ce qui les garde fonctionnels pendant un redémarrage du service.

**La normalisation est un contrat à deux moitiés.** `src/lib/normalize.ts` doit rendre
exactement ce que rend `domain.Normalize` en Go. Les 121 paires de
`web/testdata/normalization.json` sont lues par les deux tests ; si les deux
implémentations divergent, l'un des deux rougit. Le piège est connu et documenté dans le
fichier : `'ß'.toUpperCase()` vaut `"SS"` en JavaScript et `"ß"` en Go, d'où un pliage par
le bas, point de code par point de code.

**Aucun plafond de tuiles, nulle part.** C'est le défaut le plus coûteux de l'ancienne
application — 120 emplacements par catégorie, franchis en production, six produits
vendables invisibles sans un message ni une ligne de journal. `test/grid.test.ts` rend les
331 pesables du catalogue réel et les 126 d'« Autres » : c'est ce qui interdit qu'un
plafond revienne par la fenêtre.

**Le DTO est celui du serveur, pas le nôtre.** `src/lib/dto.ts` et les types de
`src/lib/catalog.ts` recopient `internal/web/dto.go` et `internal/web/catalog.go`, champ
pour champ. Quand une valeur arrive avec un jumeau `_text` — `net_text`,
`unit_price_text` —, on l'affiche : on ne réimplémente pas la virgule décimale française.

**L'événement SSE est NOMMÉ.** Le serveur émet `event: state`, donc il s'écoute par
`addEventListener('state', …)`. `EventSource.onmessage` ne reçoit que les événements
anonymes et resterait muet pour toujours.
