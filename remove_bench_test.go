package sm_test

import (
	"fmt"
	"slices"
	"strconv"
	"testing"
)

func BenchmarkRemove(b *testing.B) {
	// Create a map with 1k keys from 1_000_000 to 1_100_000.
	haystack := map[string]struct{}{}
	for i := 0; i < 1000; i++ {
		key := strconv.Itoa(1_000_000 + 100*i)
		haystack[key] = struct{}{}
	}

	for keys := 5; keys <= 20; keys += 5 {
		b.Run(fmt.Sprintf("%d keys", keys), func(b *testing.B) {
			// Create a list of #keys needles to found in map. 1 every [lcm(100, 40) / min(100, 40) = 5] needles should be in map.
			needleSlice := []string{}
			for i := 0; i < keys; i++ {
				needleSlice = append(needleSlice, strconv.Itoa(1_000_000+40*i))
			}

			// Map version of the needle list.
			needleMap := map[string]bool{}
			for _, needle := range needleSlice {
				needleMap[needle] = true
			}

			b.Run("map", func(b *testing.B) {
				found := 0
				for i := 0; i < b.N; i++ {
					for key := range haystack {
						if needleMap[key] {
							found++
						}
					}
				}
			})

			b.Run("slice", func(b *testing.B) {
				found := 0
				for i := 0; i < b.N; i++ {
					for key := range haystack {
						if slices.Contains(needleSlice, key) {
							found++
						}
					}
				}
			})
		})
	}
}
