// Command push-templates pushes the TRMNL plugin templates to TRMNL.
//
// It reads <dir>/plugins.yml (template name to plugin id) and, for each
// entry with an id, imports <dir>/<name>/settings.yml and full.liquid
// through the trmnl CLI. It puts the poller key into the polling header on
// the way out. With --render <outdir> it renders each template with its
// fixture through TRMNL's render-markup endpoint instead, and writes the
// HTML to <outdir>.
//
// Two secrets pass through this tool: TRMNL_API_KEY (only the CLI child
// process uses it) and TRMNL_POLLER_KEY. Neither one goes into a log line,
// an error or argv. The request body goes to the CLI on stdin.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	placeholder     = "__POLLER_KEY__"
	headerName      = "X-Poller-Key"
	minPollerKeyLen = 32
	cliTimeoutFlag  = "30s" // the CLI's own --rsh-timeout
	maxErrorText    = 500
)

// pushTimeout bounds every CLI call. A variable so tests can shorten it.
var pushTimeout = 60 * time.Second

// Runner runs one trmnl CLI call. It gets the arguments after the program
// name and the request body for stdin. It returns the CLI's stdout. On
// failure it returns an error, and it may also return the stdout.
type Runner func(ctx context.Context, args []string, stdin []byte) ([]byte, error)

// lookChrome returns the path of a Chrome binary, or "" when there is none.
// A variable so tests can turn the screenshot off.
var lookChrome = func() string {
	p, err := exec.LookPath("google-chrome")
	if err != nil {
		return ""
	}
	return p
}

// leftover matches a placeholder that build does not replace: any
// __NAME__ token, ${...}, or a Liquid tag that names the poller key.
var leftover = regexp.MustCompile(`(?i)__[a-z0-9_]+__|\$\{[^}]*\}|\{\{\s*poller_?key`)

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// build reads the template in dir and returns the two files to push. It
// replaces __POLLER_KEY__ in settings.yml with key.
//
// An import with an empty polling_headers clears the header stored on
// TRMNL, and the wall then shows 404 pages. So build parses the result and
// refuses it unless polling_url and an X-Poller-Key header are present.
func build(dir, key string) (map[string]string, error) {
	if err := checkKeyChars(key); err != nil {
		return nil, err
	}
	sb, err := os.ReadFile(filepath.Join(dir, "settings.yml"))
	if err != nil {
		return nil, err
	}
	lb, err := os.ReadFile(filepath.Join(dir, "full.liquid"))
	if err != nil {
		return nil, err
	}
	settings, liquid := string(sb), string(lb)

	if !strings.Contains(settings, placeholder) {
		return nil, fmt.Errorf("%s/settings.yml has no %s placeholder", dir, placeholder)
	}
	if leftover.MatchString(strings.ReplaceAll(settings, placeholder, "")) {
		return nil, fmt.Errorf("%s/settings.yml has an unresolved placeholder", dir)
	}
	if leftover.MatchString(liquid) {
		return nil, fmt.Errorf("%s/full.liquid has an unresolved placeholder", dir)
	}
	settings = strings.ReplaceAll(settings, placeholder, key)

	if err := checkSettings(settings, key); err != nil {
		return nil, fmt.Errorf("%s/settings.yml: %s", dir, redact(err.Error(), key))
	}
	return map[string]string{"settings.yml": settings, "full.liquid": liquid}, nil
}

// checkKeyChars refuses a key that could break out of the header line.
func checkKeyChars(key string) error {
	if key == "" {
		return errors.New("poller key is empty")
	}
	for _, r := range key {
		if r < 0x21 || r > 0x7e {
			return errors.New("poller key has a character outside printable ASCII")
		}
	}
	return nil
}

// checkSettings parses the built settings.yml. Its errors must not carry
// the key: the caller redacts them as a second guard.
func checkSettings(settings, key string) error {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(settings), &doc); err != nil {
		return fmt.Errorf("not valid YAML: %w", err)
	}
	if u, _ := doc["polling_url"].(string); strings.TrimSpace(u) == "" {
		return errors.New("polling_url is missing or empty")
	}
	headers, _ := doc["polling_headers"].(string)
	if strings.TrimSpace(headers) == "" {
		return errors.New("polling_headers is missing or empty; an import would clear the stored header")
	}
	found := false
	for _, line := range strings.Split(headers, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, val, ok := splitHeader(line)
		if !ok || name == "" || val == "" {
			return errors.New("polling_headers has a line that is not 'Name: value'")
		}
		if strings.EqualFold(name, headerName) && val == key {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("polling_headers has no %s line with the poller key", headerName)
	}
	return nil
}

