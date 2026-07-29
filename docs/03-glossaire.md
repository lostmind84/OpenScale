# 03 — Glossaire de traduction

Le code d'OpenScale est écrit **en anglais** — paquets, types, fonctions, champs, tables SQL,
clés de configuration, routes HTTP — tandis que la documentation, les messages affichés à
l'écran et les libellés imprimés restent **en français**. Ce document est le pont entre les
deux : il fixe, pour chaque terme du cahier des charges, l'identifiant anglais exact qui devra
apparaître dans le code.

Ce glossaire est **l'autorité unique sur le nommage**. Il ne s'agit pas d'une suggestion ni
d'un point de départ : tout écart introduit une divergence entre la spécification française et
l'implémentation anglaise, et cette divergence coûte plus cher à chaque itération. Il doit donc
être respecté **à la lettre pendant toute l'implémentation**, jusqu'aux tests, aux scripts de
déploiement et aux noms de fichiers.

Les justifications ne sont pas décoratives : elles documentent les alternatives écartées, pour
éviter que la même question soit rouverte à chaque revue. La section
[Décisions sur les termes délicats](#décisions-sur-les-termes-délicats) traite les cas où un
mot français recouvre plusieurs concepts (ou l'inverse) ; la section
[Conventions de nommage](#conventions-de-nommage) résume les règles générales à appliquer.

> **Règle de complétude.** Si un identifiant n'est pas dans ce glossaire : le traduire selon
> les mêmes conventions, puis **le signaler** dans le compte rendu de l'étape pour qu'il soit
> ajouté ici. Le glossaire doit rester exhaustif.

---

## Paquets et répertoires

| Actuel (FR) | Cible (EN) | Justification |
| --- | --- | --- |
| `balance/` (racine du dépôt, nom du binaire) | `OpenScale/` (dépôt), `openscale` (binaire, module Go, `openscale serve`) | Le produit a été renommé : il ne s'appelle plus « Balance ». Le nom propre du produit est désormais `openscale`, et il ne reste PLUS AUCUN identifiant nommé `balance`. Le mot ne survit que pour l'APPAREIL, et seulement en français à l'écran (« La balance ne répond plus. ») ou dans un nom d'objet système qui désigne l'appareil (`/dev/balance-serial`). |
| `cmd/openscale/` | `cmd/openscale/` | Chemin du binaire, inchangé (nom de produit). |
| `cmd/openscale/drivers.go` | `cmd/openscale/drivers.go` | Déjà anglais. |
| `internal/noyau/` | `internal/domain/` | « noyau » = noyau MÉTIER pur. `core` est vague et surchargé, `kernel` évoque un OS. `domain` est le terme consacré (DDD) et lit bien : `domain.Label`, `domain.Product`, `domain.Transition`. Go idiomatique : paquet court, minuscules, singulier. |
| `internal/noyau/quantite.go` | `internal/domain/quantity.go` | Types atomiques Cents/Grams/Micrometers + RoundingPolicy. |
| `internal/noyau/texte.go` | `internal/domain/text.go` | Normalisation de texte. |
| `internal/noyau/ean13.go` | `internal/domain/ean13.go` | Acronyme, inchangé. |
| `internal/noyau/tarif.go` | `internal/domain/pricing.go` | Le fichier porte la règle de prix entière (tiers, règles, calcul) : `pricing.go` est plus juste que `tariff.go`. |
| `internal/noyau/produit.go` | `internal/domain/product.go` | Direct. |
| `internal/noyau/garde.go` | `internal/domain/safeguard.go` | « garde-fou » métier = safeguard. Réservé : `guard` désigne les gardes de transition de la machine à états (§6.6), concept différent. |
| `internal/noyau/mesure.go` | `internal/domain/measurement.go` | Contient Measurement, Stability, WeightLatch, RateMeter. |
| `internal/noyau/machine.go` | `internal/domain/machine.go` | Déjà anglais (state machine). |
| `internal/noyau/preparer.go` | `internal/domain/prepare.go` | Fichier nommé d'après la fonction `Prepare`. |
| `internal/noyau/gabarit.go` | `internal/domain/template.go` | Gabarit d'étiquette = template de mise en page. Voir piège : le « gabarit » d'un EAN-13 est un `pattern`, pas un `template`. |
| `internal/noyau/config.go` | `internal/domain/config.go` | Déjà anglais. |
| `internal/noyau/profils.go` | `internal/domain/profiles.go` | Direct. |
| `internal/noyau/trame/` | `internal/domain/frame/` | « trame » série = frame. Paquet court, minuscule, singulier. |
| `internal/balance/` | `internal/scale/` | Ici « balance » désigne l'APPAREIL : `scale` en anglais naturel. Lève l'homonymie avec le nom du produit. |
| `internal/balance/serie/` | `internal/scale/serial/` | Direct. |
| `internal/balance/serie/boucle.go` | `internal/scale/serial/loop.go` | Fichier nommé d'après `Loop`. |
| `internal/balance/gramxfoc/` | `internal/scale/gramxfoc/` | Nom de modèle matériel, inchangé. |
| `internal/balance/absent/` | `internal/scale/absent/` | « absent » est un mot anglais et décrit exactement la source de poids vide. |
| `internal/balance/rejeu/` | `internal/scale/replay/` | Direct. |
| `internal/balance/conformite/` | `internal/scale/conformance/` | Suite de conformité imposée à tout driver. |
| `internal/impression/` | `internal/printing/` | `printing` (l'activité) plutôt que `print` (verbe) ou `printer` (l'appareil) : le paquet porte le rendu ET les drivers. |
| `internal/impression/rendu.go` | `internal/printing/render.go` | Direct. |
| `internal/impression/symbole.go` | `internal/printing/symbol.go` | Direct (symbole EAN-13). |
| `internal/impression/raster/` | `internal/printing/raster/` | Déjà anglais. |
| `internal/impression/sbpl/` | `internal/printing/sbpl/` | Acronyme constructeur, inchangé. |
| `internal/impression/transport/` | `internal/printing/transport/` | Déjà anglais. |
| `internal/impression/apercu/` | `internal/printing/preview/` | « aperçu » PDF/PNG = preview. Cohérent avec la valeur de configuration `printer.type = "preview"`. |
| `internal/catalogue/` | `internal/catalog/` | Orthographe US, cohérente avec le reste du code. |
| `internal/catalogue/depot_local/` | `internal/catalog/localdrop/` | Répertoire de dépôt local. Go : pas d'underscore dans un nom de paquet. La valeur de configuration correspondante est `local_drop` (snake_case). |
| `internal/catalogue/webdav/` | `internal/catalog/webdav/` | Protocole, inchangé. |
| `internal/catalogue/csvodoo/` | `internal/catalog/csvodoo/` | Déjà neutre (format d'échange Odoo). |
| `internal/stockage/` | `internal/store/` | Go idiomatique : `store` plutôt que `storage` ou `persistence` pour une couche de dépôts SQLite. |
| `internal/poste/` | `internal/station/` | « poste de pesée » = weighing station. `terminal` évoque un TTY, `till` une caisse enregistreuse (faux sens : ce poste n'encaisse pas). |
| `internal/poste/hub.go` | `internal/station/hub.go` | Déjà anglais. |
| `internal/poste/ports/` | `internal/station/ports/` | Déjà anglais ; interfaces déclarées côté consommateur. |
| `internal/web/` | `internal/web/` | Déjà anglais. |
| `internal/web/flux.go` | `internal/web/stream.go` | Flux SSE = stream. Cohérent avec la route `/api/v1/stream`. |
| `internal/kiosk/` | `internal/kiosk/` | Déjà anglais. |
| `internal/plateforme/` | `internal/platform/` | Direct. |
| `internal/plateforme/horloge.go` | `internal/platform/clock.go` | Seule implémentation réelle de `Clock`. |
| `internal/diag/` | `internal/diag/` | Abréviation identique dans les deux langues. |
| — | `internal/update/` | Paquet neuf (ADR-040) : lire les versions publiées, télécharger, vérifier, préparer la bascule. Ne s'appelle pas `updater` — c'est le paquet qui met à jour, pas l'objet. |
| `internal/obs/` | `internal/obs/` | Observabilité / observability : identique. |
| `internal/assets/` | `internal/assets/` | Déjà anglais. |
| `internal/web/dist` | `internal/web/dist` | Sortie Vite committée, inchangée. |
| `web/` (front Svelte + TS) | `web/` | Déjà anglais. |
| `web/testdata/normalisation.json` | `web/testdata/normalization.json` | Fixture partagée Go + Vitest : le nom doit être identique des deux côtés. |
| `internal/balance/testdata/trames/` | `internal/scale/testdata/frames/` | Corpus vivant de trames. |
| `testdata/` · `deploy/{windows,linux}/` · `migrations/` | identiques | Déjà anglais. |
| `tools/frontiere/verifier.sh` | `tools/boundary/check.sh` | « les cinq coupes » = frontières architecturales = boundaries. |
| cible Makefile `frontiere` | cible Makefile `boundary` | `make boundary` ; cohérent avec `tools/boundary/`. |
| `tools/dependances/` | `tools/deps/` | Compare `go.mod` aux deux tables d'inventaire (§17.1, `THIRD-PARTY.md`), dans les deux sens. `deps` est l'abréviation qu'emploie déjà `go mod`, et elle nomme le répertoire comme la cible. |
| cible Makefile `dependances` | cible Makefile `deps` | `make deps` ; cohérent avec `tools/deps/`, comme `make boundary` l'est avec `tools/boundary/` (ADR-039). |
| `<donnees>/` | `<data>/` | Répertoire de données ; flag `--data`, variable `OPENSCALE_DATA`. |
| `<donnees>/catalogue/entrant/` | `<data>/catalog/incoming/` | Répertoire surveillé du dépôt local. |
| `<donnees>/catalogue/{archives,rejets}/` | `<data>/catalog/{archives,rejected}/` | « rejets » = fichiers rejetés. |
| `<donnees>/etiquettes/` | `<data>/labels/` | 30 dernières étiquettes capturées. |
| `<donnees>/images_produits/` | `<data>/product_images/` | Source `image_directory` (héritage). |
| `<donnees>/logs/balance.log` | `<data>/logs/openscale.log` | Le journal texte porte le nom du PRODUIT, et le produit s'appelle `openscale`. |
| `config.json.invalide-<horodate>` | `config.json.invalid-<timestamp>` | Copie du fichier fautif. |
| `balance.db` | `openscale.db` | Nom du produit. Tranché **par le code** : `internal/store` n'écrit jamais `balance.db` et ses tests ouvrent `openscale.db`. |
| `balance.db.avant-vN-<horodate>` | `openscale.db.before-vN-<timestamp>` | Sauvegarde `VACUUM INTO` pré-migration. Le suffixe est construit par `(DB).migrate` à partir du chemin reçu ; seule la base change de nom. |
| `<binaire>.precedent` | `<binary>.previous` | Sauvegarde de mise à jour. |
| `installer.ps1` · `desinstaller.ps1` · `durcissement.ps1` · `mettre-a-jour.ps1` · `demarrer.bat` | `install.ps1` · `uninstall.ps1` · `harden.ps1` · `update.ps1` · `start.bat` | Scripts = code livré, donc anglais. Leur CONTENU affiché à l'écran reste français. |
| `restauration.json` | `restore.json` | Sauvegarde des réglages écrasés par l'installeur. |
| `fiche-installation.txt` | `install-sheet.txt` | Nom de fichier = code ; le contenu imprimé reste français. |
| `DEPANNAGE.md` | `TROUBLESHOOTING.md` | Nom de fichier anglais, contenu français (E2). |
| `balance-kiosk.xml` (tâche planifiée) | `openscale-kiosk.xml` | Nom du produit. La tâche elle-même devient `OpenScale-Kiosk`. |
| `INSTALLATION.md` · `SHA256SUMS` · `config-lacagette.json` · `flv_demo.csv` · `flv_<n>.csv` | identiques | INSTALLATION est identique dans les deux langues ; `lacagette` et `flv` sont des noms propres / un format d'échange externe imposé. |
| `<rejet>.motif.txt` | `<rejected>.reason.txt` | Cohérent avec le champ `reason`. |
| `trames.txt` (capture) | `frames.txt` | Cohérent avec `frame`. |
| `index.html` · `admin.html` | `index.html` · `admin.html` | Déjà anglais. |

---

## Types

| Actuel (FR) | Cible (EN) |
| --- | --- |
| `Centimes` | `Cents` |
| `Grammes` | `Grams` |
| `Micrometres` | `Micrometers` |
| `Duree` (durée JSON en ms) | `Duration` (`domain.Duration`) |
| `PolitiqueArrondi` | `RoundingPolicy` |
| `ArrondiCommercial` | `RoundHalfUp` |
| `ArrondiTronque` | `RoundTowardZero` |
| `ArrondiPair` | `RoundHalfToEven` |
| `Kilogramme` (const) | `Kilogram` |
| `MaxPoids` (const) | `MaxWeight` |
| `PrixUnitaireMax` (const) | `MaxUnitPrice` |
| `EAN13` | `EAN13` |
| `PlanPrefixe` | `PrefixPlan` |
| `planInterne` (var, table constante) | `internalPlan` |
| `ModeVente` | `SaleMode` |
| `AuPoids` | `ByWeight` |
| `ALUnite` | `ByUnit` |
| `Produit` | `Product` |
| `Categorie` | `Category` |
| `Catalogue` | `Catalog` |
| `Qualification` | `Qualification` |
| `Pesable` | `Weighable` |
| `NonPesable` | `NotWeighable` |
| `Anomalie` | `Anomaly` |
| `Tarif` | `PriceTier` |
| `ReglesTarification` | `PricingRules` |
| `LigneTarif` | `PriceLine` |
| `Etiquette` | `Label` |
| `SeuilsPesee` | `WeighingLimits` |
| `EntreeGarde` | `CheckInput` |
| `Diagnostic` | `Diagnostic` |
| `Mesure` | `Measurement` |
| `Stabilite` | `Stability` |
| `Stable` / `Instable` / `Inconnue` / `StabiliteSansObjet` | `Stable` / `Unstable` / `StabilityUnknown` / `StabilityNotApplicable` |
| `Figeur` | `WeightLatch` |
| `EtatFigeage` | `LatchState` |
| `Cadencemetre` | `RateMeter` |
| `PolitiqueStabilite` | `StabilityPolicy` |
| `ModeBloquant` (const) / mode « informatif » | `ModeBlocking` / `ModeAdvisory` |
| `Etat` (machine à états) | `State` |
| `Initialisation` | `Initializing` |
| `Attente` | `Idle` |
| `ProduitArme` | `ProductArmed` |
| `PoidsPresent` | `WeightPresent` |
| `PoidsStable` | `WeightStable` |
| `AttenteStabilite` | `AwaitingStability` |
| `SaisieTare` | `EnteringTare` |
| `SaisieManuellePoids` | `EnteringWeight` |
| `ModeManuel` | `ManualMode` |
| `Validation` (état) | `Validating` |
| `Impression` (état) | `Printing` |
| `Succes` (état) | `Succeeded` |
| `Refus` (état) | `Rejected` |
| `Erreur` (état) | `Faulted` |
| `BalancePerdue` (état) | `ScaleLost` |
| `HorsService` (état) | `OutOfService` |
| `Evenement` | `Event` |
| `MesureRecue` | `MeasurementReceived` |
| `PerteBalance` (événement) | `ScaleDisconnected` |
| `BalanceRetrouvee` | `ScaleReconnected` |
| `ProduitTouche` | `ProductTapped` |
| `ToucheTare` | `TareTapped` |
| `TareValidee` | `TareConfirmed` |
| `PoidsSaisiValide` | `ManualWeightConfirmed` |
| `ImpressionTerminee` | `PrintFinished` |
| `DemandeReimpression` | `ReprintRequested` |
| `CatalogueDisponible` | `CatalogReady` |
| `Annuler` (événement) | `Cancel` |
| `Acquitter` (événement IHM) | `Dismiss` |
| `Tic` | `Tick` |
| `Effet` | `Effect` |
| `EffetImprimer` | `PrintEffect` |
| `EffetJournaliser` | `RecordEffect` |
| `EffetMessage` | `MessageEffect` |
| `EffetSon` | `SoundEffect` |
| `EffetAccuser` | `AckEffect` |
| `EffetTechnique` | `TechnicalLogEffect` |
| `EffetArmerTimer` | `ArmTimerEffect` |
| `EffetAppliquerCatalogue` | `ApplyCatalogEffect` |
| `Modele` | `Model` |
| `Contexte` (entrées de Transition) | `TransitionContext` |
| `Accuse` | `Ack` |
| `Gabarit` (étiquette) | `Template` |
| `Element` (d'un gabarit) | `Element` |
| `Faute` (validation) | `Fault` |
| `Configuration` | `Config` |
| `Registres` | `Registries` |
| `Locale` | `Locale` |
| `Media` | `Media` |
| `Balance` (interface) | `Scale` |
| `Imprimante` (interface) | `Printer` |
| `Transport` (interface) | `Transport` |
| `SourceCatalogue` (interface) | `CatalogSource` |
| `Horloge` (interface) | `Clock` |
| `JournalTechnique` (interface) | `TechnicalLog` |
| `Decodeur` (interface) | `Decoder` |
| `DescripteurBalance` | `ScaleDescriptor` |
| `DescripteurImprimante` | `PrinterDescriptor` |
| `Descripteur` (impression) | `PrinterDescriptor` — le type nu `Descriptor` n'existe pas : seule la MÉTHODE s'appelle `Descriptor()`. Un type par appareil, parce que les deux ne déclarent pas les mêmes capacités |
| `Capacites` (balance) | `Capabilities` — `Tare`, `Stability`, `Overload` |
| `Capacites` (imprimante) | `PrinterCapabilities` — homonymie levée : les deux appareils ne déclarent rien de commun, et un seul type `Capabilities` aurait porté huit champs dont trois vides selon le porteur |
| `TravailImpression` | `PrintJob` |
| `Recu` (retour d'`Imprimer`) | `PrintReceipt` |
| `StatutImprimante` | `PrinterStatus` |
| `EvenementBalance` | `ScaleEvent` |
| `StatutDeconnecte` / `StatutConnecte` | `StatusDisconnected` / `StatusConnected` |
| `Lot` (catalogue) | `Batch` |
| `ResultatLot` | `BatchResult` |
| `Signalement` | `Finding` |
| `Motif` | `Reason` |
| `Erreur` (impression, typée) | `PrintError` |
| `Genre` (d'erreur d'impression) | `Kind` |
| `GenreDonnees` / `GenreGabarit` / `GenreTransitoire` / `GenreConsommable` / `GenreConfig` / `GenreInterne` | `KindData` / `KindTemplate` / `KindTransient` / `KindConsumable` / `KindConfig` / `KindInternal` |
| `OptionsRendu` | `RenderOptions` |
| `OptionsSymbole` | `SymbolOptions` |
| `DiagnosticSymbole` | `SymbolDiagnostic` |
| `Options` (série) | `Options` |
| `Accumulateur` | `Accumulator` |
| `Unite` / `UniteKg` / `UniteG` (parseur de trame) | `Unit` / `UnitKg` / `UnitGram` |
| `Poste` (struct de §11.4) | `Station` |
| `Hub` | `Hub` |
| `Snapshot` | `Snapshot` |
| `Message` (bandeau) | `Message` |
| `Service` (impression) | `Service` |
| `encodeur` (SBPL) | `encoder` |
| `travail` (job interne du Hub) | `job` |
| `cleFace` | `faceKey` |
| `Pesee` (enregistrement au journal) | `Weighing` |
| `Idempotence` (cache des 32 dernières clés) | `IdempotencyCache` |
| `Serveur` (web) | `Server` |
| `App` | `App` |
| `DB` | `DB` |
| `ErrEAN13Format` | `ErrEAN13Format` |
| `ErrEAN13Cle` | `ErrEAN13CheckDigit` |
| `ErrPrefixeHorsPlan` | `ErrPrefixNotInPlan` |
| `ErrLargeurHorsPlan` | `ErrWidthNotInPlan` |
| `ErrChargeHorsCapacite` | `ErrPayloadOutOfRange` |
| `ErrGabaritNonNul` | `ErrPatternNotZeroed` |
| `ErrPrefixeIncoherent` | `ErrPrefixModeMismatch` |
| `ErrQuantiteNulle` | `ErrZeroQuantity` |
| `ErrGrilleIncoherente` | `ErrInconsistentTiers` |
| `ErrNonReconnue` (trame) | `ErrUnrecognizedFrame` |
| `ErrNonSupporte` (transport unidirectionnel) | `ErrUnsupported` |
| `ErrBoucleTerminee` | `ErrLoopStopped` |
| `ErrOccupe` (worker d'impression saturé) | `ErrBusy` |
| `ProfilNeutre` | `NeutralProfile` |

---

## Fonctions et méthodes

| Actuel (FR) | Cible (EN) |
| --- | --- |
| `(PolitiqueArrondi).Diviser` | `(RoundingPolicy).Divide` |
| `Cle` (clé de contrôle EAN-13) | `CheckDigit` |
| `Composer` | `Compose` |
| `Generer` | `Generate` |
| `Diagnostiquer` | `Diagnose` |
| `Modules` | `Modules` |
| `Quantifier` | `Quantize` |
| `Parser` (`noyau.Parser`, code-barres) | `ParseEAN13` |
| `Tarifer` | `Price` |
| `(ReglesTarification).TriParOrdre` | `(PricingRules).SortedTiers` |
| `(Etiquette).Chercher` | `(Label).Find` |
| `Evaluer` | `Evaluate` |
| `SeuilPoids` | `MinWeight` |
| `(Figeur).Alimenter` | `(WeightLatch).Feed` |
| `(Cadencemetre).Observer` | `(RateMeter).Observe` |
| `(Cadencemetre).Mediane` | `(RateMeter).Median` |
| `(Cadencemetre).Peremption` | `(RateMeter).Expiry` |
| `Transition` | `Transition` |
| `Preparer` | `Prepare` |
| `Normaliser` | `Normalize` |
| `Qualifier` | `Qualify` |
| `(Configuration).Valider` | `(Config).Validate` |
| `(Gabarit).Valider` | `(Template).Validate` |
| `(Configuration).Empreinte` | `(Config).Fingerprint` |
| `(Configuration).Exporter` | `(Config).Export` |
| `Analyser` (trame) | `Parse` |
| `(Accumulateur).Alimenter` | `(Accumulator).Feed` |
| `enGrammes` | `toGrams` |
| `Boucle` (série) | `Loop` |
| `Emettre` | `Emit` |
| `Rasteriser` | `Rasterize` |
| `DessinerEAN13` | `DrawEAN13` |
| `dessinerHRI` | `drawHRI` |
| `bord` (closure) | `edge` |
| `seuiller` | `applyThreshold` |
| `(encodeur).media` | `(encoder).media` |
| `(encodeur).graphique` | `(encoder).graphic` |
| `(encodeur).emettreHexa` | `(encoder).writeHex` |
| `(Erreur).Reessayable` | `(PrintError).Retryable` |
| `(Service).Imprimer` | `(Service).Print` |
| `Descripteur()` | `Descriptor()` |
| `Demarrer` | `Start` |
| `Fermer` | `Close` |
| `Imprimer` | `Print` |
| `Statut` | `Status` |
| `AutoTest` | `SelfTest` |
| `Nom()` (`Transport`, `SourceCatalogue`) | `Name()` |
| `Ecrire` (`Transport`) | `Write` |
| `Interroger` (`Transport`) | `Query` |
| `Decrire` (`Transport`) | `Describe` |
| `Suivant` (`SourceCatalogue`) | `Next` |
| `Acquitter` (`SourceCatalogue`) | `Acknowledge` |
| `Maintenant` (`Horloge`) | `Now` |
| `Apres` (`Horloge`) | `After` |
| `Ticker` (`Horloge`) | `Ticker` |
| `Budget` (`ports.Budget`) | `WithBudget` (`ports.WithBudget`) |
| `Technique` (`JournalTechnique.Technique`) | `Technical` |
| `(Hub).boucle` | `(Hub).run` |
| `(Hub).executer` | `(Hub).execute` |
| `(Hub).publier` | `(Hub).publish` |
| `(Hub).construireSnapshot` | `(Hub).buildSnapshot` |
| `(Hub).Soumettre` | `(Hub).Submit` |
| `(Hub).Abonner` | `(Hub).Subscribe` |
| `(Hub).appliquerAbonnement` | `(Hub).applySubscription` |
| `(Hub).FermerAbonnes` | `(Hub).CloseSubscribers` |
| `(Hub).Etat` | `(Hub).State` |
| `(Hub).Termine` | `(Hub).Done` |
| `(Hub).arretPropre` | `(Hub).gracefulStop` |
| `(Hub).tech` | `(Hub).logTechnical` |
| `repondre` | `reply` |
| `accuseParDefaut` | `defaultAck` |
| `(anneau).Ajouter` / `(anneau).Lire` | `(ring).Add` / `(ring).Entries` |
| `(Idempotence).Voir` / `(Idempotence).Poser` | `(IdempotencyCache).Lookup` / `(IdempotencyCache).Store` |
| `(Poste).Recharger` | `(Station).Reload` |
| `(Poste).redemarrerBalance` | `(Station).restartScale` |
| `(Poste).redemarrerImprimante` | `(Station).restartPrinter` |
| `(Poste).redemarrerCatalogue` | `(Station).restartCatalog` |
| `(Poste).rebinderEcoute` | `(Station).rebindListener` |
| `(Poste).annulerBalance` | `(Station).cancelScale` |
| `(Poste).journaliserSiErr` | `(Station).logIfErr` |
| `empreinteBloc` | `BlockFingerprint` — **exporté**, contrairement à ce que cette table annonçait : l'empreinte par bloc sert à l'écran d'administration (« quels blocs ont bougé ? »), donc hors du paquet `domain` |
| `attendreTout` | `waitAll` |
| `(App).Arreter` | `(App).Stop` |
| `annulerRacine` | `cancelRoot` |
| `Drainer` | `Drain` |
| `(catalogue).Attendre` | `(catalog).Wait` |
| `catalogue.Boucle` (veille) | `catalog.Watch` |
| `Ouvrir` (stockage) | `Open` |
| `(DB).migrer` | `(DB).migrate` |
| `(DB).conserverNDernieresSauvegardes` | `(DB).keepLastBackups` |
| `RemplacerCatalogue` | `ReplaceCatalog` |
| `OuvrirTest` | `OpenTest` |
| `(Serveur).flux` | `(Server).stream` |
| `ecrire` (événement SSE) | `writeEvent` |
| `Routes` | `Routes` |
| `poste.Nouveau` | `station.New` |
| `faux.NouvelleBalance` / `NouvelleImprimante` / `NouvelleHorloge` | `fake.NewScale` / `NewPrinter` / `NewClock` |
| `(faux.Balance).Pousser` | `(fake.Scale).Push` |
| `(faux.Horloge).Avancer` | `(fake.Clock).Advance` |
| `(faux.Imprimante).Travaux` | `(fake.Printer).Jobs` |
| `lireAccuse` | `readAck` |
| `attendreResultatSSE` | `waitSSEResult` |
| `chargerConfig` | `loadConfig` |
| `lotAil` | `garlicBatch` |
| `estPortOccupe` | `isPortInUse` |
| `repondALaSonde` | `respondsToProbe` |
| `conformite.Suite` | `conformance.Suite` |
| `TestFloatCasseSur741Masses` | `TestFloatBreaksOn741Weights` |
| `TestPerteBalanceDeclencheeParLeStatutSeul` | `TestScaleLossTriggeredByStatusAlone` |
| `TestMesurePerimeeRefuseLaPesee` | `TestExpiredMeasurementRejectsWeighing` |
| `TestArmementExpireAvantLeSacDuSuivant` | `TestArmingExpiresBeforeNextCustomerBag` |
| `TestAucuneFuiteSurCommandeSansAccuse` | `TestNoLeakOnCommandWithoutAck` |
| `TestArretAvecQuatreAbonnesNePaniquePas` | `TestStopWithFourSubscribersDoesNotPanic` |
| `TestArretNAttendPasLHorlogeReelle` | `TestStopDoesNotWaitOnRealClock` |
| `TestPeseeBoutEnBout` | `TestWeighingEndToEnd` |
| `Sauvegarder-Reglages` (PowerShell) | `Backup-Settings` |
| `Exiger-Succes` (PowerShell) | `Assert-Success` |
| `Nouveau-MotDePasse` (PowerShell) | `New-Password` |
| `Ecrire-FicheInstallation` (PowerShell) | `Write-InstallSheet` |

---

## Champs et variables

| Actuel (FR) | Cible (EN) |
| --- | --- |
| `Etiquette.Produit` / `Mode` / `PoidsBrut` / `Tare` / `PoidsNet` / `Quantite` | `Label.Product` / `Mode` / `GrossWeight` / `Tare` / `NetWeight` / `Quantity` |
| `Etiquette.Lignes` / `Principale` / `Reference` (lignes de tarif) | `Label.Lines` / `PrimaryLine` / `ReferenceLine` |
| `Etiquette.CodeBarre` / `JobID` | `Label.Barcode` / `JobID` |
| `LigneTarif.Tarif` / `PrixUnitaire` / `Montant` | `PriceLine.Tier` / `UnitPrice` / `Amount` |
| `Tarif.Code` / `Libelle` / `Abrege` / `CoefNum` / `CoefDen` / `Ordre` | `PriceTier.Code` / `Label` / `Abbrev` / `Discount` / `Rank` |
| `ReglesTarification.Tarifs` / `CodePrincipal` / `CodeSecondaires` / `CodeReference` / `ArrondiPrix` / `ArrondiTarif` | `PricingRules.Tiers` / `PrimaryCode` / `SecondaryCodes` / `ReferenceCode` / `AmountRounding` / `UnitPriceRounding` |
| Codes de tarif `"ADHERENT"` / `"SOLIDAIRE"` | `"MEMBER"` / `"SOLIDARITY"` |
| `Produit.ID` / `Nom` / `Reference` / `Mode` / `PrixUnitaire` | `Product.ID` / `Name` / `Reference` / `Mode` / `UnitPrice` |
| `Produit.Libelle` (suffixe « €/kg ») | `Product.PriceSuffix` |
| `Produit.CategorieCode` / `Qualification` / `Motif` / `LigneCSV` / `ImageSHA` | `Product.CategoryCode` / `Qualification` / `Reason` / `CSVLine` / `ImageSHA` |
| `PlanPrefixe.Prefixe` / `Mode` / `LargeurRef` / `LargeurCharge` / `Decimales` / `Libelle` | `PrefixPlan.Prefix` / `Mode` / `RefWidth` / `PayloadWidth` / `Decimals` / `PriceLabel` |
| `Mesure.Brut` / `Tare` / `Quantite` / `Stabilite` / `Horodate` / `Seq` | `Measurement.Gross` / `Tare` / `Quantity` / `Stability` / `Timestamp` / `Seq` |
| `EtatFigeage.Fige` / `Brut` / `Depuis` | `LatchState.Latched` / `Gross` / `Held` |
| `Figeur.ancre` / `ancreOK` / `pol` | `WeightLatch.anchor` / `hasAnchor` / `policy` |
| `Cadencemetre.intervalles` / `precedente` | `RateMeter.intervals` / `previous` |
| `PolitiqueStabilite.Mode` / `DureeMin` / `ToleranceGrammes` / `Timeout` / `ComportementTimeout` | `StabilityPolicy.Mode` / `MinDuration` / `ToleranceGrams` / `Timeout` / `OnTimeout` |
| `PolitiqueStabilite.TauxMinBloquant` / `FenetreTauxMin` | `StabilityPolicy.MinLatchRate` / `LatchRateWindow` |
| `PolitiqueStabilite.PeremptionPlancher` / `PeremptionPlafond` / `FacteurPeremption` | `StabilityPolicy.ExpiryFloor` / `ExpiryCeiling` / `ExpiryFactor` |
| `Contexte.Cfg` / `Maintenant` / `DerniereMesure` / `AgeDerniereMesure` / `Peremption` / `Catalogue` | `TransitionContext.Cfg` / `Now` / `LastMeasurement` / `MeasurementAge` / `Expiry` / `Catalog` |
| `Modele.ProduitEnCours` / `PoidsFige` / `Etiquette` | `Model.CurrentProduct` / `LatchedWeight` / `Label` |
| `ArmementMax` (constante 10 s) | `MaxArmingTime` |
| `BasculeMax` / `IHM.DelaiBascule` (constante 10 s) | `MaxSwitchIdle` / `UI.SwitchDelay` |
| `Faute.Champ` / `Message` / `Valeurs` | `Fault.Field` / `Message` / `Values` |
| `Capacites.Raster` / `Statut` / `Massicot` / `MaxExemplaires` / `DotsParMM` | `PrinterCapabilities.Raster` / `Status` / `Cutter` / `MaxCopies` / `DotsPerMM` |
| `Descripteur.ID` / `Libelle` / `Capacites` | `PrinterDescriptor.ID` / `Label` / `Capabilities` — et `ScaleDescriptor.ID` / `Label` / `Capabilities` / `NominalRate` |
| `DescripteurBalance.CadenceNominale` | `ScaleDescriptor.NominalRate` |
| `Options.Port` / `Baud` / `Bits` / `Parite` / `Stop` / `Decodeur` / `TailleLecture` / `BackoffMin` / `BackoffMax` | `Options.Port` / `Baud` / `Bits` / `Parity` / `Stop` / `Decoder` / `ReadBufferSize` / `BackoffMin` / `BackoffMax` |
| `EvenementBalance.Statut` / `Mesure` / `Err` | `ScaleEvent.Status` / `Measurement` / `Err` |
| `Erreur.Genre` / `Op` / `Message` | `PrintError.Kind` / `Op` / `Message` |
| `OptionsRendu.Annote` | `RenderOptions.Annotate` |
| `OptionsSymbole.XDots` / `YDots` / `ModuleMilliDots` / `HauteurBarresDots` / `DescenteGardesDots` / `HRIFace` / `HRIHauteurDots` | `SymbolOptions.XDots` / `YDots` / `ModuleMilliDots` / `BarHeightDots` / `GuardDescentDots` / `HRIFace` / `HRIHeightDots` |
| `DiagnosticSymbole.ModuleUM` / `ModuleDots` / `Grandissement` / `LargeurTotaleUM` / `HauteurBarresUM` / `HauteurNormeUM` / `TauxHauteur` / `ModuleEntier` / `Avertissements` | `SymbolDiagnostic.ModuleUM` / `ModuleDots` / `Magnification` / `TotalWidthUM` / `BarHeightUM` / `StandardHeightUM` / `HeightRatio` / `IntegerModule` / `Warnings` |
| `Element.CorpsUM` / `CorpsMinUM` | `Element.FontSizeUM` / `MinFontSizeUM` |
| `Gabarit.SeuilTexte` / `Media.DotsParMM` | `Template.TextThreshold` / `Media.DotsPerMM` |
| `cleFace{Fonte, PPEM, Gras}` | `faceKey{Font, PPEM, Bold}` |
| `delaisReessai` (var) | `retryDelays` |
| `h.modele` / `h.seq` / `h.derniereMesure` / `h.cadence` | `h.model` / `h.seq` / `h.lastMeasurement` / `h.rate` |
| `h.mesures` / `h.commandes` / `h.impressions` / `h.impressionsFinies` / `h.journaux` | `h.measurements` / `h.commands` / `h.printJobs` / `h.printResults` / `h.journalEntries` |
| `h.catalogueApplique` / `h.lotEnAttente` / `h.catalogue` | `h.incomingCatalog` / `h.pendingBatch` / `h.catalog` |
| `h.abonnements` / `h.abonnes` | `h.subscriptions` / `h.subscribers` |
| `h.reponseEnCours` / `h.derniereInteraction` / `h.idem` | `h.pendingReply` / `h.lastInteraction` / `h.idempotency` |
| `h.dernierPublie` / `h.dernierePublication` / `h.publicationEnAttente` | `h.lastPublished` / `h.lastPublishedAt` / `h.publishPending` |
| `h.anneau` / `h.etat` / `h.compteurs` / `h.horloge` / `h.termine` / `h.nominale` / `h.son` / `h.jrn` | `h.ring` / `h.state` / `h.counters` / `h.clock` / `h.done` / `h.nominalRate` / `h.sound` / `h.log` |
| `differes` (événements réinjectés) | `deferred` |
| `compteurs.PeseesNonJournalisees` | `counters.UnloggedWeighings` |
| `compteurs.FermeturesBalanceNonConfirmees` | `counters.UnconfirmedScaleCloses` |
| `Message.Niveau` / `Code` / `Texte` / `Expire` | `Message.Level` / `Code` / `Text` / `ExpiresAt` |
| `Snapshot.Perimee` / `Revision` | `Snapshot.Expired` / `Revision` |
| `p.balanceFini` / `ferme` (canaux) | `s.scaleDone` / `closed` |
| `ctxRacine` | `rootCtx` |
| Codes de garde-fou : `SURCHARGE` / `MESURE_PERIMEE` / `PANIER_ABSENT` / `BALANCE_VIDE` / `TARE_REQUISE` | `OVERLOAD` / `MEASUREMENT_EXPIRED` / `BASKET_MISSING` / `SCALE_EMPTY` / `TARE_REQUIRED` |
| `POIDS_INSTABLE` / `TARE_INVALIDE` / `POIDS_TROP_FAIBLE` / `POIDS_TROP_LOURD` | `WEIGHT_UNSTABLE` / `TARE_INVALID` / `WEIGHT_TOO_LOW` / `WEIGHT_TOO_HIGH` |
| `UNITES_HORS_BORNES` / `MONTANT_HORS_CAPACITE` / `PRIX_NUL` / `PRODUIT_LEGER_TOLERE` / `PRODUIT_NON_PROPOSE` | `UNITS_OUT_OF_RANGE` / `AMOUNT_OUT_OF_CAPACITY` / `ZERO_PRICE` / `LIGHT_PRODUCT_ALLOWED` / `PRODUCT_WITHDRAWN` |
| Motifs d'import : `LIGNE_ILLISIBLE` / `PRIX_ILLISIBLE` / `SANS_CODE_BARRES` / `CODE_BARRES_INVALIDE` | `UNREADABLE_ROW` / `PRICE_UNREADABLE` / `NO_BARCODE` / `INVALID_BARCODE` |
| `PRODUIT_PREEMBALLE` / `CODE_INTERNE_NON_PESABLE` / `ZONE_DE_RESERVATION_OCCUPEE` | `PREPACKAGED_PRODUCT` / `INTERNAL_CODE_NOT_WEIGHABLE` / `RESERVED_ZONE_NOT_EMPTY` |
| `UNITE_INCONNUE` / `UNITE_DIVERGENTE` / `ENTETE_INATTENDU` / `CATEGORIE_INCONNUE` | `UNKNOWN_UNIT` / `UNIT_MISMATCH` / `UNEXPECTED_HEADER` / `UNKNOWN_CATEGORY` |
| `IMAGE_ABSENTE` / `IMAGE_INVALIDE` / `IMAGE_TROP_GRANDE` / `GLYPHE_MANQUANT` | `IMAGE_MISSING` / `IMAGE_INVALID` / `IMAGE_TOO_LARGE` / `MISSING_GLYPH` |
| Codes ERR : `ERR-BAL-nn` / `ERR-IMP-nn` / `ERR-CAT-nn` / `ERR-BDD-nn` | `ERR-SCL-nn` / `ERR-PRN-nn` / `ERR-CAT-nn` / `ERR-DB-nn` |
| Codes ERR : `ERR-IHM-nn` / `ERR-KIO-nn` / `ERR-SYS-nn` / `ERR-CFG-nn` | `ERR-UI-nn` / `ERR-KSK-nn` / `ERR-SYS-nn` / `ERR-CFG-nn` |
| FieldID du gabarit : `nom_produit` / `quantite` / `prix_unitaire_principal` | `product_name` / `quantity` / `primary_unit_price` |
| FieldID du gabarit : `prix_total_secondaire` / `prix_total_principal` / `code_barres` | `secondary_total_price` / `primary_total_price` / `barcode` |
| Noms de gabarits : `pesee_identique` / `pesee_module_entier` / `pesee_neutre_mono` | `weighing_identical` / `weighing_integer_module` / `weighing_neutral_single` |
| Auto-tests : `etiquette` / `mire` / `reglette` | `label` / `alignment` / `ruler` |
| Sons : `EffetSon("ok")` | `SoundEffect("ok")` |
| Variables CSS : `--fond` / `--surface` / `--bordure` / `--encre` / `--encre-douce` | `--bg` / `--surface` / `--border` / `--ink` / `--ink-muted` |
| Variables CSS : `--attente` / `--pret` / `--alerte` / `--panne` / `--focus` | `--waiting` / `--ready` / `--warning` / `--fault` / `--focus` |
| clé `meta.etiquettes_depuis_rouleau` | clé `meta` `labels_since_roll` |

### Champs apparus à l'implémentation

Ces champs portent des types déjà nommés ci-dessus, mais le cahier des charges ne les
nommait pas. Ils n'ont donc pas d'antécédent français : la colonne de gauche dit ce qu'ils
portent.

| Ce qu'il porte | Identifiant |
| --- | --- |
| La balance se déclare hors capacité (drapeau `OL` de la trame) | `Measurement.Overload` |
| Ce que la balance déclare savoir faire | `Capabilities.Tare` / `Stability` / `Overload` |
| Tout ce que la machine retient entre deux événements, au-delà du produit et du poids figé | `Model.State` / `Source` / `Tare` / `Units` / `Latch` / `LatchState` / `ArmedAt` / `StartedAt` / `JobID` / `IdempotencyKey` / `Diagnostics` / `FaultCode` / `LastLabel` / `LastPrintedAt` / `Reprinted` |
| La réponse rendue à une commande | `Ack.Accepted` / `State` / `Code` / `Message` / `JobID` · `AckEffect.Ack` / `Key` |
| Le contenu d'un effet | `MessageEffect.Level` / `Code` / `Text` / `Duration` · `TechnicalLogEffect.Level` / `Source` / `Code` / `Message` / `Detail` · `PrintEffect.Label` / `Reprint` · `RecordEffect.Weighing` · `ApplyCatalogEffect.Catalog` · `ArmTimerEffect.Duration` · `SoundEffect.Name` |
| Ce qu'un événement d'IHM transporte | `ProductTapped.ProductID` / `Tare` / `Units` / `SeenWeight` / `MeasurementSeq` / `Key` · `TareConfirmed.Tare` / `Key` · `ManualWeightConfirmed.Weight` / `Key` · `ReprintRequested.JobID` / `Key` |
| Ce qu'un événement de matériel transporte | `MeasurementReceived.M` · `ScaleDisconnected.Err` · `PrintFinished.JobID` / `Duration` / `Err` · `CatalogReady.Catalog` |
| Tout ce qu'`Evaluate` a le droit de lire | `CheckInput.ProductID` / `ProductOffered` / `ProductMinWeight` / `Mode` / `EncodesPrice` / `Gross` / `Tare` / `Quantity` / `Overload` / `Stability` / `StabilityBlocking` / `MeasurementAge` / `Expiry` / `PrimaryAmount` / `ReferenceAmount` |
| Une réponse à « cette pesée peut-elle produire une étiquette ? » | `Diagnostic.Code` / `Message` / `Severity` / `ProductID` |
| Les bornes numériques d'un poste | `WeighingLimits.EmptyMax` / `BasketCheckEnabled` / `BasketMin` / `BasketMax` / `MinWeight` / `MaxWeight` / `MaxTare` / `MinUnits` / `MaxUnits` / `MaxAmount` |
| Tout ce que `Prepare` lit, et ce qu'il répond | `PrepareInput.Product` / `Measurement` / `Rules` / `Limits` / `Decision` / `Expiry` / `MeasurementAge` / `StabilityBlocking` / `JobID` · `Preparation.Label` / `Priced` / `Diagnostics` / `Refusal` |
| Un élément posé sur l'étiquette | `Element.Field` / `XUM` / `YUM` / `WidthUM` / `HeightUM` / `Align` / `Bold` / `AutoBold` / `Framed` / `When` |
| Le gabarit lui-même | `Template.Name` / `Media` / `Elements` / `Symbol` / `PrintableWidthUM` / `PrintableHeightUM` / `OffsetXDots` / `OffsetYDots` / `TruncationAccepted` · `Media.WidthUM` / `HeightUM` |
| Où et de quelle taille le symbole est tracé, en micromètres | `SymbolGeometry.XUM` / `YUM` / `ModuleMilliDots` / `BarHeightUM` / `GuardDescentUM` / `HRIHeightUM` |
| Un rayon de la grille | `Category.Code` / `Label` / `Rank` / `Color` / `Visible` |
| L'enregistrement d'une pesée au journal | `Weighing.ID` / `OccurredAt` / `Station` / `JobID` / `IdempotencyKey` / `ProductID` / `ProductName` / `Reference` / `Mode` / `GrossWeight` / `Tare` / `NetWeight` / `Quantity` / `BaseUnitPrice` / `Lines` / `Barcode` / `Source` / `Stability` / `RateMS` / `Frame` / `Result` / `Detail` / `DurationMS` · `WeighingLine.TierCode` / `UnitPrice` / `Amount` |
| L'enregistrement d'un import | `Import.ID` / `OccurredAt` / `Source` / `FileName` / `SHA256` / `ByteCount` / `RowsRead` / `UnreadableRows` / `Weighable` / `NotWeighable` / `Anomalies` / `UnitMismatches` / `ImagesDecoded` / `ImagesRejected` / `ProductsWithdrawn` / `Result` / `Code` / `Reason` / `DurationMS` |
| Ce qu'un import a à dire sur une ligne | `Finding.ImportID` / `CSVLine` / `ProductID` / `Code` / `Issue` / `Message` / `Value` |
| Une photo, une décision humaine, un contenu banni | `Image.SHA256` / `ByteCount` / `Format` / `Width` / `Height` / `SeenAt` · `LocalDecision.ProductID` / `Offered` / `MinWeightG` / `Reason` / `DecidedAt` / `DecidedBy` · `QuarantineEntry.SHA256` / `FailureCount` / `FirstFailureAt` / `LastFailureAt` / `Code` / `Reason` |
| Les blocs de la configuration, un champ par bloc de §11.2 | `Config.Version` / `Readme` / `ModifiedAt` / `Station` / `Network` / `UI` / `Scale` / `Printer` / `Pricing` / `Barcode` / `Limits` / `Stability` / `Catalog` / `Journal` / `Admin` / `Maintenance` |
| Le contenu de chaque bloc | `StationConfig.Number` / `Name` / `Coop` · `NetworkConfig.Listen` / `AdminOnLAN` · `UIConfig.Language` / `Sound` / `IdleTimeoutSeconds` / `ReprintWindowSeconds` / `ShowGridPrices` · `ScaleConfig.Type` / `Present` / `ManualEntryAllowed` / `DegradeAfterSeconds` / `Options` · `PrinterConfig.Type` / `Template` / `Options` · `CatalogConfig.Type` / `Options` / `Images` / `Categories` / `FallbackCategory` · `ImagesConfig.Source` / `Path` · `JournalConfig.MaxRows` / `MaxDays` / `MaxTechnical` · `AdminConfig.PasswordHash` / `RecoveryCodeHash` / `SessionMinutes` / `AttemptsPerMinute` · `MaintenanceConfig.WeeklyIntegrityCheck` / `DiskAlertMB` · `BarcodeConfig.VerifyReferenceCheckDigit` |
| Ce qu'un binaire sait charger, et ce qu'un driver déclare | `Registries.Scales` / `Printers` / `Transports` / `CatalogSources` / `Templates` / `Paths` · `DriverDescriptor.ID` / `Label` / `Options` · `OptionSchema.Key` / `Kind` / `Required` / `Min` / `Max` / `Values` / `Options` |
| Les deux points d'injection d'une liaison série | `Options.Open` / `Clock` |

---

## Tables et colonnes SQL

| Actuel (FR) | Cible (EN) |
| --- | --- |
| table `meta (cle, valeur, modifie_le)` | table `meta (key, value, updated_at)` |
| table `imports` | table `imports` |
| `imports.horodate` / `source` / `fichier` / `sha256` / `octets` | `imports.occurred_at` / `source` / `file_name` / `sha256` / `byte_count` |
| `imports.lignes_lues` / `lignes_illisibles` | `imports.rows_read` / `unreadable_rows` |
| `imports.pesables` / `non_pesables` / `anomalies` / `unites_divergentes` | `imports.weighable` / `not_weighable` / `anomalies` / `unit_mismatches` |
| `imports.images_decodees` / `images_refusees` / `produits_retires` | `imports.images_decoded` / `images_rejected` / `products_withdrawn` |
| `imports.resultat` / `code` / `motif` / `duree_ms` | `imports.result` / `code` / `reason` / `duration_ms` |
| `imports.resultat IN ('applique','inchange','rejete','echec')` | `imports.result IN ('applied','unchanged','rejected','failed')` |
| `imports.source IN ('depot_local','webdav','manuel')` | `imports.source IN ('local_drop','webdav','manual')` |
| `idx_imports_sha` / `idx_imports_horodate` | `idx_imports_sha` / `idx_imports_occurred_at` |
| table `quarantaine (sha256, echecs, premier_echec, dernier_echec, code, motif)` | table `quarantine (sha256, failure_count, first_failure_at, last_failure_at, code, reason)` |
| table `categories (code, libelle, ordre, couleur, visible)` | table `categories (code, label, rank, color, visible)` |
| codes de catégorie : `fruits` / `legumes` / `vrac` / `autres` | `fruits` / `vegetables` / `bulk` / `other` |
| table `images (sha256, octets, format, largeur, hauteur, vue_le)` | table `images (sha256, byte_count, format, width, height, seen_at)` |
| table `produits` | table `products` |
| `produits.id` / `nom` / `reference` / `mode` / `libelle_prix` | `products.id` / `name` / `reference` / `mode` / `price_suffix` |
| `produits.prix_unitaire` (centimes) | `products.unit_price_cents` |
| `produits.categorie_code` / `qualification` / `motif` / `ligne_csv` / `image_sha256` | `products.category_code` / `qualification` / `reason` / `csv_line` / `image_sha256` |
| `produits.vu_le` / `retire_le` / `dernier_import_id` | `products.seen_at` / `withdrawn_at` / `last_import_id` |
| `mode IN ('au_poids','a_l_unite')` | `mode IN ('by_weight','by_unit')` |
| `qualification IN ('pesable','non_pesable','anomalie')` | `qualification IN ('weighable','not_weighable','anomaly')` |
| `idx_produits_grille` / `idx_produits_reference` | `idx_products_grid` / `idx_products_reference` |
| table `decisions_locales (produit_id, proposer, poids_min_g, motif, horodate, par)` | table `local_decisions (product_id, offered, min_weight_g, reason, decided_at, decided_by)` |
| table `signalements (import_id, ligne_csv, produit_id, code, issue, message, valeur)` | table `findings (import_id, csv_line, product_id, code, issue, message, value)` |
| `signalements.issue IN ('anomalie','information')` | `findings.issue IN ('anomaly','info')` |
| `idx_signalements_import` | `idx_findings_import` |
| table `pesees` | table `weighings` |
| `pesees.horodate` / `poste` / `job_id` / `cle_idempotence` | `weighings.occurred_at` / `station` / `job_id` / `idempotency_key` |
| `pesees.produit_id` / `produit_nom` / `reference` / `mode` | `weighings.product_id` / `product_name` / `reference` / `mode` |
| `pesees.poids_brut_g` / `tare_g` / `poids_net_g` / `quantite` | `weighings.gross_weight_g` / `tare_g` / `net_weight_g` / `quantity` |
| `pesees.prix_unitaire_base_c` | `weighings.base_unit_price_cents` |
| `pesees.code_barre` / `source` / `stabilite` / `cadence_ms` / `trame` | `weighings.barcode` / `source` / `stability` / `rate_ms` / `frame` |
| `pesees.resultat` / `detail` / `duree_ms` | `weighings.result` / `detail` / `duration_ms` |
| `pesees.source IN ('balance','manuelle','rejeu')` | `weighings.source IN ('scale','manual','replay')` |
| `pesees.stabilite IN ('stable','instable','inconnue','sans_objet')` | `weighings.stability IN ('stable','unstable','unknown','not_applicable')` |
| `pesees.resultat IN ('envoye','refus','erreur','reimpression')` | `weighings.result IN ('sent','rejected','failed','reprint')` |
| `idx_pesees_horodate` / `idx_pesees_resultat` / `idx_pesees_produit` | `idx_weighings_occurred_at` / `idx_weighings_result` / `idx_weighings_product` |
| table `pesee_lignes (pesee_id, code_tarif, prix_unitaire_c, montant_c)` | table `weighing_lines (weighing_id, tier_code, unit_price_cents, amount_cents)` |
| table `technique (horodate, niveau, source, code, message, detail)` | table `technical_log (occurred_at, level, source, code, message, detail)` |
| `technique.niveau IN ('debug','info','avertissement','erreur','critique')` | `technical_log.level IN ('debug','info','warn','error','critical')` |
| `technique.source IN ('balance','imprimante','catalogue','ihm','config','http','systeme')` | `technical_log.source IN ('scale','printer','catalog','ui','config','http','system')` |
| `idx_technique_horodate` / `idx_technique_code` | `idx_technical_log_occurred_at` / `idx_technical_log_code` |
| tables `SauvegardeProduits`, `Produits`, `Stats`, `TableProduitsLegers`, `SystemeDefaut`, `Systeme_Dimensions` (ancienne base Access) | **INCHANGÉS** — noms réels de l'existant, cités comme preuve |

---

## Clés de configuration et routes

| Actuel (FR) | Cible (EN) |
| --- | --- |
| `version` / `_lisez_moi` / `modifie_le` | `version` / `_readme` / `modified_at` |
| `poste{numero, nom, coop}` | `station{number, name, coop}` |
| `reseau{ecoute, admin_reseau_local}` | `network{listen, admin_on_lan}` |
| `ihm{langue, son, delai_inactivite_s, delai_reimpression_s, afficher_prix_grille}` | `ui{language, sound, idle_timeout_s, reprint_window_s, show_grid_prices}` |
| `balance{type, presente, saisie_manuelle_autorisee, delai_avant_degradation_s}` | `scale{type, present, manual_entry_allowed, degrade_after_s}` |
| `balance.options{port, baud, bits, parite, stop, backoff_min_ms, backoff_max_ms}` | `scale.options{port, baud, bits, parity, stop, backoff_min_ms, backoff_max_ms}` |
| `balance.type : gram-xfoc-rs \| gram-xfoc-plus` | `scale.type : gram-xfoc-rs \| gram-xfoc-plus` (inchangés, noms matériels) |
| `imprimante{type, gabarit}` | `printer{type, template}` |
| `imprimante.type : raster \| sbpl \| apercu` | `printer.type : raster \| sbpl \| preview` |
| `imprimante.options.transport : winspool \| devfile \| tcp \| fichier` | `printer.options.transport : winspool \| devfile \| tcp \| file` |
| `imprimante.options.file` (file d'impression Windows) | `printer.options.queue` |
| `imprimante.options{chemin, adresse, exemplaires}` | `printer.options{path, address, copies}` |
| `imprimante.options.secours{actif, transport, file}` | `printer.options.fallback{enabled, transport, queue}` |
| `imprimante.options{noircissement, vitesse, decalage_x, decalage_y, inverser_bits, capacite_rouleau}` | `printer.options{darkness, speed, offset_x, offset_y, invert_bits, roll_capacity}` |
| `tarification{tarifs, code_principal, code_secondaires, code_reference, arrondi_prix, arrondi_tarif}` | `pricing{tiers, primary_code, secondary_codes, reference_code, amount_rounding, unit_price_rounding}` |
| `tarifs[]{code, libelle, abrege, remise_pourcent, ordre}` | `tiers[]{code, label, abbrev, discount_percent, rank}` |
| `arrondi : commercial \| tronque \| pair` | `rounding : half_up \| truncate \| half_even` |
| `code_barre{verifier_cle_reference}` | `barcode{verify_reference_check_digit}` |
| clés refusées par le contrôle 20 : `decimales_poids`, `largeur_champ_unites`, `prefixe_poids`, `prefixe_unite`, `contenu`, `regles_par_prefixe`, `coef_num`, `coef_den` | `weight_decimals`, `units_field_width`, `weight_prefix`, `unit_prefix`, `content`, `rules_by_prefix`, `coef_num`, `coef_den` |
| `seuils{vide_max, panier_actif, panier_min, panier_max}` | `limits{empty_max_g, basket_check_enabled, basket_min_g, basket_max_g}` |
| `seuils{poids_min, poids_max, tare_max, unites_min, unites_max, montant_max}` | `limits{min_weight_g, max_weight_g, max_tare_g, min_units, max_units, max_amount_cents}` |
| `stabilite{mode, duree_min_ms, tolerance_grammes, timeout_ms, au_timeout}` | `stability{mode, min_duration_ms, tolerance_g, timeout_ms, on_timeout}` |
| `stabilite.mode : informatif \| bloquant` | `stability.mode : advisory \| blocking` |
| `au_timeout : avertir_et_imprimer \| refuser \| saisie_manuelle` | `on_timeout : warn_and_print \| reject \| manual_entry` |
| `stabilite{peremption_plancher_ms, peremption_plafond_ms, facteur_peremption, taux_min_bloquant, fenetre_taux_min_ms}` | `stability{expiry_floor_ms, expiry_ceiling_ms, expiry_factor, min_latch_rate, latch_rate_window_ms}` |
| `catalogue.type : depot_local \| webdav` | `catalog.type : local_drop \| webdav` |
| `catalogue.options{url, utilisateur, mot_de_passe, separateur}` | `catalog.options{url, username, password, separator}` |
| clé sans équivalent dans l'existant, née d'ADR-038 : le répertoire surveillé par `depot_local` | `catalog.options.directory` — `local_drop` **seule** (contrôle 47) ; le paquet la déclare sous `localdrop.DirectoryOption`, et `internal/domain` la réécrit en `catalogDirectoryOption` parce que le noyau n'importe aucun driver |
| `catalogue.options{scrutation_s, stabilite_scrutations}` | `catalog.options{poll_interval_s, stable_polls}` |
| `catalogue.options{taille_max_mo, taille_max_image_ko}` | `catalog.options{max_file_size_mb, max_image_size_kb}` |
| `catalogue.options{taux_minimal_lisibles, baisse_max_pesables, echecs_avant_rejet}` | `catalog.options{min_readable_ratio, max_weighable_drop, failures_before_reject}` |
| `catalogue.options{archives_max, archives_jours}` | `catalog.options{max_archives, archive_days}` |
| `catalogue.images{source, chemin}` | `catalog.images{source, path}` |
| `images.source : csv \| repertoire_images \| aucune` | `images.source : csv \| image_directory \| none` |
| `catalogue.categorie_de_repli` | `catalog.fallback_category` |
| `catalogue.categories[]{code, libelle, ordre, couleur, visible}` | `catalog.categories[]{code, label, rank, color, visible}` |
| `journal{max_lignes, max_jours, max_technique}` | `journal{max_rows, max_days, max_technical}` |
| `admin{hash_mot_de_passe, hash_code_secours, session_minutes, tentatives_par_minute}` | `admin{password_hash, recovery_code_hash, session_minutes, attempts_per_minute}` |
| `maintenance{verif_integrite_hebdo, alerte_disque_mo}` | `maintenance{weekly_integrity_check, disk_alert_mb}` |
| `GET /api/v1/flux` (événement SSE « etat ») | `GET /api/v1/stream` (SSE event `"state"`) |
| `GET /api/v1/catalogue` | `GET /api/v1/catalog` |
| `POST /api/v1/peser` | `POST /api/v1/weigh` |
| corps de `/peser {produit_id, tare_g, unites, poids_manuel_g, poids_vu_g, mesure_seq, cle}` | `{product_id, tare_g, units, manual_weight_g, seen_weight_g, measurement_seq, key}` |
| `POST /api/v1/reimprimer {job_id, cle}` | `POST /api/v1/reprint {job_id, key}` |
| `POST /api/v1/annuler` · `POST /api/v1/acquitter` | `POST /api/v1/cancel` · `POST /api/v1/dismiss` |
| `POST /api/v1/ihm/erreur {message, pile}` | `POST /api/v1/ui/error {message, stack}` |
| `GET /healthz` · `GET /readyz` · `GET /images/{sha}.{ext}` | inchangés |
| `POST /admin/api/depannage/*` | `POST /admin/api/troubleshooting/*` |
| `/depannage/reimprimer` · `/recharger-catalogue` · `/saisie-manuelle` | `/troubleshooting/reprint` · `/reload-catalog` · `/manual-entry` |
| `/depannage/rouleau-change` · `/imprimante-secours` | `/troubleshooting/roll-changed` · `/fallback-printer` |
| `/depannage/tester-balance` · `/tester-imprimante` · `/etiquette-test` | `/troubleshooting/test-scale` · `/test-printer` · `/test-label` |
| `POST /admin/api/catalogue/importer` | `POST /admin/api/catalog/import` |
| `GET /admin/api/sante` · `GET /admin/api/diagnostic.zip` | `GET /admin/api/health` · `GET /admin/api/diagnostic.zip` |
| `POST /admin/api/session` · `/session/secours` | `POST /admin/api/session` · `/session/recovery` |
| `GET\|PUT /admin/api/config` · `POST /admin/api/config/confirmer` | `GET\|PUT /admin/api/config` · `POST /admin/api/config/confirm` |
| `GET /admin/api/config/export?materiel=0` · `POST /admin/api/config/import` | `GET /admin/api/config/export?hardware=0` · `POST /admin/api/config/import` |
| `GET /admin/api/config/versions` · `POST /admin/api/config/restaurer` | `GET /admin/api/config/versions` · `POST /admin/api/config/restore` |
| `GET /admin/api/empreinte` | `GET /admin/api/fingerprint` |
| `GET /admin/api/imprimantes` · `POST /admin/api/imprimantes/rechercher` | `GET /admin/api/printers` · `POST /admin/api/printers/discover` |
| `POST /admin/api/balance/detecter` · `/balance/capturer` | `POST /admin/api/scale/detect` · `/scale/capture` |
| `POST /admin/api/imprimante/test?quoi=mire\|reglette` | `POST /admin/api/printer/test?what=alignment\|ruler` |
| `GET /admin/api/etiquette/apercu.png?gabarit=…&demo=1&dual=1` | `GET /admin/api/label/preview.png?template=…&demo=1&dual=1` |
| `POST /admin/api/catalogue/recharger` · `/catalogue/oublier-quarantaine` | `POST /admin/api/catalog/reload` · `/catalog/forget-quarantine` |
| `POST /admin/api/produits/{id}/decision {proposer, poids_min_g, motif}` | `POST /admin/api/products/{id}/decision {offered, min_weight_g, reason}` |
| `GET /admin/api/journal` · `/journal/export.csv` · `/admin/api/technique` · `/admin/api/imports` | `GET /admin/api/journal` · `/journal/export.csv` · `/admin/api/technical` · `/admin/api/imports` |
| `POST /admin/api/rejouer` | `POST /admin/api/replay` |
| (supprimée) `POST /admin/api/redemarrer` | (supprimée) `POST /admin/api/restart` |
| sous-commandes : `serve`, `kiosk`, `doctor`, `capture`, `etiquette`, `rejouer` | `serve`, `kiosk`, `doctor`, `capture`, `label`, `replay` |
| sous-commandes : `config valider\|exporter\|importer\|motdepasse` | `config validate\|export\|import\|password` |
| sous-commandes de démonstration : `codebarre`, `prix` | `barcode`, `price` |
| drapeaux : `--donnees`, `--duree`, `--gabarit`, `--poids`, `--pu`, `--grille` | `--data`, `--duration`, `--template`, `--weight`, `--unit-price`, `--tiers` |
| drapeaux de test : `--balance rejeu --imprimante apercu` | `--scale replay --printer preview` |
| variables d'environnement : `BALANCE_CONFIG`, `BALANCE_DONNEES` | `OPENSCALE_CONFIG`, `OPENSCALE_DATA` |
| condition de gabarit : `when: "multi_tarif"` | `when: "multi_tier"` |
| champs de gabarit : `gras_auto`, `troncature_assumee`, `module_milli_dots`, `hri_hauteur_um`, `hauteur_barres_um`, `descente_gardes_um`, `decalage_x/y`, `corps_um` | `auto_bold`, `truncation_accepted`, `module_milli_dots`, `hri_height_um`, `bar_height_um`, `guard_descent_um`, `offset_x/y`, `font_size_um` |
| `media{largeur_um, hauteur_um, dots_par_mm}` | `media{width_um, height_um, dots_per_mm}` |

---

## Identifiants nés de l'implémentation — L1, L2, L3

Les tables précédentes traduisent un vocabulaire français préexistant. Les identifiants
recensés ici n'en ont pas : ils sont apparus **pendant** l'écriture du code, là où le
cahier des charges nommait un comportement sans nommer le type qui le porte. Leur inventer
une colonne « Actuel (FR) » serait fabriquer une source ; elle est donc remplacée par ce
que l'identifiant désigne, relevé sur son `godoc` et non deviné.

Ils sont ici parce que la **règle de complétude** l'exige, et ils obéissent aux mêmes
conventions que le reste : anglais, `PascalCase` exporté, `Err` + condition pour les
sentinelles, familles de constantes préfixées par leur type, unités portées par le nom.

> **Périmètre.** L1, L2, L3 — noyau métier, stockage, balance — plus ce que L8 a ajouté à
> `internal/platform` (service, notification systemd, inhibition de veille) et le paquet
> `internal/kiosk` entier. Les paquets `printing`, `catalog` et `web` n'ont encore livré
> que ce qui est listé plus bas ; le reste de leurs identifiants sera ajouté au fil des
> lots, selon la même règle.

> **Ce qui n'est PAS répété ici.** Un verbe déjà traduit une fois dans
> [Fonctions et méthodes](#fonctions-et-méthodes) vaut pour tous ses porteurs :
> `Start`, `Close`, `Descriptor`, `Print`, `SelfTest`, `Status`, `Name`, `Write`, `Query`,
> `Describe`, `Next`, `Acknowledge`, `Now`, `After`, `Ticker`, `Technical`, `Feed`,
> `Reset`. `serial.Scale`, `absent.Scale`, `replay.Scale` et `SystemClock` les implémentent
> sans rien renommer — c'est même ce qui rend les drivers interchangeables, et la suite
> `conformance.Suite` le vérifie. De même, `TB.Cleanup` / `Fatalf` / `Helper` / `TempDir`
> est l'API de `*testing.T`, et `(ErrUnknownFont).Error` celle de l'interface `error` : ces
> noms ne sont pas les nôtres et ne se traduisent pas.

### `internal/domain` — types

| Identifiant | Ce qu'il désigne |
| --- | --- |
| `StationConfig` · `NetworkConfig` · `UIConfig` · `ScaleConfig` · `PrinterConfig` · `CatalogConfig` · `ImagesConfig` · `JournalConfig` · `AdminConfig` · `MaintenanceConfig` · `BarcodeConfig` | Les blocs de premier niveau de `config.json`, un type Go par bloc. Le suffixe `Config` est **obligatoire** : sans lui, `Station`, `Network` ou `Journal` entreraient en collision avec le poste de §11.4, la couche réseau et le journal des pesées. |
| `DriverOptions` | La moitié spécifique au driver d'un bloc matériel ou catalogue (`scale.options`, `printer.options`, `catalog.options`) — un sac de clés que le noyau ne connaît pas et ne veut pas connaître. |
| `OptionSchema` | La déclaration d'UNE option d'un driver : sa clé, sa forme, ses bornes. C'est ce qui permet à un driver enfichable d'être validé sans que le noyau connaisse ses réglages. |
| `OptionKind` | La forme qu'une option accepte. Constantes `OptionText`, `OptionInt`, `OptionBool`, `OptionEnum`, `OptionRatio`, `OptionURL`, `OptionHostPort`, `OptionGroup`. |
| `DriverDescriptor` | Ce que la validation d'une configuration a besoin de savoir d'un driver : son `ID`, son `Label` français, son schéma d'options. Distinct de `ScaleDescriptor` et `PrinterDescriptor`, qui décrivent une INSTANCE en service. |
| `PathChecker` | Les questions qu'une validation pure ne peut pas trancher : que peut faire le service de ce chemin ? Interface de deux méthodes, `Readable` et `Droppable`, injectée pour que `Validate` reste sans I/O. Une valeur `nil` est un état légitime : « on ne peut pas savoir ». |
| `Preparation` | Ce que le chemin de calcul unique répond d'une pesée : `Label`, `Priced`, `Diagnostics`, `Refusal`. |
| `PrepareInput` | Tout ce que `Prepare` a le droit de lire. Même intention que `CheckInput` pour `Evaluate` : la fonction ne va rien chercher. |
| `Severity` | Qui doit agir sur un diagnostic. Constantes `Info` (affiché, imprime quand même) et `Blocking` (l'étiquette s'arrête). |
| `ScaleStatus` | Ce qu'un driver dit de sa liaison — le type qui porte `StatusConnected` / `StatusDisconnected`. |
| `PrinterCapabilities` | Voir la table des types : homonyme scindé de `Capabilities`, qui ne décrit plus que la balance. |
| `SymbolGeometry` | Où et de quelle taille le symbole EAN-13 est tracé, **en micromètres**, dans le gabarit. À ne pas confondre avec `SymbolOptions`, qui est l'entrée du rastériseur en dots (L4). |
| `Import` | L'enregistrement d'UN import de catalogue — la ligne de la table `imports`. |
| `Finding` | Le type annoncé par la décision « signalement » existe désormais : une chose qu'un import a à dire sur UNE ligne. |
| `Image` | La photo d'un produit, adressée par son contenu — la ligne de la table `images`. |
| `LocalDecision` | Le jugement humain sur un produit, distinct de la qualification calculée — la ligne de `local_decisions`. |
| `QuarantineEntry` | Un contenu de fichier qui a échoué, et combien de fois — la ligne de `quarantine`. |
| `WeighingLine` | Ce qu'un tarif a coûté sur une pesée — la ligne de `weighing_lines`. Distinct de `PriceLine`, qui est le calcul ; `WeighingLine` est ce qui en est **archivé**. |

### `internal/domain` — familles de constantes

| Famille | Membres | Ce qu'elle nomme |
| --- | --- | --- |
| `Code*` | `CodeOverload`, `CodeMeasurementExpired`, `CodeBasketMissing`, `CodeScaleEmpty`, `CodeTareRequired`, `CodeWeightUnstable`, `CodeTareInvalid`, `CodeWeightTooLow`, `CodeWeightTooHigh`, `CodeUnitsOutOfRange`, `CodeAmountOutOfCapacity`, `CodeZeroPrice`, `CodeLightProductAllowed`, `CodeProductWithdrawn` | Les 14 garde-fous, **dans l'ordre d'évaluation**. Leur VALEUR est le code `SCREAMING_SNAKE_CASE` déjà figé plus haut (`"OVERLOAD"`, …) ; le préfixe `Code` est ce qui les rend cherchables et les distingue des états. |
| `Finding*` | `FindingUnreadableRow`, `FindingPriceUnreadable`, `FindingZeroPrice`, `FindingInvalidBarcode`, `FindingReservedZoneNotEmpty`, `FindingNoBarcode`, `FindingPrepackagedProduct`, `FindingInternalCodeNotWeighable`, `FindingUnknownUnit`, `FindingUnitMismatch`, `FindingUnexpectedHeader`, `FindingUnknownCategory`, `FindingImageInvalid`, `FindingImageTooLarge`, `FindingMissingGlyph` | Les 15 codes de signalement d'import. Même règle : le préfixe est l'identifiant, la valeur est le code déjà figé. |
| `Field*` | `FieldProductName`, `FieldQuantity`, `FieldPrimaryUnitPrice`, `FieldSecondaryTotalPrice`, `FieldPrimaryTotalPrice`, `FieldBarcode` | La liste **fermée** des `FieldID` qu'un gabarit peut placer. Fermée, parce qu'un gabarit d'exploitant ne doit pas pouvoir inventer un champ que le moteur ne sait pas remplir. |
| `Align*` | `AlignLeft`, `AlignRight` | Alignement du texte dans la boîte d'un élément. |
| `When*` | `WhenAlways` (chaîne vide), `WhenMultiTier` | La condition d'affichage d'un élément — la valeur de `when:`. |
| `Result*` | `ResultSent`, `ResultRejected`, `ResultFailed`, `ResultReprint` | Fin d'une pesée : la valeur de `weighings.result`. |
| `Import*` | `ImportApplied`, `ImportUnchanged`, `ImportRejected`, `ImportFailed` | Fin d'un import : la valeur de `imports.result`. |
| `Source*` | `SourceScale`, `SourceManual`, `SourceReplay` | Provenance d'un poids : la valeur de `weighings.source`. |
| `CatalogSource*` | `CatalogSourceLocalDrop`, `CatalogSourceWebDAV`, `CatalogSourceManual` | Provenance d'un catalogue : la valeur de `imports.source`. |
| `Issue*` | `IssueAnomaly`, `IssueInfo` | Gravité d'un signalement : la valeur de `findings.issue`. |
| `Image*` | `ImageJPEG`, `ImagePNG`, `ImageGIF`, `ImageBMP` | Les quatre formats d'image acceptés : la valeur de `images.format`. |
| `ImageSource*` | `ImageSourceCSV`, `ImageSourceDirectory`, `ImageSourceNone` | La valeur de `catalog.images.source`. |
| `Level*` | `LevelInfo`, `LevelWarn`, `LevelError` | Niveau d'un message de bandeau et d'un événement technique. **Volontairement dupliqué** avec `internal/store` : le noyau ne peut pas importer le stockage (§5.2), les deux listes s'accordent par revue et par test, jamais par import. |
| `Printer*` | `PrinterRaster`, `PrinterSBPL`, `PrinterPreview` | Les trois drivers d'impression : la valeur de `printer.type`. |
| `Transport*` | `TransportWinspool`, `TransportDevfile`, `TransportTCP`, `TransportFile` | Les quatre transports d'octets : la valeur de `printer.options.transport`. |
| `OnTimeout*` | `OnTimeoutWarnAndPrint`, `OnTimeoutReject`, `OnTimeoutManualEntry` | Ce que fait le mode bloquant quand il expire : la valeur de `stability.on_timeout`. |
| `Option*` | voir `OptionKind` | La forme d'une option de driver. |

### `internal/domain` — constantes isolées

| Identifiant | Ce qu'elle vaut, et pourquoi elle existe |
| --- | --- |
| `MinModuleMilliDots` · `MaxModuleMilliDots` | Les bornes du module du code-barres, en milli-dots — la règle dure n° 9 de §7.5. |
| `MinFontSizeUM` | Le plancher **dur** de corps de texte, 14,4 dots à 8 dots/mm. Ce n'est pas un réglage : en dessous, la tête thermique ne rend plus le glyphe. |
| `InkedWidthDots` · `InkedHeightDots` | La géométrie **encrée** de l'étiquette de production, en dots à 8 dots/mm. C'est contre elle que la règle dure n° 3 mesure un gabarit — le contenu encré, jamais la boîte du contrôle Access (§7.2, ADR-029). |
| `SuccessMessageDuration` · `RejectMessageDuration` | Combien de temps un message de bandeau survit quand rien ne le retire. Constantes du CODE et non réglages : elles remplacent `success_delay_ms` et `reject_delay_ms` (ADR-025). |
| `DefaultTemplateName` | Le gabarit que `config-lacagette.json` sélectionne. |

### `internal/domain` — fonctions et méthodes

| Identifiant | Ce qu'il fait |
| --- | --- |
| `ParseCents` | Convertit un montant décimal en centimes entiers **sans jamais passer par un flottant**. |
| `(Cents).Euro` · `(Grams).Kilos` | Formatent un montant et une masse comme l'étiquette les imprime : virgule décimale française. |
| `ErrPriceFormat` | Sentinelle de `ParseCents` : ce n'est pas un prix exploitable. |
| `PlanFor` | Rend le plan de numérotation qui gouverne un gabarit d'EAN-13. |
| `RequireMode` | Rend `ErrPrefixModeMismatch` quand le mode de vente qu'un appelant croit contredit celui du préfixe. Le préfixe fait foi. |
| `NewCatalog` | Fige un instantané immuable à partir des lignes qu'un import a produites. |
| `(Catalog).ByID` · `Products` · `Categories` · `Len` · `WeighableCount` | Lecture de l'instantané. Toutes rendent des **copies** : l'immuabilité est tenue par la méthode, pas par la politesse de l'appelant. |
| `(Product).String` · `(Qualification).String` · `(SaleMode).String` · `(Stability).String` · `(State).String` · `(Severity).String` · `(ScaleStatus).String` · `(RoundingPolicy).String` · `(OptionKind).String` · `(Fault).String` · `(Duration).String` | Une seule orthographe par valeur, partagée par le journal, la base et l'écran. Là où la valeur est stockée, `String` rend **exactement** ce que la colonne SQL accepte. |
| `SingleTierRules` · `LaCagetteRules` | Les deux grilles livrées : mono-tarif du profil neutre, et la grille établie par les preuves (A7). |
| `ErrProductNotWeighable` | Sentinelle de `Prepare` : un produit que la grille n'aurait jamais dû proposer. |
| `Evaluate` → `FirstBlocking` | `Evaluate` rend TOUS les diagnostics ; `FirstBlocking` rend celui sur lequel la machine à états agit, ou `nil`. |
| `(Diagnostic).Blocks` | Ce diagnostic arrête-t-il l'étiquette ? |
| `(CheckInput).Net` · `(Measurement).Net` | La masse réellement vendue : brut moins tare. |
| `DefaultMessage` · `MessagePlaceholders` · `CheckMessage` | Le libellé français livré d'un code, les interpolations qu'il admet, et la validation d'un libellé qu'un exploitant a réécrit. |
| `DefaultStabilityPolicy` | La politique livrée : `advisory` (ADR-005). |
| `NewWeightLatch` · `(WeightLatch).Reset` | Construction du figeur, et oubli de son ancre quand la balance est rouverte. |
| `(RateMeter).Reset` · `Observations` · `RateIsTooSlow` | Idem pour le cadencemètre, plus le nombre d'intervalles connus et **l'unique** condition d'alerte partagée par le tableau de bord et le journal. |
| `IdenticalTemplate` · `IntegerModuleTemplate` · `NeutralSingleTemplate` · `ShippedTemplates` | Les trois gabarits livrés, et leur table par nom. `IdenticalTemplate` est **la source de vérité de la géométrie** — pas les tables de §7.2, qui décrivent l'existant et sont amendées par ADR-029. |
| `(Media).MilliDots` | Convertit une longueur en micromètres en milli-dots entiers. |
| `(SymbolGeometry).HeightUM` · `TotalWidthMilliDots` | Où le bloc du symbole finit, et son hors-tout zones de silence comprises. Une seule définition de chacun, et c'est ici. |
| `(Template).MaxOffsetDots` | Jusqu'où le réglage de décalage d'un bénévole peut aller dans chaque direction. |
| `(Element).Active` · `Right` · `Bottom` | L'élément est-il dessiné pour une grille de N tarifs, et où finit sa boîte. |
| `CanonicalJSON` | Le JSON canonique d'une valeur : clés triées, sans espace. C'est ce qui rend une empreinte reproductible. |
| `(Config).Retired` | Les chemins pointés des clés **retirées** que le fichier portait — pour dire « cette clé n'existe plus » au lieu de l'ignorer en silence. |
| `CheckPrice` | La faute que porte un prix livré dans un fichier de configuration (contrôle 43). |
| `RoundingSpellings` | Les trois orthographes admissibles d'une politique d'arrondi. |
| `(Registries).ScaleTypes` · `PrinterTypes` · `TransportNames` · `CatalogSourceNames` · `TemplateNames` · `Template` | Ce qu'un bénévole a le droit de choisir, dans un ordre stable. C'est le registre qui décide, jamais une liste en dur dans un écran. |
| `(DriverOptions).Has` · `Text` · `Int` · `Bool` · `Ratio` · `Group` · `Keys` | Lecture typée d'une option de driver, chacune rendant aussi « présente et de la bonne forme ? ». |
| `(Weighing).Line` | La ligne archivée d'un code de tarif, ou `nil`. Pendant de `(Label).Find` côté calcul. |
| `(Decoder).Feed` · `(Decoder).Reset` | Les deux méthodes du seul point de variation par modèle d'une balance série : livrer des octets, et oublier ce qui restait quand le port est rouvert. Homonymes assumés de `(Accumulator).Feed` / `Reset` — c'est le même contrat à un étage de plus. |
| `(PathChecker).Readable` · `(PathChecker).Droppable` | Ce chemin est-il lisible ? Le service peut-il y créer **et supprimer** un fichier ? Toute l'I/O dont `Validate` a besoin, et la raison pour laquelle elle est injectée plutôt qu'appelée. Deux questions et non une : l'acquittement d'un import **est** une suppression (ADR-004), donc un répertoire qu'on ne peut que lire reboucle indéfiniment. |
| `MarshalJSON` / `UnmarshalJSON` sur `Config`, `Category`, `WeighingLimits`, `RoundingPolicy`, `Duration` | La sérialisation de `config.json`. `UnmarshalJSON` **conserve ce que l'objet ne nomme pas** : un bloc partiellement écrit ne remet pas les autres clés à zéro. |

### `internal/domain/frame`

| Identifiant | Ce qu'il désigne |
| --- | --- |
| `MaxBuffer` | Combien d'octets l'accumulateur garde avant de se resynchroniser. |
| `(Accumulator).Reset` | Jette le tampon quand le port est rouvert : une demi-trame d'avant la coupure ne doit pas se recoller à une demi-trame d'après. |
| `(Accumulator).Pending` | Combien d'octets attendent la fin de leur trame — l'observation qui rend la resynchronisation testable. |
| `Accumulator.Resyncs` | Combien de fois l'accumulateur a jeté un tampon saturé. Compteur exporté parce que c'est le chiffre que le corpus vivant surveille : une resynchronisation est normale, une cadence de resynchronisations ne l'est pas. |

### `internal/scale` — le registre

| Identifiant | Ce qu'il désigne |
| --- | --- |
| `Registry` · `NewRegistry` | L'ensemble des drivers de balance avec lesquels ce binaire a été construit. |
| `(Registry).Register` | Ajoute un driver. C'est **la ligne unique** promise par §5.2. |
| `(Registry).New` · `Descriptors` | Instancie le driver que nomme `scale.type` ; liste ce dont l'écran d'administration a besoin pour sa liste déroulante. |
| `Driver` | Un modèle de balance tel que le registre le connaît : `Driver.Descriptor` (identité et capacités), `Driver.Options` (schéma d'options), `Driver.New` (la fabrique). |
| `Factory` | Construit une instance de driver à partir des options que porte une configuration. |
| `ErrUnknownDriver` | Un `scale.type` auquel aucun driver de ce binaire ne répond. |

### `internal/scale/serial`, `gramxfoc`, `absent`, `replay`, `conformance`

| Identifiant | Ce qu'il désigne |
| --- | --- |
| `serial.Opener` · `serial.OpenSystemPort` | L'ouverture du port, isolée derrière une fonction injectable — et son implémentation réelle, `go.bug.st/serial`, pur Go. C'est ce qui rend la boucle testable **sans matériel**. |
| `serial.Options.Open` · `Options.Clock` | Les deux points d'injection de `Options` : par où l'on ouvre, et quelle horloge mesure les délais. |
| `serial.ParseOptions` · `serial.OptionSchema` | Traduit les options de `config.json` en réglages de liaison ; déclare les sept options d'une liaison série pour que la validation les connaisse sans que le noyau les code en dur. |
| `serial.ErrAlreadyStarted` · `absent.ErrAlreadyStarted` · `replay.ErrAlreadyStarted` | Une instance de driver qu'on a tenté de démarrer deux fois. Sentinelle par paquet : chaque driver répond de lui-même. |
| `gramxfoc.IDRS` · `gramxfoc.IDPlus` | Les deux valeurs de `scale.type` de la famille GRAM : `gram-xfoc-rs` et `gram-xfoc-plus`. |
| `gramxfoc.Descriptor` · `gramxfoc.Drivers` | L'identité d'un modèle GRAM, et les deux entrées de registre de la famille. |
| `absent.ID` · `absent.Label` | Le nom de la source de poids vide partout où une source est nommée, et **son libellé français** pour le bénévole. |
| `absent.New` · `absent.Scale` · `absent.ErrNoScale` | La source vide de la saisie manuelle, et la cause portée par chacun de ses événements. |
| `replay.ID` · `replay.Label` · `replay.DefaultCadence` | Idem pour le rejeu, plus le délai donné aux enregistrements qui n'en déclarent pas. |
| `replay.Source` | Ce qu'il faut rejouer et à quelle vitesse : `Name`, `Frames`, `Decoder`, `Clock`, `Cadence`, `Speed`, `Repeat`. |
| `replay.Script` · `replay.Step` · `replay.Parse` | Une capture transformée en pas jouables : `Script.Steps`. Un pas = attendre `Step.Delay`, puis livrer `Step.Raw` au décodeur. |
| `(replay.Script).Pace` | L'intervalle que le script **déclare** : la MÉDIANE de ses pas. C'est ce que `openscale replay` annonce. |
| `(replay.Scale).Script` | La capture analysée, pour que la commande dise ce qu'elle va jouer avant de le jouer. |
| `replay.ErrEmptyCapture` · `ErrNoClock` · `ErrScriptExhausted` | Une capture sans aucun enregistrement ; un rejeu câblé sans l'horloge injectée ; un rejeu arrivé au bout. |
| `conformance.Subject` | Le driver soumis à la suite : `Name`, `New`, `Frames`, `Feed`, `Patience`, `Unstartable`, `RequireDisconnectCause`. |
| `conformance.MaxExpressibleGrams` | La masse la plus lourde que la grammaire de trames de §9.2 sait exprimer. Borne du corpus, pas du métier — `MaxWeight` reste la borne métier. |

### `internal/store`

| Identifiant | Ce qu'il désigne |
| --- | --- |
| `Clock` | L'interface d'horloge **du paquet `store`**, réduite à `Now`. Redéclarée côté consommateur, comme `ports.Clock` : le stockage n'a besoin ni de `After` ni de `Ticker`. |
| `Batch` | Un catalogue entier tel qu'un import l'a produit : `Import`, `Products`, `Categories`, `Images`, `Findings`. Homonyme volontaire de `ports.Batch`, qui est le même objet **avant** d'entrer en base. |
| `ProductRow` | `ProductRow.Product` plus les trois faits de stockage que le type du noyau ne porte pas : `SeenAt`, `WithdrawnAt`, `LastImportID`. |
| `ImportOutcome` | Ce qu'un import a fait au catalogue : `ImportID`, `Inserted`, `Updated`, `Withdrawn`. |
| `Retention` · `DefaultRetention` | La politique de purge du journal — `MaxRows`, `MaxDays`, `MaxTechnical` — nommée d'après le bloc de configuration `journal`, et la politique livrée. |
| `JournalFilter` · `TechnicalFilter` | Le filtre d'une page de journal (`Since`, `Until`, `Result`, `ProductID`, `Limit`, `Offset`) ou de journal technique (`Since`, `Until`, `Level`, `Source`, `Code`, `Limit`, `Offset`). Un filtre nul signifie « la page la plus récente ». |
| `TechnicalEntry` | Une ligne du journal technique roulant : `ID`, `OccurredAt`, `Level`, `Source`, `Code`, `Message`, `Detail`. |
| `LogSource*` | `LogSourceScale`, `LogSourcePrinter`, `LogSourceCatalog`, `LogSourceUI`, `LogSourceConfig`, `LogSourceHTTP`, `LogSourceSystem` — les valeurs que `technical_log.source` accepte. Le préfixe `LogSource` évite la collision avec les `Source*` du noyau, qui nomment la provenance d'un POIDS. |
| `Level*` | `LevelDebug`, `LevelInfo`, `LevelWarn`, `LevelError`, `LevelCritical` — les valeurs de `technical_log.level`. La liste du stockage est plus longue que celle du noyau, qui n'a pas à connaître `debug` ni `critical`. |
| `Meta*` | `MetaLabelsSinceRoll` (déjà figée comme clé `labels_since_roll`), `MetaLastIntegrityCheck`. Les clés nommées de la table `meta`. |
| `(DB).Meta` · `MetaAll` · `SetMeta` · `AddMeta` | Lecture, écriture et incrément d'une clé `meta`. `AddMeta` existe pour que le compteur de rouleau ne se lise pas puis s'écrive en deux temps. |
| `(DB).ReplaceCatalog` · `LoadCatalog` · `AllProducts` · `Product` · `Image` | Le remplacement **transactionnel** d'un catalogue, et les lectures qui en découlent. |
| `(DB).RecordWeighing` · `Weighings` · `WeighingByJobID` · `CountWeighings` · `PurgeWeighings` | Le journal des pesées. |
| `(DB).RecordTechnical` · `TechnicalEntries` · `CountTechnical` · `PurgeTechnical` | Le journal technique. |
| `(DB).RecordImport` · `Imports` · `Findings` · `LastAppliedImport` | L'historique d'imports, en ajout seul (ADR-015). |
| `(DB).RecordContentFailure` · `Quarantine` · `QuarantineEntries` · `ForgetQuarantine` | La quarantaine, séparée de l'historique : **seul** un échec de CONTENU y compte. |
| `(DB).SaveDecision` · `Decision` · `ClearDecision` · `LocalDecisions` | La décision humaine « ne plus proposer ce produit » (ADR-017). |
| `(DB).Backup` · `Vacuum` · `IntegrityCheck` · `SchemaVersion` · `Path` · `Close` | L'entretien du fichier et ce qu'un paquet de diagnostic en extrait. |
| `(DB).Retention` · `SetRetention` | La politique en vigueur, et son remplacement à chaud (§11.4). |
| `ErrNotFound` | Une ligne adressée par sa clé n'existe pas. Rendue **à la place de** `sql.ErrNoRows`, pour qu'aucun appelant n'ait à importer `database/sql`. |
| `ErrPurge` | Un échec de purge survenu **après** qu'une pesée a été validée — le service se dégrade, il ne tombe pas (ADR-013). |
| `TB` · `OpenTest` · `FixedClock` · `TestEpoch` | Le double d'ouverture de base pour les tests : la part de `*testing.T` dont `OpenTest` a besoin, une base migrée en répertoire temporaire, une horloge arrêtée, et l'instant auquel elle est arrêtée. |

### `internal/station/ports`

| Identifiant | Ce qu'il désigne |
| --- | --- |
| `PrinterHealth` | À quel point le statut d'une imprimante mérite d'être cru. Constantes `PrinterReady`, `PrinterConsumable`, `PrinterFaulted`, `PrinterUnknown`. |
| `PrinterUnknown` | La réponse **honnête** d'un transport unidirectionnel : on ne sait pas. Elle existe pour qu'aucun code n'ait à mentir « prête » faute de canal de retour. |
| `NopTechnicalLog` | Journal technique qui jette tout. Il existe pour qu'un driver sous test ne déréférence jamais un `nil`, et pour qu'un appelant n'ait pas à en fabriquer un. |
| `Batch` | Un catalogue entier prêt à remplacer celui en service : `ID`, `Source`, `FileName`, `Bytes`, `RowsRead`, `UnreadableRows`, `Products`, `Images`, `Findings`. |
| `BatchResult` | Ce que le poste a fait d'un lot, et ce que `Acknowledge` reçoit : `Result`, `Code`, `Reason`. |
| `PrintJob` | Une étiquette à imprimer : `Label`, `Template`, `Copies`, `Locale`. |
| `PrintReceipt` | Un travail **remis à un transport** : `JobID`, `Bytes`, `Duration`. Jamais « imprimé » — voir « Reçu » → `PrintReceipt`. |
| `PrinterStatus` | Ce que l'appareil dit de lui-même : `Health`, `Detail`, `PendingJobs`, `Raw`. |

### `internal/platform` et `internal/fake`

| Identifiant | Ce qu'il désigne |
| --- | --- |
| `platform.SystemClock` · `NewSystemClock` | La seule implémentation réelle de `ports.Clock`, et **le seul endroit du dépôt où `time.Now()` a le droit d'être appelé**. `go run ./tools/boundary` le vérifie. |
| `(fake.Clock).Set` · `Now` · `After` · `Ticker` · `Pending` | L'horloge des tests : elle n'avance que quand un test le lui dit. `Pending` compte les attentes enregistrées — c'est ce qui permet d'assurer qu'un arrêt n'a laissé fuir aucun timer. |
| `platform.ServiceName` | Le nom sous lequel le SCM enregistre le poste : `OpenScale`. Épelé UNE fois, du côté Go comme du côté PowerShell (`$script:ServiceName`), et un test de `deploy/` compare les deux. |
| `platform.ServiceSpec` · `ServiceState` | Ce qu'on dit au gestionnaire de services une fois, à l'installation, et ce qu'il en dit ensuite. `ServiceSpec.StopBudget` est le budget d'arrêt que le superviseur doit accorder — il vient de `station.ShutdownBudget`, jamais d'un nombre écrit à côté. |
| `platform.InstallService` · `RemoveService` · `StartService` · `StopService` · `QueryService` | Les cinq gestes de `openscale service …`. `QueryService` ouvre le SCM en LECTURE SEULE : lire un état n'est pas administrer, et un `status` qui exigerait l'élévation répondrait « accès refusé » au bénévole qui suit TROUBLESHOOTING.md. |
| `platform.ErrServiceUnsupported` | Sentinelle du jumeau non-Windows : sous Linux, ce travail est celui de l'unité systemd, et le message le dit au lieu d'annoncer un refus du SCM. |
| `platform.StartedByServiceManager` · `RunAsService` | Ce qui permet à `openscale serve` d'être la MÊME sous-commande tapée dans un terminal et lancée par le SCM. Un binaire qui n'aurait parlé le protocole que derrière un drapeau supplémentaire ne démarrerait pas pour qui l'oublie. |
| `platform.ServiceNotifier` · `(ServiceNotifier).Ready` · `Alive` · `Stopping` · `Status` · `WatchdogInterval` | Le `Type=notify` de §15.3 : trois phrases dites à systemd sur une socket datagramme, et la période que l'unité attend. Le notificateur d'un poste que personne n'écoute — tout poste Windows, tout poste lancé à la main — répond sans échouer. |
| `platform.KeepAwake` | `SetThreadExecutionState`, la ceinture par-dessus les bretelles de `powercfg` (§15.2). Le jumeau Linux ne refuse pas : sur un poste sous `cage`, il n'y a aucun économiseur d'écran à inhiber. |
| `station.ShutdownBudget` | La somme des attentes BORNÉES de l'arrêt de §13.4. Elle existe pour que ni `TimeoutStopSec` ni le `WaitHint` du SCM ne recopient ces durées : §13.4 raconte ce que coûte un budget écrit à côté du code qu'il devait couvrir. |

### `internal/kiosk`

| Identifiant | Ce qu'il désigne |
| --- | --- |
| `Browser` · `Find` · `WindowsCandidates` · `LinuxCandidates` · `LookBrowser` | Le navigateur du kiosque et l'ordre de recherche de §15.2 — `msedge.exe`, `chrome.exe`, `chromium.exe`. `LookBrowser` cherche aussi dans les répertoires de programmes, là où Edge et Chrome se cachent du PATH d'un compte de service. |
| `Arguments` · `(Browser).IsEdge` | La ligne de commande de §15.2, dont `--edge-kiosk-type=fullscreen` qui n'existe que chez Edge. |
| `Supervisor` · `Options` · `New` · `(Supervisor).Run` | Le superviseur : il relance le navigateur en moins de deux secondes et n'a aucun autre pouvoir. `Options.Alive` interroge `/healthz` et JAMAIS `/readyz`. |
| `Process` · `ExecLauncher` | Le navigateur vu d'ici — quelque chose qui se termine et qu'on peut terminer — et son unique implémentation réelle. L'interface existe pour que toute la supervision soit prouvable sans navigateur sur la machine. |
| `CrashCounter` · `(CrashCounter).Record` · `ShortLives` · `CrashLimit` · `ShortLife` · `CrashWindow` | La règle « au-delà de 20 morts en moins de 10 s dans l'heure » de §15.2, et les trois nombres avec lesquels elle est écrite. Une vie longue remet le compte à zéro. |
| `RescueReason` · `RescueWaiting` · `RescueCrashLoop` · `WriteRescuePage` · `RescueFileName` · `CodeCrashLoop` | Les deux pages locales : « Le poste redémarre… » pendant que le service démarre, et la page `ERR-KSK-02` quand l'affichage n'arrive pas à rester ouvert. Deux raisons distinctes parce qu'on quitte la première dès que le poste répond, jamais la seconde. |
| `AliveProbe` · `DefaultProfileDir` · `RelaunchDelay` · `AwakePeriod` · `StationRecheck` · `ProbeBudget` | La sonde de vivacité, le profil dédié effacé à chaque démarrage, et les quatre périodes de la boucle. |

### `internal/printing`

| Identifiant | Ce qu'il désigne |
| --- | --- |
| `Library` · `NewLibrary` | Les polices embarquées analysées une fois, et les faces qui en dérivent, mémoïsées. |
| `(Library).Face` · `Parsed` · `FaceCount` · `Close` | Une face à une taille donnée ; la police analysée ; combien de faces sont mémoïsées (pour qu'un test puisse le vérifier) ; la libération. |
| `Font` · `Carlito` · `DejaVuSansCondensed` | La famille avec laquelle le moteur sait rendre — la valeur de la clé `font` d'un gabarit. `Carlito` est le clone métrique de Calibri retenu par ADR-020 ; `DejaVuSansCondensed` sert les gabarits neutres et le repli. |
| `ErrUnknownFont` | Un gabarit qui nomme une police que le binaire ne porte pas. Erreur **typée** et non sentinelle, parce qu'elle doit dire LAQUELLE : `ErrUnknownFont.Font`. |

### `internal/update`

Le paquet d'ADR-040. Tout y est calculable ; la seule fonction privilégiée vit dans
`internal/platform`.

| Identifiant | Ce qu'il désigne |
| --- | --- |
| `Version` · `ParseVersion` · `(Version).Compare` · `IsPrerelease` · `String` | Un numéro de publication, ordonné. `Patch` est normalisé — `v0.1` et `0.1.0` nomment la même version, et la première publiée de ce dépôt s'appelait `v0.1`. Une préversion se classe SOUS sa propre version. |
| `ErrNotAVersion` | Un tag qui n'est pas un numéro : `banc-de-test`, `avant-migration`, et `dev` que porte un binaire compilé hors tag. |
| `Release` · `Asset` · `(Release).Asset` | Une publication et ses fichiers joints. `Tag` est le tag **tel que publié** — c'est de lui que dérive le nom des archives, jamais de `Version.String()`. `Asset.URL` est `browser_download_url` et jamais le champ `url`, qui rendrait du JSON décrivant l'archive. |
| `Source` · `GitHubSource` · `(GitHubSource).Latest` · `DefaultBaseURL` | L'interface déclarée côté consommateur, et son implémentation sur `/repos/{owner}/{repo}/releases/latest` — ce point d'entrée exclut déjà brouillons et préversions, c'est son contrat. `DefaultBaseURL` est **compilé** : ce qu'un fichier peut nommer est le dépôt, jamais l'hôte. |
| `ErrNoRelease` · `ErrUnreachable` | « ce dépôt n'a rien publié de stable » (404, **pas une panne**) contre « le serveur n'a pas pu être lu » (réseau, proxy, limite de débit). Les confondre annoncerait à un poste qu'il est à jour alors que personne n'en sait rien. |
| `Stager` · `Staged` · `(Stager).Stage` | Télécharge, compare à l'empreinte publiée, extrait. Rien n'est gardé sur un refus. L'empreinte est calculée **en écrivant**, jamais sur un tampon. |
| `ErrAssetMissing` · `ErrChecksumMismatch` · `ErrUnsafeArchive` | Pas d'archive pour cette plateforme (ou une archive qui ne porte pas ce que la bascule attend) ; l'archive ne correspond pas à son empreinte ; l'archive écrit hors de son répertoire. Le troisième n'est pas théorique : l'archive vient du réseau et un processus `LocalSystem` l'extrait. |
| `State` · `Check` · `Pending` · `Outcome` | L'état sur disque, sous `<data>/updates/`. Des fichiers et non la base : rien ici ne vaut une migration, et un poste qui ne démarre pas doit pouvoir dire ce qui lui est arrivé — c'est le moment où une base est ce sur quoi on ne peut pas compter. |
| `(State).TakeOutcome` · `LastOutcome` · `ClearPending` | Consommer le compte rendu **une seule fois** (le renommage rend l'opération idempotente : un poste redémarré trois fois ne journalise pas trois fois la même bascule), servir le dernier à l'écran, abandonner une bascule qui ne rendra jamais compte. |
| `SwapBudget` · `(Pending).Stale` | Quinze minutes, au-delà desquelles une bascule est réputée n'avoir jamais démarré. Sans cette borne, un `Start()` qui rend `nil` sans rien lancer murait le poste : `pending.json` écrit, aucun `outcome.json` à venir, et `ErrAlreadyRunning` opposé à toute mise à jour ultérieure. Un instant de départ **nul** compte comme périmé. |
| `StatusSucceeded` · `StatusRolledBack` · `StatusRolledBackUnhealthy` · `StatusNotStarted` | Les quatre issues qu'`update.ps1` écrit, et que l'écran dit en quatre phrases différentes. |
| `Service` · `Paths` · `Status` · `(Service).Check` · `Status` · `Apply` | L'orchestration. `Apply` écrit `pending.json` **avant** de rendre la main : le script arrête le service, donc rien d'écrit après ne le serait jamais. `Status` lit le disque et ne sonde pas — la page doit se dessiner tout de suite. |
| `Guard` | « le poste peut-il tomber ? », posée au Hub. Déclarée ici, côté consommateur ; `*station.Hub` la satisfait. |
| `ErrVersionMoved` · `ErrBusy` · `ErrAlreadyRunning` · `BusyError` | Trois refus qui ne se confondent pas. `BusyError` est un **type** et non un message formaté, parce que la phrase remonte telle quelle jusqu'à l'écran : la retrouver en coupant un préfixe casserait à la première reformulation. |
| `platform.UpdateSpec` · `platform.ApplyUpdate` · `platform.ErrUpdateUnsupported` | La seule fonction privilégiée. Les chemins sont **absolus** et viennent du processus qui tourne, jamais des défauts du script — qui pointent `Program Files`. |
| `station.Poller` · `(Station).runUpdateWorker` | Le sondage quotidien, sur l'horloge injectée. `Poller` rend une **chaîne** et ne nomme aucun type d'`internal/update` : la station n'a pas à savoir ce qu'est une publication. |
| `domain.UpdateConfig` · `DefaultUpdateRepository` | Le bloc `update` du fichier. Un couple `owner/repo` et jamais une URL — c'est le seul champ qui désigne d'où viendra du code privilégié. L'absence de la clé est **légale** et vaut le défaut. |

### Codes `ERR-xxx-nn` réellement alloués par le code

La table de correspondance `BAL→SCL`, `IMP→PRN`, `BDD→DB`, `IHM→UI`, `KIO→KSK` reste celle
des « Décisions sur les termes délicats ». Ce tableau-ci recense les codes **qu'un binaire
émet aujourd'hui**, avec le message que lit un bénévole — identifiant anglais, contenu
français.

| Code | Où il est émis | Message affiché ou journalisé |
| --- | --- | --- |
| `ERR-SCL-01` | `serial` — `codeLinkLost` | « La balance ne répond plus. » — le port était ouvert et répondait, il s'est tu. |
| `ERR-SCL-02` | `domain/machine.go` — perte de balance vue par le Hub | Bandeau : « Le poids n'est plus disponible. Vous pouvez saisir le poids à la main. » · Journal : « La balance ne répond plus. » |
| `ERR-SCL-03` | `serial` — `codePortUnavailable` | « Le port de la balance ne peut pas être ouvert. » — absent, occupé, accès refusé. Remède différent d'`ERR-SCL-01`, donc code différent. |
| `ERR-SCL-04` | `serial` — `codeUnusableOptions` | « Les réglages de la balance sont inutilisables. » — aucun nouvel essai ne les corrigera. |
| `ERR-SCL-05` | `serial` — `codeCloseRefused` | « Le port de la balance n'a pas pu être refermé proprement. » — conséquence propre : la réouverture d'un port EXCLUSIF peut échouer ensuite. |
| `ERR-SCL-09` | `station` — `codeManualEntryRequested` | « Le poste est passé en saisie manuelle du poids. » — **demandée** depuis l'écran de dépannage (§14.4). Code distinct d'`ERR-SCL-03` exprès : « quelqu'un l'a demandé » et « le port ne s'ouvre pas » sont les deux réponses possibles à *« pourquoi ce poste est-il en saisie manuelle ce matin ? »*, et un seul code les rendrait indiscernables dans le journal. |
| `ERR-PRN-01` | `domain/machine.go` | Bandeau : « L'imprimante ne répond pas. Prévenez un responsable. » · Journal : « Impression échouée. » |
| `ERR-CFG-01` | `domain/machine.go`, `NeutralProfile` | « Le poste ne peut pas calculer les prix (ERR-CFG-01). Prévenez un responsable. » |
| `ERR-DB-01` | `store.ErrDatabaseUnusable` | Le fichier ou son répertoire est inutilisable. Le détail français est enveloppé au cas par cas : « ouverture de … impossible », « base endommagée : … », « contrôle d'intégrité impossible ». |
| `ERR-DB-02` | `store.ErrSchemaFromNewerVersion` | « base créée par une version plus récente (schéma N, ce binaire connaît M). Mettez l'application à jour. » |
| `ERR-KSK-02` | `kiosk.CodeCrashLoop` — page de secours du superviseur | « Le poste rencontre un problème » + « l'affichage n'arrive pas à rester ouvert (N arrêts en moins de 10 secondes dans la dernière heure) ». C'est le seul code que §15.2 attribue au kiosque, et il est réservé à la BOUCLE DE PLANTAGE : la page d'attente (« Le poste redémarre… ») n'en porte aucun, parce que §15.4 n'en donne aucun à cette ligne et qu'un code inventé serait répété au téléphone comme si un binaire l'émettait. |

| `ERR-UPD-01` | `web.codeUpdateUnreachable` | « Impossible de joindre le serveur des versions. » Le réseau, un proxy qui rend du HTML, une limite de débit épuisée. **Distinct de « aucune version publiée »**, qui n'est pas une panne et répond 200 avec une phrase : les confondre annoncerait à un poste qu'il est à jour alors que personne n'en sait rien. |
| `ERR-UPD-02` | `web.codeUpdateChecksum` | « Le fichier téléchargé est abîmé. Rien n'a été installé. » L'archive ne correspond pas à l'empreinte publiée ; le staging est effacé. |
| `ERR-UPD-03` | `web.codeUpdateBusy` | Le poste est occupé. **Le message est celui du garde-fou, rendu tel quel** — le Hub sait s'il s'agit d'une pesée ou d'un catalogue en attente de mise en service, la couche HTTP ne le sait pas, et paraphraser perdrait la seule information sur laquelle on peut agir. |
| `ERR-UPD-04` | `web.codeUpdateInFlight` | « Une mise à jour est déjà en cours. » Une bascule dont le budget de quinze minutes est dépassé n'est plus « en cours » : elle est effacée, et le poste réessaie. |
| `ERR-UPD-05` | `web.codeUpdateUnsupported` | « La mise à jour depuis l'écran n'existe que sous Windows. » La route existe et refuse, plutôt que d'être absente : un 404 enverrait chercher une faute de frappe. |
| `ERR-UPD-06` | *libellé de `outcome.json`* | La bascule a échoué et la version précédente a été remise ; **le poste fonctionne**. N'appelle personne. Le code sort d'`update.ps1` (`exit 10`), non d'une constante Go. |
| `ERR-UPD-07` | *libellé de `outcome.json`* | La bascule a échoué **et le poste ne répond pas** (`exit 11`). Demande quelqu'un tout de suite : `openscale doctor`, et le chemin de la sauvegarde. |
| `ERR-UPD-08` | `web.codeUpdateNoArchive` | « Cette version ne contient pas de fichier pour ce poste. » Un fork qui a renommé ses archives, ou une publication sans `SHA256SUMS-archives.txt`. |
| `ERR-UPD-09` | `web.codeUpdateMoved` | « Une autre version est parue depuis l'affichage de cette page. Rechargez-la. » **Deux codes et non un avec `-03`** : « attendez un instant » et « rechargez la page » ne sont pas la même instruction. |

> **Codes cités mais pas encore alloués.** `ERR-SCL-08` (poignée déjà relâchée, journalisée
> par le Hub), `ERR-CAT-03` et `ERR-CAT-05` (échec de contenu, fichier lu mais non
> supprimé) apparaissent dans des commentaires et des tests de L2/L3, portés par la couche
> qui les émettra en L6/L7. Ils ne sont **pas** des constantes du code aujourd'hui. Le
> numéro leur est réservé ; le libellé viendra avec l'émetteur.

---

## Décisions sur les termes délicats

Chaque décision ci-dessous a été arbitrée une fois pour toutes. Les alternatives « ÉCARTÉES »
ne doivent pas être réintroduites, même localement.

### « noyau » → `domain`

Paquet `internal/domain`. **ÉCARTÉS** : `core` (vague, employé pour tout et n'importe quoi),
`kernel` (connote un noyau d'OS ou un noyau de calcul). `domain` dit exactement ce que le
document dit — le métier pur, testable, sans I/O — et se lit bien à l'usage : `domain.Label`,
`domain.Transition`, `domain.Prepare`.

### « poste » → `station`

Paquet `internal/station`, config `station.number`, colonne `weighings.station`. **ÉCARTÉS** :
`till` (= caisse enregistreuse : faux sens, ce poste n'encaisse rien), `terminal` (connote un
TTY/terminal texte), `workstation` (trop long, connote un PC de bureau). `station` est le mot
naturel de « weighing station » en libre-service.

### « figeur » → `WeightLatch`

Méthode `Feed`, retour `LatchState{Latched, Gross, Held}`, état `weighings.stability` inchangé.
**ÉCARTÉS** : `Freezer` (= congélateur dans une épicerie — inacceptable ici), `Stabilizer` (le
composant ne stabilise rien, il constate), `Holder`. « Latch » est le terme d'ingénierie exact
pour « verrouiller une valeur à un instant » et le verbe `latch` sert aussi au taux :
`min_latch_rate`.

### « cadencemètre » → `RateMeter`

Méthodes `Observe`, `Median`, `Expiry` ; colonne `weighings.rate_ms` ; descripteur
`NominalRate`. **ÉCARTÉS** : `Cadencemeter` (calque), `FrequencyMeter` (la grandeur mesurée est
un intervalle, pas une fréquence), `SampleRateMonitor` (trop long). `RateMeter` est court,
prononçable, cherchable.

### « gabarit » → `Template` **ou** `pattern`, jamais les deux au même endroit

Le mot a DEUX sens et ils ne doivent JAMAIS partager un nom.

1. **Gabarit d'étiquette** (mise en page, `pesee_identique`) → `Template` / `template` : type
   `domain.Template`, config `printer.template`, erreur `KindTemplate`, `weighing_identical`.
2. **Gabarit d'un EAN-13** — la référence 13 chiffres dont la charge utile est à zéro, argument
   de `Generer` → `pattern` : `Generate(pattern EAN13, payload int64, width int)`, erreur
   `ErrPatternNotZeroed`.

Traduire les deux par `template` rendrait `ErrGabaritNonNul` incompréhensible.

### « qualification » → `Qualification`

Type `Qualification` avec `Weighable` / `NotWeighable` / `Anomaly` ; colonne
`products.qualification IN ('weighable','not_weighable','anomaly')` ; fonction `Qualify`.
**ÉCARTÉS** : `Valid`/`Invalid` (c'est exactement l'erreur d'interprétation que l'ADR-021
corrige), `Status`, `Eligibility`. « NotWeighable » doit rester neutre à l'écran : le libellé
français reste « non pesable — c'est normal ».

### « adhérent » / « solidaire » → `MEMBER` / `SOLIDARITY`

Codes de tarif `MEMBER` et `SOLIDARITY`, type `PriceTier`, champ `PricingRules.Tiers`, table
`weighing_lines.tier_code`. **ÉCARTÉS** : `ADHERENT` (calque), `MEMBRE`. **ATTENTION** : la
valeur `abbrev` imprimée sur l'étiquette reste « A » et « S » — c'est du CONTENU affiché au
client, gelé par l'arbitrage A1 (reproduction à l'identique), pas un identifiant. De même
`label` = « Adhérent » / « Solidaire » reste français.

### « charge utile » → `payload`

`PrefixPlan.PayloadWidth`, `ErrPayloadOutOfRange`, paramètre `payload int64`. **ÉCARTÉS** :
`load`, `data`, `value`. Cohérent avec l'usage télécom/protocole, et distinct de `reference`
(la partie fixe).

### « plan de numérotation » → `numbering plan`

En prose anglaise : `numbering plan`. En code : type `PrefixPlan`, variable `internalPlan`,
erreurs `ErrPrefixNotInPlan` / `ErrWidthNotInPlan`, message de panique
« inconsistent numbering plan: `<prefix>` ». **ÉCARTÉS** : `scheme` (déjà pris par les URL),
`numberingScheme`. Le mot « plan » est conservé parce que le document en fait un contrat nommé
avec la caisse.

### « garde-fou » → `safeguard`, « garde » (transition) → `guard`

Deux concepts distincts, deux mots distincts.

1. Les 14 règles métier de §6.4 = `safeguard` : fichier `safeguard.go`, section
   « Safety checks », fonction `Evaluate`, type `CheckInput`, `WeighingLimits`.
2. La colonne « Garde » de la table de transitions §6.6 = `guard` au sens machine à états
   (condition de franchissement) — terme consacré, on le garde.
3. « garde-fou de dernier ressort » dans un commentaire → `last-resort guard`.

Ne jamais nommer `Guard` un type du paquet safeguard.

### « pesée » : l'acte vs l'enregistrement

**L'ACTE** : verbe `weigh` (route `POST /api/v1/weigh`, `Hub.Submit`), nom `weighing` en prose,
`pesees.duree_ms` → `weighings.duration_ms`. **L'ENREGISTREMENT** : type `Weighing`, table
`weighings`, `weighing_lines`, `RecordEffect`. Le mot anglais couvre les deux comme le
français ; ce qui compte est que la table s'appelle `weighings` (pluriel, snake_case) et le
type `Weighing` (singulier), **jamais `Weight`** — `Weight` est la grandeur, pas l'événement.

### « balance » : le produit vs l'appareil

Deux sens, et **aucun des deux ne s'écrit `balance` en code**.

Le PRODUIT ne s'appelle plus « Balance » : il s'appelle **OpenScale**. Binaire `openscale`,
module Go `openscale`, dépôt `OpenScale/`, service Windows, base `openscale.db`, journaux
`openscale.log`, répertoire d'installation `OpenScale`. La règle précédente — « `balance`
survit comme nom de produit » — est **caduque** et ce qu'il en reste dans un document est un
reliquat de renommage, pas une décision.

L'APPAREIL est `scale` PARTOUT : paquet `internal/scale`, interface `Scale`, config
`scale.type`, `ScaleEvent`, `ErrScaleDisconnected`, `ERR-SCL-nn`, `weighings.source='scale'`,
`technical_log.source='scale'`. **Aucun identifiant ne désigne l'appareil par le mot
`balance`.**

Le mot français « balance » ne subsiste donc que là où ce n'est pas un identifiant :

1. le **contenu** affiché à un bénévole ou à un client — « La balance ne répond plus. »,
   « La balance est en surcharge. Retirez votre article. », « Ce poste fonctionne sans
   balance : le poids est saisi à la main. » ;
2. un **nom d'objet système qui désigne l'appareil**, et un seul : le symlink udev
   `/dev/balance-serial`.

### FAUX AMI CRITIQUE : « file » d'impression → `queue`

`imprimante.options.file` désigne la FILE d'impression Windows, pas un fichier. Il devient
`printer.options.queue` (et `fallback.queue`). Traduire par `file` produirait un contresens
complet, d'autant que `transport: "fichier"` devient `transport: "file"` — les deux se
seraient télescopés.

### « acquitter » : trois sens

1. `SourceCatalogue.Acquitter` (archive puis supprime le fichier) → `Acknowledge` ;
   `Acquitter(Applique)` → `Acknowledge(Applied)`.
2. L'événement IHM `Acquitter` (le client ferme un message) → `Dismiss`, route
   `POST /api/v1/dismiss`.
3. `Accuse` / `EffetAccuser` (la réponse rendue au POST) → `Ack` / `AckEffect`, jamais
   `Acknowledgement` en entier dans un identifiant.

### « Contexte » du noyau → `TransitionContext`

PAS `Context`. Le paquet manipule déjà `context.Context` ; un type `domain.Context`
provoquerait des lignes `ctx context.Context, c domain.Context` illisibles.
`TransitionContext` révèle l'intention (« tout ce que la transition a le droit de lire ») et
n'entre en collision avec rien.

### États `Erreur` → `Faulted` et `Refus` → `Rejected`

`Erreur` (état de la machine) → `Faulted`, PAS `Error` : en Go `Error` est réservé à l'idiome
des erreurs et une constante `Error` de type `State` sèmerait la confusion. `Refus` → état
`Rejected`, résultat SQL `result='rejected'`, effet de message ; jamais `Refusal` ni `Denied`.
Cohérent avec `imports.result='rejected'`.

### « trame » → `frame`

Partout : paquet `domain/frame`, `Parse(frame []byte, now time.Time)`, `Accumulator`,
`weighings.frame`, `testdata/frames/`, `frames.txt`, `--duration`. **ÉCARTÉS** : `telegram`
(usage industriel mais opaque pour un relecteur), `packet` (connote le réseau).

### « journal » : trois choses

1. **Journal des pesées** → table `weighings` + `weighing_lines`, worker `journalWorker`, bloc
   de config `journal`.
2. **Journal technique** → table `technical_log`, interface `TechnicalLog`, méthode
   `Technical`, route `/admin/api/technical`.
3. **Journaux texte** (slog + rotation) → `logs/`.

Ne jamais nommer `Log` le journal des pesées ni `Journal` le journal technique.

### « signalement » → `Finding`

Table `findings`. **ÉCARTÉS** : `Report` (déjà pris : « le rapport d'import » = `import report`,
l'export CSV pour Odoo), `Warning` (un finding peut être une simple information), `Issue`
(connote un ticket). `findings.issue IN ('anomaly','info')` conserve le mot `issue` pour la
GRAVITÉ, comme dans le document.

### « Reçu » → `PrintReceipt`

Retour de `Imprimante.Imprimer` → `PrintReceipt`, pas `Receipt` seul : dans un contexte
d'épicerie, `Receipt` se lirait comme le ticket de caisse, qui n'existe pas dans ce système.
`Imprimer(ctx, job PrintJob) (PrintReceipt, error)`.

### « seuils » → `limits`

Pas `thresholds`. Le bloc contient des bornes min/max (`min_weight_g`, `max_amount_cents`), pas
des seuils de déclenchement ; `WeighingLimits` se lit naturellement. Exception assumée : la
fonction `SeuilPoids` devient `MinWeight`, parce qu'elle rend une borne basse et non un
« seuil ».

### « péremption » → `expiry`

Jamais `perishability`, ni `staleness` : la grandeur est une durée de validité.
`MEASUREMENT_EXPIRED`, `Snapshot.Expired`, `RateMeter.Expiry`, `expiry_floor_ms` /
`expiry_ceiling_ms` / `expiry_factor`.

### « libellé » : polysémique, traduit selon ce qu'il porte

- (a) Libellé d'affichage d'un tarif, d'une catégorie, d'un driver → `label`.
- (b) `Produit.Libelle` = le suffixe de prix (« €/kg », « € le litre ») → `PriceSuffix` /
  colonne `price_suffix` : l'appeler `label` masquerait sa nature.
- (c) `PlanPrefixe.Libelle` = suffixe par défaut du préfixe → `PriceLabel`.

Et `Etiquette` (l'objet imprimé) → `Label` : c'est le sens principal, il ne doit pas être dilué.

### « ordre » → `rank`

Jamais `order` : `order` est un mot réservé en SQL (`ORDER BY`) et signifie « commande » dans un
contexte commerce. `tiers[].rank`, `categories.rank`, `PriceTier.Rank`.

### Suffixe des montants → `_cents`

Le document mélange `_c` (`prix_unitaire_c`, `montant_c`) et rien du tout (`prix_unitaire`). On
UNIFIE en `_cents` : `products.unit_price_cents`, `weighings.base_unit_price_cents`,
`weighing_lines.unit_price_cents`, `weighing_lines.amount_cents`, `limits.max_amount_cents`.
Les masses gardent `_g`, les longueurs `_um`, les durées `_ms`. Un même concept, une seule
convention.

### Codes `ERR-xxx-nn`

Ce sont des IDENTIFIANTS (constantes du code, affichées à l'écran pour le support téléphonique),
donc traduits — mais UNIQUEMENT selon cette table, aucun traducteur ne doit en inventer :
`BAL→SCL`, `IMP→PRN`, `BDD→DB`, `IHM→UI`, `KIO→KSK` ; `CAT`, `SYS`, `CFG` inchangés. Les
numéros ne bougent pas : `ERR-BAL-08` → `ERR-SCL-08`. Le MESSAGE porté par le code reste
français.

Les codes **réellement alloués** par le binaire, avec leur libellé français, sont listés
dans [Codes `ERR-xxx-nn` réellement alloués par le code](#codes-err-xxx-nn-réellement-alloués-par-le-code).
Un numéro cité dans un commentaire n'est pas un numéro alloué : il l'est le jour où une
constante le porte.

### Auto-tests

`etiquette` → `label`, `reglette` → `ruler`, et `mire` → `alignment` (pas `test-pattern`, deux
mots, ni `crosshair` qui ne décrit que les coins). La route devient `?what=alignment|ruler`, la
valeur `label` ayant son doublon non authentifié `/troubleshooting/test-label`.

### NE PAS TRADUIRE — noms réels de l'existant

Cités comme preuve dans le document : `Module1.bas`, `FormulaireCalcul.cls`,
`FormulaireSquelette`, `FormulaireClavier`, `FormulairePaveNumeriqueUnites`,
`FormulaireTimerMessages.SupprimeFenetres`, `EtataImprimer.report`, `Image0…Image119`,
`LabelPoidsBandeau`, `LabelAPayer`, `Prixaukilo`, `PoidsUnites`, `CodeBarre`, `Produit`,
`Prix`, `LabelHeure`, `TableProduitsLegers`, `SystemeDefaut`, `Systeme_Dimensions`,
`SauvegardeProduits`, `RapportIntegrite`, `Decimales_Prix`, `Decimales_Poids`,
`Recup_Odoo_activee`, `ProduitIndisponibleSurErreur`, `Delai_idle_en_s`,
`FAideDecimalesPoids`, `BalanceConnectee`, `ReformatePoidsBalanceXFOCRS`,
`gPoidsBalanceConnectee`, `_Poste1..4`. Les traduire détruirait la valeur de preuve du
document.

### NE PAS TRADUIRE — format d'échange imposé par Odoo

L'en-tête CSV `"id";"nom";"code-barre";"prix";"categorie";"unite";"image"` (comparé OCTET À
OCTET par le parseur), les valeurs de la colonne `unite` (`kg`, `Litre(s)`, `Unité(s)`, accents
et parenthèses compris), les lettres de catégorie `F`/`L`/`V`/`A`, le nom `flv_<n>.csv` et
`flv_demo.csv`. Ce sont des constantes de l'adaptateur, pas du code à nous. Les identifiants Go
qui les reçoivent, eux, sont anglais (`Product.Name`, `Product.Reference`, `category_code`).

### NE PAS TRADUIRE — vocabulaire matériel et protocolaire

Commandes SBPL `<A>`, `<A1>`, `<A3>`, `<#E>`, `<CS>`, `<%>`, `<V>`, `<H>`, `<G>`, `<Q>`, `<Z>`,
`<BD>`, `<LD>`, `ENQ` ; `winspool`, `devfile`, `RAW`, `DOC_INFO_1.pDatatype`, `SATO WS408`,
`GRAM XFOC RS/+`, `gram-xfoc-rs`, `gram-xfoc-plus`, `COM8`, `/dev/usb/lp0`,
`/dev/sato-pesee`, `AutoAdminLogon`, `TimeoutStopSec`. Le symlink udev `balance-serie`
**est tranché** : c'est `/dev/balance-serial`, écrit ainsi dans `internal/scale/serial` et
dans l'aide de `openscale capture`. Le mot `balance` y désigne l'APPAREIL, et c'est le seul
nom d'objet système où il subsiste.

### « dépôt » : trois sens

Le répertoire de dépôt (`depot_local`) → `local_drop` / paquet `localdrop` /
`<data>/catalog/incoming/` ; les « 6 dépôts » de la couche SQLite (design pattern) →
`repositories`, dans le paquet `internal/store` ; et le **dépôt Git** → `repository`.

Ce troisième sens n'était plus « en prose seulement » depuis ADR-040 : il porte la clé
`update.repository`, le champ `update.GitHubSource.Repository` et le paramètre `repository`
des trois méthodes de `update.Service`. C'est le SEUL des trois qui soit au singulier dans
le code, et c'est ce qui les distingue à la lecture : `repositories` est la couche SQLite,
`repository` est le dépôt Git, `local_drop` est un répertoire.

### « scrutation » → `poll`

`poll_interval_s`, `stable_polls` (et non `scan`, qui évoque la douchette de caisse — laquelle
est bien un `scanner` dans la prose des tests de recette).

### « aperçu » → `preview`

Partout et sans exception : driver `preview`, `printer.type = "preview"`, paquet
`internal/printing/preview`, route `/admin/api/label/preview.png`, « aperçu du diff » →
`diff preview`. Jamais `overview`.

### « état » → `State` ou `health`

`State` pour la machine à états et `Snapshot.State` ; mais « état de santé / feu » → `health`,
`/admin/api/health`. `GET /api/v1/etat` n'existe que comme événement SSE nommé `"state"`.

### Deux « délais » qui ne doivent pas se ressembler

`delai_inactivite_s` → `idle_timeout_s` (vide une SAISIE abandonnée) et
`delai_reimpression_s` → `reprint_window_s` (fenêtre pendant laquelle la barre basse reste
active). `window` dit qu'on peut agir, `timeout` dit qu'on va perdre quelque chose.

### Conflit Clean Code / Go idiomatique — signalé une fois

Clean Code proscrit les identifiants courts, Go les exige dans les portées courtes. On garde
donc `i`, `r`, `w`, `p`, `e`, `err`, `ctx`, `tx`, `db`, `h` (le Hub), `g` (le gabarit) tels
quels dans les corps de fonction, et on ne préfixe aucune interface par `I` (`Scale`, `Printer`,
`Clock`, `Transport`, et non `IScale`). De même les paquets restent courts et au singulier
(`domain`, `scale`, `store`, `frame`) et les interfaces sont déclarées côté consommateur
(`station/ports`), ce qui interdit de les renommer en `ScaleInterface` ou de les déplacer côté
implémentation. **LE GO IDIOMATIQUE GAGNE.**

### Conventions de documentation — à rappeler une seule fois

Le commanditaire a demandé « TSDoc sur tout élément public » ; TSDoc est propre à TypeScript. On
applique donc **godoc en Go** (commentaire commençant par le nom de l'identifiant, phrase
complète terminée par un point : `// Prepare builds a printable label from a product and a
measurement.`) et **TSDoc en TypeScript/Svelte** (`/** … */` avec `@param`, `@returns`,
`@throws`, `@example`). Une seule règle par langage, chacune dans son langage.

---

## Conventions de nommage

Règles générales appliquées par le glossaire. Elles servent à traduire tout identifiant qui n'y
figure pas encore.

### Langue

- **Code en anglais** : paquets, fichiers, types, fonctions, champs, variables, constantes,
  tables et colonnes SQL, clés de configuration, routes, drapeaux CLI, variables
  d'environnement, noms de scripts, noms de tests.
- **Contenu en français** : documentation, commentaires de spécification, messages affichés à
  l'écran, libellés imprimés sur l'étiquette, textes des scripts d'installation, contenu des
  fichiers `TROUBLESHOOTING.md` / `install-sheet.txt`.
- **Exceptions gelées** : noms propres (`openscale`, `lacagette`, `flv`), noms matériels et
  protocolaires, format d'échange Odoo, identifiants de l'ancienne base Access.

### Go

- **Paquets** : courts, minuscules, singuliers, sans underscore ni majuscule
  (`domain`, `scale`, `store`, `frame`, `printing`, `localdrop`). Un paquet nomme un concept,
  pas une couche technique.
- **Fichiers** : minuscules, un mot quand c'est possible, nommés d'après le type ou la fonction
  principale (`prepare.go` pour `Prepare`, `loop.go` pour `Loop`).
- **Exportés en `PascalCase`, non exportés en `camelCase`**. Aucune abréviation inventée ; les
  acronymes gardent leur casse d'usage (`EAN13`, `ID`, `SHA`, `HRI`, `CSV`, `DB`, `UM`).
- **Aucun préfixe `I` sur les interfaces** (`Scale`, `Printer`, `Clock`, `Transport`), qui sont
  déclarées **côté consommateur** (`station/ports`) et non côté implémentation.
- **Identifiants courts autorisés dans les portées courtes** (`i`, `r`, `w`, `p`, `e`, `err`,
  `ctx`, `tx`, `db`, `h`, `g`) : Go idiomatique prime sur Clean Code sur ce point.
- **Erreurs sentinelles** : `Err` + `PascalCase` décrivant la condition
  (`ErrPatternNotZeroed`, `ErrPrefixNotInPlan`). Erreur typée = `PrintError` avec un champ
  `Kind` dont les constantes sont préfixées par leur famille (`KindData`, `KindTemplate`, …).
- **Familles de constantes préfixées par leur type** : `Round*` (`RoundHalfUp`), `Status*`
  (`StatusConnected`), `Mode*` (`ModeBlocking`), `Kind*`, `Unit*` (`UnitKg`), `Stability*`.
- **États** : adjectif ou gérondif (`Idle`, `Initializing`, `Validating`, `Printing`,
  `Succeeded`, `Rejected`, `Faulted`). Jamais un nom réservé à un idiome Go (`Error`).
- **Événements** : fait accompli au participe passé (`MeasurementReceived`, `ProductTapped`,
  `TareConfirmed`, `PrintFinished`, `ScaleDisconnected`). **Effets** : suffixe `Effect`
  (`PrintEffect`, `RecordEffect`, `ArmTimerEffect`).
- **Méthodes** : verbe à l'infinitif anglais (`Prepare`, `Evaluate`, `Qualify`, `Normalize`,
  `Submit`, `Subscribe`, `Rasterize`). Les accesseurs ne portent pas de préfixe `Get`.
- **Unités portées par le nom du champ** : `...Grams`, `...UM` (micromètres), `...Dots`,
  `...MilliDots`, durées typées `time.Duration` ou `domain.Duration`. Les montants sont des
  `Cents`.
- **Tests** : `TestXxx` en anglais, phrase lisible décrivant le comportement vérifié
  (`TestExpiredMeasurementRejectsWeighing`). Doubles de test dans un paquet `fake`, constructeurs
  `NewScale`, `NewPrinter`, `NewClock`.

### SQL

- **`snake_case` partout**, sans accent ni majuscule.
- **Tables au pluriel** (`products`, `weighings`, `findings`, `imports`, `categories`,
  `images`, `local_decisions`, `weighing_lines`, `technical_log`, `quarantine`, `meta`) ;
  **colonnes au singulier**.
- **Horodatages en `*_at`** : `occurred_at`, `updated_at`, `seen_at`, `withdrawn_at`,
  `decided_at`, `first_failure_at`, `last_failure_at`.
- **Suffixes d'unité obligatoires et uniques** : `_cents` pour les montants, `_g` pour les
  masses, `_um` pour les longueurs, `_ms` pour les durées, `_count` pour les dénombrements
  (`byte_count`, `failure_count`).
- **Clés étrangères** : `<table_au_singulier>_id` (`product_id`, `import_id`, `weighing_id`,
  `last_import_id`).
- **Valeurs énumérées** : minuscules `snake_case`, en anglais, contraintes par un `CHECK`
  (`'by_weight'`, `'not_weighable'`, `'local_drop'`, `'warn_and_print'`, `'not_applicable'`).
- **Index** : `idx_<table>_<colonne(s)>` (`idx_weighings_occurred_at`, `idx_findings_import`).
- **`rank`, jamais `order`** (mot réservé SQL et faux ami commerce).
- Les identifiants hérités de l'ancienne base Access ne sont **jamais** renormalisés.

### Configuration, routes et CLI

- **Clés JSON en `snake_case`** ; blocs de premier niveau au singulier et en nom commun
  (`station`, `network`, `ui`, `scale`, `printer`, `pricing`, `barcode`, `limits`, `stability`,
  `catalog`, `journal`, `admin`, `maintenance`).
- **L'unité fait partie du nom de la clé** : `_s`, `_ms`, `_g`, `_um`, `_mb`, `_kb`, `_cents`,
  `_ratio`. Une valeur sans unité explicite est un défaut de nommage.
- **Booléens formulés comme une assertion vraie** : `present`, `enabled`, `visible`, `offered`,
  `manual_entry_allowed`, `show_grid_prices`, `weekly_integrity_check`, `invert_bits`. Pas de
  négation dans le nom.
- **Valeurs énumérées en minuscules `snake_case`** (`local_drop`, `half_up`, `truncate`,
  `half_even`, `advisory`, `blocking`, `manual_entry`, `image_directory`), sauf noms matériels
  qui gardent leur graphie d'origine (`gram-xfoc-rs`, `winspool`, `devfile`, `sbpl`).
- **Routes** : minuscules, segments en `kebab-case` (`/troubleshooting/roll-changed`,
  `/fallback-printer`), ressources au pluriel (`/printers`, `/products`, `/imports`), actions à
  l'infinitif (`/weigh`, `/reprint`, `/cancel`, `/dismiss`, `/import`, `/reload`, `/discover`).
  API publique préfixée `/api/v1`, API d'administration `/admin/api`.
- **Paramètres de requête et corps JSON** en `snake_case` anglais (`?what=alignment|ruler`,
  `?hardware=0`, `{product_id, tare_g, units, manual_weight_g}`).
- **Drapeaux CLI** en `--kebab-case` (`--data`, `--duration`, `--unit-price`, `--template`) ;
  sous-commandes en un seul mot minuscule (`serve`, `kiosk`, `doctor`, `capture`, `label`,
  `replay`, `validate`, `export`, `import`, `password`).
- **Variables d'environnement** : `OPENSCALE_` + `SCREAMING_SNAKE_CASE` (`OPENSCALE_CONFIG`,
  `OPENSCALE_DATA`). Le préfixe suit le nom du produit ; il n'y en a **que deux**, et
  §11.1 interdit toute autre variable d'environnement.
- **Codes métier affichés au support** en `SCREAMING_SNAKE_CASE` anglais (`WEIGHT_UNSTABLE`,
  `MEASUREMENT_EXPIRED`, `UNREADABLE_ROW`) ; codes techniques au format `ERR-<3 lettres>-<nn>`
  selon la table figée (`SCL`, `PRN`, `CAT`, `DB`, `UI`, `KSK`, `SYS`, `CFG`), numéros
  inchangés.
- **Variables CSS** en `--kebab-case` courtes et sémantiques (`--bg`, `--ink`, `--ink-muted`,
  `--ready`, `--fault`).

### Documentation du code — godoc et TSDoc

- **Go → godoc.** Tout élément exporté (paquet, type, fonction, méthode, constante, champ
  public) porte un commentaire qui **commence par le nom de l'identifiant** et forme une
  **phrase complète terminée par un point** :
  `// Prepare builds a printable label from a product and a measurement.`
  Le commentaire de paquet vit dans un seul fichier (`doc.go` ou le fichier principal).
  Documenter le **contrat** — préconditions, erreurs renvoyées, unités, effets de bord — pas la
  paraphrase du corps.
- **TypeScript / Svelte → TSDoc.** Tout élément public porte un bloc `/** … */` avec, selon le
  cas, `@param`, `@returns`, `@throws`, `@example`. Pas de `@type` redondant avec la signature.
- **Une seule règle par langage** : jamais de TSDoc dans du Go, jamais de style godoc dans du
  TypeScript.
- **Langue des commentaires** : les identifiants cités restent anglais ; la prose explicative
  suit la langue du fichier de documentation associé. Les commentaires de code publics sont
  rédigés en anglais, cohérents avec les identifiants qu'ils décrivent.
- **Références croisées** : citer le glossaire lorsqu'un identifiant traduit pourrait surprendre
  (`safeguard` vs `guard`, `Template` vs `pattern`, `queue` vs `file`).

---

> **Rappel final.** Ce glossaire fait autorité. Tout identifiant absent doit être traduit selon
> les conventions ci-dessus, **puis signalé** pour être ajouté au présent document.
