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

func consumerReviewChooseNodes(job obj, revision string, commitMS int64, wanted []omb3255ExpectedRoot) ([]omb3255ExpectedRoot, error) {
	return omb3255ChooseNodesFor(job, revision, commitMS, wanted, consumerReviewNodeValid)
}

func consumerReviewNodeValid(row obj) bool {
	id, root, parent := omb3255Text(row["id"]), omb3255Text(row["rootId"]), omb3255Text(row["parentId"])
	if id == "c998d376-5fe2-4832-bac8-5e8a049fc61d" && (root != "0a54f034-1aae-4a1e-b65c-932d89a6b80b" || parent != root) {
		return false
	}
	if omb3255RootValid(row) {
		return true
	}
	// The normal supervisor API records a delegated execution as depth 1,
	// retaining the root/parent from the sending supervisor. Only this purpose
	// admits that shape; old commands continue to require their original roots.
	return consumerReviewUUID(id) && consumerReviewUUID(root) && root != id && parent == root &&
		omb3255Int(row["depth"]) == 1 && omb3255Text(row["botId"]) == omb3255Bot &&
		omb3255Text(row["threadId"]) == omb3255Task && omb3255Text(row["status"]) == "completed" &&
		omb3255Text(row["outcome"]) == "done" && omb3255Int(row["executions"]) == 1
}

func consumerReviewUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
