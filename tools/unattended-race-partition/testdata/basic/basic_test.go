package basic

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestAlpha(t *testing.T) { fixtureCase(t, "TestAlpha") }

func TestBeta(t *testing.T) {
	t.Run("one", func(t *testing.T) { fixtureCase(t, "TestBeta/one") })
	t.Run("two", func(t *testing.T) { fixtureCase(t, "TestBeta/two") })
}

func TestGamma(t *testing.T) { fixtureCase(t, "TestGamma") }

func TestDelta(t *testing.T) { fixtureCase(t, "TestDelta") }

func FuzzEcho(f *testing.F) {
	f.Add("seed")
	f.Fuzz(func(t *testing.T, s string) {
		if Echo(s) != s {
			t.Fatal("Echo changed its input")
		}
	})
}

func BenchmarkEcho(b *testing.B) {
	for b.Loop() {
		Echo("x")
	}
}

func ExampleEcho() {
	fmt.Println(Echo("hello"))
	// Output: hello
}

// fixtureCase fails, hangs or exits in the case that the environment names.
func fixtureCase(t *testing.T, name string) {
	switch name {
	case os.Getenv("FIXTURE_FAIL"):
		t.Fatalf("%s failed on request", name)
	case os.Getenv("FIXTURE_HANG"):
		time.Sleep(time.Hour)
	case os.Getenv("FIXTURE_EXIT"):
		os.Exit(0)
	}
}
