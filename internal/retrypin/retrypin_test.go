package retrypin

import (
	"fmt"
	"os"
	"testing"
)

// helpers around the pinned variable's live state
func lookup(t *testing.T) (value string, had bool) {
	return os.LookupEnv(EnvVar)
}

func value(t *testing.T) string {
	return os.Getenv(EnvVar)
}

func osSetenv(t *testing.T, v string) {
	t.Setenv(EnvVar, v)
}

func TestPin(t *testing.T) {
	cases := []struct {
		method  string
		retries int
		value   string
		pin     bool
	}{
		{"POST", NotSet, "0", true},  // DF-044 default: one attempt
		{"PATCH", NotSet, "0", true}, // DF-044 default: one attempt
		{"POST", 0, "0", true},       // hard disable == single attempt
		{"POST", 1, "0", true},       // 1 attempt is not replay
		{"POST", 2, "", false},       // explicit opt-in to write replay
		{"POST", 3, "", false},       // explicit opt-in to write replay
		{"GET", NotSet, "", false},   // engine default (3 attempts)
		{"GET", 0, "0", true},        // hard disable for reads too
		{"GET", 1, "1", true},        // single attempt by ceiling
		{"GET", 3, "3", true},        // attempt ceiling 3
		{"PUT", NotSet, "", false},   // idempotent per HTTP semantics
		{"DELETE", 2, "2", true},
		{"delete", NotSet, "", false}, // method case-insensitive
	}
	for _, tc := range cases {
		value, pin := Pin(tc.method, tc.retries)
		if value != tc.value || pin != tc.pin {
			t.Errorf("Pin(%s, %d) = (%q, %v); want (%q, %v)", tc.method, tc.retries, value, pin, tc.value, tc.pin)
		}
	}
}

func TestHoldRestoresEnvironment(t *testing.T) {
	t.Run("env was unset before the pin", func(t *testing.T) {
		if _, had := lookup(t); had {
			t.Fatalf("%s must be unset for this test, got %q", EnvVar, value(t))
		}
		err := Hold("0", func() error {
			if got := value(t); got != "0" {
				t.Errorf("inside pin: %s = %q, want \"0\"", EnvVar, got)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, had := lookup(t); had {
			t.Errorf("pin leaked: %s still set to %q", EnvVar, value(t))
		}
	})

	t.Run("operator's value is restored after the pin", func(t *testing.T) {
		t.Setenv(EnvVar, "5")
		err := Hold("0", func() error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		if got := value(t); got != "5" {
			t.Errorf("%s = %q after pin; want operator value \"5\"", EnvVar, got)
		}
	})
}

func TestAround(t *testing.T) {
	t.Run("write with no explicit retries runs under the pin", func(t *testing.T) {
		err := Around("POST", NotSet, func() error {
			if got := value(t); got != "0" {
				t.Errorf("inside pin: %s = %q, want \"0\"", EnvVar, got)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("write opted into replay leaves the environment alone", func(t *testing.T) {
		osSetenv(t, "7")
		err := Around("POST", 3, func() error {
			if got := value(t); got != "7" {
				t.Errorf("opt-in must not touch %s; got %q, want \"7\"", EnvVar, got)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := value(t); got != "7" {
			t.Errorf("%s = %q; want \"7\"", EnvVar, got)
		}
	})

	t.Run("read with no explicit retries leaves the environment alone", func(t *testing.T) {
		osSetenv(t, "2")
		err := Around("GET", NotSet, func() error {
			if got := value(t); got != "2" {
				t.Errorf("no-pin read must keep %s; got %q, want \"2\"", EnvVar, got)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := value(t); got != "2" {
			t.Errorf("%s = %q; want \"2\"", EnvVar, got)
		}
	})

	t.Run("call error propagates under the pin", func(t *testing.T) {
		want := fmt.Errorf("boom")
		err := Around("POST", NotSet, func() error { return want })
		if err != want {
			t.Errorf("Around returned %v; want %v", err, want)
		}
		if _, had := lookup(t); had {
			t.Errorf("pin leaked: %s still set", EnvVar)
		}
	})
}
