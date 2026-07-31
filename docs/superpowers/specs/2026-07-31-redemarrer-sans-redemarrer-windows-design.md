# Redémarrer sans redémarrer Windows

> Conception validée le 31/07/2026. **Non implémentée à ce jour.** La référence tenue à
> jour reste `docs/02-architecture.md`.

## Le problème

Un poste installé est enfermé : sous `cage` l'unité du kiosque écrit qu'« il n'y a
littéralement rien vers quoi s'échapper », et sous Shell Launcher le kiosque **remplace**
l'explorateur Windows. Depuis cet écran, aucun geste de reprise n'existe.

Trois besoins, dans l'ordre où ils se présentent :

1. **Un `config.json` édité à la main n'est relu qu'au démarrage du service.** Le
   `_readme` du fichier le dit lui-même : « Édition manuelle : arrêtez le service, éditez,
   redémarrez. » Arrêter et redémarrer demande une console en administrateur, que le
   kiosque interdit.
2. **Aucun filet pour les cas que personne n'a prévus.** Quand quelque chose est figé, la
   seule sortie connue est le redémarrage de la machine.
3. **Redémarrer la machine elle-même n'est atteignable par aucun écran.**

Aujourd'hui la réponse aux trois est la même : couper le courant, ou trouver un clavier et
un compte administrateur.

## Ce que ce document ne remet pas en cause

**ADR-027 supprime `POST /admin/api/restart`, et cette décision tient.** Son objet est
nommé : « **aucun bloc de configuration** n'exige un redémarrage du processus ». Le
rechargement à chaud en trois temps couvre tous les blocs, `network.listen` compris ; aucun
réglage ne doit ramener un bandeau « redémarrez pour appliquer ». Rien ici ne le rouvre.

Ce que l'ADR ne traite pas, c'est le **dépannage** : un poste en vrac pour une raison qui
n'est pas un réglage. Ce document ajoute ce geste-là, et il le fait par le mécanisme
qu'ADR-027 désigne lui-même comme le seul légitime — « celui que le SCM ou systemd
déclenche tout seul ».

Un ADR neuf portera cette distinction, en citant ADR-027, pour qu'aucune relecture future
ne conclue à un oubli.

## Ce qui existe déjà, et qu'on ne réécrit pas

| Pièce | Ce qu'elle apporte |
|---|---|
| `internal/platform/service_windows.go:333` | `serviceHandler.Execute` rend **1** quand la station s'arrête d'elle-même : c'est ce qui déclenche les reprises `sc failure` (5 s, 10 s, 30 s) |
| `deploy/linux/openscale.service` | `Restart=always`, `RestartSec=2` : un arrêt propre suffit à relancer |
| `cmd/openscale/serve.go:519` | Le `select` d'où part l'arrêt ordonné de §13.4, déjà couvert par les tests d'arrêt |
| `internal/web/config.go:457` — `restoreConfig` | Lit un document **sur disque**, le valide, le recharge à chaud, arme le compte à rebours de 60 s |
| `internal/station/hub.go:265` — `UpdateGuard` | Refuse de couper le poste pendant une pesée ou une bascule de catalogue, avec sa phrase française |
| `cmd/openscale/service.go` | `install · uninstall · start · stop · status`, tous guardés |
| `internal/platform/console_windows.go` | Précédent d'appel d'API Windows en pur Go, avec ses tests |

Trois conséquences directes :

1. **Aucun script neuf n'est nécessaire.** La station peut demander son propre
   redémarrage en s'arrêtant proprement avec un code non nul ; le superviseur fait le
   reste. On n'écrit pas de second chemin d'arrêt, on n'ajoute pas de processus détaché.
2. **Le service a déjà les droits** dont il a besoin sous Windows : `LocalSystem`. Sous
   Linux, non — voir le § « Le point d'installation qui conditionne le geste 3 ».
3. **Le garde existe.** Il refuse déjà exactement les moments où couper le poste perdrait
   quelque chose.

