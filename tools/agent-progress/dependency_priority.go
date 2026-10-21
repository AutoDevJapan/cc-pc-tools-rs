package main

import (
	"sort"
	"strconv"
)

// Resolve current business ownership before propagating a request. A code
// repository's rank is not the requester's business priority.
func mcpCurrentTargets(items map[int64]todoItem, jobs []loadedJob, policy priorityPolicy, references map[string][]int64) map[int64]mcpResolution {
	ordered := append([]loadedJob(nil), jobs...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].meta.CreatedAt != ordered[j].meta.CreatedAt {
			return ordered[i].meta.CreatedAt > ordered[j].meta.CreatedAt
		}
		return ordered[i].meta.ID < ordered[j].meta.ID
	})
	targets, chosen := map[int64]mcpResolution{}, map[int64]bool{}
	for _, loaded := range ordered {
		job := loaded.meta
		if !liveStatus(job.Status) && job.Status != "pending" {
			continue
		}
		ids := positiveIDs(append(append(append([]int64{}, job.RelatedTodoIDs...), references[job.ID]...), job.TodoID))
		ids, target := mcpRelations(job, ids, items, policy)
		id := mcpPrimary(job, ids, items)
		if id == 0 || chosen[id] {
			continue
		}
		chosen[id] = true
		if item, exists := items[id]; exists && !mcpResolve(item, policy, strconv.FormatInt(id, 10)).scoped &&
			(target.state == "verified" || target.state == "owner_declared") {
			targets[id] = target
		}
	}
	return targets
}

// Use the same recorded edges for propagation and for the independent urgency
// map. A min that includes the code repository's own rank loses this evidence.
func priorityDependencies(item todoItem, items map[int64]todoItem) []int64 {
	if item.Status == "done" || item.Status == "block" || item.WaitReady != nil {
		return nil
	}
	var targets []int64
	for _, id := range pending(append(append([]int64{}, item.WaitOn...), item.WorkWaitOn...), items) {
		if _, exists := items[id]; exists {
			targets = append(targets, id)
		}
	}
	return targets
}

func todoPriorityMaps(items map[int64]todoItem, labels map[int64]label, businessTargets map[int64]mcpResolution) (map[int64]int, map[int64]int) {
	ranks := map[int64]int{}
	for id := range items {
		ranks[id] = labels[id].rank
		if business, exists := businessTargets[id]; exists {
			ranks[id] = business.rank
		}
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
