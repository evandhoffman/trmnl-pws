package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// testKey is 32+ printable characters, with characters that JSON escapes.
const testKey = `k3y<with>&"quote-0123456789abcdefXYZ`

const goodSettings = `---
strategy: polling
polling_url: https://poller.example.com/v1/countdown
polling_verb: get
polling_headers: |-
  X-Poller-Key: __POLLER_KEY__
refresh_interval: 60
name: Countdown
`

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// tmplDir writes one template directory and returns its path.
func tmplDir(t *testing.T, settings, liquid string) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "countdown")
	writeFile(t, filepath.Join(d, "settings.yml"), settings)
	writeFile(t, filepath.Join(d, "full.liquid"), liquid)
	writeFile(t, filepath.Join(d, "fixture.json"), `{"columns":[]}`)
	return d
}

func TestBuildSubstitutesKey(t *testing.T) {
	d := tmplDir(t, goodSettings, "<div>{{ columns[0].name }}</div>")
	got, err := build(d, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got["settings.yml"], "X-Poller-Key: "+testKey) {
		t.Errorf("key not substituted:\n%s", strings.ReplaceAll(got["settings.yml"], testKey, "<KEY>"))
	}
	if strings.Contains(got["settings.yml"], "__POLLER_KEY__") {
		t.Error("placeholder left in settings.yml")
	}
	if got["full.liquid"] != "<div>{{ columns[0].name }}</div>" {
		t.Errorf("full.liquid changed: %q", got["full.liquid"])
	}
	names := make([]string, 0, len(got))
	for n := range got {
		names = append(names, n)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"full.liquid", "settings.yml"}) {
		t.Errorf("files = %v, want only settings.yml and full.liquid", names)
	}
}

func TestBuildRealCountdownTemplate(t *testing.T) {
	got, err := build(filepath.Join("..", "..", "templates", "countdown"), testKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got["settings.yml"], "polling_url: https://trmnl-poller.evanhoffman.com/v1/countdown") {
		t.Error("polling_url missing from the built settings.yml")
	}
}

func TestBuildRefusesEmptyKey(t *testing.T) {
	d := tmplDir(t, goodSettings, "x")
	if _, err := build(d, ""); err == nil {
		t.Fatal("empty key accepted")
	}
}

func TestBuildRefusesBadKeyCharacters(t *testing.T) {
	d := tmplDir(t, goodSettings, "x")
	for _, k := range []string{"abc\nX-Evil: 1", "has space", "tab\there", "uni\u00e9code"} {
		if _, err := build(d, k); err == nil {
			t.Errorf("key %q accepted", k)
		}
	}
}

func TestBuildRefusesMissingPlaceholder(t *testing.T) {
	d := tmplDir(t, strings.ReplaceAll(goodSettings, "__POLLER_KEY__", "literal-value"), "x")
	if _, err := build(d, testKey); err == nil {
		t.Fatal("settings.yml without the placeholder accepted")
	}
}

func TestBuildRefusesLeftoverPlaceholder(t *testing.T) {
	for _, extra := range []string{
		"X-Second: __poller_key__",
		"X-Second: ${POLLER_KEY}",
		"X-Second: {{ POLLER_KEY }}",
		"X-Second: __OTHER_SECRET__",
	} {
		s := strings.ReplaceAll(goodSettings, "  X-Poller-Key: __POLLER_KEY__", "  X-Poller-Key: __POLLER_KEY__\n  "+extra)
		d := tmplDir(t, s, "x")
		if _, err := build(d, testKey); err == nil {
			t.Errorf("leftover %q accepted", extra)
		}
	}
	// The same spelling in full.liquid is also refused.
	d := tmplDir(t, goodSettings, "{{ POLLER_KEY }}")
	if _, err := build(d, testKey); err == nil {
		t.Error("placeholder in full.liquid accepted")
	}
}

// An empty polling_headers on import wipes the stored header on TRMNL.
func TestBuildGuaranteesPollingHeadersAndURL(t *testing.T) {
	cases := map[string]string{
		"null headers, placeholder only in a comment": "---\npolling_url: https://p.example.com/x\npolling_headers:\n# __POLLER_KEY__\n",
		"empty headers string":                        "---\npolling_url: https://p.example.com/x\npolling_headers: \"\"\n# __POLLER_KEY__\n",
		"headers without the poller key":              "---\npolling_url: https://p.example.com/x\npolling_headers: |-\n  X-Other: v\n# __POLLER_KEY__\n",
		"missing polling_url":                         strings.ReplaceAll(goodSettings, "polling_url: https://poller.example.com/v1/countdown\n", ""),
		"empty polling_url":                           strings.ReplaceAll(goodSettings, "https://poller.example.com/v1/countdown", `""`),
		"not yaml":                                    "a: [unclosed\n__POLLER_KEY__",
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := build(tmplDir(t, s, "x"), testKey)
			if err == nil {
				t.Fatal("accepted")
			}
			if strings.Contains(err.Error(), testKey) {
				t.Error("error contains the key")
			}
		})
	}
}

