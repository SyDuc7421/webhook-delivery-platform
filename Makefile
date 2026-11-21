BACKEND    := backend
MODULE     := github.com/SyDuc7421/webhook-delivery-platform/backend
APP        ?= api
PKG        := ./...
DB_URL     ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
COMPOSE    := docker compose -f deploy/docker-compose.yml --env-file .env
GO         ?= go
IN_BE      := cd $(BACKEND) &&

-include .env
export

.DEFAULT_GOAL := help

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | awk -F: '{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## setup: install tooling, git hooks and .env
setup:
	@test -f .env || cp .env.example .env
	$(IN_BE) $(GO) mod download
	$(GO) install github.com/air-verse/air@latest
	$(GO) install mvdan.cc/gofumpt@latest
	$(GO) install golang.org/x/tools/cmd/goimports@latest
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	$(GO) install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	$(GO) install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	$(GO) install github.com/vektra/mockery/v2@latest
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	npm install
	npx husky
	pre-commit install --hook-type pre-commit --hook-type commit-msg
	@echo "setup complete — run 'make up'"

## up: start the local stack (postgres, kafka, redis, observability)
up:
	$(COMPOSE) up -d --wait

## up-apps: start the stack plus every app process in containers
up-apps:
	$(COMPOSE) --profile apps up -d --wait

## down: stop the stack
down:
	$(COMPOSE) --profile apps down --remove-orphans

## clean: stop and delete volumes
clean:
	$(COMPOSE) --profile apps down -v --remove-orphans

## topics: (re)create Kafka topics
topics:
	$(COMPOSE) run --rm kafka-init

## run: run one process with live reload (APP=api|relay|dispatcher|delivery-worker|retry-worker)
run:
	$(IN_BE) APP=$(APP) air -c .air.toml

## build: compile every binary under cmd/
build:
	$(IN_BE) CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$$(git describe --tags --always --dirty)" -o bin/ ./cmd/...

## fmt: format code
fmt:
	$(IN_BE) gofumpt -l -w .
	$(IN_BE) goimports -local $(MODULE) -w .

## lint: run golangci-lint
lint:
	$(IN_BE) golangci-lint run --timeout 5m

## vet: go vet
vet:
	$(IN_BE) $(GO) vet $(PKG)

## test: unit tests
test:
	$(IN_BE) $(GO) test -race -shuffle=on -count=1 -covermode=atomic -coverprofile=coverage.out $(PKG)

## test-int: integration + failure tests (testcontainers, needs Docker)
test-int:
	$(IN_BE) $(GO) test -race -count=1 -tags=integration ./test/...

## cover: html coverage report
cover: test
	$(IN_BE) $(GO) tool cover -html=coverage.out -o coverage.html
	@echo "open $(BACKEND)/coverage.html"

## check: fmt + vet + lint + test (the gate)
check: fmt vet lint test

## gen: run all code generation
gen:
	$(IN_BE) sqlc generate
	$(IN_BE) mockery
	$(IN_BE) $(GO) generate $(PKG)

## vuln: scan dependencies
vuln:
	$(IN_BE) govulncheck $(PKG)

## tidy: go mod tidy + verify
tidy:
	$(IN_BE) $(GO) mod tidy && $(GO) mod verify

## migrate-new: create a migration (name=...)
migrate-new:
	@test -n "$(name)" || (echo "usage: make migrate-new name=add_events_table" && exit 1)
	migrate create -ext sql -dir $(BACKEND)/migrations -seq $(name)

## migrate-up: apply migrations
migrate-up:
	migrate -path $(BACKEND)/migrations -database "$(DB_URL)" up

## migrate-down: roll back one migration (LOCAL ONLY)
migrate-down:
	migrate -path $(BACKEND)/migrations -database "$(DB_URL)" down 1

## migrate-force: force version (version=...)
migrate-force:
	migrate -path $(BACKEND)/migrations -database "$(DB_URL)" force $(version)

## logs: follow a service (svc=api)
logs:
	$(COMPOSE) logs -f $(svc)

## psql: open a psql shell
psql:
	$(COMPOSE) exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

.PHONY: help setup up up-apps down clean topics run build fmt lint vet test test-int cover check gen \
        vuln tidy migrate-new migrate-up migrate-down migrate-force logs psql
