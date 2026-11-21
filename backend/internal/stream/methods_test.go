package stream

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// The gateway only serves GET and HEAD. A mutation that allowed every method passed the suite.
func TestOnlyGetAndHeadAreServed(t *testing.T) {
	f := newFixture(t)
	u := f.url(t, vid, "hls/"+vid+"/master.m3u8")
	pu, _ := url.Parse(u)
	h := f.h
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		rr := httptest.NewRecorder()
		f.mux = http.NewServeMux()
		f.mux.Handle("/stream/{token}/{key...}", h) // every method reaches the handler; only it may refuse
		f.mux.ServeHTTP(rr, httptest.NewRequest(m, pu.Path, nil))
		if rr.Code != http.StatusOK {
			t.Errorf("%s: %d", m, rr.Code)
		}
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rr := httptest.NewRecorder()
		f.mux = http.NewServeMux()
		f.mux.Handle("/stream/{token}/{key...}", h)
		f.mux.ServeHTTP(rr, httptest.NewRequest(m, pu.Path, nil))
		if rr.Code != http.StatusMethodNotAllowed || rr.Header().Get("Allow") == "" {
			t.Errorf("%s: %d allow=%q, want 405 with an Allow header", m, rr.Code, rr.Header().Get("Allow"))
		}
		if rr.Body.Len() > 64 {
			t.Errorf("%s: a refused method must not return the object (%d bytes)", m, rr.Body.Len())
		}
	}
}
