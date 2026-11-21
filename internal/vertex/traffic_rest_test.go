package vertex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const restEngine = "projects/p/locations/us-central1/reasoningEngines/1"

// fakeAgentRuntime answers the REST calls the traffic client makes, in the
// shapes the v1beta1 API returns.
type fakeAgentRuntime struct {
	mu        sync.Mutex
	traffic   string // raw trafficConfig JSON, or "" for none
	patches   []string
	opPolls   int
	opPending int
	opError   bool
}

func (f *fakeAgentRuntime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/v1beta1/")
	switch {
	case r.Method == http.MethodGet && path == restEngine:
		body := `{"name":"` + restEngine + `"`
		if f.traffic != "" {
			body += `,"trafficConfig":` + f.traffic
		}
		_, _ = io.WriteString(w, body+"}")
	case r.Method == http.MethodPatch && path == restEngine:
		if r.URL.Query().Get("updateMask") != trafficUpdateMask {
			http.Error(w, "bad mask", http.StatusBadRequest)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		f.patches = append(f.patches, string(raw))
		_, _ = io.WriteString(w, `{"name":"`+restEngine+`/operations/9"}`)
	case r.Method == http.MethodGet && path == restEngine+"/operations/9":
		f.opPolls++
		if f.opPolls <= f.opPending {
			_, _ = io.WriteString(w, `{"name":"`+restEngine+`/operations/9"}`)
			return
		}
		if f.opError {
			_, _ = io.WriteString(w, `{"done":true,"error":{"code":3,"message":"bad target"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"done":true,"response":{}}`)
	case r.Method == http.MethodGet && path == restEngine+"/runtimeRevisions":
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = io.WriteString(w, `{"reasoningEngineRuntimeRevisions":[{"name":"`+restEngine+
				`/runtimeRevisions/1","state":"ACTIVE","createTime":"2026-11-21T08:27:41.697444Z",`+
				`"spec":{"deploymentSpec":{"env":[{"name":"PROMPTPACK_VERSION","value":"v1.0.0"}]}}}],`+
				`"nextPageToken":"p2"}`)
			return
		}
		_, _ = io.WriteString(w, `{"reasoningEngineRuntimeRevisions":[{"name":"`+restEngine+
			`/runtimeRevisions/2","state":"ACTIVE","createTime":"2026-11-21T11:09:42Z"}]}`)
	default:
		http.NotFound(w, r)
	}
}

func newTestREST(f *fakeAgentRuntime) (*trafficREST, func()) {
	srv := httptest.NewServer(f)
	return &trafficREST{http: srv.Client(), endpoint: srv.URL, pollInterval: time.Millisecond}, srv.Close
}

func TestGetTrafficDefaultsToAlwaysLatest(t *testing.T) {
	r, done := newTestREST(&fakeAgentRuntime{})
	defer done()
	got, err := r.getTraffic(context.Background(), restEngine)
	if err != nil || !got.AlwaysLatest {
		t.Fatalf("got %+v, %v; want always latest", got, err)
	}
}

func TestGetTrafficReadsAManualSplit(t *testing.T) {
	r, done := newTestREST(&fakeAgentRuntime{traffic: `{"trafficSplitManual":{"targets":[` +
		`{"runtimeRevisionName":"` + restEngine + `/runtimeRevisions/1","percent":100}]}}`})
	defer done()
	got, err := r.getTraffic(context.Background(), restEngine)
	if err != nil {
		t.Fatalf("getTraffic: %v", err)
	}
	if !got.equal(pinnedTo(restEngine + "/runtimeRevisions/1")) {
		t.Errorf("got %+v", got)
	}
}

func TestSetTrafficSendsTheSplitAndWaitsForTheOperation(t *testing.T) {
	f := &fakeAgentRuntime{opPending: 2}
	r, done := newTestREST(f)
	defer done()
	split := splitBetween(restEngine+"/runtimeRevisions/1", restEngine+"/runtimeRevisions/2", 10)
	if err := r.setTraffic(context.Background(), restEngine, split); err != nil {
		t.Fatalf("setTraffic: %v", err)
	}
	if f.opPolls != 3 {
		t.Errorf("operation polled %d times, want until done (3)", f.opPolls)
	}
	var body struct {
		TrafficConfig restTrafficConfig `json:"trafficConfig"`
	}
	if err := json.Unmarshal([]byte(f.patches[0]), &body); err != nil {
		t.Fatalf("patch body: %v", err)
	}
	if got := trafficFromREST(&body.TrafficConfig); !got.equal(split) {
		t.Errorf("sent %s", f.patches[0])
	}
}

func TestSetTrafficAlwaysLatest(t *testing.T) {
	f := &fakeAgentRuntime{}
	r, done := newTestREST(f)
	defer done()
	if err := r.setTraffic(context.Background(), restEngine, &Traffic{AlwaysLatest: true}); err != nil {
		t.Fatalf("setTraffic: %v", err)
	}
	if f.patches[0] != `{"trafficConfig":{"trafficSplitAlwaysLatest":{}}}` {
		t.Errorf("sent %s", f.patches[0])
	}
}

func TestSetTrafficReportsAFailedOperation(t *testing.T) {
	r, done := newTestREST(&fakeAgentRuntime{opError: true})
	defer done()
	err := r.setTraffic(context.Background(), restEngine, pinnedTo("x"))
	if err == nil || !strings.Contains(err.Error(), "bad target") {
		t.Fatalf("err = %v, want the operation's error", err)
	}
}

func TestListRevisionsPagesAndReadsThePackVersion(t *testing.T) {
	r, done := newTestREST(&fakeAgentRuntime{})
	defer done()
	revs, err := r.listRevisions(context.Background(), restEngine)
	if err != nil {
		t.Fatalf("listRevisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("got %d revisions, want both pages", len(revs))
	}
	if revs[0].PackVersion != "v1.0.0" || revs[1].PackVersion != "" {
		t.Errorf("pack versions = %q, %q", revs[0].PackVersion, revs[1].PackVersion)
	}
	if newestRevision(revs).Name != restEngine+"/runtimeRevisions/2" {
		t.Error("newest revision is not the later one")
	}
}

func TestRESTNotFoundIsErrEngineNotFound(t *testing.T) {
	r, done := newTestREST(&fakeAgentRuntime{})
	defer done()
	_, err := r.getTraffic(context.Background(), "projects/p/locations/l/reasoningEngines/gone")
	if !errors.Is(err, ErrEngineNotFound) {
		t.Errorf("err = %v, want ErrEngineNotFound", err)
	}
}
