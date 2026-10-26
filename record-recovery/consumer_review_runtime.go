package main

const consumerReviewSourceRoot = "D:/CC/Work/unattended-operations-20261013-wt/todo-3387-discord"

// Only the completed consumer repair review. This never reacquires the old
// private-case review, changes its admission, or pins a requested model.
func consumerReviewRuntimeRoots() []omb3255ExpectedRoot {
	return []omb3255ExpectedRoot{{
		"c998d376-5fe2-4832-bac8-5e8a049fc61d", 1792985405622, 1792985551232,
		"125df4c40339e3a344925e437c2162d29256d5d6be03888e0e019caa39aa29d6",
	}}
}
