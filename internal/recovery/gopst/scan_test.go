package gopst

import "testing"

func TestSafeNextTurnsPanicIntoEndOfScan(t *testing.T) {
	ok, panicText := safeNext(func() bool { panic("malformed property context") })
	if ok {
		t.Fatal("safeNext returned true after panic")
	}
	if panicText != "malformed property context" {
		t.Fatalf("panic text = %q", panicText)
	}
}

func TestSafeNextPreservesNormalIterator(t *testing.T) {
	ok, panicText := safeNext(func() bool { return true })
	if !ok {
		t.Fatal("safeNext changed a normal iterator result")
	}
	if panicText != "" {
		t.Fatalf("unexpected panic text = %q", panicText)
	}
}
