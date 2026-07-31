# OpenScale

Poste de pesée libre-service pour épicerie coopérative.

Le client pose son sac sur une balance connectée, touche l'image de son produit sur un
écran tactile, une étiquette code-barres s'imprime aussitôt. Il la colle sur son sac ;
la caisse la scanne. Un toucher, une étiquette, sans confirmation.

OpenScale remplace une application Microsoft Access de 2015 encore en service, dont il
reprend les fonctionnalités et les contrats externes — le format du code-barres lu par
la caisse, la géométrie de l'étiquette — mais aucune ligne de code.

**Développement terminé, éprouvé sur banc réel — SATO WS408 et GRAM XFOC —, pas encore
en service.** Il reste la recette sur un poste pilote. Ce qui est prouvé, ce qui ne
l'est pas et ce qui reste ouvert : [SUIVI.md](SUIVI.md).

## Essayer, sans balance et sans imprimante

Un poste complet tourne sur votre machine en quatre commandes. **Les deux colonnes font
la même chose** — prenez celle de votre système, et lancez les deux dernières depuis un
second terminal.

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
make build

./bin/openscale serve \
  --config testdata/config-demo.json \
  --data /tmp/openscale-demo

cp testdata/catalog/flv_demo.csv \
  /tmp/openscale-demo/catalog/incoming/flv_2.csv

curl -X POST http://127.0.0.1:8085/api/v1/weigh \
  -H "Content-Type: application/json" \
  -d '{"product_id":"894","manual_weight_g":1236,"key":"essai-1"}'
```

</td><td>

```powershell
pwsh -File ./make.ps1 build

.\bin\openscale.exe serve `
  --config testdata\config-demo.json `
  --data $env:TEMP\openscale-demo

Copy-Item testdata\catalog\flv_demo.csv `
  $env:TEMP\openscale-demo\catalog\incoming\flv_2.csv

Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:8085/api/v1/weigh -ContentType "application/json" `
  -Body '{"product_id":"894","manual_weight_g":1236,"key":"essai-1"}'
