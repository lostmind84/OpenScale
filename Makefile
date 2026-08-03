# OpenScale — cibles de développement et de livraison.
#
# Il n'y a PAS de `export CGO_ENABLED=0` global, et c'est une correction, pas un
# oubli (important-3, docs/02-architecture.md §16.4) : le détecteur de course
# repose sur ThreadSanitizer et EXIGE cgo. Un CGO_ENABLED=0 global fait échouer
# `make test` dès la première exécution, et le premier développeur qui rencontre
# l'erreur retire `-race` — on perd alors la seule vérification automatique des
# trois invariants de concurrence du Hub.
#
# La cible `test` fait donc DEUX passes : une avec cgo pour `-race`, une sans pour
# prouver que la configuration réellement livrée compile et passe.
#
# Sous Windows sans GNU make, `.\make.ps1 <cible>` expose les mêmes cibles.

# VERSION est DÉRIVÉE par défaut et SURCHARGEABLE : `make release VERSION=v0.1`.
#
# Le workflow de publication l'impose, parce que dans une release la version EST le tag.
# Et parce que `--dirty` suffixe le nom dès que l'arbre est modifié : la publication
# reconstruit l'écran client juste avant, dont les noms de fichiers portent une empreinte
# du contenu, si bien que les archives sortiraient en « -dirty » d'un dépôt parfaitement
# sain. `?=` et non `:=` pour que la variable d'environnement suffise aussi.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    := $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

TARGETS := windows/amd64 linux/amd64 linux/arm64

.PHONY: all test vet lint audit boundary deps driver build dist release front front-check clean cover help

all: test build

help:
	@echo "Cibles : test · driver · vet · lint · audit · boundary · deps · build · dist · release · cover · front · front-check · clean"

# GOLANGCI est cherché dans le PATH d'abord, puis dans le GOPATH, parce que
# `go install` y dépose l'outil sans que le PATH le sache toujours sous Windows.
GOLANGCI ?= $(shell command -v golangci-lint 2>/dev/null || echo "$$(go env GOPATH)/bin/golangci-lint")

# La version est ÉPINGLÉE ici, et c'est le seul endroit où elle est écrite : la
# CI, make.ps1 et ce fichier la lisent tous d'ici. Un développeur sur une
# version plus récente verrait rouge là où la CI voit vert — ou l'inverse, ce
# qui est pire, parce que personne ne cherche la cause d'un vert.
GOLANGCI_VERSION ?= v2.12.2

# front construit l'écran client vers internal/web/dist, qui est COMMITÉ : `go
# build` doit fonctionner sur une machine sans Node (§14.1).
front:
	@if [ -f web/package.json ]; then \
	  npm --prefix web ci && npm --prefix web run build; \
	else \
	  echo "front : web/package.json absent — rien à construire"; \
	fi

# front-check est la qualité du front : types, tests, puis le budget de §14.1
# mesuré sur le dist fraîchement construit — jamais supposé.
front-check: front
	npm --prefix web run check
	npm --prefix web test
	npm --prefix web run budget

vet:
	go vet ./...

# lint est BLOQUANTE et VERTE, et les deux vont ensemble : un jeu de règles qui
# rougit dès le premier jour n'est pas lu, il est contourné. .golangci.yml ne
# contient donc que ce que le dépôt tient AUJOURD'HUI, et il écrit à côté de
# chaque linter écarté le nombre de signalements qu'il produirait — pour que
# « pas activé » ne se lise jamais « sans valeur ».
#
# L'outil s'installe HORS module, et c'est ADR-039 qui l'impose : `make deps`
# compare go.mod aux deux tables de §17.1 dans les deux sens, et une dépendance
# de développement inscrite là y ouvrirait un écart permanent. Pas de tools.go.
# La CI lit la version par ici plutôt qu'en la recopiant dans son fichier. Une
# cible ordinaire, et non une astuce de ligne de commande : ce qu'on ne peut pas
# lancer à la main pour vérifier finit par casser sans qu'on sache où.
.PHONY: golangci-version
golangci-version:
	@echo $(GOLANGCI_VERSION)

lint:
	@test -x "$(GOLANGCI)" || { \
	  echo "lint : golangci-lint introuvable."; \
	  echo "       go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)"; \
	  exit 1; \
	}
	$(GOLANGCI) run ./...

# audit active TOUT et ne bloque rien. Elle ne tourne pas dans l'intégration
# continue : elle se lance à la main quand on ouvre un lot de qualité, et son
# relevé sert à le dimensionner. Le `|| true` est la cible même de cette règle,
# pas un oubli — une cible d'inventaire qui échoue est une cible qu'on cesse de
# lancer.
audit:
	@test -x "$(GOLANGCI)" || { \
	  echo "audit : golangci-lint introuvable."; \
	  echo "        go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)"; \
	  exit 1; \
	}
	@$(GOLANGCI) run -c .golangci-audit.yml ./... || true
	@echo "audit : relevé ci-dessus. Les raisons de chaque exclusion sont dans .golangci.yml"

boundary:
	go run ./tools/boundary

