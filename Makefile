.PHONY: build test up down

# Build both binaries into ./bin
build:
	go build -o bin/gateway ./cmd/server
	go build -o bin/upstream ./cmd/upstream

# Run the black-box HTTP test suite (the single project seam)
test:
	go test ./...

# Bring up the full demo stack
up:
	docker compose up --build -d

# Tear down the demo stack
down:
	docker compose down
