projectname?=telegram-send-go
gui_name?=telegram-upload-watcher-gui
gui_dir?=./go/gui
uname_s := $(shell uname -s)
ifeq ($(uname_s),Linux)
	WAILS_TAGS := -tags webkit2_41
else
	WAILS_TAGS :=
endif

default: help

.PHONY: help
help: ## list makefile targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "%-30s %s\n", $$1, $$2}'

.PHONY: build
build: ## build go binary
	@go build -ldflags "-X main.version=$(shell git describe --abbrev=0 --tags 2>/dev/null || echo dev) -X main.buildTime=$(shell date +%Y%m%d%H%M%S) -X main.gitCommit=$(shell git rev-parse HEAD 2>/dev/null || echo unknown)" -o $(projectname) ./go

.PHONY: wails-check
wails-check: ## verify wails cli is installed
	@command -v wails >/dev/null 2>&1 || (echo "wails not found. Install with: go install github.com/wailsapp/wails/v2/cmd/wails@latest" && exit 1)

.PHONY: build-gui
build-gui: wails-check ## build wails gui binary
	@cd $(gui_dir) && wails build $(WAILS_TAGS)

.PHONY: build-all
build-all: build build-gui ## build cli + gui

.PHONY: build-static
build-static: ## build static go binary
	@CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -extldflags \"-static\" -X main.version=$(shell git describe --abbrev=0 --tags 2>/dev/null || echo dev) -X main.buildTime=$(shell date +%Y%m%d%H%M%S) -X main.gitCommit=$(shell git rev-parse HEAD 2>/dev/null || echo unknown)" -o $(projectname) ./go

.PHONY: install
install: ## install go binary
	@go install -ldflags "-X main.version=$(shell git describe --abbrev=0 --tags 2>/dev/null || echo dev) -X main.buildTime=$(shell date +%Y%m%d%H%M%S) -X main.gitCommit=$(shell git rev-parse HEAD 2>/dev/null || echo unknown)" ./go

.PHONY: run
run: ## run the go cli
	@go run -ldflags "-X main.version=$(shell git describe --abbrev=0 --tags 2>/dev/null || echo dev) -X main.buildTime=$(shell date +%Y%m%d%H%M%S) -X main.gitCommit=$(shell git rev-parse HEAD 2>/dev/null || echo unknown)" ./go --help

.PHONY: dev-gui
dev-gui: wails-check ## run wails gui dev server
	@cd $(gui_dir) && wails dev $(WAILS_TAGS)

.PHONY: test
test: ## run go tests
	@go test ./go/...

.PHONY: clean
clean: ## clean artifacts
	@rm -rf coverage.out dist/ $(projectname) $(projectname).exe $(gui_dir)/build
