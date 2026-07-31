BINARY := bin/cnpg-console
PKG    := ./...

# Toolchain Go >= 1.24. Si ~/go-sdk/go existe on l'utilise, sinon le `go` du PATH.
# L'environnement n'autorise pas le download auto de toolchain → GOTOOLCHAIN=local.
GO ?= $(shell if [ -x "$$HOME/go-sdk/go/bin/go" ]; then echo "$$HOME/go-sdk/go/bin/go"; else echo go; fi)
export GOTOOLCHAIN := local

.PHONY: all build run test vet fmt tidy web-build docker clean

all: build

## build: compile le binaire (embarque internal/web/dist si présent)
build:
	CGO_ENABLED=0 $(GO) build -o $(BINARY) ./cmd/cnpg-console

## run: lance le serveur en local (nécessite un config.yaml)
run:
	$(GO) run ./cmd/cnpg-console

## test: tests unitaires
test:
	$(GO) test $(PKG)

vet:
	$(GO) vet $(PKG)

fmt:
	$(GO) fmt $(PKG)

tidy:
	$(GO) mod tidy

## web-build: build la SPA Vue vers internal/web/dist
web-build:
	cd web && npm ci && npm run build

## docker: build l'image (SPA + binaire) depuis la racine
docker:
	docker build -f deploy/docker/Dockerfile -t cnpg-console:dev .

clean:
	rm -rf bin
