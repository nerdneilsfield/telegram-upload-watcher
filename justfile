projectname := "telegram-upload-watcher"
gobin := "telegram-send-go"
go_cmd := "./go/cmd/telegram-send-go-cli"

default:
    @just --list

# Build Go binary
build:
    @if [ "{{os()}}" = "windows" ]; then \
        go build -o {{gobin}}.exe {{go_cmd}}; \
    else \
        go build -o {{gobin}} {{go_cmd}}; \
    fi

# Install Go binary
install:
    go install {{go_cmd}}

# Run Go CLI
run:
    go run {{go_cmd}} --help

# Format Go files
fmt:
    gofmt -w go

# Run Go tests
test:
    go test ./go/...

# Clean artifacts
clean:
    rm -rf {{gobin}} {{gobin}}.exe dist coverage.out
