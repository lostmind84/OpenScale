# Régler le catalogue de votre coopérative

Chaque poste attend un fichier CSV de produits, le lit, puis l'applique. **Par défaut**
il cherche `flv_<n>.csv`, où `<n>` est le numéro du poste, et le supprime une fois lu :
c'est ce que fait La Coope, dont l'Odoo écrit un fichier par poste. Si votre serveur
publie un autre nom, ou un seul fichier pour tous vos postes, deux réglages suffisent. Ils
se trouvent dans l'administration, page **Catalogue**, sous la source choisie
(`local_drop` ou `webdav`, les deux les portent).

## Les deux réglages

| Réglage | Ce qu'il dit | Défaut |
|---|---|---|
| **Nom du fichier** (`file_name`) | Le nom que le poste attend. `{n}` y est remplacé par le numéro du poste. | `flv_{n}.csv` |
| **Après lecture** (`after_read`) | `delete` : le poste supprime le fichier une fois lu, et c'est cette suppression qui dit au producteur que la livraison est prise. `keep` : le poste ne supprime jamais rien. | `delete` |

Quelques exemples de **Nom du fichier** :

- `flv_{n}.csv` : un fichier par poste, `flv_1.csv` pour le poste 1, `flv_2.csv` pour le
  poste 2. C'est le défaut ; ne rien écrire revient au même.
- `flv.csv` : **un seul** fichier, que tous les postes lisent.
- `{n}-produits.csv` : un fichier par poste, sous un autre nom.

C'est un **nom de fichier**, jamais un chemin : un `/`, un `\`, un `:` ou un `..` est refusé
à l'enregistrement. Le dossier se règle ailleurs, dans l'adresse (WebDAV) ou dans le
répertoire (`local_drop`). Une **espace au début ou à la fin** du nom est refusée aussi : elle
arrive avec un copier-coller, ne se voit sur aucun écran, et le poste attendrait un fichier
qui la porte.

L'écran montre, avant l'enregistrement, le nom que le poste va chercher et, pour WebDAV,
l'adresse complète. **Lisez cette ligne** : c'est elle que le poste utilisera.

## Quand choisir `keep`

Choisissez **« Laisser le fichier en place »** quand plusieurs postes lisent **le même
fichier**, ou quand le serveur ne vous laisse pas supprimer ce qu'il publie. Sans `keep`,
un fichier partagé disparaît dès que le premier poste l'a lu, et les autres l'attendent
pour rien.

Ce que fait le poste en `keep` :

- il **ne supprime jamais** le fichier, qu'il ait été appliqué ou refusé ;
- il retient, dans `<data>/catalog/watermark.json`, de quoi reconnaître le dernier fichier
  qu'il a pris (sa taille et sa date). Un fichier qui n'a pas changé n'est pas relu, même
  après un redémarrage ; un fichier qui change est relu ;
- il oublie ce repère dès qu'il redémarre en `delete`, pour ne pas ignorer un fichier
  nouveau le jour où vous reviendrez à `keep` ;
- il garde, comme avant, une copie de chaque fichier appliqué dans ses archives.

Pour partager un fichier entre postes, `keep` et un nom sans `{n}` suffisent : il n'y a pas
d'autre réglage.

## Les trois symptômes

### Le tableau de bord écrit « en attente de `<nom>` »

Le fichier n'est pas là où le poste le cherche. Le nom écrit est celui que le poste attend,
le plus souvent pas celui que votre serveur publie. Comparez-le au nom réel, et corrigez
le **Nom du fichier** ou ce que le serveur publie. Le poste le dit aussi une fois dans le
journal quand le fichier disparaît, et une fois quand il revient ; avec `keep`, cette
disparition est un avertissement, parce que ce n'est pas le poste qui a retiré le fichier.

L'écran client, la page Dépannage et la zone de dépôt de la page Catalogue donnent le même
nom : le service l'envoie, aucun écran ne le recompose.

### L'enregistrement refuse l'adresse parce qu'elle finit par `.csv`

Contrôle 52. L'adresse WebDAV désigne le **dossier**, et le poste y ajoute lui-même le nom
du fichier. Une adresse `https://serveur/dossier/flv.csv` lui ferait demander
`…/flv.csv/flv_1.csv`, un chemin qui n'existe pas. Retirez le nom de l'adresse et mettez-le
dans **Nom du fichier**.

### Une note dit que le fichier sera supprimé par le premier poste qui le lit

Vous avez écrit un nom **sans `{n}`** avec **« Supprimer le fichier après lecture »** :
tous les postes lisent le même fichier, et le premier à le lire le supprime. Ce n'est pas
un refus, la note ne bloque rien. Ou bien un seul poste lit ce fichier, et vous pouvez
ignorer la note ; ou bien plusieurs le lisent, et il faut passer à **« Laisser le fichier
en place »**.

## Un serveur WebDAV qui ne donne pas de date

Certains serveurs n'envoient pas la date de modification du fichier (l'en-tête
`getlastmodified`). Sans date, le poste ne peut pas savoir qu'un fichier a changé, et la
taille seule ne suffit pas à le dire. En `keep`, il **relit donc le fichier à chaque
scrutation**, toutes les quelques secondes. C'est sans conséquence sur le catalogue : un
contenu identique à celui déjà appliqué ne change rien. Un fichier refusé n'est pas compté
une deuxième fois en quarantaine tant que son contenu ne change pas.

Si cette relecture permanente vous gêne, c'est le serveur qu'il faut régler pour qu'il
envoie la date.

## Pour aller plus loin

- La décision et ses conséquences : ADR-061, dans le registre des décisions de
  [`docs/02-architecture.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/02-architecture.md).
- Le déroulé complet d'une scrutation, du repère `keep` et des contrôles 51 et 52 :
  [`docs/02-architecture.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/02-architecture.md),
  § 10.1 et § 11.3.
- Brancher une autre origine de produits (API d'ERP, autre format) :
  [`docs/08-ajouter-une-source-de-catalogue.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/08-ajouter-une-source-de-catalogue.md).
