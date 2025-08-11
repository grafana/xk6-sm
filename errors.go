// Copyright (C) 2025 Grafana Labs.
// SPDX-License-Identifier: AGPL-3.0-only

package sm

type stringError string

func (e stringError) Error() string {
	return string(e)
}

var _ error = stringError("")

const (
	errOutputFilenameRequired = stringError("output filename required")
)
