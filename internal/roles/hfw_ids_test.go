package roles

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The HowlFutureWorks identifiers HowlForge maps onto, from
// github.com/howlcipher/HowlFutureWorks at 4a6ea1bb7ab7732a5771171cf1812c79db594572
// (roles/*.md and routing/capability-registry.yaml). Update both together.
var (
	hfwRoles = []string{
		"auditor", "dev-lead", "developer", "devops", "engineering-manager", "owner", "product",
		"qa", "release-manager", "rnd", "security", "software-architect", "sre", "support", "writer",
	}
	hfwExecutors = []string{"agy", "astra", "claude", "codex"}
)

func contains(list []string, v string) bool {
	i := sort.SearchStrings(list, v)
	return i < len(list) && list[i] == v
}

func TestHowlFutureWorksIdentifiers(t *testing.T) {
	sort.Strings(hfwRoles)
	sort.Strings(hfwExecutors)
	check := func(dir, key string, known []string, requireAll bool) {
		files, err := filepath.Glob(filepath.Join("..", "..", "config", dir, "*.yaml"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no %s config: %v", dir, err)
		}
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				ID       string            `yaml:"id"`
				Metadata map[string]string `yaml:"metadata"`
			}
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			v, ok := doc.Metadata[key]
			if !ok {
				if requireAll {
					t.Errorf("%s: %s has no metadata.%s", f, doc.ID, key)
				}
				continue
			}
			if !contains(known, v) {
				t.Errorf("%s: metadata.%s %q is not a HowlFutureWorks id (%s)", f, key, v, strings.Join(known, ", "))
			}
		}
	}
	check("roles", "hfw_role", hfwRoles, true)
	check("runtimes", "hfw_executor", hfwExecutors, false)
}
