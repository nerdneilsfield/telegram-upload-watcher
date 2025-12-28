projectname := "telegram-upload-watcher"
gobin := "telegram-send-go"
go_cmd := "./go"

default:
    @just --list

# Build Go binary
build:
    @if [ "{{os()}}" = "windows" ]; then \
        go build -ldflags "-X main.version=$(git describe --abbrev=0 --tags 2>/dev/null || echo dev) -X main.buildTime=$(date +%Y%m%d%H%M%S) -X main.gitCommit=$(git rev-parse HEAD 2>/dev/null || echo unknown)" -o {{gobin}}.exe {{go_cmd}}; \
    else \
        go build -ldflags "-X main.version=$(git describe --abbrev=0 --tags 2>/dev/null || echo dev) -X main.buildTime=$(date +%Y%m%d%H%M%S) -X main.gitCommit=$(git rev-parse HEAD 2>/dev/null || echo unknown)" -o {{gobin}} {{go_cmd}}; \
    fi

# Build static Go binary
build-static:
    @if [ "{{os()}}" = "windows" ]; then \
        CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -extldflags \"-static\" -X main.version=$(git describe --abbrev=0 --tags 2>/dev/null || echo dev) -X main.buildTime=$(date +%Y%m%d%H%M%S) -X main.gitCommit=$(git rev-parse HEAD 2>/dev/null || echo unknown)" -o {{gobin}}.exe {{go_cmd}}; \
    else \
        CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -extldflags \"-static\" -X main.version=$(git describe --abbrev=0 --tags 2>/dev/null || echo dev) -X main.buildTime=$(date +%Y%m%d%H%M%S) -X main.gitCommit=$(git rev-parse HEAD 2>/dev/null || echo unknown)" -o {{gobin}} {{go_cmd}}; \
    fi

# Install Go binary
install:
    go install -ldflags "-X main.version=$(git describe --abbrev=0 --tags 2>/dev/null || echo dev) -X main.buildTime=$(date +%Y%m%d%H%M%S) -X main.gitCommit=$(git rev-parse HEAD 2>/dev/null || echo unknown)" {{go_cmd}}

# Run Go CLI
run:
    go run -ldflags "-X main.version=$(git describe --abbrev=0 --tags 2>/dev/null || echo dev) -X main.buildTime=$(date +%Y%m%d%H%M%S) -X main.gitCommit=$(git rev-parse HEAD 2>/dev/null || echo unknown)" {{go_cmd}} --help

# Format Go files
fmt:
    gofmt -w go

# Run Go tests
test:
    go test ./go/...

# Clean artifacts
clean:
    rm -rf {{gobin}} {{gobin}}.exe dist coverage.out