func TestBuildMissingFiles(t *testing.T) {
	d := tmplDir(t, goodSettings, "x")
	if err := os.Remove(filepath.Join(d, "full.liquid")); err != nil {
		t.Fatal(err)
	}
	if _, err := build(d, testKey); err == nil {
		t.Fatal("missing full.liquid accepted")
	}
}

func goodFiles(t *testing.T) map[string]string {
	t.Helper()
	f, err := build(tmplDir(t, goodSettings, "<div>hi</div>"), testKey)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPushSendsBodyOnStdin(t *testing.T) {
	files := goodFiles(t)
	var gotArgs []string
	var gotStdin []byte
	runner := func(_ context.Context, args []string, stdin []byte) ([]byte, error) {
		gotArgs, gotStdin = args, stdin
		return []byte(`{"data":{"settings_yaml":"x"}}`), nil
	}
	if err := push(context.Background(), 42, files, runner); err != nil {
		t.Fatal(err)
	}
	want := []string{"import-plugin-setting-files", "42", "-o", "json", "--rsh-timeout", "30s"}
	if !slices.Equal(gotArgs, want) {
		t.Errorf("args = %v, want %v", gotArgs, want)
	}
	for _, a := range gotArgs {
		if strings.Contains(a, testKey) {
			t.Error("key in argv")
		}
	}
	var body struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(gotStdin, &body); err != nil {
		t.Fatalf("stdin is not JSON: %v", err)
	}
	if len(body.Files) != 2 || body.Files["settings.yml"] != files["settings.yml"] || body.Files["full.liquid"] != files["full.liquid"] {
		t.Errorf("body files = %v", slices.Collect(func(yield func(string) bool) {
			for k := range body.Files {
				if !yield(k) {
					return
				}
			}
		}))
	}
}

func TestPushErrorNeverContainsKey(t *testing.T) {
	files := goodFiles(t)
	echo := func(_ context.Context, _ []string, stdin []byte) ([]byte, error) {
		// A hostile CLI: it echoes the body on stdout and inside the error.
		return stdin, fmt.Errorf("exit status 4: %s", stdin)
	}
	err := push(context.Background(), 7, files, echo)
	if err == nil {
		t.Fatal("expected an error")
	}
	esc, _ := json.Marshal(testKey)
	for _, bad := range []string{testKey, string(esc[1 : len(esc)-1])} {
		if strings.Contains(err.Error(), bad) {
			t.Errorf("error contains the key (%q form): %s", bad, err)
		}
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("error does not show the redaction: %s", err)
	}
	// errors.Unwrap must not give a path to the raw text either.
	for e := errors.Unwrap(err); e != nil; e = errors.Unwrap(e) {
		if strings.Contains(e.Error(), testKey) {
			t.Error("wrapped error contains the key")
		}
	}
}

func TestPushRedactsEnvKeys(t *testing.T) {
	t.Setenv("TRMNL_API_KEY", "api-key-value-123456")
	runner := func(context.Context, []string, []byte) ([]byte, error) {
		return nil, errors.New("denied for api-key-value-123456")
	}
	err := push(context.Background(), 7, goodFiles(t), runner)
	if err == nil || strings.Contains(err.Error(), "api-key-value-123456") {
		t.Fatalf("err = %v", err)
	}
}

func TestPushRefusesCLIErrorBody(t *testing.T) {
	runner := func(context.Context, []string, []byte) ([]byte, error) {
		return []byte(`{"error":"This API key lacks the capability"}`), nil
	}
	err := push(context.Background(), 7, goodFiles(t), runner)
	if err == nil || !strings.Contains(err.Error(), "lacks the capability") {
		t.Fatalf("err = %v", err)
	}
}

func TestPushRefusesNonJSONOutput(t *testing.T) {
	runner := func(context.Context, []string, []byte) ([]byte, error) { return []byte("ok"), nil }
	if err := push(context.Background(), 7, goodFiles(t), runner); err == nil {
		t.Fatal("non-JSON output accepted")
	}
}

func TestPushRefusesBadInput(t *testing.T) {
	ok := func(context.Context, []string, []byte) ([]byte, error) { return []byte(`{"data":{}}`), nil }
	if err := push(context.Background(), 0, goodFiles(t), ok); err == nil {
		t.Error("id 0 accepted")
	}
	if err := push(context.Background(), 7, map[string]string{"settings.yml": "x"}, ok); err == nil {
		t.Error("missing full.liquid accepted")
	}
	if err := push(context.Background(), 7, map[string]string{"settings.yml": "x", "full.liquid": "y", "fixture.json": "{}"}, ok); err == nil {
		t.Error("extra file accepted")
	}
}

func TestPushTimesOut(t *testing.T) {
	old := pushTimeout
	pushTimeout = 50 * time.Millisecond
	t.Cleanup(func() { pushTimeout = old })

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	// This runner ignores its context, so push must bound the wait itself.
	runner := func(context.Context, []string, []byte) ([]byte, error) {
		<-release
		return nil, nil
	}
	start := time.Now()
	err := push(context.Background(), 7, goodFiles(t), runner)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v", d)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v", err)
	}
}

