// Copyright (C) 2026 Grafana Labs.
// SPDX-License-Identifier: AGPL-3.0-only

package sm

import (
	"github.com/grafana/xk6-sm/internal/secrets"
	"go.k6.io/k6/secretsource"
)

func init() { //nolint:gochecknoinits // This is how k6 extensions work.
	secretsource.RegisterExtension("grafanasecrets", secrets.EntryPoint)
}
