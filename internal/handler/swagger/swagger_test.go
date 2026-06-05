package swagger

import (
	"encoding/json"
	"strings"
	"testing"

	"ablecloud.io/ablestack-api/docs"
)

func TestFilterDocForSCVMHidesGluePathsTagsAndDefinitions(t *testing.T) {
	raw := `{
		"paths": {
			"/glue": {"get": {}},
			"/glue/rgw": {"get": {}},
			"/cube/license": {"post": {}}
		},
		"tags": [
			{"name": "Glue-RGW"},
			{"name": "Cube-License"}
		],
		"definitions": {
			"ablecloud_io_ablestack-api_internal_model_glue.Response": {},
			"ablecloud_io_ablestack-api_internal_model_cube.LicenseResponse": {}
		}
	}`

	filtered, err := FilterDocForSCVM(raw, false)
	if err != nil {
		t.Fatalf("FilterDocForSCVM returned error: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(filtered, &doc); err != nil {
		t.Fatalf("filtered doc is not valid JSON: %v", err)
	}
	paths := doc["paths"].(map[string]any)
	if _, ok := paths["/glue"]; ok {
		t.Fatalf("filtered doc still contains /glue")
	}
	if _, ok := paths["/glue/rgw"]; ok {
		t.Fatalf("filtered doc still contains /glue/rgw")
	}
	if _, ok := paths["/cube/license"]; !ok {
		t.Fatalf("filtered doc removed cube path")
	}

	tags := doc["tags"].([]any)
	if len(tags) != 1 || tags[0].(map[string]any)["name"] != "Cube-License" {
		t.Fatalf("tags = %#v, want only Cube-License", tags)
	}

	defs := doc["definitions"].(map[string]any)
	if _, ok := defs["ablecloud_io_ablestack-api_internal_model_glue.Response"]; ok {
		t.Fatalf("filtered doc still contains Glue definition")
	}
	if _, ok := defs["ablecloud_io_ablestack-api_internal_model_cube.LicenseResponse"]; !ok {
		t.Fatalf("filtered doc removed Cube definition")
	}
}

func TestFilterDocForSCVMKeepsGlueOnSCVM(t *testing.T) {
	raw := `{"paths":{"/glue/status":{"get":{}}}}`
	filtered, err := FilterDocForSCVM(raw, true)
	if err != nil {
		t.Fatalf("FilterDocForSCVM returned error: %v", err)
	}
	if string(filtered) != raw {
		t.Fatalf("SCVM doc changed: %s", filtered)
	}
}

func TestGeneratedDocFilterRemovesGlueForNonSCVM(t *testing.T) {
	filtered, err := FilterDocForSCVM(docs.SwaggerInfo.ReadDoc(), false)
	if err != nil {
		t.Fatalf("FilterDocForSCVM returned error: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(filtered, &doc); err != nil {
		t.Fatalf("filtered doc is not valid JSON: %v", err)
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("filtered doc has no paths")
	}
	for path := range paths {
		if path == "/glue" || strings.HasPrefix(path, "/glue/") {
			t.Fatalf("filtered generated doc still contains Glue path: %s", path)
		}
	}
	if _, ok := paths["/cube/license"]; !ok {
		t.Fatalf("filtered generated doc removed /cube/license")
	}
}
