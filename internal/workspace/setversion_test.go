package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetVersionTouchesOnlyTheTopLevelValue(t *testing.T) {
	dir := t.TempDir()
	// Keys this binary does not model, an extension's own "version", odd
	// spacing and a shell string that Config.Save would HTML-escape.
	in := `{
  "type": "consigliere",
  "extensions": [{"name": "x", "version": "0.1.0"}],
  "version" :  "1.20.0",
  "obsidian": {"futureKey": true},
  "statuslineUpstream": "~/a.sh && echo '<b>' > /dev/null"
}
`
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetVersion(dir, "1.25.0"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(in, `"1.20.0"`, `"1.25.0"`, 1)
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	cfg, err := Detect(dir)
	if err != nil || cfg == nil || cfg.Version != "1.25.0" {
		t.Errorf("Detect after SetVersion: %+v, %v", cfg, err)
	}
}

func TestSetVersionRefusesWhenItCannotPatchSafely(t *testing.T) {
	for name, in := range map[string]string{
		"missing":    `{"type": "consigliere"}`,
		"not string": `{"type": "consigliere", "version": 3}`,
		"not object": `["version"]`,
		"broken":     `{"type": "consigliere", "version": "1`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(in), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := SetVersion(dir, "1.25.0"); err == nil {
				t.Error("want an error")
			}
			if got, _ := os.ReadFile(filepath.Join(dir, ConfigFile)); string(got) != in {
				t.Errorf("file changed despite the error: %s", got)
			}
		})
	}
}
