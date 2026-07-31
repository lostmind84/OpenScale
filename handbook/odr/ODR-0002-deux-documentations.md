# ODR-0002 — Deux documentations, jamais fusionnées

**Statut** : accepté
**Date** : 2026-07-31

## Contexte

Le dossier `docs/` s'est construit pendant la conception comme une référence
exhaustive : le fichier d'architecture dépasse à lui seul 600 ko, et le glossaire n'est
pas loin derrière. Il est écrit pour être **interrogé** — on y entre par le § ou l'ADR
que le code cite —
et non pour être lu de bout en bout. Il est la source de vérité factuelle du projet, et
il le reste.

Un développeur qui découvre le dépôt n'a pas ce besoin-là. Il veut savoir en quinze
minutes ce que fait le produit, comment le lancer, où sont les frontières, et à quoi
s'attendre en ouvrant une PR. Lui servir la référence entière revient à ne rien lui
servir.

Les deux publics veulent des choses **opposées** : l'un veut tout, l'autre veut le
minimum qui met en route.

## Décision

Deux documentations distinctes, dans deux dossiers, avec deux règles.

- **`handbook/`** — pour les humains. Court, hiérarchisé, il vaut par ce qu'il omet.
  C'est **le seul dossier publié** : `mkdocs.yml` déclare `docs_dir: handbook`.
- **`docs/`** — la référence technique. Exhaustive, elle fait foi dès qu'un détail
  compte. **Jamais publiée**, jamais résumée en place, jamais allégée.

Aucune fusion. Aucune génération de l'une depuis l'autre.

## Alternatives écartées

| Alternative | Pourquoi non |
|---|---|
| Une seule documentation, allégée | Un arbitrage écrit noir sur blanc dans `docs/` est ce qui empêche de le rouvrir tous les six mois. Alléger, c'est perdre les raisons — et les raisons sont ce qui a coûté le plus cher |
| Publier `docs/` tel quel | Des pages de plusieurs centaines de ko, des renvois écrits pour un navigateur de dépôt, et un nouvel arrivant qui referme l'onglet |
| Générer `handbook/` depuis `docs/` | Aucun mécanisme ne sait choisir ce qu'il faut omettre. Le résultat serait un résumé qui dériverait en silence à chaque modification de la source |
| Publier les deux, en deux sections du site | Deux tons, deux publics et deux niveaux d'exhaustivité dans une même navigation : le lecteur ne sait plus lequel fait autorité |

## Conséquences

**Les renvois vers `docs/` sont des URL GitHub absolues.** `docs/` n'existe pas dans le
site publié : un lien relatif y serait mort. C'est une contrainte permanente pour qui
écrit dans `handbook/`.

**Une duplication est assumée.** La page [Démarrer](../getting-started.md) recouvre en partie
[`docs/05-demarrage-rapide.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/05-demarrage-rapide.md).
C'est le prix de l'existence des deux publics, et il est jugé inférieur à celui d'un
renvoi qui enverrait le débutant dans la référence.

**`handbook/` n'a pas le droit d'inventer.** Quand une information manque — une commande,
une version, un prérequis —, on écrit `> **TODO(dev)** : …` et on continue. Une page
trouée et honnête reste utilisable ; une page plausible et fausse fait perdre une
après-midi.

**Rien ne vérifie automatiquement que `handbook/` dit vrai.** `mkdocs build --strict`
attrape les liens internes cassés, rien de plus : il ne sait pas si une commande a
changé de nom.

> **TODO(dev)** : décider s'il faut un contrôle en CI qui rejoue les commandes de
> `getting-started.md`, ou si une relecture à chaque version suffit.

**Une ligne dans `CLAUDE.md`** rappelle la séparation, parce que la tentation de fusionner
reviendra — et parce qu'un agent qui ne la connaîtrait pas commencerait par « ranger ».