// --- run: plugins.yml, key checks, logging ---

type call struct {
	args  []string
	stdin []byte
}

type fakeRunner struct {
	mu    sync.Mutex
	calls []call
	reply func(args []string, stdin []byte) ([]byte, error)
}

func (f *fakeRunner) run(_ context.Context, args []string, stdin []byte) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{args, stdin})
	f.mu.Unlock()
	if f.reply != nil {
		return f.reply(args, stdin)
	}
	return []byte(`{"data":{"settings_yaml":"x"}}`), nil
}

// tree writes plugins.yml and the template dirs under one root.
func tree(t *testing.T, plugins string, names ...string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "plugins.yml"), plugins)
	for _, n := range names {
		writeFile(t, filepath.Join(root, n, "settings.yml"), goodSettings)
		writeFile(t, filepath.Join(root, n, "full.liquid"), "<div>{{ columns[0].name }}</div>")
		writeFile(t, filepath.Join(root, n, "fixture.json"), `{"columns":[{"name":"Denver"}]}`)
	}
	return root
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

var fullKey = strings.Repeat("a", 40)

func doRun(t *testing.T, f *fakeRunner, e map[string]string, args ...string) (int, string) {
	t.Helper()
	old := lookChrome
	lookChrome = func() string { return "" }
	t.Cleanup(func() { lookChrome = old })
	var out bytes.Buffer
	code := run(args, env(e), &out, f.run)
	return code, out.String()
}

func TestRunPushesEachEntry(t *testing.T) {
	root := tree(t, "countdown: 12\nother: 34\n", "countdown", "other")
	f := &fakeRunner{}
	code, out := doRun(t, f, map[string]string{"TRMNL_API_KEY": "api-secret-0001", "TRMNL_POLLER_KEY": fullKey}, "--dir", root)
	if code != 0 {
		t.Fatalf("code = %d\n%s", code, out)
	}
	if len(f.calls) != 2 || f.calls[0].args[1] != "12" || f.calls[1].args[1] != "34" {
		t.Fatalf("calls = %+v", f.calls)
	}
	for _, s := range []string{fullKey, "api-secret-0001"} {
		if strings.Contains(out, s) {
			t.Errorf("log output contains a secret: %s", out)
		}
	}
}

func TestRunSkipsZeroAndMissingID(t *testing.T) {
	root := tree(t, "countdown: 0\nweather:\n", "countdown", "weather")
	f := &fakeRunner{}
	// Short key and no API key are fine when every entry is skipped.
	code, out := doRun(t, f, map[string]string{"TRMNL_POLLER_KEY": "short"}, "--dir", root)
	if code != 0 {
		t.Fatalf("code = %d\n%s", code, out)
	}
	if len(f.calls) != 0 {
		t.Errorf("runner called: %+v", f.calls)
	}
	for _, n := range []string{"countdown", "weather"} {
		if !strings.Contains(out, "skipping "+n+": no plugin id yet") {
			t.Errorf("no skip line for %s:\n%s", n, out)
		}
	}
}

