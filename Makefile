.PHONY: all test fmt fmt-check build run shared rust cpp check clean

GOCACHE := $(CURDIR)/.gocache
GO      ?= go
BUILD   := $(CURDIR)/build

export GOCACHE

all: build

test:
	$(GO) test ./...

fmt:
	gofmt -w ./cmd ./engine ./capi

fmt-check:
	@out="$$(gofmt -l ./cmd ./engine ./capi 2>/dev/null)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

build:
	mkdir -p $(BUILD)
	$(GO) build -o $(BUILD)/yousdb ./cmd/yousdb

run:
	$(GO) run ./cmd/yousdb

shared:
	mkdir -p $(BUILD)
	$(GO) build -buildmode=c-shared -o $(BUILD)/libyousdb.dylib ./capi

rust: shared
	cargo build --manifest-path clients/rust/Cargo.toml --release

cpp: shared
	cmake -S clients/cpp -B $(BUILD)/cpp -DCMAKE_BUILD_TYPE=Release
	cmake --build $(BUILD)/cpp --parallel

check: fmt-check test
	@echo "ok"

clean:
	rm -rf $(BUILD) .gocache clients/rust/target
