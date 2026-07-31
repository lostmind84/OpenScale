# OpenScale

Poste de pesée libre-service pour épicerie coopérative. Le client pose son sac sur une
balance, touche l'image de son produit sur un écran tactile, une étiquette code-barres
sort aussitôt. Il la colle sur son sac ; la caisse la scanne. Un seul binaire Go porte
tout — service, écran client, administration — sans runtime à installer. Chaque poste
vit seul : sa configuration, sa base SQLite, son catalogue, aucun serveur central.

!!! info "Où en est le projet"

    Développement terminé, éprouvé sur banc réel avec une balance GRAM XFOC et une
    imprimante SATO WS408. Pas encore en service : il reste la recette sur un poste
    pilote. L'état détaillé se lit dans
    [SUIVI.md](https://github.com/lostmind84/OpenScale/blob/main/SUIVI.md).

## Par où commencer

| Page | Ce qu'elle donne |
|---|---|
| [Démarrer](getting-started.md) | Du `git clone` à un poste qui tourne sur votre machine, sans balance ni imprimante |
| [Architecture](architecture.md) | La carte des composants, et les quatre frontières qui tiennent le code |
| [Décisions d'architecture](odr/README.md) | Ce qui a été tranché, et comment en trancher une nouvelle |
| [Contribuer](contributing.md) | Branches, commits, tests, ce que la CI refuse |

## Ce handbook, et la référence

Ce handbook est court **par choix**. Il vous met en route et vous montre où regarder ;
il ne remplace rien.

La référence technique complète vit dans le dossier `docs/` du dépôt. Elle est
volumineuse, écrite pour être lue par morceaux, et **elle fait foi** dès qu'un détail
compte :

- [`docs/02-architecture.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/02-architecture.md)
  — les 22 sections, tous les ADR, le code des interfaces
- [`docs/03-glossaire.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/03-glossaire.md)
  — le lexique de nommage, qui fait autorité
- [`docs/07-ajouter-un-materiel.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/07-ajouter-un-materiel.md)
  — brancher une balance ou une imprimante non gérées
- [`docs/08-ajouter-une-source-de-catalogue.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/08-ajouter-une-source-de-catalogue.md)
  — aller chercher les produits ailleurs que dans un CSV

Pour installer un poste réel — pas pour développer —, lisez
[INSTALLATION.md](https://github.com/lostmind84/OpenScale/blob/main/INSTALLATION.md),
écrit pour un bénévole.

## Licence

AGPL-3.0-or-later. Le choix vise un produit qui circule entre coopératives : ajouter un
modèle de balance doit être une contribution isolée, et cette contribution doit revenir
à tout le monde.
