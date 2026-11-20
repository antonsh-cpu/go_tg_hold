package profile

import (
	"testing"
	"time"
)

func TestDeterministic(t *testing.T) {
	a := FirstName(NewRand("15551234567"), Female)
	b := FirstName(NewRand("15551234567"), Female)
	if a != b {
		t.Fatalf("same seed gave %q and %q", a, b)
	}
}

func TestBioLength(t *testing.T) {
	for _, b := range bios {
		if len(b) > 70 {
			t.Errorf("bio too long (%d): %q", len(b), b)
		}
	}
}

func TestBirthdayAgeRange(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	r := NewRand("seed")
	for i := 0; i < 2000; i++ {
		b := Birthday(r, now, 22, 40)
		age := now.Year() - b.Year()
		if now.Before(b.AddDate(age, 0, 0)) {
			age--
		}
		if age < 22 || age > 40 {
			t.Fatalf("age %d out of range for %v", age, b)
		}
	}
}

func TestUsername(t *testing.T) {
	r := NewRand("seed")
	for i := 0; i < 500; i++ {
		u := Username(r, "Emily", "Miller")
		if len(u) < 5 || len(u) > 32 || u[0] < 'a' || u[0] > 'z' {
			t.Fatalf("bad username %q", u)
		}
	}
}
