package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	printagent "print-agent"
)

func TestParse(t *testing.T) {
	data := []byte(`# comment
; other comment

PRINT_AGENT_API_URL=https://app.example.test
STORE_ADDRESS_LINE1="Rue de l'Exemple 1"
STORE_PHONE='01 23'
QUOTED="a \"b\" c"
EMPTY=
`)
	entries, problems := Parse(data)
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	m := Map(entries)
	want := map[string]string{
		"PRINT_AGENT_API_URL": "https://app.example.test",
		"STORE_ADDRESS_LINE1": "Rue de l'Exemple 1",
		"STORE_PHONE":         "01 23",
		"QUOTED":              `a "b" c`,
		"EMPTY":               "",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
	if entries[0].Line != 4 {
		t.Errorf("line = %d", entries[0].Line)
	}
}

func TestParseSyntaxErrors(t *testing.T) {
	for _, bad := range []string{"export A=1", "NOEQUALS", "1A=x", `A="open`} {
		_, problems := Parse([]byte(bad))
		if !HasFatal(problems) {
			t.Errorf("%q: no fatal problem", bad)
		}
	}
}

func TestValidate(t *testing.T) {
	good := []byte("PRINT_AGENT_API_URL=https://app.example.test\nPRINT_AGENT_API_KEY=k123456789\n" +
		"PRINT_AGENT_API_SECRET=\"s3cr3t\"\nPRINT_AGENT_POLL_INTERVAL=2s\n")
	if _, problems := Validate(good); len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}

	_, problems := Validate(printagent.EnvTemplate)
	if !HasFatal(problems) {
		t.Fatal("the untouched template must not validate")
	}

	bad := []byte("PRINT_AGENT_API_URL=app.example.test\nPRINT_AGENT_API_KEY=k\n" +
		"PRINT_AGENT_API_SECRET=s\nPRINT_AGENT_SYNC_INTERVAL=10\nSTORE_PHON=1\n")
	_, problems = Validate(bad)
	joined := ""
	for _, p := range problems {
		joined += p.String() + "\n"
	}
	for _, want := range []string{"must be an http(s) URL", "PRINT_AGENT_SYNC_INTERVAL", "warning: line 5: unknown variable STORE_PHON"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}

	_, problems = Validate([]byte("PRINT_AGENT_API_URL=https://x.test\n"))
	if !HasFatal(problems) {
		t.Fatal("missing credentials must be fatal")
	}
}

func TestMask(t *testing.T) {
	cases := []struct{ key, value, want string }{
		{"PRINT_AGENT_API_SECRET", "topsecretvalue", "********"},
		{"PRINT_AGENT_API_KEY", "abcd1234efgh", "abcd********"},
		{"PRINT_AGENT_API_KEY", "short", "********"},
		{"GITHUB_TOKEN", "ghp_xxxxxxxxxxxx", "********"},
		{"PRINT_AGENT_API_URL", "https://app.example.test", "https://app.example.test"},
		{"PRINT_AGENT_API_SECRET", "", ""},
	}
	for _, c := range cases {
		if got := Mask(c.key, c.value); got != c.want {
			t.Errorf("Mask(%s, %q) = %q, want %q", c.key, c.value, got, c.want)
		}
	}
}

func TestLoadDoesNotOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(path, []byte("ENVFILE_TEST_A=fromfile\nENVFILE_TEST_B=fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVFILE_TEST_A", "fromenv")
	t.Setenv("ENVFILE_TEST_B", "")
	_ = os.Unsetenv("ENVFILE_TEST_B")
	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("ENVFILE_TEST_A") != "fromenv" || os.Getenv("ENVFILE_TEST_B") != "fromfile" {
		t.Fatalf("A=%q B=%q", os.Getenv("ENVFILE_TEST_A"), os.Getenv("ENVFILE_TEST_B"))
	}
}
