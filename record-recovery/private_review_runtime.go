package main

// A separate adapter keeps the new private-case review out of all completed
// PR448/486/495/509/512 review scopes. No request settings or model is pinned.
func privateReviewRuntimeRoots() []omb3255ExpectedRoot {
	return []omb3255ExpectedRoot{
		{"e3069b1f-0901-4def-b70f-a140a39d7f4b", 1792959872006, 1792959954969, "261a6eafa25997cd62c63d68b7a069cfc34ee765f679812f0bc2f76732aa9399"},
	}
}
