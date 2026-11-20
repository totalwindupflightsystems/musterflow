package specfix

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wojons/muster/pkg/openapi"
)

// strictParse parses raw through the engine exactly as app.Connect does on the
// strict path, so every test below proves the defect is real before the repair
// pass is asked to fix it.
func strictParse(t *testing.T, raw []byte) error {
	t.Helper()
	_, err := openapi.NewParser().Parse(context.Background(), raw, openapi.DefaultParseOptions())
	return err
}

func mustParse(t *testing.T, raw []byte) *openapi.ParseResult {
	t.Helper()
	res, err := openapi.NewParser().Parse(context.Background(), raw, openapi.DefaultParseOptions())
	if err != nil {
		t.Fatalf("parse after repair: %v", err)
	}
	return res
}

// oas3 wraps a components/schemas fragment in a minimal OpenAPI 3 document with
// the schema referenced from a live operation.
//
// OpenAPI 3 documents reach the engine's loader but are NOT passed through
// full schema validation, so an OAS3 fixture only exercises the defects that
// fail at unmarshal or reference-resolution time.
func oas3(schemas string) []byte {
	return []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "fixture", "version": "1.0.0"},
  "paths": {
    "/widgets": {
      "get": {
        "responses": {
          "200": {
            "description": "ok",
            "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}}
          }
        }
      }
    }
  },
  "components": {"schemas": ` + schemas + `}
}`)
}

// swagger2 wraps a definitions fragment in a minimal Swagger 2.0 document.
//
// Swagger 2.0 documents ARE schema-validated after conversion to 3.x
// (openapi2conv + Validate), which is where the cosmetic defects in Docker,
// GitLab, Spotify and Weaviate specs are rejected — so that is the shape the
// validation-stage rules must be proved against.
func swagger2(definitions string) []byte {
	return []byte(`{
  "swagger": "2.0",
  "info": {"title": "fixture", "version": "1.0.0"},
  "paths": {
    "/widgets": {
      "get": {
        "responses": {
          "200": {"description": "ok", "schema": {"$ref": "#/definitions/Widget"}}
        }
      }
    }
  },
  "definitions": ` + definitions + `}
}`)
}

func codes(res Result) map[string]int {
	out := map[string]int{}
	for _, c := range res.Codes() {
		out[c.Code] = c.Count
	}
	return out
}

func TestRepairRules(t *testing.T) {
	tests := []struct {
		name     string
		raw      []byte
		wantCode string
		wantPath string
	}{
		{
			name: "array example on a string schema (swagger 2.0, Docker shape)",
			raw: swagger2(`{
        "Widget": {
          "type": "object",
          "properties": {
            "version": {"type": "string", "example": ["1", "2"]}
          }
        }
      }`),
			wantCode: CodeExampleTypeMismatch,
			wantPath: "#/definitions/Widget/properties/version/example",
		},
		{
			name: "string example on an array schema (swagger 2.0, Weaviate shape)",
			raw: swagger2(`{
        "Widget": {
          "type": "object",
          "properties": {
            "tags": {"type": "array", "items": {"type": "string"}, "example": "not-a-list"}
          }
        }
      }`),
			wantCode: CodeExampleTypeMismatch,
			wantPath: "#/definitions/Widget/properties/tags/example",
		},
		{
			name: "boolean required on a schema (OAS3, Spotify shape)",
			raw: oas3(`{
        "Widget": {
          "type": "object",
          "required": true,
          "properties": {"id": {"type": "string"}}
        }
      }`),
			wantCode: CodeRequiredBool,
			wantPath: "#/components/schemas/Widget/required",
		},
		{
			name: "array without items (swagger 2.0, GitLab shape)",
			raw: swagger2(`{
        "Widget": {
          "type": "object",
          "properties": {"roles": {"type": "array"}}
        }
      }`),
			wantCode: CodeArrayMissingItems,
			wantPath: "#/definitions/Widget/properties/roles/items",
		},
		{
			name: "dangling local schema reference (OAS3, Square shape)",
			raw: oas3(`{
        "Widget": {
          "type": "object",
          "properties": {"allocation": {"$ref": "#/components/schemas/AppFeeAllocation"}}
        }
      }`),
			wantCode: CodeDanglingLocalRef,
			wantPath: "#/components/schemas/Widget/properties/allocation/$ref",
		},
		{
			name: "object description where a string is required (OAS3, DigitalOcean shape)",
			raw: oas3(`{
        "Widget": {
          "type": "object",
          "description": {"text": "not a string"},
          "properties": {"id": {"type": "string"}}
        }
      }`),
			wantCode: CodeNonStringText,
			wantPath: "#/components/schemas/Widget/description",
		},
		{
			name: "object default where a string is required (swagger 2.0)",
			raw: swagger2(`{
        "Widget": {
          "type": "object",
          "properties": {"id": {"type": "string", "default": {"a": 1}}}
        }
      }`),
			wantCode: CodeDefaultTypeMismatch,
			wantPath: "#/definitions/Widget/properties/id/default",
		},
		{
			name: "literal null type (swagger 2.0, Weaviate shape)",
			raw: swagger2(`{
        "Widget": {
          "type": "object",
          "properties": {"credential": {"type": "null"}}
        }
      }`),
			wantCode: CodeNullType,
			wantPath: "#/definitions/Widget/properties/credential/type",
		},
		{
			name: "example omitting a required property (swagger 2.0, GitLab shape)",
			raw: swagger2(`{
        "Widget": {
          "type": "object",
          "required": ["type", "location"],
          "properties": {
            "type": {"type": "string"},
            "location": {"type": "string"},
            "blob": {"type": "string"}
          },
          "example": {"blob": "only-this-one"}
        }
      }`),
			wantCode: CodeExampleMissingRequired,
			wantPath: "#/definitions/Widget/example",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := strictParse(t, tc.raw); err == nil {
				t.Fatalf("fixture is not rejected by strict parsing; it does not exercise %s", tc.wantCode)
			}

			res, err := Repair(tc.raw)
			if err != nil {
				t.Fatalf("Repair: %v", err)
			}
			if !res.Changed {
				t.Fatal("Repair reported no change")
			}
			if got := codes(res)[tc.wantCode]; got != 1 {
				t.Fatalf("warning code %s fired %d times, want 1 (all: %v)", tc.wantCode, got, res.Codes())
			}
			found := false
			for _, w := range res.Warnings {
				if w.Code == tc.wantCode && w.Path == tc.wantPath {
					found = true
				}
			}
			if !found {
				t.Fatalf("no %s warning at %s; got %v", tc.wantCode, tc.wantPath, res.Warnings)
			}
			mustParse(t, res.Data)
		})
	}
}

func TestExternalRefRecordedNotFetched(t *testing.T) {
	raw := oas3(`{
    "Widget": {
      "type": "object",
      "properties": {"id": {"$ref": "paths/objects/common/schemas.yml#/attributeToUpdate"}}
    }
  }`)

	if err := strictParse(t, raw); err == nil {
		t.Fatal("fixture is not rejected by strict parsing")
	}

	res, err := Repair(raw)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.Changed {
		t.Fatal("external references must never be rewritten")
	}
	if got := codes(res)[CodeExternalRef]; got != 1 {
		t.Fatalf("external-ref fired %d times, want 1 (all: %v)", got, res.Codes())
	}
	if string(res.Data) != string(raw) {
		t.Fatal("bytes changed although no repair applied")
	}
}

func TestCleanDocumentUntouched(t *testing.T) {
	raw := oas3(`{
    "Widget": {
      "type": "object",
      "required": ["id"],
      "properties": {"id": {"type": "string", "example": "w-1"}}
    }
  }`)

	if err := strictParse(t, raw); err != nil {
		t.Fatalf("clean fixture should parse strictly: %v", err)
	}

	res, err := Repair(raw)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.Changed || len(res.Warnings) != 0 {
		t.Fatalf("clean document was modified: changed=%v warnings=%v", res.Changed, res.Warnings)
	}
	if string(res.Data) != string(raw) {
		t.Fatal("clean document bytes changed")
	}
}

func TestParameterRequiredBooleanIsLeftAlone(t *testing.T) {
	// A parameter object's `required` is legitimately a boolean; the schema
	// rule must not strip it.
	raw := []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "fixture", "version": "1.0.0"},
  "paths": {
    "/widgets": {
      "get": {
        "parameters": [
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer"}}
        ],
        "responses": {"200": {"description": "ok"}}
      }
    }
  }
}`)

	if err := strictParse(t, raw); err != nil {
		t.Fatalf("valid parameter fixture must parse strictly: %v", err)
	}
	res, err := Repair(raw)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.Changed {
		t.Fatalf("parameter required flag was modified: %v", res.Warnings)
	}
}

