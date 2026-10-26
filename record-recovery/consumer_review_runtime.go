package main

const consumerReviewSourceRoot = "D:/CC/Work/unattended-operations-20261013-wt/todo-3387-discord"

// Only the two remaining adapter reviews in the consumer repair chain. The
// consumer and duplicate-fix reviews were already observed by the previous
// clean reader; their saved proofs are retained without reacquisition.
func consumerReviewRuntimeRoots() []omb3255ExpectedRoot {
	return []omb3255ExpectedRoot{
		{"96d3ab14-f541-4773-a5d7-c446118cafdb", 1792986099371, 1792986239900, "ab6a5ebf6f49ea002369c644e16dd705a8d47274b83a08d318dc7bf494918009"},
		{"e1aab6dd-fbd9-44eb-80ba-78df8da40750", 1792987954721, 1792988160737, "c9def9aa13869d44c33c2858bbb02a592eec281db83400b824e149684dbec1ed"},
	}
}

func consumerReviewChooseNodes(job obj, revision string, commitMS int64, wanted []omb3255ExpectedRoot) ([]omb3255ExpectedRoot, error) {
	return omb3255ChooseNodesFor(job, revision, commitMS, wanted, consumerReviewNodeValid)
}

func consumerReviewNodeValid(row obj) bool {
	id, root, parent := omb3255Text(row["id"]), omb3255Text(row["rootId"]), omb3255Text(row["parentId"])
	parents := map[string]string{
		"96d3ab14-f541-4773-a5d7-c446118cafdb": "be6bbc17-a433-4e78-a594-645523a25771",
		"e1aab6dd-fbd9-44eb-80ba-78df8da40750": "8fb320ec-260e-40bf-8653-fdf6404a6fea",
	}
	if expected, fixed := parents[id]; fixed && (root != expected || parent != expected) {
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
