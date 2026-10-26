package main

// Exact normal result metadata for the new PR512 review only. The existing
// collector adds at most one review of its own compiled revision. Old completed
// PR448/486/495/509 review observations are not reacquired through this entry.
func unstartedReviewRuntimeRoots() []omb3255ExpectedRoot {
	return []omb3255ExpectedRoot{
		{"837aab36-0c9d-4867-8b9f-db9952bc3931", 1792942185190, 1792943381103, "030deaea7fea9045388865b5452942e1622a0627254fde129047356e217f7c5c"},
		{"6d821a13-a98a-4c7b-ac81-ca82eb8012cd", 1792945279719, 1792945733971, "363976843c92a6a30f9623110b782b49889947651ea0ff0e89a87ee0da0a1188"},
	}
}
