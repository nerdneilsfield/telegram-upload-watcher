projectname := "telegram-upload-watcher"
gobin := "telegram-send-go"
go_cmd := "./go"
gui_dir := "./go/gui"
gui_bin := "telegram-upload-watcher-gui"

default:
    @just --list

# Build Go binary
build:
    @if [ "{{os()}}" = "windows" ]; then \
        go build -ldflags "-X main.version=$(git describe --abbrev=0 --tags 2>/dev/null || echo dev) -X main.buildTime=$(date +%Y%m%d%H%M%S) -X main.gitCommit=$(git rev-parse HEAD 2>/dev/null || echo unknown)" -o {{gobin}}.exe {{go_cmd}}; \
    else \
        go build -ldflags "-X main.version=$(git describe --abbrev=0 --tags 2>/dev/null || echo dev) -X main.buildTime=$(date +%Y%m%d%H%M%S) -X main.gitCommit=$(git rev-parse HEAD 2>/dev/null || echo unknown)" -o {{gobin}} {{go_cmd}}; \
    fi

# Build GUI binary (Wails)
build-gui:
    @command -v wails >/dev/null 2>&1 || { echo "wails not found. Install with: go install github.com/wailsapp/wails/v2/cmd/wails@latest"; exit 1; }
    @if [ "{{os()}}" = "linux" ]; then \
        cd {{gui_dir}} && wails build -tags webkit2_41; \
    else \
        cd {{gui_dir}} && wails build; \
    fi

# Build CLI + GUI
build-all: build build-gui

# Run GUI dev server
dev-gui:
    @command -v wails >/dev/null 2>&1 || { echo "wails not found. Install with: go install github.com/wailsapp/wails/v2/cmd/wails@latest"; exit 1; }
    @if [ "{{os()}}" = "linux" ]; then \
        cd {{gui_dir}} && wails dev -tags webkit2_41; \
    else \
        cd {{gui_dir}} && wails dev; \
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
    rm -rf {{gobin}} {{gobin}}.exe dist coverage.out {{gui_dir}}/build