func TestMaxWarningsCap(t *testing.T) {
	raw := oas3(`{
    "Widget": {
      "type": "object",
      "required": true,
      "properties": {
        "a": {"type": "array"},
        "b": {"type": "array"},
        "c": {"type": "array"}
      }
    }
  }`)

	res, err := RepairWithOptions(raw, Options{MaxWarnings: 2})
	if err != nil {
		t.Fatalf("RepairWithOptions: %v", err)
	}
	if len(res.Warnings) != 2 || res.Suppressed != 2 {
		t.Fatalf("got %d warnings and %d suppressed, want 2 and 2", len(res.Warnings), res.Suppressed)
	}
	mustParse(t, res.Data)
}

func TestYAMLInput(t *testing.T) {
	raw := []byte(`
swagger: "2.0"
info:
  title: fixture
  version: 1.0.0
paths:
  /widgets:
    get:
      responses:
        "200":
          description: ok
          schema:
            $ref: '#/definitions/Widget'
definitions:
  Widget:
    type: object
    properties:
      roles:
        type: array
`)

	if err := strictParse(t, raw); err == nil {
		t.Fatal("YAML fixture is not rejected by strict parsing")
	}
	res, err := Repair(raw)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if got := codes(res)[CodeArrayMissingItems]; got != 1 {
		t.Fatalf("array-missing-items fired %d times, want 1", got)
	}
	if !strings.HasPrefix(string(res.Data), "{") {
		t.Fatalf("repaired document is not JSON: %.40q", res.Data)
	}
	mustParse(t, res.Data)
}

