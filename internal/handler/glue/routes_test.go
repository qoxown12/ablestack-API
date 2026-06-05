package glue

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegisterRoutesIfSCVMRegistersOnSCVM(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("ABLESTACK_NODE_ROLE", "scvm")

	router := gin.New()
	if ok := RegisterRoutesIfSCVM(router.Group("/api/v1/glue")); !ok {
		t.Fatalf("RegisterRoutesIfSCVM = false, want true")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/glue", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/glue status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestRegisterRoutesIfSCVMDoesNotRegisterOnHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("ABLESTACK_NODE_ROLE", "host")

	router := gin.New()
	if ok := RegisterRoutesIfSCVM(router.Group("/api/v1/glue")); ok {
		t.Fatalf("RegisterRoutesIfSCVM = true, want false")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/glue", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/v1/glue status = %d, want %d, body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
