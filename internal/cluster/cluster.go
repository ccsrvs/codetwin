// Package cluster implements DBSCAN over an arbitrary distance function.
// Rather than reporting O(n²) pairs, DBSCAN groups families of similar
// snippets into clusters — each cluster is one refactoring opportunity.
package cluster

const noise = -1

// Result holds the cluster label for each input point.
// Label == -1 means the point was classified as noise (no cluster).
type Result struct {
	Labels      []int
	NumClusters int
}

// DistFunc returns the distance between points i and j (in [0,1]).
type DistFunc func(i, j int) float64

// DBSCAN clusters n points using the provided distance function.
//
//	eps     — maximum distance for two points to be considered neighbors
//	minPts  — minimum number of neighbors to form a core point
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

		// inSeeds tracks which points have ever been queued for this
		// cluster's expansion, so densely-connected graphs don't enqueue
		// the same point multiple times. Without this, a fully-connected
		// graph with n points enqueues O(n²) entries and recomputes
		// neighbors O(n³) times — the practical "hang" pathology on
		// codebases where a single big cluster swallows most chunks.
		inSeeds := make([]bool, n)
		inSeeds[i] = true

		labels[i] = clusterID
		seeds := make([]int, 0, len(nb))
		for _, k := range nb {
			if !inSeeds[k] {
				inSeeds[k] = true
				seeds = append(seeds, k)
			}
		}

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
					if !inSeeds[k] {
						inSeeds[k] = true
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

// Components partitions points into connected components under the
// symmetric link predicate — single-linkage: two points share a
// component when a chain of directly linked points connects them.
// link is called with the point *values* (e.g. snippet indices), not
// their positions in the slice.
//
// Used to split low-cohesion DBSCAN clusters at a stricter bound:
// re-link the cluster's members with a tighter predicate and each
// resulting component becomes its own cluster (singleton components
// have no partner at the stricter bound and drop out as noise).
//
// Deterministic: each component's members keep their input order, and
// components are ordered by their first member's input position.
func Components(points []int, link func(a, b int) bool) [][]int {
	var comps [][]int
	assigned := make([]bool, len(points))
	for start := range points {
		if assigned[start] {
			continue
		}
		assigned[start] = true
		// BFS over slice positions; comp doubles as the queue.
		comp := []int{start}
		for qi := 0; qi < len(comp); qi++ {
			cur := comp[qi]
			for cand := range points {
				if !assigned[cand] && link(points[cur], points[cand]) {
					assigned[cand] = true
					comp = append(comp, cand)
				}
			}
		}
		// BFS discovers positions out of order; restore input order.
		sortInts(comp)
		members := make([]int, len(comp))
		for i, pos := range comp {
			members[i] = points[pos]
		}
		comps = append(comps, members)
	}
	return comps
}

// sortInts is an insertion sort — component sizes are small and this
// avoids pulling in the sort package for one call site.
func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
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