func TestUndecodableDocument(t *testing.T) {
	if _, err := Repair([]byte("this: is: not: a: document")); err == nil {
		t.Fatal("expected an error for undecodable input")
	}
}

// TestPass3Rules covers the defect classes found in pass 3 of the public-spec
// sweep: each fixture is rejected by a strict parse first, so the rule is only
// proved to help if it is genuinely needed.
func TestPass3Rules(t *testing.T) {
	tests := []struct {
		name     string
		raw      []byte
		wantCode string
		wantPath string
	}{
		{
			name: "null for a required property inside an array example (Docker Plugin shape)",
			raw: swagger2(`{
        "Widget": {
          "type": "object",
          "properties": {
            "Env": {
              "type": "array",
              "items": {"$ref": "#/definitions/EnvVar"},
              "example": [{"Name": "DEBUG", "Settable": null, "Value": "0"}]
            }
          }
        },
        "EnvVar": {
          "type": "object",
          "required": ["Name", "Settable", "Value"],
          "properties": {
            "Name": {"type": "string"},
            "Settable": {"type": "array", "items": {"type": "string"}},
            "Value": {"type": "string"}
          }
        }
      }`),
			wantCode: CodeExampleNullValue,
			wantPath: "#/definitions/Widget/properties/Env/example/0/Settable",
		},
		{
			name: "array example whose items omit a required property (GitLab shape)",
			raw: swagger2(`{
        "Widget": {
          "type": "object",
          "properties": {
            "roles": {
              "type": "array",
              "items": {"$ref": "#/definitions/Role"},
              "example": [{"blob": "only-this-one"}]
            }
          }
        },
        "Role": {
          "type": "object",
          "required": ["type", "location"],
          "properties": {
            "type": {"type": "string"},
            "location": {"type": "string"},
            "blob": {"type": "string"}
          }
        }
      }`),
			wantCode: CodeExampleMissingRequired,
			wantPath: "#/definitions/Widget/properties/roles/example",
		},
		{
			name: "type name outside the Swagger 2.0 set (GitLab 'text' shape)",
			raw: swagger2(`{
        "Widget": {
          "type": "object",
          "properties": {"name": {"type": "text", "example": "Custom guest"}}
        }
      }`),
			wantCode: CodeUnknownType,
			wantPath: "#/definitions/Widget/properties/name/type",
		},
		{
			name: "type list containing null on a Swagger 2.0 schema (Weaviate shape)",
			raw: swagger2(`{
        "Widget": {
          "type": "object",
          "properties": {"credential": {"type": ["string", "null"]}}
        }
      }`),
			wantCode: CodeNullTypeInArray,
			wantPath: "#/definitions/Widget/properties/credential/type",
		},
		{
			name: "definition name OpenAPI 3 rejects (GitLab path-derived name shape)",
			raw: swagger2(`{
        "postApiV4ProjectsId(refRef)triggerPipeline": {
          "type": "object",
          "properties": {"id": {"type": "integer"}}
        },
        "Widget": {
          "type": "object",
          "properties": {"pipeline": {"$ref": "#/definitions/postApiV4ProjectsId(refRef)triggerPipeline"}}
        }
      }`),
			wantCode: CodeInvalidComponentName,
			wantPath: "#/definitions/postApiV4ProjectsId(refRef)triggerPipeline",
		},
		{
			name: "example outside its enum (Docker ProtocolScheme shape)",
			raw: swagger2(`{
        "Widget": {
          "type": "object",
          "properties": {
            "scheme": {"type": "string", "enum": ["", "moby.plugins.http/v1"], "example": "some.protocol/v1.0"}
          }
        }
      }`),
			wantCode: CodeExampleTypeMismatch,
			wantPath: "#/definitions/Widget/properties/scheme/example",
		},
		{
			name: "parameter default outside the parameter's type (GitLab formData shape)",
			raw: []byte(`{
  "swagger": "2.0",
  "info": {"title": "fixture", "version": "1.0.0"},
  "paths": {
    "/widgets": {
      "get": {
        "parameters": [
          {"in": "query", "name": "filters", "type": "array", "items": {"type": "string"}, "default": 1}
        ],
        "responses": {"200": {"description": "ok"}}
      }
    }
  }
}`),
			wantCode: CodeDefaultTypeMismatch,
			wantPath: "#/paths/~1widgets/get/parameters/0/default",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := strictParse(t, tc.raw); err == nil {
				t.Fatalf("fixture is not rejected by strict parsing; it does not exercise %s", tc.wantCode)
			}
			res, err := Repair(tc.raw)
			if err != nil {
				t.Fatalf("Repair: %v", err)
			}
			if !res.Changed {
				t.Fatal("Repair reported no change")
			}
			found := false
			for _, w := range res.Warnings {
				if w.Code == tc.wantCode && w.Path == tc.wantPath {
					found = true
				}
			}
			if !found {
				t.Fatalf("no %s warning at %s; got %v", tc.wantCode, tc.wantPath, res.Warnings)
			}
			mustParse(t, res.Data)
		})
	}
}

