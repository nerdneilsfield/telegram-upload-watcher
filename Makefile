projectname?=telegram-send-go

default: help

.PHONY: help
help: ## list makefile targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "%-30s %s\n", $$1, $$2}'

.PHONY: build
build: ## build go binary
	@go build -o $(projectname) ./go/cmd/telegram-send-go

.PHONY: install
install: ## install go binary
	@go install ./go/cmd/telegram-send-go

.PHONY: run
run: ## run the go cli
	@go run ./go/cmd/telegram-send-go --help

.PHONY: test
test: ## run go tests
	@go test ./go/...

.PHONY: clean
clean: ## clean artifacts
	@rm -rf coverage.out dist/ $(projectname) $(projectname).exe
