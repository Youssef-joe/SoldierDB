.PHONY: all test fmt fmt-check build run shared ctest rust cpp check clean

GOCACHE := $(CURDIR)/.gocache
GO      ?= go
CC      ?= cc
BUILD   := $(CURDIR)/build
CFLAGS  ?= -O2 -Wall -Wextra

UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
SHARED_EXT := dylib
RUN_LIB_PATH := DYLD_LIBRARY_PATH=$(BUILD)
else
SHARED_EXT := so
RUN_LIB_PATH := LD_LIBRARY_PATH=$(BUILD)
endif
SHARED_LIB := $(BUILD)/libyousdb.$(SHARED_EXT)

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
	$(GO) build -buildmode=c-shared -o $(SHARED_LIB) ./capi

ctest: shared
	$(CC) $(CFLAGS) -std=c11 -I$(BUILD) capi/test/smoke.c $(SHARED_LIB) -o $(BUILD)/capi_smoke
	rm -f $(BUILD)/capi_smoke.db
	$(RUN_LIB_PATH) $(BUILD)/capi_smoke $(BUILD)/capi_smoke.db

rust: shared
	cargo build --manifest-path clients/rust/Cargo.toml --release

cpp: shared
	cmake -S clients/cpp -B $(BUILD)/cpp -DCMAKE_BUILD_TYPE=Release
	cmake --build $(BUILD)/cpp --parallel

check: fmt-check test ctest
	@echo "ok"

clean:
	rm -rf $(BUILD) .gocache clients/rust/target