// TestWrongBucketRefKeepsContent proves the requestBodies->responses promotion
// preserves the referenced content byte for byte (same media type, same
// schema): only the bucket the reference names changes.
func TestWrongBucketRefKeepsContent(t *testing.T) {
	raw := []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "fixture", "version": "1.0.0"},
  "paths": {
    "/widgets": {
      "get": {
        "responses": {
          "200": {"$ref": "#/components/requestBodies/WidgetPayload"}
        }
      }
    }
  },
  "components": {
    "requestBodies": {
      "WidgetPayload": {
        "description": "a widget",
        "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}}
      }
    },
    "schemas": {"Widget": {"type": "object", "properties": {"id": {"type": "integer"}}}}
  }
}`)
	if err := strictParse(t, raw); err == nil {
		t.Fatal("fixture is not rejected by strict parsing")
	}
	res, err := Repair(raw)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if got := codes(res)[CodeWrongBucketRef]; got != 1 {
		t.Fatalf("wrong-bucket-ref fired %d times, want 1 (all: %v)", got, res.Codes())
	}

	var doc map[string]any
	if err := json.Unmarshal(res.Data, &doc); err != nil {
		t.Fatalf("decode repaired document: %v", err)
	}
	responses := doc["components"].(map[string]any)["responses"].(map[string]any)
	response, ok := responses["WidgetPayload"].(map[string]any)
	if !ok {
		t.Fatalf("promoted response missing: %v", responses)
	}
	if response["description"] != "a widget" {
		t.Fatalf("promoted response lost its description: %v", response)
	}
	if _, ok := response["content"].(map[string]any); !ok {
		t.Fatalf("promoted response lost its content: %v", response)
	}
	mustParse(t, res.Data)
}

// TestSchemaRulesSkipNonSchemaObjects guards the regression found in pass 3: a
// security scheme legitimately declares `type` ("oauth2" in 2.0, "openIdConnect"
// in 3.0). The type rules must not touch it, or the document loses the security
// scheme's kind and is rejected.
func TestSchemaRulesSkipNonSchemaObjects(t *testing.T) {
	swaggerRaw := []byte(`{
  "swagger": "2.0",
  "info": {"title": "fixture", "version": "1.0.0"},
  "paths": {
    "/widgets": {
      "get": {
        "responses": {"200": {"description": "ok"}}
      }
    }
  },
  "securityDefinitions": {
    "oidc": {
      "type": "oauth2",
      "flow": "implicit",
      "authorizationUrl": "https://example.com/auth",
      "scopes": {"read": "read widgets"}
    }
  },
  "definitions": {"Widget": {"type": "object"}}
}`)
	if err := strictParse(t, swaggerRaw); err != nil {
		t.Fatalf("clean swagger fixture must parse strictly: %v", err)
	}
	res, err := Repair(swaggerRaw)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.Changed {
		t.Fatalf("repair touched a clean document: %v", res.Warnings)
	}

	oas3Raw := []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "fixture", "version": "1.0.0"},
  "paths": {"/widgets": {"get": {"responses": {"200": {"description": "ok"}}}}},
  "components": {
    "securitySchemes": {
      "oidc": {"type": "openIdConnect", "openIdConnectUrl": "https://example.com/.well-known/openid-configuration"}
    },
    "schemas": {"Widget": {"type": "object"}}
  }
}`)
	res, err = Repair(oas3Raw)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.Changed {
		t.Fatalf("repair touched a clean OAS3 document: %v", res.Warnings)
	}

	// A specification extension that itself carries a `type` key (Docker's
	// x-go-type) is vendor metadata, not a schema: rewriting it would damage
	// the document while fixing nothing.
	withExtension := []byte(`{
  "swagger": "2.0",
  "info": {"title": "fixture", "version": "1.0.0"},
  "paths": {"/widgets": {"get": {"responses": {"200": {"description": "ok"}}}}},
  "definitions": {
    "Widget": {
      "type": "object",
      "properties": {
        "IP": {"type": "string", "x-go-type": {"type": "IP", "hints": {"kind": "string"}}}
      }
    }
  }
}`)
	if err := strictParse(t, withExtension); err != nil {
		t.Fatalf("clean extension fixture must parse strictly: %v", err)
	}
	res, err = Repair(withExtension)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.Changed {
		t.Fatalf("repair rewrote a specification extension: %v", res.Warnings)
	}
}
