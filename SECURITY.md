# Signaler une faille de sécurité

## Comment

**N'ouvrez pas d'issue publique.** Utilisez le formulaire privé de GitHub :
onglet **Security** du dépôt → **Report a vulnerability**. Il crée une conversation que
seuls les mainteneurs voient, et où un correctif peut être préparé avant d'être annoncé.

Ce qui aide, dans l'ordre :

1. la version (`openscale --version`, ou le nom de l'archive téléchargée) et la plateforme ;
2. la route ou la commande concernée, et ce qu'un attaquant obtient ;
3. **d'où il agit** — c'est la question qui décide de la gravité ici : depuis la boucle
   locale du poste, depuis le réseau de la coopérative, ou depuis Internet ;
4. de quoi reproduire, si c'est court. Un `curl` vaut mieux qu'une capture d'écran.

## Ce que vous pouvez attendre

Le projet est maintenu par une équipe bénévole d'une coopérative alimentaire, pas par une
entreprise avec une astreinte. Concrètement :

- **accusé de réception sous 7 jours**. Sans réponse au bout de deux semaines, relancez :
  ce n'est pas un silence, c'est une boîte que personne n'a ouverte ;
- un avis sur la gravité et une intention de correctif dans le mois ;
- **seule la dernière version publiée reçoit des correctifs.** Il n'y a pas de branche de
  maintenance : le parc tient sur quatre postes, et la mise à jour se fait depuis l'écran
  d'administration.

Nous n'avons **pas de programme de récompense** et pas de budget pour en avoir un. Votre
nom figurera dans les notes de version si vous le souhaitez.

## Le modèle de menace, et ce qui n'est pas une faille

Un poste OpenScale est une machine en libre-service dans un magasin, sur le réseau local
d'une coopérative, utilisée par des bénévoles et par des adhérents. Le service **n'écoute
que `127.0.0.1`** dans la configuration livrée. Plusieurs décisions découlent de ce
contexte, elles sont documentées, et **les signaler comme des failles nous fera vous
répondre en pointant cette section** :

- **L'accès physique au poste vaut l'accès administrateur.** Qui peut débrancher
  l'imprimante peut aussi arrêter le poste. C'est écrit dans `docs/02-architecture.md`
  §15.2, et c'est pourquoi le mot de passe d'ouverture de session Windows est en clair
  dans le registre — `deploy/windows/harden.ps1 -AutologonSecret` explique comment le
  déplacer vers les secrets LSA pour qui le veut.
- **Les gestes de dépannage ne demandent pas de mot de passe** (ADR-033) : tester la
  balance, tester l'imprimante, recharger le catalogue, changer le rouleau, produire
  `diagnostic.zip`. Le critère n'est pas la porte mais l'acte — *ce qui change ce que le
  poste vend, ou la façon dont il pèse* est protégé, le reste ne l'est pas. Un bénévole
  seul devant un poste muet doit pouvoir le diagnostiquer.
- **Le service HTTP est en clair.** Il n'écoute que la boucle locale ; un certificat
  auto-signé sur un kiosque coûterait un écran d'avertissement et une échéance à
  surveiller, pour rien. Si vous mettez `network.admin_on_lan` à vrai, le mot de passe
  d'administration voyage en clair sur le réseau local : c'est un réglage, pas la valeur
  livrée, et c'est à vous de décider.
- **Le symbole EAN-13 de l'étiquette est volontairement tronqué.** Ce n'est pas un défaut
  de rendu : un symbole conforme n'entre pas sur 40 × 25 mm avec les cinq champs texte, et
  la caisse lit le code depuis quinze ans. C'est un compromis assumé, pas un correctif en
  attente.
- **Les mises à jour sont vérifiées par condensat, pas par signature.** `SHA256SUMS-archives.txt`
  prouve que les octets sont arrivés entiers, jamais qu'ils sont les bons : la racine de
  confiance est l'accès en écriture à ce dépôt GitHub. Un système de signature demanderait
  à une équipe bénévole de gérer une clé privée pendant des années, et une clé perdue
  bloquerait toutes les mises à jour du parc — panne plus probable que la menace qu'elle
  écarte. C'est un arbitrage, il est ouvert à la discussion, mais il est délibéré.

**Est en revanche tout à fait dans le périmètre**, et nous intéresse : tout ce qui
s'exploite **sans être devant le poste** — depuis une page web ouverte sur la machine,
depuis le réseau de la coopérative, depuis un fichier CSV que le producteur dépose, depuis
une réponse du partage WebDAV, depuis une release GitHub. Une élévation de privilèges du
compte du kiosque vers `LocalSystem` nous intéresse aussi, même si elle suppose un clavier.

## Ce que le dépôt contient et ne contient pas

`testdata/catalog/flv.csv` et `flv_1.csv` sont des **exports Odoo authentiques** : 508
produits, leurs prix et 181 photos. Ils sont ici parce qu'ils font foi sur le format contre
toute documentation, et leur présence est une décision du commanditaire. Ils ne portent
aucune donnée personnelle : identifiant, nom, code-barres, prix, catégorie, unité, image.

Les coordonnées d'infrastructure — hôtes, comptes, adresses — ont été retirées et
remplacées par la réserve `example.org`. `docs/00-donnees-retirees.md` dit lesquelles, où
vivent les vraies, et comment le balayage a été fait. Aucune URL, aucune adresse n'est
compilée dans le binaire : les valeurs d'un site sont un fichier livré (ADR-026).

Le journal des pesées ne porte **aucun identifiant de client** — produit, poids, prix,
code-barres, palier tarifaire, et rien d'autre. C'est pourquoi son export ne demande pas de
mot de passe.