func TestRunRefusesShortPollerKey(t *testing.T) {
	root := tree(t, "countdown: 12\n", "countdown")
	for _, k := range []string{"", "short", strings.Repeat("a", 31)} {
		f := &fakeRunner{}
		code, out := doRun(t, f, map[string]string{"TRMNL_API_KEY": "api", "TRMNL_POLLER_KEY": k}, "--dir", root)
		if code == 0 || len(f.calls) != 0 {
			t.Errorf("key len %d: code = %d, calls = %d", len(k), code, len(f.calls))
		}
		if k != "" && strings.Contains(out, k) {
			t.Error("output contains the short key")
		}
	}
}

func TestRunRefusesMissingAPIKey(t *testing.T) {
	root := tree(t, "countdown: 12\n", "countdown")
	f := &fakeRunner{}
	code, _ := doRun(t, f, map[string]string{"TRMNL_POLLER_KEY": fullKey}, "--dir", root)
	if code == 0 || len(f.calls) != 0 {
		t.Errorf("code = %d, calls = %d", code, len(f.calls))
	}
}

func TestRunFailsOnUnknownDirectoryAndBadYAML(t *testing.T) {
	e := map[string]string{"TRMNL_API_KEY": "api", "TRMNL_POLLER_KEY": fullKey}
	f := &fakeRunner{}
	if code, _ := doRun(t, f, e, "--dir", tree(t, "ghost: 5\n")); code == 0 {
		t.Error("unknown directory accepted")
	}
	if code, _ := doRun(t, f, e, "--dir", tree(t, "ghost: 0\n")); code == 0 {
		t.Error("unknown directory accepted for a skipped entry")
	}
	if code, _ := doRun(t, f, e, "--dir", tree(t, "countdown: [unclosed\n", "countdown")); code == 0 {
		t.Error("bad YAML accepted")
	}
	if code, _ := doRun(t, f, e, "--dir", tree(t, "countdown: abc\n", "countdown")); code == 0 {
		t.Error("non-integer id accepted")
	}
	if code, _ := doRun(t, f, e, "--dir", tree(t, "../evil: 3\n", "countdown")); code == 0 {
		t.Error("path traversal name accepted")
	}
	if code, _ := doRun(t, f, e, "--dir", t.TempDir()); code == 0 {
		t.Error("missing plugins.yml accepted")
	}
	if len(f.calls) != 0 {
		t.Errorf("runner called: %+v", f.calls)
	}
}

func TestRunReportsPushFailureWithoutKey(t *testing.T) {
	root := tree(t, "countdown: 12\nother: 34\n", "countdown", "other")
	f := &fakeRunner{reply: func(_ []string, stdin []byte) ([]byte, error) {
		return stdin, fmt.Errorf("boom: %s", stdin)
	}}
	code, out := doRun(t, f, map[string]string{"TRMNL_API_KEY": "api", "TRMNL_POLLER_KEY": fullKey}, "--dir", root)
	if code == 0 {
		t.Fatal("failure not reported")
	}
	if strings.Contains(out, fullKey) {
		t.Errorf("output contains the key:\n%s", out)
	}
	if len(f.calls) != 2 {
		t.Errorf("want both entries tried, got %d calls", len(f.calls))
	}
}

// --- render mode ---

