.PHONY: web build test run
web:
	cd frontend && npm ci && npm run build
build: web
	go build -o bin/spotis ./cmd/spotis
test:
	go vet ./... && go test -race ./...
run:
	SPOTIS_PUBLIC_URL=http://localhost:8080 SPOTIS_DATA_DIR=./data go run ./cmd/spotis
