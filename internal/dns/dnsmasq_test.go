package dns

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteConfigKeepsWildcardAndApexIndependent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dnsmasq.conf")
	routes := []Route{
		{Domain: "*.mydomain.test", ProjectID: "ldev_aaaaaaaaaaaa"},
		{Domain: "mydomain.test", ProjectID: "ldev_aaaaaaaaaaaa"},
		{Domain: "gkb.vm", ProjectID: "ldev_aaaaaaaaaaaa"},
	}
	if err := writeConfig(path, routes); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"host-record=mydomain.test,127.0.0.1\n",
		"address=/*.mydomain.test/127.0.0.1\n",
		"host-record=gkb.vm,127.0.0.1\n",
		"local=/test/\n",
		"local=/vm/\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "host-record=api.mydomain.test") {
		t.Fatal("wildcard route must not expand into synthetic host records")
	}
}

func TestValidateDomainRequiresSupportedSuffixAndAcceptsWildcard(t *testing.T) {
	for _, name := range []string{"mydomain.test", "*.mydomain.test", "API.MYDOMAIN.TEST", "gkb.vm", "*.gkb.vm"} {
		if err := ValidateDomain(name); err != nil {
			t.Errorf("ValidateDomain(%q): %v", name, err)
		}
	}
	for _, name := range []string{"mydomain.local", "*mydomain.test", "mydomain.test/path", "127.0.0.1", "gkb.vm.example"} {
		if err := ValidateDomain(name); err == nil {
			t.Errorf("ValidateDomain(%q) unexpectedly succeeded", name)
		}
	}
}

func TestUniqueRoutesDoesNotCollapseApexAndWildcard(t *testing.T) {
	routes := uniqueRoutes([]Route{
		{Domain: "mydomain.test", ProjectID: "ldev_aaaaaaaaaaaa"},
		{Domain: "*.mydomain.test", ProjectID: "ldev_aaaaaaaaaaaa"},
		{Domain: "mydomain.test", ProjectID: "ldev_aaaaaaaaaaaa"},
	})
	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(routes))
	}
}