func TestRenderWritesHTMLAndSendsFixture(t *testing.T) {
	root := tree(t, "countdown: 0\n", "countdown")
	out := filepath.Join(t.TempDir(), "out")
	f := &fakeRunner{reply: func([]string, []byte) ([]byte, error) {
		return []byte(`{"data":"<div>Denver</div>"}`), nil
	}}
	code, log := doRun(t, f, map[string]string{"TRMNL_API_KEY": "api-secret-0001"}, "--dir", root, "--render", out)
	if code != 0 {
		t.Fatalf("code = %d\n%s", code, log)
	}
	got, err := os.ReadFile(filepath.Join(out, "countdown.html"))
	if err != nil || string(got) != "<div>Denver</div>" {
		t.Fatalf("html = %q, %v", got, err)
	}
	if len(f.calls) != 1 || !slices.Equal(f.calls[0].args, []string{"render-markup", "-o", "json", "--rsh-timeout", "30s"}) {
		t.Fatalf("calls = %+v", f.calls)
	}
	var body struct {
		Markup    string         `json:"markup"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(f.calls[0].stdin, &body); err != nil {
		t.Fatal(err)
	}
	if body.Markup != "<div>{{ columns[0].name }}</div>" || body.Variables["columns"] == nil {
		t.Errorf("body = %+v", body)
	}
}

func TestRenderFailsOnLiquidError(t *testing.T) {
	root := tree(t, "countdown: 0\n", "countdown")
	f := &fakeRunner{reply: func([]string, []byte) ([]byte, error) {
		return []byte(`{"data":"Liquid error: Liquid syntax error (line 1): 'if' tag was never closed"}`), nil
	}}
	out := filepath.Join(t.TempDir(), "out")
	code, log := doRun(t, f, map[string]string{"TRMNL_API_KEY": "api"}, "--dir", root, "--render", out)
	if code == 0 {
		t.Fatalf("Liquid error not detected\n%s", log)
	}
	// The HTML is still written, so the PR artifact shows the failure.
	if _, err := os.Stat(filepath.Join(out, "countdown.html")); err != nil {
		t.Errorf("html not written on Liquid error: %v", err)
	}
}

func TestRenderFailsOnRunnerError(t *testing.T) {
	root := tree(t, "countdown: 0\n", "countdown")
	f := &fakeRunner{reply: func([]string, []byte) ([]byte, error) { return nil, errors.New("exit status 4 api-secret-0001") }}
	code, log := doRun(t, f, map[string]string{"TRMNL_API_KEY": "api-secret-0001"}, "--dir", root, "--render", t.TempDir())
	if code == 0 {
		t.Fatal("runner error not reported")
	}
	if strings.Contains(log, "api-secret-0001") {
		t.Errorf("log contains the API key:\n%s", log)
	}
}

func TestRenderSkipsWithoutAPIKey(t *testing.T) {
	root := tree(t, "countdown: 0\n", "countdown")
	f := &fakeRunner{}
	code, log := doRun(t, f, map[string]string{}, "--dir", root, "--render", t.TempDir())
	if code != 0 || len(f.calls) != 0 {
		t.Fatalf("code = %d, calls = %d", code, len(f.calls))
	}
	if !strings.Contains(log, "::notice::") {
		t.Errorf("no workflow notice:\n%s", log)
	}
}

func TestRenderSkipsTemplateWithoutFixture(t *testing.T) {
	root := tree(t, "countdown: 0\n", "countdown")
	if err := os.Remove(filepath.Join(root, "countdown", "fixture.json")); err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{}
	code, _ := doRun(t, f, map[string]string{"TRMNL_API_KEY": "api"}, "--dir", root, "--render", t.TempDir())
	if code != 0 || len(f.calls) != 0 {
		t.Fatalf("code = %d, calls = %d", code, len(f.calls))
	}
}

func TestRenderNeverNeedsPollerKey(t *testing.T) {
	// Render sends full.liquid and the fixture only; settings.yml stays home.
	root := tree(t, "countdown: 0\n", "countdown")
	f := &fakeRunner{reply: func([]string, []byte) ([]byte, error) { return []byte(`{"data":"ok"}`), nil }}
	code, _ := doRun(t, f, map[string]string{"TRMNL_API_KEY": "api"}, "--dir", root, "--render", t.TempDir())
	if code != 0 {
		t.Fatal("render failed without a poller key")
	}
	if strings.Contains(string(f.calls[0].stdin), "polling_headers") {
		t.Error("render body contains settings.yml")
	}
}

// --- real runner ---

func fakeCLI(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-trmnl")
	writeFile(t, p, "#!/bin/sh\n"+script+"\n")
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExecRunnerPassesStdinArgsAndEnv(t *testing.T) {
	t.Setenv("TRMNL_API_KEY", "from-parent-env")
	bin := fakeCLI(t, `printf '%s|' "$*" "$TRMNL_API_KEY"; cat`)
	out, err := newExecRunner(bin)(context.Background(), []string{"a", "b"}, []byte("BODY"))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "a b|from-parent-env|BODY" {
		t.Errorf("out = %q", out)
	}
}

func TestExecRunnerFailureCarriesStderr(t *testing.T) {
	bin := fakeCLI(t, `echo "bad thing" >&2; echo '{"error":"x"}'; exit 4`)
	out, err := newExecRunner(bin)(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "bad thing") || !strings.Contains(err.Error(), "exit status 4") {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(string(out), `"error"`) {
		t.Errorf("stdout lost: %q", out)
	}
}

func TestExecRunnerKillsOnContext(t *testing.T) {
	bin := fakeCLI(t, `exec sleep 30`)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := newExecRunner(bin)(ctx, nil, nil); err == nil {
		t.Fatal("expected an error")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v", d)
	}
}
