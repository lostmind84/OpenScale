# Démarrage rapide — un poste complet sans balance ni imprimante

Quatre commandes suffisent à faire tourner un poste sur une machine de développement.
C'est le chemin le plus court pour voir ce que fait ce dépôt. **Les deux colonnes font la
même chose** — prenez celle de votre système.

## 1. Construire et lancer le poste

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
make build

./bin/openscale serve \
  --config testdata/config-demo.json \
  --data /tmp/openscale-demo
```

</td><td>

```powershell
pwsh -File ./make.ps1 build

.\bin\openscale.exe serve `
  --config testdata\config-demo.json `
  --data $env:TEMP\openscale-demo
```

</td></tr></table>

Ouvrez <http://127.0.0.1:8085> : c'est l'écran client, avec sa grille vide. **Laissez-le
tourner** et ouvrez un second terminal pour la suite.

## 2. Déposer le catalogue de démonstration

60 produits tirés d'un vrai export, une photo sur deux :

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
cp testdata/catalog/flv_demo.csv \
  /tmp/openscale-demo/catalog/incoming/flv_2.csv
```

</td><td>

```powershell
Copy-Item testdata\catalog\flv_demo.csv `
  $env:TEMP\openscale-demo\catalog\incoming\flv_2.csv
```

</td></tr></table>

La grille se remplit en quelques secondes, et le fichier disparaît : **sa suppression est
l'acquittement** (§10.1). Le nom compte — `flv_2.csv` — parce que c'est le poste n° 2 que
`config-demo.json` déclare, et chaque poste ne lit que le fichier qui porte son numéro.

## 3. Peser, sans balance

Le poste n'en a pas, donc il est en saisie manuelle et le dit à l'écran.

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
curl -X POST http://127.0.0.1:8085/api/v1/weigh \
  -H "Content-Type: application/json" \
  -d '{"product_id":"894","manual_weight_g":1236,"key":"essai-1"}'
```

</td><td>

```powershell
$corps = @{
  product_id      = "894"
  manual_weight_g = 1236
  key             = "essai-1"
} | ConvertTo-Json

Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:8085/api/v1/weigh `
  -ContentType "application/json" -Body $corps
```

</td></tr></table>

L'étiquette est écrite dans le sous-répertoire `labels/` des données : la trame qu'une
vraie imprimante recevrait, octet pour octet. C'est le transport `file` de §8.4, qui
existe pour exactement cet usage — la commande ci-dessous en donne la taille.

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
ls -l /tmp/openscale-demo/labels/
```

</td><td>

```powershell
Get-ChildItem $env:TEMP\openscale-demo\labels\
```

</td></tr></table>

**`config-demo.json` diffère de la configuration de production sur trois points et
trois seulement** : `scale.present` est `false` (pas de balance), le transport de
l'imprimante est `file` au lieu de la file Windows, et la source du catalogue est le
dépôt local au lieu de WebDAV. Tout le reste — tarifs, garde-fous, gabarit d'étiquette —
est celui de la coopérative.

## Voir l'étiquette sans rien lancer

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
./bin/openscale label \
  --template weighing_identical \
  --demo --dual --pdf etiquette.pdf
```

</td><td>

```powershell
.\bin\openscale.exe label `
  --template weighing_identical `
  --demo --dual --pdf etiquette.pdf
```

</td></tr></table>

Un PDF **à imprimer à 100 %** et mesurable au réglet.

## Le diagnostic, qui fonctionne même quand rien ne démarre

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
./bin/openscale doctor \
  --config testdata/config-demo.json \
  --data /tmp/openscale-demo
```

</td><td>

```powershell
.\bin\openscale.exe doctor `
  --config testdata\config-demo.json `
  --data $env:TEMP\openscale-demo
```

</td></tr></table>

Quinze contrôles qui disent chacun ce qui a été vérifié, le verdict, et **ce qu'il faut
faire** si c'est rouge.

Sur une machine de développement, `doctor` conclut « ce poste ne peut pas fonctionner en
l'état » — et il a raison de le dire, mais pas de vous inquiéter. Ses reproches portent
sur l'installation d'un poste de **production** : le service n'est pas enregistré, la
tâche du kiosque n'existe pas, le redémarrage sans intervention n'est pas configuré, la
suspension USB sélective est active. Aucun des quatre n'empêche `serve` de tourner comme
ci-dessus. Ils comptent le jour où le poste doit revenir seul après une coupure de
courant.

## À quoi sert `openscale`, le binaire

Un seul exécutable porte tout : le service, l'écran client, l'administration, les outils
de diagnostic et les commandes de mise au point. `openscale --help` les liste. Les plus
utiles :

| Commande | Ce qu'elle fait |
|---|---|
| `serve` | **Lance le poste.** C'est ce que démarre le service Windows ou l'unité systemd |
| `kiosk` | Ouvre l'écran client en plein écran et le relance s'il se ferme |
| `service install` | Enregistre le poste comme service Windows |
| `doctor` | Les quinze contrôles ; `--zip` produit le fichier à envoyer au support |
| `config validate` | Liste **toutes** les fautes d'un fichier de configuration, en français |
| `config export` | La configuration à cloner vers les autres postes, sans le bloc matériel |
| `config fingerprint` | L'empreinte de 8 caractères à comparer entre postes |
| `label` | Sort une étiquette en PDF ou PNG, sans imprimante |
| `capture` | Enregistre ce que dit la balance, et mesure sa cadence réelle |
| `replay` | Rejoue un fichier de trames : poids, figeage, cadence médiane |
| `barcode` · `price` | Le code-barres et les prix d'une pesée, depuis un terminal |

`make build` produit ce binaire dans `bin/`. **Il suffit pour tout essayer, mais ce n'est
pas ce qu'on installe sur un poste** : l'installation a besoin des scripts et des
documents qui l'accompagnent — voir [`06-developpement.md`](06-developpement.md).
