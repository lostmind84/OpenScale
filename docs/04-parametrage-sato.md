# Paramétrage de la SATO WS408

> Relevé des réglages de production, extrait des copies d'écran du document
> « Procédure configuration Imprimante Sato WS408 » de Cooperatic (installation du
> pilote et préférences d'impression). Le document lui-même n'est pas versionné : il
> pèse 27 Mo de captures, et tout ce qu'il porte de décisif est ici.
>
> **Ces valeurs font autorité sur la géométrie de l'étiquette** (ADR-003, amendé le
> 29/07/2026) : elles sont ce que l'imprimante a réellement en mémoire, et elles
> gouvernent le tirage même quand on lui parle en RAW.

## Pourquoi ces réglages survivent au RAW

Le pilote SATO ne se contente pas de rendre : l'onglet « Options d'impression » se
termine par un bouton **« envoyer ces informations à l'imprimante »**, qui **écrit les
réglages dans l'appareil**. Ils restent donc actifs lorsque OpenScale court-circuite le
rendu du pilote pour envoyer son propre bitmap en RAW (ADR-002, ADR-007).

C'est ce qui rend le tableau ci-dessous utilisable : on ne le lit pas pour reproduire
une configuration, on le lit pour savoir **dans quoi on imprime**.

## Support — onglet « Début de page »

| Réglage | Valeur |
|---|---|
| Format | Personnalisé |
| Exemplaires | 1 |
| **Largeur** | **35 mm** |
| **Hauteur** | **25 mm** |
| **Type de support** | **Étiquettes avec espaces** (capteur de gap) |
| Rotation | 0° — Portrait |

**Le support physique fait 38 × 25 mm**, mesuré au pied à coulisse sur le rouleau du
banc. Les 3 mm d'écart en largeur sont une **marge volontaire** : la zone imprimable
est délibérément plus étroite que l'étiquette.

À 8 dots/mm, ces 35 × 25 mm valent **280 × 200 dots** — les deux nombres que
`internal/domain/templates.go` déclare désormais, et que `<ESC>A1` transporte.

Le coin des étiquettes est **arrondi, environ 1 mm de rayon**. Il n'y a pas de papier
dans l'angle : c'est ce qui rendait la mire d'alignement invisible avant qu'on ne rentre
ses croix (`internal/printing/raster/selftest.go`).

## Impression — onglet « Options d'impression »

| Réglage | Valeur | Commande SBPL correspondante |
|---|---|---|
| **Vitesse** | **127 mm/s** = 5 ips | `<ESC>CS5` |
| **Contraste** | **4** | `<ESC>#E4` |
| **Gamme de contraste** | **B** | `<ESC>#E4B` |
| **Mode d'impression** | **Thermique direct** | — (aucun ruban) |
| Décalage en haut | 0 mm | `<ESC>A3V+0000` |
| Décalage à gauche | 0 mm | `<ESC>A3H+0000` |
| Numéro du format | 1 | — |

**La configuration livrée portait `darkness: 3` et `speed: 4`**, sans rapport avec la
production. Elle est alignée sur 4 et 5 depuis le banc : sans quoi les quatre postes
auraient imprimé plus clair et plus lent que ce que fait la SATO aujourd'hui, ce
qu'ADR-003 interdit.

**Thermique direct** : aucun ruban à approvisionner, le rouleau suffit.

## Protocole

La case **« Sortie non-standard protocole » est décochée**, donc l'imprimante tourne en
**protocole standard** : chaque travail doit être encadré par `STX` … `ETX`. Un travail
sans `ETX` n'est jamais considéré comme terminé — les octets restent dans le tampon,
l'appareil se bloque et il faut le redémarrer. C'est le premier des trois défauts que le
banc a levés (`internal/printing/sbpl/sbpl.go`).

Le protocole standard est **bidirectionnel** : une requête `ENQ` (0x05) reçoit en retour
une trame `STX <identifiant 2 octets> <code d'état 1 octet> <compteur 6 chiffres> ETX`.
Mesurée sur l'imprimante du banc, au repos et en bon état :

```
02  20 20  41  30 30 30 30 30 30  03
STX  « ␠␠ »  « A »   « 000000 »   ETX
```

Le manuel SBPL confirme que l'identifiant vaut deux espaces quand aucun travail n'est en
cours. **La table complète des codes d'état n'est pas dans le manuel SBPL** — il la
renvoie au document « Interface: High Speed RS-232C », que nous n'avons pas.

## Installation du pilote — ce qui reste à savoir

- Le pilote s'installe par `PrnInst.exe`, et **la version du site n'est pas compatible**
  avec les balances : Cooperatic distribue une version épinglée sur son nuage.
  Sans effet sur OpenScale, qui ne passe pas par le rendu du pilote.
- L'imprimante reçoit une **adresse IP fixée au routeur**, et une **étiquette collée sur
  l'appareil** porte cette adresse et son nom (`Sato_1`, `Sato_2`…). C'est la procédure
  que §8.4 décrit, et elle est appliquée dans le parc.
- Le port Windows créé est un **`SATOV6 Advanced Port Monitor`**, pas un port TCP/IP
  standard. Il transporte correctement les travaux RAW d'OpenScale, vérifié au banc.
- Pour un modèle **SATO Cutter**, un réglage supplémentaire : mode d'opération
  « Massicot ». Hors périmètre — `SATO WS408_CUTTER` n'est plus piloté (§19).

## Un piège relevé au banc

**Cette imprimante n'accepte qu'une seule connexion TCP à la fois**, et un travail mal
formé la laisse injoignable jusqu'à un redémarrage. Pendant ce temps le spouleur boucle
en `SynSent` et **tous** les travaux échouent — y compris ceux qui sont corrects.

Conséquence pratique de diagnostic : un « rien ne sort » peut vouloir dire « les octets
sont faux » **ou** « l'imprimante ne répond plus depuis le travail précédent ». Les
distinguer se fait en regardant l'état de la connexion, pas la file d'attente.