```

</td></tr></table>

<http://127.0.0.1:8085> est l'écran client. Le catalogue de démonstration — 60 produits
d'un vrai export — remplit la grille en quelques secondes, puis le fichier disparaît :
**sa suppression est l'acquittement**. La pesée écrit dans `labels/` la trame qu'une
vraie imprimante recevrait, octet pour octet.

Le reste — sortir l'étiquette en PDF, lancer le diagnostic, la liste des commandes du
binaire — est dans [`docs/05-demarrage-rapide.md`](docs/05-demarrage-rapide.md).

## Ce que ça fait

- Lecture du poids sur balance série, avec repli en saisie manuelle
- Grille de produits tactile par catégorie, recherche insensible aux accents
- Calcul du prix, gestion de la tare, produits vendus à l'unité
- Garde-fous de pesée : balance vide, non tarée, panier absent, produits légers
- Génération du code-barres EAN-13 et impression de l'étiquette
- Import du catalogue produits depuis un export Odoo
- Écran d'administration utilisable par des bénévoles non-informaticiens

## Choix techniques

- **Un seul binaire** : backend Go et interface web embarquée, sans runtime à installer.
  Cross-compilé pour Windows, Linux et linux-arm64 — un poste se déploie en copiant un
  fichier.
- **Zéro cgo**, ce qui rend cette cross-compilation triviale et sans chaîne C.
- **Drivers enfichables** pour la balance et l'imprimante, choisis par configuration :
  l'ajout d'un modèle est une contribution isolée.
- **Chaque poste est autonome** — sa configuration, sa base SQLite, son catalogue. Aucun
  serveur central, aucune dépendance réseau pour peser.

## Documentation

**Pour découvrir le projet : <https://lostmind84.github.io/OpenScale/>** — le *handbook*,
court et hiérarchisé, écrit pour un développeur qui arrive. Sa source est `handbook/`.

La table ci-dessous est la référence technique. Elle est exhaustive, et elle fait foi dès
qu'un détail compte.

| Fichier | Contenu |
|---|---|
| [`docs/02-architecture.md`](docs/02-architecture.md) | La référence : 22 sections, 52 ADR, le code des interfaces |
| [`docs/03-glossaire.md`](docs/03-glossaire.md) | Le lexique de nommage, qui fait autorité |
| [`docs/04-parametrage-sato.md`](docs/04-parametrage-sato.md) | Ce que l'imprimante a en mémoire, et la géométrie de l'étiquette |
| [`docs/05-demarrage-rapide.md`](docs/05-demarrage-rapide.md) | La démonstration pas à pas, et les commandes du binaire |
| [`docs/06-developpement.md`](docs/06-developpement.md) | Cibles de construction, règles vérifiées, fabrication et publication |
| [`docs/07-ajouter-un-materiel.md`](docs/07-ajouter-un-materiel.md) | **Brancher une balance, une imprimante ou un transport non gérés** : la marche à suivre, le banc de conformité, les pièges déjà payés |
| [`docs/08-ajouter-une-source-de-catalogue.md`](docs/08-ajouter-une-source-de-catalogue.md) | **Aller chercher les produits ailleurs que dans un CSV** : API d'un ERP, autre format, autre dépôt. Les deux axes, l'identité d'un lot, l'acquittement sans suppression |
| [`docs/01-etat-des-lieux.md`](docs/01-etat-des-lieux.md) | L'application d'origine, ses règles et ses défauts |
| [`docs/00-donnees-retirees.md`](docs/00-donnees-retirees.md) | Coordonnées et adresses retirées du dépôt, et pourquoi |
| [`docs/reference-existant/`](docs/reference-existant/) | Analyse détaillée du legacy, à consulter au besoin |
| [`SUIVI.md`](SUIVI.md) | Avancement, points bloquants, journal |
| [`CLAUDE.md`](CLAUDE.md) | Conventions de développement |

## Développer

Go 1.26.5, Node 22 seulement pour le front. Pas de chaîne C, pas de Docker.

```bash
make test        # ou : pwsh -File ./make.ps1 test
make build
```

Code et commentaires en **anglais**, documentation et messages utilisateur en
**français**. Les cibles, les coupes architecturales vérifiées par `make boundary` et le
détecteur de course sont détaillés dans
[`docs/06-developpement.md`](docs/06-developpement.md).

## Installer un poste

Sur un Windows nu, sans dépôt, sans Go et sans archive à décompresser — les droits
administrateur sont demandés en cours de route :

```powershell
irm https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/windows/bootstrap.ps1 | iex
```

<details>
<summary>Depuis une invite de commandes (<code>cmd</code>)</summary>

```cmd
curl -fsSL https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/windows/bootstrap.cmd -o %TEMP%\openscale.cmd && %TEMP%\openscale.cmd
```

</details>

La commande prend la dernière version publiée, **vérifie son empreinte avant de la
décompresser**, pose trois questions — mot de passe de la session du poste, production ou
pilote, ouverture de session automatique — puis installe tout : compte Windows dédié,
service, tâche du kiosque, réglages d'alimentation, fiche d'installation à ranger dans le
classeur du magasin.

Le parcours complet du bénévole — redémarrage de recette, balance, imprimante, catalogue,
et **l'installation par clé USB d'un poste sans Internet** — est dans
[`INSTALLATION.md`](INSTALLATION.md).

## Déployer

Un poste ne s'installe pas en copiant `openscale.exe` : il lui faut les scripts, la tâche
planifiée ou les unités systemd, la configuration et les documents du bénévole.
`make release` assemble le tout en une archive par plateforme, et pousser un tag de
version fait la même chose sur la page *Releases*. C'est cette archive que la commande
ci-dessus télécharge.

La fabrication des archives est décrite dans
[`docs/06-developpement.md`](docs/06-developpement.md).

## Licence

**GNU Affero General Public License v3.0 ou ultérieure** — voir [`LICENSE`](LICENSE).

Le choix est celui d'un produit destiné à circuler entre coopératives. L'architecture est
faite pour qu'ajouter un modèle de balance ou d'imprimante soit *une contribution isolée*
— un paquet et une ligne ; l'AGPL garantit que cette contribution revient à toutes les
coopératives, et pas seulement à celle qui a payé le développement.

Les composants tiers gardent leur propre licence, toutes compatibles : voir
[`THIRD-PARTY.md`](THIRD-PARTY.md).
