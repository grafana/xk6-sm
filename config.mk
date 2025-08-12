# This file is included from the main makefile. Anything that is
# specific to this module should go in this file.

PLATFORMS = $(sort $(HOST_OS)/$(HOST_ARCH) linux/amd64 linux/arm64 darwin/arm64)
LOCAL_K6_VERSION             := $(shell GOWORK=off go list -m go.k6.io/k6 | cut -d' ' -f2)
LOCAL_GSM_API_CLIENT_VERSION := $(shell GOWORK=off go list -m github.com/grafana/gsm-api-go-client | cut -d' ' -f2)
