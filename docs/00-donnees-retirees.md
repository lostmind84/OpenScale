# 00 — Données retirées du dépôt

Ce dépôt est destiné à circuler entre coopératives (licence AGPL-3.0). Les valeurs
ci-dessous sont des **coordonnées d'infrastructure et des adresses de personnes** : elles
n'apportent rien à qui reprend le code, et tout à qui cherche une porte d'entrée. Elles ont
donc été retirées **du contenu des documents et de l'historique Git**, par réécriture des
deux premiers commits.

**Il n'y a pas d'autre copie dans ce dépôt.** Les valeurs réelles vivent dans le classeur
d'exploitation du magasin et dans la base Access d'origine, qui n'est pas versionnée
(`*.mdb` et `*.accdb` sont refusés par `.gitignore` — cette base contient en clair le mot
de passe administrateur et les identifiants WebDAV et SMTP).

## Ce qui a été remplacé

| Ce que porte le dépôt | Nature de la valeur réelle | Où la trouver |
|---|---|---|
| `https://dav.example.org:8001/` | hôte WebDAV de **production**, HTTPS sur port non standard, alimenté par l'Odoo du prestataire | classeur d'exploitation · base `Systeme.AdresseReseau` |
| `https://dav.example.org:8002/dav_partage/` | l'hôte WebDAV **antérieur**, présent en commentaire dans le code Access | idem, code d'origine |
| `dev@example.org` | adresse personnelle du développeur de l'application d'origine, codée en dur dans `Module1.bas` — destinataire unique de `EnvoyerMailmb` et copie systématique de toutes les alertes | `Module1.bas:3615`, `:6264`, `:6486`, `:6654`, `:6743` |
| `salaries@example.org` | liste de diffusion des salariés, valeur de `Systeme.MailIntegrite` | base `SystemeDefaut` |
| `balances@example.org` | compte SMTP émetteur (`UtilisateurMail` = `MailEmetteur`) | base `SystemeDefaut` |
| `achat@example.org` | `MailIntegrite` de la table `Systeme` *vivante* de la base sauvegardée — une **autre** coopérative que celle du parc, `NomCoop = "Les Amis de la Coopé"` | base `Systeme`, offset 24 509 733 |
| `contact@example.org` | `MailBalanceDeconnectee` de cette même table | idem |

> **Le premier balayage a manqué les deux dernières, et la leçon vaut d'être écrite.** Il
> cherchait des **motifs devinés** — les fragments de noms d'hôte déjà repérés à l'œil — au
> lieu du motif générique d'une adresse. Deux adresses vivaient sur un domaine voisin, que
> personne n'avait remarqué : elles sont passées à travers, et il a fallu réécrire
> l'historique une seconde fois. Le balayage qui fait foi désormais est celui-ci, sur les
> fichiers **versionnés** uniquement, et il ne devine rien :
>
> ```
> git ls-files -z | xargs -0 grep -hoE "[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}"
> git ls-files -z | xargs -0 grep -hoE "https?://[^ )\"'\`]+"
> ```
>
> Il reste deux résultats attendus, et ils sont légitimes :
> `http://schemas.microsoft.com/cdo/configuration/` — l'espace de noms CDO, un identifiant
> de schéma qui ne désigne aucun hôte de la coopérative — et les adresses en `example.org`
> de ce tableau. `reference/test_etiquette_EtataImprimer.pdf` a été ouvert : flux compressés,
> aucune chaîne lisible, aucune métadonnée d'auteur.

Les **ports** (`8001`, `8002`), les **chemins** (`/dav_partage/`) et les **noms de champs**
sont conservés : ils portent une information technique — un port non standard est
précisément ce qui interdit le chemin UNC et impose le driver `webdav` (§10.1) — et ils ne
désignent aucun hôte joignable.

## Ce qui n'a PAS été retiré, et pourquoi

- **Le nom de la coopérative** (« La Cagette »). Il est partout : `config-lacagette.json`,
  `LaCagetteRules()`, les arbitrages A1 à A7. Le masquer reviendrait à réécrire la
  conception, et il ne donne accès à rien.
- **`testdata/catalog/flv.csv` et `flv_1.csv`** — 508 produits réels, prix et 181 photos.
  Décision du commanditaire : *« rien de grave, ce n'est qu'un échantillon ancien »*. Ces
  fichiers **font foi sur le format** contre toute documentation ; les retirer priverait le
  dépôt de ses fixtures les plus utiles et rendrait invérifiables une vingtaine de chiffres
  de `docs/02-architecture.md`.
- **Les numéros de ligne du code Access** (`Module1.bas:6903`). Ils sont la valeur de preuve
  de l'analyse, et le fichier auquel ils renvoient n'est pas dans ce dépôt.
- **Le nom du prestataire, « Cooperatic »** — dix mentions. C'est un nom d'entreprise, au
  même titre qu'« Odoo » ou « SATO », et c'est l'entité à qui l'inconnue n° 9 de §21 demande
  d'écrire. Le **nom d'hôte** qui le portait, lui, est remplacé : un nom d'entreprise ne
  désigne aucun service joignable, un sous-domaine si.

## Ce que cela change pour la mise en service

Rien au code : aucune URL, aucune adresse n'est compilée dans le binaire. `NeutralProfile()`
ne contient aucune URL, et les valeurs d'un site sont un **fichier livré**, pas du code
(ADR-026). Il y a donc un seul endroit à renseigner le jour de l'installation, et c'est déjà
l'inconnue n° 9 de `docs/02-architecture.md` §21 : le bloc `catalog.options` de
`config-lacagette.json`.

> **Aux relecteurs de `docs/`** : partout où un document dit « la valeur réelle est … » ou
> « l'URL relevée en base est … » suivi d'un `example.org`, la phrase décrit **la forme** de
> la valeur, pas son contenu. Le contenu est ici, et il est vide exprès.
