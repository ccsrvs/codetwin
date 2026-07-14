package fixture

// groupIssuesByLabel maps each label to the issue indices that carry
// it, skipping unlabeled entries. Classic map-of-slices accumulator.
func groupIssuesByLabel(issues []Issue) map[string][]int {
	byLabel := make(map[string][]int)
	for i, issue := range issues {
		for _, label := range issue.Labels {
			if label == "" {
				continue
			}
			byLabel[label] = append(byLabel[label], i)
		}
	}
	return byLabel
}