// splitHeader splits "Name: value" or "name=value".
func splitHeader(line string) (name, val string, ok bool) {
	i := strings.IndexAny(line, ":=")
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

// push imports files into plugin id. Its errors never contain a key.
func push(ctx context.Context, id int, files map[string]string, runner Runner) error {
	return pushWith(ctx, id, files, runner, os.Getenv("TRMNL_API_KEY"), os.Getenv("TRMNL_POLLER_KEY"))
}

// pushWith is push with the secrets to redact passed in. It always
// redacts the header values found in files["settings.yml"] as well.
func pushWith(ctx context.Context, id int, files map[string]string, runner Runner, secrets ...string) error {
	if id <= 0 {
		return errors.New("plugin id must be positive")
	}
	if len(files) != 2 || !hasKeys(files, "settings.yml", "full.liquid") {
		return errors.New("push takes exactly settings.yml and full.liquid")
	}
	secrets = append(secrets, headerSecrets(files["settings.yml"])...)

	body, err := encodeJSON(map[string]any{"files": files})
	if err != nil {
		return err
	}
	args := []string{"import-plugin-setting-files", strconv.Itoa(id), "-o", "json", "--rsh-timeout", cliTimeoutFlag}
	out, err := callCLI(ctx, runner, args, body)
	if err != nil {
		return fmt.Errorf("import into plugin %d failed: %s", id, failText(err, out, secrets))
	}
	if msg, err := cliFailure(out); err != nil {
		return fmt.Errorf("import into plugin %d: %s", id, redact(err.Error(), secrets...))
	} else if msg != "" {
		return fmt.Errorf("import into plugin %d rejected: %s", id, clip(redact(msg, secrets...)))
	}
	return nil
}

func hasKeys(m map[string]string, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

// headerSecrets returns the header values in settings.yml's polling_headers.
func headerSecrets(settings string) []string {
	var doc map[string]any
	if yaml.Unmarshal([]byte(settings), &doc) != nil {
		return nil
	}
	headers, _ := doc["polling_headers"].(string)
	var out []string
	for _, line := range strings.Split(headers, "\n") {
		if _, val, ok := splitHeader(line); ok && val != "" {
			out = append(out, val)
		}
	}
	return out
}

// callCLI runs the runner under pushTimeout. It also stops waiting at the
// deadline when a runner ignores its context.
func callCLI(ctx context.Context, runner Runner, args []string, stdin []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	type result struct {
		out []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := runner(ctx, args, stdin)
		ch <- result{out, err}
	}()
	select {
	case r := <-ch:
		if ctx.Err() != nil && r.err != nil {
			return r.out, timeoutErr(ctx, args)
		}
		return r.out, r.err
	case <-ctx.Done():
		return nil, timeoutErr(ctx, args)
	}
}

func timeoutErr(ctx context.Context, args []string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("trmnl %s timed out after %s", args[0], pushTimeout)
	}
	return fmt.Errorf("trmnl %s cancelled", args[0])
}

// cliFailure reads the CLI's JSON output. It returns a non-empty message
// when the CLI reports an error, and an error when the output is not the
// expected JSON.
func cliFailure(out []byte) (string, error) {
	var resp struct {
		Error any             `json:"error"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", errors.New("the CLI output is not JSON")
	}
	if resp.Error != nil {
		if s, ok := resp.Error.(string); ok {
			return s, nil
		}
		return "unspecified error", nil
	}
	if len(resp.Data) == 0 || string(resp.Data) == "null" {
		return "", errors.New("the CLI output has no data")
	}
	return "", nil
}

// failText joins the error and the CLI output, redacted and clipped.
func failText(err error, out []byte, secrets []string) string {
	s := err.Error()
	if t := strings.TrimSpace(string(out)); t != "" {
		s += ": " + t
	}
	return clip(redact(s, secrets...))
}

func clip(s string) string {
	if len(s) > maxErrorText {
		return s[:maxErrorText] + "..."
	}
	return s
}

// redact replaces each secret, in plain and escaped forms, with [redacted].
func redact(s string, secrets ...string) string {
	var forms []string
	for _, sec := range secrets {
		if sec == "" {
			continue
		}
		forms = append(forms, sec)
		for _, esc := range []bool{true, false} {
			var b bytes.Buffer
			enc := json.NewEncoder(&b)
			enc.SetEscapeHTML(esc)
			if enc.Encode(sec) == nil {
				j := strings.TrimSuffix(b.String(), "\n")
				forms = append(forms, j[1:len(j)-1])
			}
		}
		q := strconv.Quote(sec)
		forms = append(forms, q[1:len(q)-1])
	}
	// Longest first, so a form that contains another one goes whole.
	slices.SortFunc(forms, func(a, b string) int { return len(b) - len(a) })
	for _, f := range forms {
		s = strings.ReplaceAll(s, f, "[redacted]")
	}
	return s
}

func encodeJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// newExecRunner returns a Runner that runs the program bin. The child gets
// the parent's environment, which carries TRMNL_API_KEY.
func newExecRunner(bin string) Runner {
	return func(ctx context.Context, args []string, stdin []byte) ([]byte, error) {
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Stdin = bytes.NewReader(stdin)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		cmd.WaitDelay = 5 * time.Second
		if err := cmd.Run(); err != nil {
			return stdout.Bytes(), fmt.Errorf("%s: %w: %s", filepath.Base(bin), err, strings.TrimSpace(stderr.String()))
		}
		return stdout.Bytes(), nil
	}
}

// entry is one line of plugins.yml.
type entry struct {
	name string
	id   int // 0 means no plugin yet
}

// loadPlugins reads <dir>/plugins.yml and checks every template directory.
func loadPlugins(dir string) ([]entry, error) {
	b, err := os.ReadFile(filepath.Join(dir, "plugins.yml"))
	if err != nil {
		return nil, err
	}
	var m map[string]*int
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("plugins.yml: %w", err)
	}
	var out []entry
	for name, id := range m {
		if !nameRE.MatchString(name) {
			return nil, fmt.Errorf("plugins.yml: bad template name %q", name)
		}
		e := entry{name: name}
		if id != nil {
			e.id = *id
		}
		if e.id < 0 {
			return nil, fmt.Errorf("plugins.yml: %s has a negative id", name)
		}
		if st, err := os.Stat(filepath.Join(dir, name)); err != nil || !st.IsDir() {
			return nil, fmt.Errorf("plugins.yml: %s has no directory under %s", name, dir)
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b entry) int { return strings.Compare(a.name, b.name) })
	return out, nil
}

// run executes the tool and returns the process exit code.
func run(args []string, getenv func(string) string, stdout io.Writer, runner Runner) int {
	log := slog.New(slog.NewTextHandler(stdout, nil))
	fs := flag.NewFlagSet("push-templates", flag.ContinueOnError)
	fs.SetOutput(stdout)
	dir := fs.String("dir", "templates", "directory with plugins.yml and one directory per template")
	renderDir := fs.String("render", "", "render each template with its fixture into this directory instead of pushing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	apiKey, pollerKey := getenv("TRMNL_API_KEY"), getenv("TRMNL_POLLER_KEY")
	ctx := context.Background()

	entries, err := loadPlugins(*dir)
	if err != nil {
		log.Error("load plugins", "error", redact(err.Error(), apiKey, pollerKey))
		return 1
	}
	if *renderDir != "" {
		return renderAll(ctx, log, stdout, *dir, *renderDir, entries, apiKey, runner)
	}

	var pending []entry
	for _, e := range entries {
		if e.id == 0 {
			log.Info(fmt.Sprintf("skipping %s: no plugin id yet", e.name))
			continue
		}
		pending = append(pending, e)
	}
	if len(pending) == 0 {
		return 0
	}
	if len(pollerKey) < minPollerKeyLen {
		log.Error(fmt.Sprintf("TRMNL_POLLER_KEY must be at least %d characters", minPollerKeyLen))
		return 1
	}
	if apiKey == "" {
		log.Error("TRMNL_API_KEY is empty")
		return 1
	}

	failed := 0
	for _, e := range pending {
		files, err := build(filepath.Join(*dir, e.name), pollerKey)
		if err == nil {
			err = pushWith(ctx, e.id, files, runner, apiKey, pollerKey)
		}
		if err != nil {
			log.Error("push failed", "template", e.name, "plugin", e.id, "error", redact(err.Error(), apiKey, pollerKey))
			failed++
			continue
		}
		log.Info("pushed", "template", e.name, "plugin", e.id)
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// renderAll renders every template that has a fixture, through TRMNL's
// render-markup endpoint, and writes <outdir>/<name>.html. A "Liquid error"
// in the text fails the run: the endpoint answers 200 with the error as text.
func renderAll(ctx context.Context, log *slog.Logger, stdout io.Writer, dir, outDir string, entries []entry, apiKey string, runner Runner) int {
	if apiKey == "" {
		// A pull request from a fork has no secrets.
		fmt.Fprintln(stdout, "::notice::TRMNL_API_KEY is empty; skipping the template render")
		log.Info("TRMNL_API_KEY is empty; skipping the template render")
		return 0
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Error("create output directory", "error", err)
		return 1
	}
	chrome := lookChrome()
	failed := 0
	for _, e := range entries {
		fixture, err := os.ReadFile(filepath.Join(dir, e.name, "fixture.json"))
		if errors.Is(err, os.ErrNotExist) {
			log.Info(fmt.Sprintf("skipping %s: no fixture.json", e.name))
			continue
		}
		if err == nil {
			err = renderOne(ctx, log, filepath.Join(dir, e.name), outDir, e.name, fixture, chrome, apiKey, runner)
		}
		if err != nil {
			log.Error("render failed", "template", e.name, "error", redact(err.Error(), apiKey))
			failed++
			continue
		}
		log.Info("rendered", "template", e.name)
	}
	if failed > 0 {
		return 1
	}
	return 0
}

func renderOne(ctx context.Context, log *slog.Logger, tmplDir, outDir, name string, fixture []byte, chrome, apiKey string, runner Runner) error {
	liquid, err := os.ReadFile(filepath.Join(tmplDir, "full.liquid"))
	if err != nil {
		return err
	}
	var vars map[string]any
	if err := json.Unmarshal(fixture, &vars); err != nil {
		return fmt.Errorf("fixture.json is not a JSON object: %w", err)
	}
	body, err := encodeJSON(map[string]any{"markup": string(liquid), "variables": vars})
	if err != nil {
		return err
	}
	args := []string{"render-markup", "-o", "json", "--rsh-timeout", cliTimeoutFlag}
	out, err := callCLI(ctx, runner, args, body)
	if err != nil {
		return errors.New(failText(err, out, []string{apiKey}))
	}
	if msg, err := cliFailure(out); err != nil {
		return err
	} else if msg != "" {
		return fmt.Errorf("render-markup rejected: %s", clip(msg))
	}
	html, err := renderedText(out)
	if err != nil {
		return err
	}
	htmlPath := filepath.Join(outDir, name+".html")
	if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
		return err
	}
	if i := strings.Index(html, "Liquid error"); i >= 0 {
		end := min(i+200, len(html))
		return fmt.Errorf("the template has a Liquid error: %s", strings.ReplaceAll(html[i:end], "\n", " "))
	}
	if chrome != "" {
		screenshot(ctx, log, chrome, htmlPath, filepath.Join(outDir, name+".png"))
	}
	return nil
}

// renderedText returns the "data" of a render-markup reply: a string, or an
// array of strings.
func renderedText(out []byte) (string, error) {
	var resp struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", err
	}
	var s string
	if json.Unmarshal(resp.Data, &s) == nil {
		return s, nil
	}
	var list []string
	if json.Unmarshal(resp.Data, &list) == nil {
		return strings.Join(list, "\n"), nil
	}
	return "", errors.New("render-markup data is neither a string nor a list of strings")
}

// screenshot writes a PNG of the HTML file. The PNG is a convenience for
// the reviewer, so a failure only warns.
func screenshot(ctx context.Context, log *slog.Logger, chrome, htmlPath, pngPath string) {
	abs, err := filepath.Abs(htmlPath)
	if err != nil {
		log.Warn("screenshot skipped", "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, chrome, "--headless", "--no-sandbox", "--disable-gpu",
		"--window-size=800,480", "--screenshot="+pngPath, "file://"+abs)
	cmd.WaitDelay = 5 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Warn("screenshot failed", "error", err, "output", clip(strings.TrimSpace(string(out))))
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, newExecRunner("trmnl")))
}
