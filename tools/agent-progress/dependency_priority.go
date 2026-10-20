package main

// Use the same recorded edges for propagation and for the independent urgency
// map. A min that includes the code repository's own rank loses this evidence.
func priorityDependencies(item todoItem, items map[int64]todoItem) []int64 {
	if item.Status == "done" || item.Status == "block" || item.WaitReady != nil {
		return nil
	}
	return pending(append(append([]int64{}, item.WaitOn...), item.WorkWaitOn...), items)
}

func todoPriorityMaps(items map[int64]todoItem, labels map[int64]label) (map[int64]int, map[int64]int) {
	ranks := map[int64]int{}
	for id := range items {
		ranks[id] = labels[id].rank
	}
	for changed := true; changed; {
		changed = false
		for id, item := range items {
			for _, dependency := range priorityDependencies(item, items) {
				if rank, exists := ranks[dependency]; exists && ranks[id] < rank {
					ranks[dependency], changed = ranks[id], true
				}
			}
		}
	}
	dependencies := map[int64]int{}
	for id, item := range items {
		for _, dependency := range priorityDependencies(item, items) {
			if rank, exists := dependencies[dependency]; !exists || ranks[id] < rank {
				dependencies[dependency] = ranks[id]
			}
		}
	}
	return ranks, dependencies
}
