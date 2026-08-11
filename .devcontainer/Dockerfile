# L'image du poste de développement — voir devcontainer.json à côté.
#
# Trois paquets, et chacun tient un banc précis. Alléger cette liste est un changement qui
# a l'air raisonnable ; deploy/devcontainer_test.go le refuse et dit ce qu'il coûterait.
FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04

# build-essential : gcc ET make. La passe `-race` de `make test` EXIGE ThreadSanitizer,
#                   donc cgo — sans lui elle se saute, et on perd la seule vérification
#                   automatique des trois invariants de concurrence du Hub (important-3).
#                   Et post-create.sh appelle `make -s golangci-version` pour lire cette
#                   version : réduire ce paquet à gcc seul casserait le script, qui a besoin
#                   de make autant que de gcc.
# zip             : la cible `release` du Makefile empaquette avec.
# systemd         : pour `systemd-analyze` seul — il ne tourne pas ici. Sans lui,
#                   TestTheUnitIsValidAccordingToSystemdItself se saute, et plus rien ne
#                   demande à systemd lui-même s'il accepte les unités livrées.
RUN apt-get update && export DEBIAN_FRONTEND=noninteractive \
 && apt-get install -y --no-install-recommends \
      build-essential \
      zip \
      systemd \
 && apt-get clean \
 && rm -rf /var/lib/apt/lists/*
