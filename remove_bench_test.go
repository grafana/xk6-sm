// Copyright (C) 2025 Grafana Labs.
// SPDX-License-Identifier: AGPL-3.0-only

package sm_test

import (
	"fmt"
	"slices"
	"strconv"
	"testing"
)

func BenchmarkRemove(b *testing.B) {
	// Create a map with 1k keys from 1_000_000 to 1_100_000.
	needles := map[string]struct{}{}

	for i := range 1000 {
		key := strconv.Itoa(1_000_000 + 100*i)
		needles[key] = struct{}{}
	}

	for keys := 5; keys <= 20; keys += 5 {
		b.Run(fmt.Sprintf("%d keys", keys), func(b *testing.B) {
			// Create a list of #keys needles to find in map. 1
			// every [lcm(100, 40) / min(100, 40) = 5] needles
			// should be in map.
			sliceHaystack := []string{}
			for i := range keys {
				sliceHaystack = append(sliceHaystack, strconv.Itoa(1_000_000+40*i))
			}

			b.Run("map", func(b *testing.B) { findNeedleInHaystackMap(b, sliceHaystack, needles) })

			b.Run("slice", func(b *testing.B) { findNeedleInHaystackSlice(b, sliceHaystack, needles) })
		})
	}
}

func findNeedleInHaystackSlice(b *testing.B, haystack []string, needles map[string]struct{}) {
	b.Helper()

	found := 0

	for range b.N {
		for needle := range needles {
			if slices.Contains(haystack, needle) {
				found++
			}
		}
	}
}

func findNeedleInHaystackMap(b *testing.B, haystack []string, needles map[string]struct{}) {
	b.Helper()

	// Build a map version of the haystack.
	mapHaystack := map[string]bool{}
	for _, needle := range haystack {
		mapHaystack[needle] = true
	}

	b.ResetTimer()

	found := 0

	for range b.N {
		for needle := range needles {
			if mapHaystack[needle] {
				found++
			}
		}
	}
}
