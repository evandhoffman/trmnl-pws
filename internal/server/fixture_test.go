package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

// fixturePath is the sample data for the TRMNL template.
const fixturePath = "../../templates/countdown/fixture.json"

// TestFixtureMatchesServer keeps the template's sample data honest: the
// mockup scenario, served through the real handler with a fixed clock, must
// give the exact content of the fixture. The fixture file is indented for
// reading, so the test indents the response the same way before it compares.
func TestFixtureMatchesServer(t *testing.T) {
	h := newHandler(t)
	rec := do(h, http.MethodGet, "/v1/countdown", lan, keyed())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	var got bytes.Buffer
	if err := json.Indent(&got, rec.Body.Bytes(), "", "  "); err != nil {
		t.Fatalf("indent response: %v", err)
	}
	got.WriteByte('\n')

	want, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("fixture differs from the server output.\ngot:\n%s\nwant:\n%s", got.Bytes(), want)
	}
}
