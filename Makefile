projectname?=telegram-send-go

default: help

.PHONY: help
help: ## list makefile targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "%-30s %s\n", $$1, $$2}'

.PHONY: build
build: ## build go binary
	@go build -ldflags "-X main.version=$(shell git describe --abbrev=0 --tags) -X main.buildTime=$(shell date +%Y%m%d%H%M%S) -X main.gitCommit=$(shell git rev-parse HEAD)" -o $(projectname) ./go

.PHONY: build-static
build-static: ## build static go binary
	@CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -extldflags \"-static\" -X main.version=$(shell git describe --abbrev=0 --tags) -X main.buildTime=$(shell date +%Y%m%d%H%M%S) -X main.gitCommit=$(shell git rev-parse HEAD)" -o $(projectname) ./go

.PHONY: install
install: ## install go binary
	@go install -ldflags "-X main.version=$(shell git describe --abbrev=0 --tags) -X main.buildTime=$(shell date +%Y%m%d%H%M%S) -X main.gitCommit=$(shell git rev-parse HEAD)" ./go

.PHONY: run
run: ## run the go cli
	@go run -ldflags "-X main.version=$(shell git describe --abbrev=0 --tags) -X main.buildTime=$(shell date +%Y%m%d%H%M%S) -X main.gitCommit=$(shell git rev-parse HEAD)" ./go --help

.PHONY: test
test: ## run go tests
	@go test ./go/...

.PHONY: clean
clean: ## clean artifacts
	@rm -rf coverage.out dist/ $(projectname) $(projectname).exe
