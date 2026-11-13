package notests

import "testing"

// helper is not a test case, so the test binary lists nothing.
func helper(t *testing.T) { t.Helper() }
