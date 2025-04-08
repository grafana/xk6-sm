ROOTDIR                  := $(abspath $(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
GO_WORKSPACE             := $(abspath $(dir $(filter-out off,$(shell go env GOWORK))))
LOCAL_K6_VERSION         := $(shell GOWORK=off go list -m go.k6.io/k6 | cut -d' ' -f2)
LOCAL_GSM_CLIENT_VERSION := $(shell GOWORK=off go list -m github.com/grafana/gsm-api-go-client | cut -d' ' -f2)
WORKSPACE_K6_VERSION     := $(shell go list -m go.k6.io/k6 | cut -d' ' -f2)

XK6_SM_SRCS              := $(shell go list -json $(ROOTDIR) | jq -r '.Dir as $$dir | .GoFiles[]? | [$$dir, .] | join("/")')

.DEFAULT_GOAL := all

.PHONY: all
all: build
	@echo "Done."

.PHONY: build
build:
	@true

build: dist/k6
dist/k6: go.mod
dist/k6: Makefile
dist/k6: $(XK6_SM_SRCS)
dist/k6:
	@mkdir -p '$(dir $@)'
	xk6 build '$(LOCAL_K6_VERSION)' \
		--output '$@' \
		--with 'github.com/grafana/xk6-sm=$(ROOTDIR)' \
		--with 'github.com/grafana/gsm-api-go-client@$(LOCAL_GSM_CLIENT_VERSION)'

.PHONY: clean
clean:
	rm -rf dist
