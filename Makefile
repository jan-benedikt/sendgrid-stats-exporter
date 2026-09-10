IMAGE    ?= chatwork/sendgrid-stats-exporter
GIT_TAG  := $(shell git tag --points-at HEAD 2>/dev/null)
GIT_HASH := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo HEAD)
VERSION  := $(shell if [ -n "$(GIT_TAG)" ]; then echo "$(GIT_TAG)"; else echo "$(GIT_HASH)"; fi)

DIST_DIR := ./dist

DOCKER_BUILD_PLATFORMS ?= linux/amd64,linux/arm64
DOCKER_BUILDX_ARGS ?= --push

LDFLAGS := -s -w \
	-X github.com/prometheus/common/version.Version=$(VERSION) \
	-X github.com/prometheus/common/version.Revision=$(GIT_HASH) \
	-X github.com/prometheus/common/version.Branch=$(GIT_BRANCH)

default: build

.PHONY: build
build:
	@echo "version: $(VERSION) hash: $(GIT_HASH) tag: $(GIT_TAG)"
	go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/exporter .

.PHONY: test
test:
	go test ./...

.PHONY: build-image
build-image:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg REVISION=$(GIT_HASH) \
		--build-arg BRANCH=$(GIT_BRANCH) \
		-t $(IMAGE) .
	docker tag $(IMAGE):latest $(IMAGE):$(VERSION)

.PHONY: push-image
push-image:
	docker push $(IMAGE)

.PHONY: build-image-multi
build-image-multi:
	docker buildx build \
		--build-arg VERSION=$(VERSION) \
		--build-arg REVISION=$(GIT_HASH) \
		--build-arg BRANCH=$(GIT_BRANCH) \
		-t $(IMAGE):$(VERSION) \
		--platform=$(DOCKER_BUILD_PLATFORMS) .

.PHONY: push-image-multi
push-image-multi:
	docker buildx build $(DOCKER_BUILDX_ARGS) \
		--build-arg VERSION=$(VERSION) \
		--build-arg REVISION=$(GIT_HASH) \
		--build-arg BRANCH=$(GIT_BRANCH) \
		-t $(IMAGE):$(VERSION) \
		--platform=$(DOCKER_BUILD_PLATFORMS) .
