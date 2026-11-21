package platform

import (
	"testing"
	"time"
)

func TestNewIDIsVersion7AndSortable(t *testing.T) {
	t.Parallel()
	a := NewID()
	b := NewID()
	if a.Version() != 7 || b.Version() != 7 {
		t.Fatalf("versions = %d %d, want 7", a.Version(), b.Version())
	}
	if a == b {
		t.Fatal("ids must differ")
	}
	if a.String() >= b.String() {
		t.Errorf("v7 ids must sort by creation: %s >= %s", a, b)
	}
}

func TestSystemClockIsUTC(t *testing.T) {
	t.Parallel()
	now := SystemClock{}.Now()
	if now.Location() != time.UTC {
		t.Errorf("location = %v, want UTC", now.Location())
	}
}
