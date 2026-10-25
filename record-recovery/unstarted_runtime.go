package main

// Exact normal result metadata for the new PR512 review only. The existing
// collector adds at most one review of its own compiled revision. Old completed
// PR448/486/495/509 review observations are not reacquired through this entry.
func unstartedReviewRuntimeRoots() []omb3255ExpectedRoot {
	return []omb3255ExpectedRoot{{"837aab36-0c9d-4867-8b9f-db9952bc3931", 1792942185190, 1792943381103, "030deaea7fea9045388865b5452942e1622a0627254fde129047356e217f7c5c"}}
}
