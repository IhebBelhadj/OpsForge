package httpapi_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// spec is api/openapi.yaml, loaded once; OpenAPI 3.1 schemas are JSON Schema 2020-12, so responses are validated
// with the same validator as the bundle.
var spec = sync.OnceValues(func() (map[string]any, *jsonschema.Compiler) {
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		panic(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		panic(err)
	}
	// Round trip through JSON: the validator wants JSON types (yaml.v3 gives map[string]any already)
	raw, _ := json.Marshal(doc)
	resource, _ := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	c := jsonschema.NewCompiler()
	if err := c.AddResource("openapi.json", resource); err != nil {
		panic(err)
	}
	return doc, c
})

var (
	coveredMu sync.Mutex
	covered   = map[string]bool{}
)

// checkContract fails the test unless the response is one the spec documents for this operation, with a body
// that matches its schema. It records the (method, path, status) as covered.
func checkContract(t *testing.T, method, path string, rec *httptest.ResponseRecorder) {
	t.Helper()
	doc, compiler := spec()
	op, _ := doc["paths"].(map[string]any)[path].(map[string]any)[strings.ToLower(method)].(map[string]any)
	if op == nil {
		t.Fatalf("%s %s is not in openapi.yaml", method, path)
	}
	status := fmt.Sprint(rec.Code)
	resp, ok := op["responses"].(map[string]any)[status].(map[string]any)
	if !ok {
		t.Fatalf("%s %s: status %s is not documented", method, path, status)
	}
	media := strings.Split(rec.Header().Get("Content-Type"), ";")[0]
	content, _ := resp["content"].(map[string]any)[media].(map[string]any)
	if content == nil {
		t.Fatalf("%s %s %s: content type %q is not documented", method, path, status, media)
	}
	ref := content["schema"].(map[string]any)["$ref"].(string)
	schema, err := compiler.Compile("openapi.json" + ref)
	if err != nil {
		t.Fatal(err)
	}
	body, err := jsonschema.UnmarshalJSON(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("%s %s: body is not JSON: %v", method, path, err)
	}
	if err := schema.Validate(body); err != nil {
		t.Fatalf("%s %s %s: body does not match %s: %v", method, path, status, ref, err)
	}
	coveredMu.Lock()
	covered[method+" "+path+" "+status] = true
	coveredMu.Unlock()
}

// TestMain fails the package when a response documented in openapi.yaml is produced by no test (only for a full
// run: with -run, most responses are skipped on purpose).
func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 && flag.Lookup("test.run").Value.String() == "" {
		if missing := uncovered(); len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "FAIL: no contract test produces: %s\n", strings.Join(missing, ", "))
			code = 1
		}
	}
	os.Exit(code)
}

func uncovered() []string {
	doc, _ := spec()
	var missing []string
	for path, item := range doc["paths"].(map[string]any) {
		for method, op := range item.(map[string]any) {
			for status := range op.(map[string]any)["responses"].(map[string]any) {
				if key := strings.ToUpper(method) + " " + path + " " + status; !covered[key] {
					missing = append(missing, key)
				}
			}
		}
	}
	sort.Strings(missing)
	return missing
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	req.Host = "127.0.0.1:8090" // httptest's default, example.com, is refused by the Host guard
	h.ServeHTTP(rec, req)
	return rec
}
