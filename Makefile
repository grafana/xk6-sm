ROOTDIR                := $(abspath $(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
GO_WORKSPACE           := $(abspath $(dir $(filter-out off,$(shell go env GOWORK))))
LOCAL_K6_VERSION       := $(shell GOWORK=off go list -m go.k6.io/k6 | cut -d' ' -f2)
WORKSPACE_K6_VERSION   := $(shell go list -m go.k6.io/k6 | cut -d' ' -f2)
# This is gross.
GSM_API_GO_CLIENT_HASH := $(shell grep github.com/grafana/gsm-api-go-client@ .github/workflows/push-pr-release.yaml | sed -e 's,.*@,,; s,[^a-f0-9].*,,')
GSM_API_GO_CLIENT      := github.com/grafana/gsm-api-go-client@$(GSM_API_GO_CLIENT_HASH)

ifneq ($(strip $(GO_WORKSPACE)),)
GSM_API_GO_CLIENT_DIR  := $(abspath $(ROOTDIR)/../gsm-api-go-client)
GSM_API_GO_CLIENT      := github.com/grafana/gsm-api-go-client=$(GSM_API_GO_CLIENT_DIR)
GSM_API_GO_CLIENT_SRCS := $(shell go list -json $(GSM_API_GO_CLIENT_DIR) | jq -r '.Dir as $$dir | .GoFiles[]? | [$$dir, .] | join("/")')
endif

XK6_SM_SRCS            := $(shell go list -json $(ROOTDIR) | jq -r '.Dir as $$dir | .GoFiles[]? | [$$dir, .] | join("/")')

.DEFAULT_GOAL := all

.PHONY: all
all: build
	@echo "Done."

.PHONY: build
build:
	@true

build: dist/k6
dist/k6: $(XK6_SM_SRCS)
	@mkdir -p '$(dir $@)'
	xk6 build '$(LOCAL_K6_VERSION)' \
		--output '$@' \
		--with 'github.com/grafana/xk6-sm=$(ROOTDIR)'

ifeq ($(strip $(GO_WORKSPACE)),)
# This is here to force a rebuild every time if we are using a remote gsm-api-go-client.
.PHONY: dist/k6-gsm
endif

build: dist/k6-gsm
dist/k6-gsm: $(XK6_SM_SRCS)
dist/k6-gsm: $(GSM_API_GO_CLIENT_SRCS)
	@mkdir -p '$(dir $@)'
	xk6 build '$(WORKSPACE_K6_VERSION)' \
		--output '$@' \
		--with 'github.com/grafana/xk6-sm=$(ROOTDIR)' \
		--with '$(GSM_API_GO_CLIENT)'

.PHONY: clean
clean:
	rm -rf dist
