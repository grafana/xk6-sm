##@ Testing

GO_TEST_ARGS ?= $(GO_PKGS)

TEST_OUTPUT := $(DISTDIR)/test

ifeq ($(CI),true)
GOTESTSUM ?= gotestsum
endif

ifeq ($(origin GOTESTSUM),undefined)
GOTESTSUM ?= ./scripts/docker-run gotestsum
endif

# CI runs default to running all tests.
TEST_SHORT ?= $(if $(filter $(CI),true),false,true)

# If TEST_SHORT is set to true, we run only short tests.
ifeq ($(strip $(TEST_SHORT)),true)
TEST_SHORT_ARG := -short
else
TEST_SHORT_ARG :=
endif

# In CI we never depend on `build-native` as that as already been done. If we
# are running only short tests, do not pass `build-native` as a dependency to
# speed things up.
ifeq ($(or $(filter $(CI),true), $(filter $(TEST_SHORT),true)),true)
EXTRA_TEST_DEPS :=
else
EXTRA_TEST_DEPS := build-native
endif

.PHONY: test-go
test-go: $(EXTRA_TEST_DEPS)
test-go: ## Run Go tests.
	$(S) echo "test backend"
	$(S) mkdir -p '$(DISTDIR)'
	# CGO_ENABLED is required for -race
	CGO_ENABLED=1 $(GOTESTSUM) \
		--format standard-verbose \
		--jsonfile $(TEST_OUTPUT).json \
		--junitfile $(TEST_OUTPUT).xml \
		-- \
		$(GO_BUILD_MOD_FLAGS) \
		-cover \
		-coverprofile=$(TEST_OUTPUT).cov \
		-race \
		$(TEST_SHORT_ARG) \
		$(GO_TEST_ARGS)
	$(S) $(ROOTDIR)/scripts/report-test-coverage $(TEST_OUTPUT).cov

.PHONY: test
test: test-go ## Run all tests.
