package unit

import "testing"

func TestManagedUnitNames(t *testing.T) {
	for _, name := range []string{"app.service", "app.timer", "app.socket", "app.path", "default.target", "-.slice", "-.mount", "home.mount", "home.automount", "swap.swap", "session-1.scope", `dev-disk-by\x2duuid.device`, "worker@.service", "worker@one.service", "org.example:worker.service"} {
		if err := ValidateName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"", "app", "../app.service", "x.service/y", "*.service", "x?.timer", "[x].service", "x\n.service", "x$(id).service", "app.unknown", `x\q.service`} {
		if err := ValidateName(name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
}
