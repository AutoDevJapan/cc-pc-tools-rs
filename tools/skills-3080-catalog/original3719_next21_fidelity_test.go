package main

import "testing"

func TestOriginal3719Next21PreservesThreeWholePacketsAndOriginalNoticeReaders(t *testing.T) {
	checkOriginal3719PendingSnapshot(t, "skills-3080-original3719-next21-268288-evaluation175645.json", "54b04b462a806d536fcf352c793e4c91a13783373544caa179b196edd64cbc62", 268, 288, 3, "20261028-171959-todo-3719", 228, 94, 134)
}
