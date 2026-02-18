.PHONY: build build-miner build-all build-all-miner clean test run run-miner

BINARY = qbxpool
MINER_BINARY = miner
OUT    = bin
MINER_ADDR ?= 127.0.0.1:3333
MINER_WORKER ?= worker.001

build:
	@mkdir -p $(OUT)
	go build -o $(OUT)/$(BINARY) ./cmd/pool

build-miner:
	@mkdir -p $(OUT)
	go build -o $(OUT)/$(MINER_BINARY) ./cmd/miner

build-all:
	@mkdir -p $(OUT)
	GOOS=linux   GOARCH=amd64 go build -o $(OUT)/$(BINARY)-linux-amd64        ./cmd/pool
	GOOS=linux   GOARCH=arm64 go build -o $(OUT)/$(BINARY)-linux-arm64        ./cmd/pool
	GOOS=darwin  GOARCH=amd64 go build -o $(OUT)/$(BINARY)-darwin-amd64       ./cmd/pool
	GOOS=darwin  GOARCH=arm64 go build -o $(OUT)/$(BINARY)-darwin-arm64       ./cmd/pool
	GOOS=windows GOARCH=amd64 go build -o $(OUT)/$(BINARY)-windows-amd64.exe  ./cmd/pool

build-all-miner:
	@mkdir -p $(OUT)
	GOOS=linux   GOARCH=amd64 go build -o $(OUT)/$(MINER_BINARY)-linux-amd64        ./cmd/miner
	GOOS=linux   GOARCH=arm64 go build -o $(OUT)/$(MINER_BINARY)-linux-arm64        ./cmd/miner
	GOOS=darwin  GOARCH=amd64 go build -o $(OUT)/$(MINER_BINARY)-darwin-amd64       ./cmd/miner
	GOOS=darwin  GOARCH=arm64 go build -o $(OUT)/$(MINER_BINARY)-darwin-arm64       ./cmd/miner
	GOOS=windows GOARCH=amd64 go build -o $(OUT)/$(MINER_BINARY)-windows-amd64.exe  ./cmd/miner

test:
	go test ./...

clean:
	rm -rf $(OUT)

run: build
	$(OUT)/$(BINARY) config.yaml

run-miner: build-miner
	$(OUT)/$(MINER_BINARY) $(MINER_ADDR) $(MINER_WORKER)
