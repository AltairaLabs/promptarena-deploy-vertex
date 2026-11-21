package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Traffic routing and runtime revisions exist only in the v1beta1 REST API: the
// published Go protos (aiplatformpb) carry neither trafficConfig nor the
// runtimeRevisions collection, so these calls go over REST with the same
// credentials the SDK client uses.
//
//	GET   v1beta1/{engine}                                   -> trafficConfig
//	PATCH v1beta1/{engine}?updateMask=traffic_config         -> long-running operation
//	GET   v1beta1/{engine}/runtimeRevisions?pageToken=…       -> reasoningEngineRuntimeRevisions[]
//	DELETE v1beta1/{engine}/runtimeRevisions/{id}             -> long-running operation
//
// A traffic-only patch does not touch the spec, so it creates no revision.

// trafficUpdateMask limits a traffic patch to the routing, so it cannot change
// the spec and create a revision.
const trafficUpdateMask = "traffic_config"

// restTimeout caps one REST call. The traffic patch's operation is waited on
// separately, under operationTimeout.
const restTimeout = 60 * time.Second

// operationTimeout caps the wait for a traffic patch to take effect. Observed
// patches finish within seconds; this leaves room for a slow control plane.
const operationTimeout = 5 * time.Minute

// operationPollInterval is how often a pending operation is re-read.
const operationPollInterval = 2 * time.Second

// envPackVersion carries the pack's version on every revision. Revisions have
// no labels of their own, so the versioned spec is the only place a value is
// readable per revision.
const envPackVersion = "PROMPTPACK_VERSION"

// trafficREST performs the REST-only calls.
type trafficREST struct {
	http     *http.Client
	endpoint string
	// pollInterval is operationPollInterval, shortened by tests.
	pollInterval time.Duration
}

type restTrafficTarget struct {
	RuntimeRevisionName string `json:"runtimeRevisionName"`
	Percent             int    `json:"percent"`
}

type restTrafficConfig struct {
	TrafficSplitAlwaysLatest *struct{} `json:"trafficSplitAlwaysLatest,omitempty"`
	TrafficSplitManual       *struct {
		Targets []restTrafficTarget `json:"targets"`
	} `json:"trafficSplitManual,omitempty"`
}

type restEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type restRevision struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	CreateTime string `json:"createTime"`
	Spec       struct {
		DeploymentSpec struct {
			Env []restEnvVar `json:"env"`
		} `json:"deploymentSpec"`
	} `json:"spec"`
}

