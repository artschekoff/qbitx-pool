BINARY = qbxpool
OUT    = bin

IMAGE ?= ghcr.io/artschekoff/qbxpool
TAG ?= latest
PLATFORM ?= linux/amd64
PLATFORM_AMD64 ?= linux/amd64
PLATFORM_386 ?= linux/386
DOCKER ?= docker

.PHONY: build push build-pool-image push-pool build-amd64 build-386 push-amd64 push-386 build-all test clean run

build-all:
	@mkdir -p $(OUT)
	GOOS=linux   GOARCH=amd64 go build -o $(OUT)/$(BINARY)-linux-amd64        ./cmd/pool
	GOOS=linux   GOARCH=arm64 go build -o $(OUT)/$(BINARY)-linux-arm64        ./cmd/pool
	GOOS=darwin  GOARCH=amd64 go build -o $(OUT)/$(BINARY)-darwin-amd64       ./cmd/pool
	GOOS=darwin  GOARCH=arm64 go build -o $(OUT)/$(BINARY)-darwin-arm64       ./cmd/pool
	GOOS=windows GOARCH=amd64 go build -o $(OUT)/$(BINARY)-windows-amd64.exe  ./cmd/pool

test:
	go test ./...

clean:
	rm -rf $(OUT)

run: build
	$(OUT)/$(BINARY) config.yaml

build-pool-image:
	$(DOCKER) build --platform $(PLATFORM) -f Dockerfile -t $(IMAGE):$(TAG) .

push-pool: build-pool-image
	$(DOCKER) push $(IMAGE):$(TAG)

push: push-pool

build-amd64:
	$(DOCKER) build --platform $(PLATFORM_AMD64) -t $(IMAGE):$(TAG) .

push-amd64: build-amd64
	$(DOCKER) push $(IMAGE):$(TAG)

build-386:
	$(DOCKER) build --platform $(PLATFORM_386) -t $(IMAGE):$(TAG) .

push-386: build-386
	$(DOCKER) push $(IMAGE):$(TAG)

build:
	@mkdir -p $(OUT)
	go build -o $(OUT)/$(BINARY) ./cmd/pool

deploy:
	wget -O- https://docker.nftwitting.com/api/deploy/compose/Zpos867-YRG7yuSbGHhx5