.PHONY: dev test lint build migrate-up migrate-down

dev:
	go run ./cmd/regent

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/regent ./cmd/regent

migrate-up:
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path migrations -database "$(DATABASE_URL)" down 1