# deps vérifie que les dépendances DÉCLARÉES sont les dépendances RÉELLES : go.mod
# comparé aux deux tables de l'inventaire, dans les deux sens (§17.1, ADR-039).
deps:
	go run ./tools/deps

# driver est la commande à lancer avant de dire qu'un driver est terminé, et elle ne
# demande NI MATÉRIEL NI RÉSEAU (docs/07-ajouter-un-materiel.md §8).
#
# Elle couvre les trois endroits où un driver incomplet se voit, et pas un de plus :
#
#   internal/scale/…      les bancs de conformité des balances, et le corpus vivant
#   internal/printing/…   ceux des imprimantes et des transports
#   cmd/openscale         les tests de REGISTRE — ce qu'un driver DÉCLARE, que nul banc
#                         ne voit : un libellé vide, un schéma d'options absent, un point
#                         d'accès qui promet une détection que rien ne peut faire
#   boundary              la coupe 2 : un seul fichier de l'arbre nomme un driver concret
#
# CE QU'ELLE NE COUVRE PAS, et il faut le savoir : la passe `-race`, l'écran client, et
# `make deps` — une dépendance nouvelle impose quatre écritures (ADR-039). C'est
# `make test` qui les couvre, et c'est lui que la CI exécute.
driver: vet
	CGO_ENABLED=0 go test ./internal/scale/... ./internal/printing/... ./cmd/openscale -count=1
	$(MAKE) boundary
	@echo "driver : conformite, registre et coupe 2 au vert — reste « make test » avant de livrer"

test: vet
	CGO_ENABLED=1 go test ./... -race -count=1
	CGO_ENABLED=0 go test ./... -count=1
	$(MAKE) boundary
	$(MAKE) deps

# Couverture par paquet, avec les seuils de §16.4 : domain 95 %, scale 90 %,
# printing 80 %, catalog 85 %.
cover:
	go test ./... -coverprofile=coverage.out -count=1
	go tool cover -func=coverage.out | tail -n 1
	@go tool cover -func=coverage.out | grep -E "internal/domain" | tail -n 1 || true

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/openscale ./cmd/openscale

# SKIP_TESTS=1 empaquette SANS rejouer la suite, et un seul appelant y a droit : le
# workflow de publication, quand il a d'abord vérifié que l'intégration continue est
# VERTE SUR LA RÉVISION EXACTE du tag. Rejouer la suite là est une seconde occasion
# d'échouer sur rien — c'est ce qui a privé v0.4 de ses archives — et ce n'est pas une
# seconde preuve : c'est la même, sur le même commit, une demi-heure plus tard.
#
# Sans cette variable, `make release` teste. Un humain qui publie depuis son poste ne
# gagne pas de temps ici, et c'est voulu.
TEST_DEP := test
ifeq ($(SKIP_TESTS),1)
TEST_DEP :=
endif

dist: $(TEST_DEP)
	@mkdir -p dist
	@for t in $(TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=""; \
	  if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
	  echo "dist : $$os/$$arch"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath \
	    -ldflags "$(LDFLAGS)" -o dist/openscale-$$os-$$arch$$ext ./cmd/openscale || exit 1; \
	done
	cd dist && sha256sum * > SHA256SUMS

# release fabrique CE QU'UN BÉNÉVOLE COPIE : une archive par cible, dont le contenu est
# celui de §17.2, et rien qu'un décompresseur à savoir utiliser.
#
# Elle dépend de dist et non de build : les trois cibles se construisent depuis n'importe
# quelle machine sans chaîne C (ADR-001), et livrer une seule d'entre elles serait perdre
# la contrepartie du « zéro cgo ».
#
# La configuration livrée est l'EXPORT SANS MATÉRIEL de testdata/config-lacagette.json,
# produit par le binaire lui-même (`config export`) et non recopié à la main : §17.2 la
# décrit « SANS le bloc matériel », et un fichier recopié emporterait le COM8 et la file
# SATO WS408_2 du poste de développement — deux valeurs qu'aucun poste du parc ne doit
# hériter, et qui feraient échouer la comparaison d'empreinte du §15.5.
release: dist build
	@rm -rf dist/staging && mkdir -p dist/staging
	@set -e; for t in $(TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=""; deploydir="linux"; \
	  if [ "$$os" = "windows" ]; then ext=".exe"; deploydir="windows"; fi; \
	  name="openscale-$(VERSION)-$$os-$$arch"; \
	  stage="dist/staging/$$name"; \
	  mkdir -p "$$stage"; \
	  cp "dist/openscale-$$os-$$arch$$ext" "$$stage/openscale$$ext"; \
	  cp deploy/$$deploydir/* "$$stage/"; \
	  cp INSTALLATION.md TROUBLESHOOTING.md LICENSE THIRD-PARTY.md "$$stage/"; \
	  ./bin/openscale config export testdata/config-lacagette.json \
	    --output "$$stage/config-lacagette.json" >/dev/null; \
	  (cd "$$stage" && sha256sum * > SHA256SUMS); \
	  (cd dist/staging && zip -qr "../$$name.zip" "$$name"); \
	  echo "release : dist/$$name.zip"; \
	done
	@rm -rf dist/staging
	@ls -l dist/*.zip

clean:
	rm -rf bin dist coverage.out
