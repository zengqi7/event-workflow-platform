package httptransport

import (
	"context"
	"event-workflow-platform/internal/app/dto"
	"event-workflow-platform/internal/service/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeAuth struct {
	principal  auth.Principal
	err        error
	admissions int
}

func (f *fakeAuth) Authenticate(context.Context, string) (auth.Principal, error) {
	return f.principal, f.err
}
func (f *fakeAuth) Admit(context.Context, auth.Principal, string, string) error {
	f.admissions++
	return nil
}
func TestAuthorizationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, token, path string
		scopes            []string
		want              int
	}{
		{"missing", "", "/api/v1/events", []string{"state:read"}, 401},
		{"scope", "valid", "/api/v1/events", nil, 403},
		{"foreign", "valid", "/api/v1/events?tenant_id=2", []string{"state:read"}, 403},
		{"derived", "valid", "/api/v1/events", []string{"state:read"}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeAuth{principal: auth.Principal{TenantID: 1, TenantCode: "acme", Scopes: tc.scopes}}
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id, err := requestTenant(r)
				if err != nil || id != 1 {
					t.Errorf("tenant not derived: %d %v", id, err)
				}
				w.WriteHeader(200)
			})
			r := httptest.NewRequest("GET", tc.path, nil)
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			authorization(next, repo).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
			if tc.want != 200 && repo.admissions != 0 {
				t.Fatal("unauthorized admission")
			}
		})
	}
}

type recordingIngestor struct {
	code  string
	calls int
}

func (f *recordingIngestor) Execute(_ context.Context, r dto.IngestEventRequest) (dto.IngestEventResponse, error) {
	f.calls++
	f.code = r.TenantCode
	return dto.IngestEventResponse{}, nil
}
func TestBatchTenantSpoofingRejected(t *testing.T) {
	next := &recordingIngestor{}
	repo := &fakeAuth{principal: auth.Principal{TenantID: 1, TenantCode: "acme", Scopes: []string{"events:write"}}}
	server := newServer(":0", tenantIngestor{next}, stubInspector{}, nil, nil)
	r := httptest.NewRequest("POST", "/api/v1/events/batch", strings.NewReader(`{"items":[{"tenant_code":"foreign"},{"tenant_code":""}]}`))
	r.Header.Set("Authorization", "Bearer valid")
	w := httptest.NewRecorder()
	authorization(server.Handler, repo).ServeHTTP(w, r)
	if w.Code != 207 || next.calls != 1 || next.code != "acme" {
		t.Fatalf("spoof escaped: status=%d calls=%d tenant=%s", w.Code, next.calls, next.code)
	}
}
func TestMissingAuthRepositoryFailsClosed(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/events", nil)
	w := httptest.NewRecorder()
	authorization(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unprotected route") }), nil).ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
