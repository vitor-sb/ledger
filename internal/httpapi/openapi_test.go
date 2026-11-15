package httpapi

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// The spec is hand-written, so this test keeps it a valid OpenAPI document
// and keeps it describing every route the server registers.
func TestOpenAPISpecIsValidAndCoversTheRoutes(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(openAPISpec)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("invalid spec: %v", err)
	}

	routes := []struct{ method, path string }{
		{"POST", "/expenses"},
		{"GET", "/expenses"},
		{"POST", "/expenses/{id}/cancellation"},
		{"POST", "/expenses/{id}/skipped-installments"},
		{"GET", "/upcoming-payments"},
		{"GET", "/monthly-forecast"},
	}
	for _, route := range routes {
		item := doc.Paths.Find(route.path)
		if item == nil || item.GetOperation(route.method) == nil {
			t.Errorf("spec does not document %s %s", route.method, route.path)
		}
	}
}
