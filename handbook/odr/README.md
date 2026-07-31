# Décisions d'architecture

Un **ODR** — *OpenScale Decision Record* — fixe une décision au moment où elle est
prise : ce qu'on savait, ce qu'on a choisi, ce qu'on a écarté, ce que ça coûte.

Il ne se modifie plus une fois accepté. Quand une décision tombe, on n'édite pas
l'ancien document : on en écrit un nouveau, et on marque le précédent *remplacé*. Ce
qui compte n'est pas l'état actuel — le code le dit déjà — mais **le raisonnement**, y
compris quand il s'est révélé faux.

**Écrivez-en un** quand la décision est coûteuse à défaire, quand elle surprendra le
prochain arrivant, ou quand quelqu'un a déjà demandé « pourquoi pas plutôt X ? ».

**N'en écrivez pas** pour un choix qu'on peut annuler en une heure, pour une règle de
style, ni pour quoi que ce soit qu'un test fait déjà respecter.

Copiez [`template.md`](template.md), prenez le numéro suivant, ouvrez une PR.

## Les décisions

| N° | Titre | Statut |
|---|---|---|
| [ODR-0001](ODR-0001-zero-cgo.md) | Zéro cgo, un binaire, trois cibles | Accepté |
| [ODR-0002](ODR-0002-deux-documentations.md) | Deux documentations, jamais fusionnées | Accepté |

## Les décisions antérieures

Ce registre commence tard : le gros des arbitrages d'OpenScale a été rendu pendant la
conception, et vit dans le dossier d'architecture sous la forme d'**ADR numérotés**,
au § 20 de
[`docs/02-architecture.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/02-architecture.md).

Ils y restent, et ils **font toujours autorité**. Deux séries, deux préfixes, aucune
ambiguïté : un `ADR-0xx` renvoie au dossier d'architecture, un `ODR-0xxx` à cette page.
Aucun ADR ne sera renuméroté en ODR — recopier cinquante documents pour la beauté du
sigle, c'est cinquante occasions de perdre une nuance.

Quand un ODR reprend un ADR — c'est le cas d'[ODR-0001](ODR-0001-zero-cgo.md) — il le
dit et pointe dessus. La version longue est là-bas.
