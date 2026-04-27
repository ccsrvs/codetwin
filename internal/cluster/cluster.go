// Package cluster implements DBSCAN over an arbitrary distance function.
// Rather than reporting O(n²) pairs, DBSCAN groups families of similar
// snippets into clusters — each cluster is one refactoring opportunity.
package cluster

const noise = -1

// Result holds the cluster label for each input point.
// Label == -1 means the point was classified as noise (no cluster).
type Result struct {
	Labels     []int
	NumClusters int
}

// DistFunc returns the distance between points i and j (in [0,1]).
type DistFunc func(i, j int) float64

// DBSCAN clusters n points using the provided distance function.
//
//   eps     — maximum distance for two points to be considered neighbors
//   minPts  — minimum number of neighbors to form a core point
func DBSCAN(n int, eps float64, minPts int, dist DistFunc) Result {
	labels := make([]int, n)
	for i := range labels {
		labels[i] = noise
	}

	clusterID := 0

	for i := 0; i < n; i++ {
		if labels[i] != noise {
			continue
		}

		nb := neighbors(i, n, eps, dist)
		if len(nb) < minPts-1 { // -1 because neighbors excludes i itself
			continue // remains noise for now
		}

		labels[i] = clusterID
		seeds := make([]int, len(nb))
		copy(seeds, nb)

		for len(seeds) > 0 {
			j := seeds[0]
			seeds = seeds[1:]

			if labels[j] == noise {
				labels[j] = clusterID
			}
			if labels[j] != noise && labels[j] != clusterID {
				continue // already assigned to another cluster
			}
			labels[j] = clusterID

			nb2 := neighbors(j, n, eps, dist)
			if len(nb2) >= minPts-1 {
				for _, k := range nb2 {
					if labels[k] == noise {
						seeds = append(seeds, k)
					}
				}
			}
		}

		clusterID++
	}

	return Result{Labels: labels, NumClusters: clusterID}
}

// Groups returns a map from cluster ID → slice of point indices.
// Noise points (label == -1) are omitted.
func Groups(result Result) map[int][]int {
	groups := make(map[int][]int)
	for i, label := range result.Labels {
		if label != noise {
			groups[label] = append(groups[label], i)
		}
	}
	return groups
}

func neighbors(i, n int, eps float64, dist DistFunc) []int {
	var nb []int
	for j := 0; j < n; j++ {
		if i != j && dist(i, j) <= eps {
			nb = append(nb, j)
		}
	}
	return nb
}
