package unit

import "testing"

func TestConfigurationReferences(t *testing.T) {
	for _, tc := range []struct {
		name, content, target string
		want                  bool
	}{
		{"literal", "[Unit]\nRequires=demo.service\n", "demo.service", true},
		{"unrelated", "[Unit]\nRequires=other.service\n", "demo.service", false},
		{"ordering", "[Unit]\nAfter=demo.service\n", "demo.service", true},
		{"install", "[Install]\nWantedBy=custom.target\n", "custom.target", true},
		{"quoting", "[Unit]\nWants=\"demo\".service\n", "demo.service", true},
		{"continued with comments", "[Unit]\nRequires=other.service \\\n# comment\n; comment\n demo.service\n", "demo.service", true},
		{"template instance", "[Unit]\nRequires=worker@production.service\n", "worker@.service", true},
		{"template specifier", "[Unit]\nRequires=worker@%i.service\n", "worker@production.service", true},
		{"escaped literal", "[Unit]\nRequires=dev-disk-by\\x2duuid.device\n", `dev-disk-by\x2duuid.device`, true},
		{"decoded literal", "[Unit]\nRequires=demo\\x2eservice\n", "demo.service", true},
		{"unrelated escaped name", "[Unit]\nRequires=dev-disk-by\\x2duuid.device\n", "demo.service", false},
		{"reset conservative", "[Unit]\nRequires=demo.service\nRequires=\n", "demo.service", true},
		{"comments ignored", "[Unit]\n# Requires=demo.service\n", "demo.service", false},
		{"exec is not dependency", "[Service]\nExecStart=/usr/bin/echo demo.service\n", "demo.service", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := References(tc.content, []string{tc.target}); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestImplicitActivationReferences(t *testing.T) {
	for _, tc := range []struct {
		source, target string
		want           bool
	}{
		{"wake.timer", "wake.service", true},
		{"wake.path", "wake.service", true},
		{"wake.socket", "wake.service", true},
		{"wake.socket", "wake@.service", true},
		{"wake@.timer", "wake@production.service", true},
		{"wake@production.timer", "wake@.service", true},
		{"wake@.timer", "other.service", false},
		{"wake.service", "wake.service", false},
	} {
		if got := ImplicitReferences(tc.source, []string{tc.target}); got != tc.want {
			t.Fatalf("source=%s target=%s got=%v", tc.source, tc.target, got)
		}
	}
}

func TestTemplateNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"worker@.service", true},
		{"worker@production.service", false},
		{"worker@.hidden.service", false},
		{"worker.service", false},
	} {
		if got := IsTemplate(tc.name); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}
