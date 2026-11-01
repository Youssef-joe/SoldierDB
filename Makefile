.PHONY: all test fmt fmt-check build run shared ctest rust cpp check clean

GOCACHE := $(CURDIR)/.gocache
GO      ?= go
CC      ?= cc
BUILD   := $(CURDIR)/build
CFLAGS  ?= -O2 -Wall -Wextra

UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
SHARED_EXT := dylib
else
SHARED_EXT := so
endif
SHARED_LIB := $(BUILD)/libyousdb.$(SHARED_EXT)

ifeq ($(UNAME_S),Darwin)
# cgo leaves a bare install name on macOS, which rpath cannot resolve. Give the
# library an @rpath install name so clients can embed $(BUILD) as their rpath.
FIX_INSTALL_NAME := install_name_tool -id @rpath/libyousdb.dylib $(SHARED_LIB)
else
FIX_INSTALL_NAME := true
endif

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
	$(FIX_INSTALL_NAME)

ctest: shared
	$(CC) $(CFLAGS) -std=c11 -I$(BUILD) capi/test/smoke.c $(SHARED_LIB) \
		-Wl,-rpath,$(BUILD) -o $(BUILD)/capi_smoke
	rm -f $(BUILD)/capi_smoke.db
	$(BUILD)/capi_smoke $(BUILD)/capi_smoke.db

rust: shared
	cargo test --manifest-path clients/rust/Cargo.toml
	cargo build --manifest-path clients/rust/Cargo.toml --release

cpp: shared
	cmake -S clients/cpp -B $(BUILD)/cpp -DCMAKE_BUILD_TYPE=Release
	cmake --build $(BUILD)/cpp --parallel
	ctest --test-dir $(BUILD)/cpp --output-on-failure

check: fmt-check test ctest rust cpp
	@echo "ok"

clean:
	rm -rf $(BUILD) .gocache clients/rust/target