type restOperation struct {
	Name  string `json:"name"`
	Done  bool   `json:"done"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// restStatusError is a non-200 response.
type restStatusError struct {
	status int
	msg    string
}

func (e *restStatusError) Error() string { return e.msg }

// do sends one request and decodes a 200 response into out.
func (r *trafficREST) do(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, restTimeout)
	defer cancel()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	target := strings.TrimRight(r.endpoint, "/") + "/v1beta1/" + path
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, errorDetailLimit))
		return &restStatusError{status: resp.StatusCode,
			msg: fmt.Sprintf("%s: %s", resp.Status, strings.TrimSpace(string(detail)))}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// getTraffic reads an engine's traffic configuration. An engine with none set
// routes to the latest revision, which is Agent Runtime's default.
func (r *trafficREST) getTraffic(ctx context.Context, engine string) (*Traffic, error) {
	var got struct {
		TrafficConfig *restTrafficConfig `json:"trafficConfig"`
	}
	if err := r.do(ctx, http.MethodGet, engine, nil, &got); err != nil {
		// Only a 404 on the engine itself means the engine is gone. A 404
		// anywhere else (an operation, a revision) is an ordinary failure:
		// reading it as a missing engine would make apply create a second one.
		var statusErr *restStatusError
		if errors.As(err, &statusErr) && statusErr.status == http.StatusNotFound {
			return nil, fmt.Errorf("%s: %w", engine, ErrEngineNotFound)
		}
		return nil, fmt.Errorf("get traffic for %q: %w", engine, err)
	}
	return trafficFromREST(got.TrafficConfig), nil
}

// trafficFromREST converts the API's traffic configuration.
func trafficFromREST(tc *restTrafficConfig) *Traffic {
	if tc == nil || tc.TrafficSplitManual == nil {
		return &Traffic{AlwaysLatest: true}
	}
	out := &Traffic{}
	for _, t := range tc.TrafficSplitManual.Targets {
		out.Targets = append(out.Targets, TrafficTarget{Revision: t.RuntimeRevisionName, Percent: t.Percent})
	}
	return out
}

// trafficToREST converts a traffic configuration for the API.
func trafficToREST(t *Traffic) *restTrafficConfig {
	if t.AlwaysLatest {
		return &restTrafficConfig{TrafficSplitAlwaysLatest: &struct{}{}}
	}
	manual := &struct {
		Targets []restTrafficTarget `json:"targets"`
	}{}
	for _, target := range t.Targets {
		manual.Targets = append(manual.Targets, restTrafficTarget{
			RuntimeRevisionName: target.Revision, Percent: target.Percent,
		})
	}
	return &restTrafficConfig{TrafficSplitManual: manual}
}

// setTraffic patches an engine's traffic configuration and waits for the
// operation to finish.
func (r *trafficREST) setTraffic(ctx context.Context, engine string, t *Traffic) error {
	body := map[string]any{"trafficConfig": trafficToREST(t)}
	var op restOperation
	path := engine + "?updateMask=" + url.QueryEscape(trafficUpdateMask)
	if err := r.do(ctx, http.MethodPatch, path, body, &op); err != nil {
		return fmt.Errorf("set traffic for %q: %w", engine, err)
	}
	return r.wait(ctx, &op)
}

// wait polls a long-running operation until it is done.
func (r *trafficREST) wait(ctx context.Context, op *restOperation) error {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()

	interval := r.pollInterval
	if interval == 0 {
		interval = operationPollInterval
	}
	for !op.Done {
		if op.Name == "" {
			return fmt.Errorf("the traffic update returned an operation with no name")
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for %s: %w", op.Name, ctx.Err())
		case <-time.After(interval):
		}
		next := restOperation{}
		if err := r.do(ctx, http.MethodGet, op.Name, nil, &next); err != nil {
			return fmt.Errorf("read operation %s: %w", op.Name, err)
		}
		if next.Name == "" {
			next.Name = op.Name
		}
		*op = next
	}
	if op.Error != nil {
		return fmt.Errorf("traffic update failed: %s (code %d)", op.Error.Message, op.Error.Code)
	}
	return nil
}

// deleteRevision deletes one runtime revision and waits for the operation.
func (r *trafficREST) deleteRevision(ctx context.Context, revision string) error {
	var op restOperation
	if err := r.do(ctx, http.MethodDelete, revision, nil, &op); err != nil {
		return fmt.Errorf("delete revision %q: %w", revision, err)
	}
	return r.wait(ctx, &op)
}

// listRevisions returns every runtime revision of an engine.
func (r *trafficREST) listRevisions(ctx context.Context, engine string) ([]Revision, error) {
	var out []Revision
	token := ""
	for {
		path := engine + "/runtimeRevisions"
		if token != "" {
			path += "?pageToken=" + url.QueryEscape(token)
		}
		var page struct {
			Revisions     []restRevision `json:"reasoningEngineRuntimeRevisions"`
			NextPageToken string         `json:"nextPageToken"`
		}
		if err := r.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, fmt.Errorf("list revisions of %q: %w", engine, err)
		}
		for i := range page.Revisions {
			out = append(out, revisionFromREST(&page.Revisions[i]))
		}
		if page.NextPageToken == "" {
			return out, nil
		}
		token = page.NextPageToken
	}
}

// revisionFromREST converts one API revision, reading the pack version from
// its spec.
func revisionFromREST(r *restRevision) Revision {
	rev := Revision{Name: r.Name, State: r.State}
	if t, err := time.Parse(time.RFC3339Nano, r.CreateTime); err == nil {
		rev.CreateTime = t
	}
	for _, env := range r.Spec.DeploymentSpec.Env {
		if env.Name == envPackVersion {
			rev.PackVersion = env.Value
		}
	}
	return rev
}
