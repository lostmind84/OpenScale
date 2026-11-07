# Diagnostiquer l'imprimante pas à pas

Page **Matériel**, bloc **Imprimante** : un diagnostic en cinq étapes, lancé **sans mot
de passe** — si l'imprimante n'imprime plus, un bénévole doit pouvoir le lancer seul,
avant même d'appeler quelqu'un. Chaque étape qui échoue affiche une phrase et un renvoi
vers la section de cette page qui lui correspond. La dernière étape **pose une
question** au bénévole : rien dans ce poste ne peut mesurer si une étiquette est
réellement sortie du rouleau. Le bouton **« Copier le rapport (pour le forum) »**
construit un texte collable à partir des cinq réponses, qu'il manque une réponse à la
cinquième ou non.

!!! info "Cette page se complète"

    Le squelette ci-dessous porte les cinq ancres que le diagnostic pointe
    (`#etape-1` à `#etape-5`). Le contenu détaillé de chaque étape — ce qu'elle observe
    précisément, et quoi faire de chaque réponse — arrive avec l'écran lui-même.

## 1. Le périphérique existe {#etape-1}

Le poste vérifie que le nœud, la file d'impression, l'adresse réseau ou le dossier
déclaré dans la configuration existe réellement. Un échec ici veut dire : le câble
n'est pas branché, la file Windows a été renommée ou supprimée, ou le dossier n'a pas
été créé.

## 2. Le poste a le droit d'y écrire {#etape-2}

Sur un nœud Linux (`/dev/usb/lp0` ou le nom stable posé par udev), le poste doit avoir
le droit d'écrire dessus — le compte du service appartient au **groupe lp**. Cette
étape ne s'applique pas à une file Windows, à une adresse réseau ni à un dossier : sans
objet n'y est pas un défaut.

## 3. L'imprimante répond quand on lui demande son état {#etape-3}

Le poste envoie la sonde de statut native (ENQ) et attend une réponse pendant 500 ms.
**Le silence n'est pas un échec** : une imprimante SATO au repos ne dit souvent rien de
sa santé, et ce silence n'empêche pas d'imprimer. Cette étape est classée *inconnu*
plutôt qu'*en échec* précisément pour cette raison.

## 4. Une étiquette de test est envoyée en entier {#etape-4}

Le poste imprime l'auto-test `label` — une étiquette de démonstration complète — et
compte les octets réellement remis au transport. Un échec ici est le même, techniquement,
qu'un échec à l'impression d'une étiquette de vente.

## 5. L'étiquette est-elle sortie ? {#etape-5}

**Cette étape est une question, pas une mesure.** Le bénévole regarde le bac de
l'imprimante et répond : l'étiquette est-elle sortie ? Si non, ce qu'il a vu (un voyant
qui clignote, un bip, rien du tout) est consigné tel qu'il l'a écrit.
