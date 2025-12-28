FROM golang:1.24-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY go ./go
ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG GIT_COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -X main.gitCommit=${GIT_COMMIT}" -o /out/telegram-send-go ./go

FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=build /out/telegram-send-go /app/telegram-send-go
COPY config.example.ini /app/config.ini

ENTRYPOINT ["/app/telegram-send-go"]
CMD ["--help"]