## Les quatre gestes

| Geste | Route ou commande | Coupure | Annulable |
|---|---|---|---|
| 1. Relire le fichier de configuration | `POST /admin/api/config/reload` | aucune | 60 s, automatique |
| 2. Redémarrer l'application | `POST /admin/api/restart` | ~5 s (Windows), ~2 s (Linux) | non |
| 3. Redémarrer l'ordinateur | `POST` / `DELETE /admin/api/reboot` | ~1 minute | 30 s, par bouton |
| 4. `openscale service restart` | CLI | ~5 s | non |

Les trois routes sont **protégées par session administrateur**, comme
`POST /admin/api/config/restore` (`internal/web/server.go:514`). Elles ne vont **pas** sur
la page Dépannage, qui est ouverte sans mot de passe (ADR-018) : couper le service n'est
pas un geste de bénévole. Elles vivent sur la page **Poste**, sous une rubrique
« Maintenance », dans l'ordre de brutalité croissante du tableau ci-dessus.

### 1. Relire le fichier de configuration

Le chemin de `restoreConfig`, à une source près :

```
configStore.Read()  →  Validate  →  controller.Reload{Next: fichier}  →  compte à rebours 60 s
                          ↓
                       422, toutes les fautes d'un coup
```

**Pas de `Save`.** Le fichier est déjà le document voulu ; le réécrire par-dessus lui-même
ferait tourner les cinq sauvegardes pour rien et effacerait la plus ancienne.

**Sur retour arrière, le fichier n'est pas réécrit.** `ReloadRequest.FileBefore` reste
`nil` : la station revient à la configuration **en mémoire** et laisse le fichier tel quel.
`writeConfig` fait l'inverse, et c'est justifié là-bas — le document venait de l'écran, qui
en garde une copie. Ici il vient de la main de l'opérateur, et l'écraser détruirait un
travail dont il n'existe aucune copie. L'écran dit alors : la configuration précédente est
revenue en service, le fichier est resté tel quel, corrigez-le et recommencez.

Conséquence assumée : après un retour arrière, le fichier sur disque et la configuration en
service **divergent**. Un redémarrage ultérieur relirait le fichier fautif — et §11.3 a
déjà la réponse, le profil neutre, qui laisse le poste démarrer et afficher ses fautes.

**Le piège à couvrir par un test.** Un `config.json` reconstruit depuis un export ne porte
**ni** `admin.password_hash` **ni** `admin.recovery_code_hash` : `Config.Export` les
retire toujours. Le contrôle 31 (`internal/domain/config.go:1249`) accepte délibérément un
hash vide — c'est l'état d'un poste entre son installation et son premier accès. Relire un
tel fichier **effacerait le mot de passe en service**, et l'administration ne serait plus
atteignable que par le code de secours imprimé sur la fiche d'installation.

Ce n'est pas une faute de configuration, donc le contrôle 31 ne doit pas changer : c'est
une faute **de ce geste-ci**. La relecture refuse en **422** un fichier dont un hash est
vide alors que la configuration en service en porte un, avec la phrase qui nomme la cause
(« ce fichier vient d'un export, qui ne porte aucun secret ») et le remède
(`openscale config password`).

### 2. Redémarrer l'application

Un troisième cas dans le `select` de `serve.go:519` :

```go
case <-restartRequested:
    fatal = &serviceFailure{Code: codeRestartAsked, Exit: exitRestart, Message: "…"}
    recordFailure(db, clock, fatal)   // l'intention est écrite AVANT de partir
```

puis l'arrêt ordonné de §13.4, **inchangé**. Code de sortie non nul → le SCM applique ses
reprises, systemd son `Restart=always`.

Trois choses que l'écran doit dire honnêtement :

- **Hors service** — `openscale serve` lancé en console, poste de développement — personne
  ne relance. La route répond **501** avec sa phrase, comme la mise à jour hors Windows
  (`update.Status.Supported`). On ne tue pas un poste qui ne se relèvera pas.
  `platform.StartedByServiceManager()` répond déjà à la question sous Windows ; sous Linux
  c'est la présence de `INVOCATION_ID` dans l'environnement.
- **Le délai n'est pas promis.** Le compteur de `sc failure` est **partagé avec les vrais
  plantages** : un poste qui a planté deux fois le matin repartira en 30 s l'après-midi.
  L'écran écrit « le poste redémarre, l'écran revient tout seul », jamais un nombre de
  secondes. (Windows répète indéfiniment la dernière action de la liste : le poste ne
  reste pas mort, il attend 30 s.)
- **Le journal d'événements Windows dira « arrêt inattendu ».** C'est le prix du mécanisme
  retenu, et il est compensé : le journal technique porte l'intention, écrite avant le
  départ, avec son code `ERR-SYS-nn` propre.

### 3. Redémarrer l'ordinateur

**Le délai est applicatif, pas système.** La route répond **202** immédiatement, arme un
compte à rebours de 30 s sur `ports.Clock`, et redémarre à l'échéance. Un bouton
**Annuler** reste affiché pendant ces trente secondes.

```
POST   /admin/api/reboot   → 202 { "at": "…", "seconds_left": 30 }
DELETE /admin/api/reboot   → 200  tant que l'échéance n'est pas passée
```

Pourquoi pas `shutdown.exe /r /t 30`, qui offre pourtant `shutdown /a` : parce que
`systemctl reboot` est immédiat et n'a rien à annuler. Un délai côté OS donnerait **deux
comportements pour un bouton**, et surtout il ne se teste pas sans redémarrer une machine.
Sur horloge injectée, l'armement, l'annulation et l'échéance se prouvent tous sans
matériel.

Un second appel pendant le compte à rebours répond **409** — pas un second redémarrage.

`platform.Reboot()`, avec le découpage de `platform.ApplyUpdate` :

| Plateforme | Appel |
|---|---|
| Windows | `shutdown.exe /r /t 0`, le service tournant en `LocalSystem` |
| Linux | `systemctl reboot` |
| Autre | `ErrRebootUnsupported`, et la route répond 501 |

**Libellé : « Redémarrer l'ordinateur »**, jamais « redémarrer le poste ». « Poste » désigne
l'application dans tout le dépôt — « Le poste redémarre… » est la page de secours du
service (§14.4). Deux sens du même mot sur un même écran, devant un bénévole, est le défaut
d'usage qui avait déjà valu au mot « version » une page « Mise à jour » séparée de la page
Poste (ADR-040). À inscrire au glossaire.

#### Le point d'installation qui conditionne le geste 3

Sous Linux, le service tourne en `User=openscale`, shell `nologin`, **sans règle polkit** :
`systemctl reboot` sera **refusé**. `deploy/linux/install.sh` doit poser une règle sous
`/etc/polkit-1/rules.d/` autorisant `org.freedesktop.login1.reboot` pour ce seul
utilisateur — et rien d'autre.

Tant que la règle n'est pas posée, la route répond **501** avec la phrase qui dit quoi
faire, jamais un échec muet. `openscale doctor` gagne un contrôle : « le poste peut-il
redémarrer l'ordinateur ? »

Sous Windows, `LocalSystem` porte le privilège : rien à ajouter.

### 4. `openscale service restart`

`stop` puis `start`, tous deux déjà écrits. **L'erreur de `stop` est retournée sans tenter
le `start`** : un service qu'on n'a pas su arrêter ne se redémarre pas, et enchaîner
masquerait la vraie faute derrière un second message.

Hors Windows, `ErrServiceUnsupported` comme les cinq autres actions, avec la phrase qui
renvoie à `systemctl restart openscale`.

## Une amélioration ciblée, dans le code qu'on touche

`Hub.UpdateGuard` gardera trois actes au lieu d'un — la mise à jour, le redémarrage de
l'application, le redémarrage de l'ordinateur. Son nom devient faux le jour où le deuxième
appelant arrive.

Renommer : `UpdateGuard` → `DowntimeGuard`, `updateGuardFor` → `downtimeGuardFor`, et la
méthode de l'interface `update.Guard`, qui reste déclarée côté consommateur et garde sa
forme ; `update_guard_test.go` → `downtime_guard_test.go`. La règle ne bouge pas d'une
ligne : elle est déjà exactement la bonne, `OutOfService` et `Faulted` compris — un poste
qui ne peut pas servir est précisément celui qu'on a besoin de redémarrer.

## Ce que l'écran promet, et ce qu'il ne promet pas

| Il dit | Il ne dit pas |
|---|---|
| « La configuration du fichier est en service. Confirmez sous 60 s. » | qu'elle est confirmée |
| « Le fichier est resté tel quel. » (après retour arrière) | que le poste tourne dessus |
| « Le poste redémarre. L'écran revient tout seul. » | en combien de secondes |
| « L'ordinateur redémarre dans 30 secondes. » | qu'il reviendra — c'est un redémarrage machine |

## Surface ajoutée, et qui peut l'atteindre

Trois routes qui coupent le service, toutes derrière la session administrateur, dont une
qui redémarre la machine. Quiconque possède le mot de passe d'administration — ou le code
de secours de la fiche d'installation — peut mettre le poste hors service pendant une
minute. C'est déjà vrai de `POST /admin/api/update/apply`, qui installe et exécute un
binaire ; la surface ajoutée ici est de nature moindre.

Ce qui la borne : le garde refuse pendant une pesée, le redémarrage machine s'annule
pendant 30 s, et chacun des trois actes s'écrit au journal technique **avant** son effet.

## Hors périmètre, et dit

- **Le kiosque (navigateur) n'est pas redémarré.** Il se reconnecte seul en SSE et son
  superviseur le relance en moins de 2 s. Surtout : un écran tactile figé ne peut pas
  recevoir le clic qui le réparerait. Le bouton n'aiderait que depuis un autre poste du
  réseau — cas réel, rare, à traiter le jour où il se présente.
- **Pas d'arrêt de l'ordinateur**, seulement un redémarrage. Un poste éteint à distance ne
  se rallume pas à distance.
- **Pas de redémarrage programmé** ni périodique.

## Tests, tous sans matériel

| Ce qui est prouvé | Comment |
|---|---|
| Relire un fichier valide le met en service et arme les 60 s | `internal/web`, banc HTTP existant |
| Relire un fichier fautif répond 422 avec **toutes** les fautes | idem |
| Relire un fichier sans hash, poste qui en porte un → 422 | le piège de l'export |
| Le retour arrière ne réécrit pas le fichier | comparaison d'octets avant / après |
| Le redémarrage passe par l'arrêt ordonné et rend un code non nul | `cmd/openscale`, `serve` avec un canal injecté |
| Hors superviseur, la route répond 501 sans rien arrêter | idem |
| Le compte à rebours du redémarrage machine s'arme, s'annule, échoit | horloge injectée + `Rebooter` de test |
| Un second appel pendant le compte à rebours répond 409 | idem |
| `service restart` n'appelle pas `start` si `stop` a échoué | `cmd/openscale`, gestionnaire de service de test |
| Le garde refuse les trois actes en pleine pesée | table d'états existante, renommée |

## Points à trancher pendant l'implémentation

1. **Où vit le compte à rebours du redémarrage machine.** Un objet dans `internal/web`
   suffit ; il ne mérite un paquet que s'il gagne un second appelant.
2. **Le code `ERR-SYS-nn` du redémarrage demandé.** `ERR-SYS-01` à `03` sont pris ;
   `ERR-SYS-08` est le redémarrage sans intervention non configuré. Prendre le prochain
   libre et l'inscrire au glossaire.
3. **La détection « je tourne sous systemd »** : `INVOCATION_ID` est le signal documenté,
   à confirmer sur le poste Debian.
