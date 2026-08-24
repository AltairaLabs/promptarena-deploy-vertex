package main

import (
	"context"
	"strings"
	"testing"

	"cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// fakeSessionClient is an in-memory stand-in for the Agent Runtime session API.
type fakeSessionClient struct {
	events    map[string][]*aiplatformpb.SessionEvent
	created   []string
	appended  []*aiplatformpb.AppendEventRequest
	getErr    error
	createErr error
}

func newFakeSessionClient() *fakeSessionClient {
	return &fakeSessionClient{events: map[string][]*aiplatformpb.SessionEvent{}}
}

func (f *fakeSessionClient) GetSession(
	_ context.Context, req *aiplatformpb.GetSessionRequest, _ ...gaxCallOption,
) (*aiplatformpb.Session, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if _, ok := f.events[req.GetName()]; !ok {
		return nil, status.Error(codes.NotFound, "no such session")
	}
	return &aiplatformpb.Session{Name: req.GetName()}, nil
}

func (f *fakeSessionClient) ListEvents(
	_ context.Context, req *aiplatformpb.ListEventsRequest, _ ...gaxCallOption,
) eventIterator {
	events, ok := f.events[req.GetParent()]
	if !ok {
		return &fakeEventIterator{err: status.Error(codes.NotFound, "no such session")}
	}
	return &fakeEventIterator{events: events}
}

func (f *fakeSessionClient) AppendEvent(
	_ context.Context, req *aiplatformpb.AppendEventRequest, _ ...gaxCallOption,
) (*aiplatformpb.AppendEventResponse, error) {
	// Author, InvocationId and Timestamp are required by the real API. The
	// fake rejects what it would reject, so an event missing one fails here
	// rather than only against a live project.
	ev := req.GetEvent()
	if ev.GetAuthor() == "" || ev.GetInvocationId() == "" || ev.GetTimestamp() == nil {
		return nil, status.Error(codes.InvalidArgument,
			"author, invocation_id and timestamp are required")
	}
	f.appended = append(f.appended, req)
	f.events[req.GetName()] = append(f.events[req.GetName()], req.GetEvent())
	return &aiplatformpb.AppendEventResponse{}, nil
}

func (f *fakeSessionClient) CreateSession(
	_ context.Context, req *aiplatformpb.CreateSessionRequest, _ ...gaxCallOption,
) (createSessionOp, error) {
	// The session's resource name comes from SessionId, exactly as Vertex
	// builds it. Keying the fake off DisplayName instead would let the store
	// write to one name and read from another and still pass.
	if req.GetSessionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if req.GetSession().GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	name := req.GetParent() + "/sessions/" + req.GetSessionId()
	f.created = append(f.created, name)
	if _, ok := f.events[name]; !ok {
		f.events[name] = nil
	}
	return fakeCreateOp{}, nil
}

type fakeEventIterator struct {
	events []*aiplatformpb.SessionEvent
	i      int
	err    error
}

func (f *fakeEventIterator) Next() (*aiplatformpb.SessionEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.i >= len(f.events) {
		return nil, iterator.Done
	}
	e := f.events[f.i]
	f.i++
	return e, nil
}

type fakeCreateOp struct{}

func (fakeCreateOp) Wait(_ context.Context, _ ...gaxCallOption) (*aiplatformpb.Session, error) {
	return &aiplatformpb.Session{}, nil
}

const testEngine = "projects/p/locations/us-central1/reasoningEngines/123"

// TestSessionStore_CarriesConversationAcrossTurns is the behaviour this whole
// store exists for: what one turn says, the next turn can read.
func TestSessionStore_CarriesConversationAcrossTurns(t *testing.T) {
	store := NewSessionStore(newFakeSessionClient(), testEngine)
	ctx := context.Background()

	if err := store.AppendMessages(ctx, "s1", []types.Message{
		{Role: roleUser, Content: "Remember 8675309."},
		{Role: roleAssistant, Content: "Noted."},
	}); err != nil {
		t.Fatalf("AppendMessages: %v", err)
	}

	state, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(state.Messages) != 2 {
		t.Fatalf("got %d messages, want 2: %+v", len(state.Messages), state.Messages)
	}
	if state.Messages[0].Role != roleUser || !strings.Contains(state.Messages[0].Content, "8675309") {
		t.Errorf("first message did not round-trip: %+v", state.Messages[0])
	}
	if state.Messages[1].Role != roleAssistant {
		t.Errorf("second message role = %q, want %q", state.Messages[1].Role, roleAssistant)
	}
}

// TestSessionStore_UnwrittenSessionIsEmptyNotAnError covers the first turn of
// every new conversation.
//
// The caller names a session nobody has written to yet. Treating the API's
// NotFound as a failure would make every conversation fail on its opening
// turn.
func TestSessionStore_UnwrittenSessionIsEmptyNotAnError(t *testing.T) {
	store := NewSessionStore(newFakeSessionClient(), testEngine)

	state, err := store.Load(context.Background(), "never-written")
	if err != nil {
		t.Fatalf("a session that does not exist yet should load empty: %v", err)
	}
	if len(state.Messages) != 0 {
		t.Errorf("got %d messages, want none", len(state.Messages))
	}
}

// TestSessionStore_CreatesTheSessionOnFirstWrite checks the session is made
// before events are appended to it.
func TestSessionStore_CreatesTheSessionOnFirstWrite(t *testing.T) {
	fake := newFakeSessionClient()
	store := NewSessionStore(fake, testEngine)

	if err := store.AppendMessages(context.Background(), "s2",
		[]types.Message{{Role: roleUser, Content: "hello"}}); err != nil {
		t.Fatalf("AppendMessages: %v", err)
	}

	if len(fake.created) != 1 {
		t.Fatalf("session was not created: %v", fake.created)
	}
	if want := testEngine + "/sessions/s2"; fake.created[0] != want {
		t.Errorf("created %q, want %q", fake.created[0], want)
	}
}

// TestSessionStore_SkipsRolesTheAPICannotCarry keeps a tool result from
// failing a turn.
//
// A session event has an author, and the API has no author for a tool result.
// Losing it costs replay fidelity; failing the append would cost the turn.
func TestSessionStore_SkipsRolesTheAPICannotCarry(t *testing.T) {
	fake := newFakeSessionClient()
	store := NewSessionStore(fake, testEngine)

	err := store.AppendMessages(context.Background(), "s3", []types.Message{
		{Role: roleUser, Content: "hi"},
		{Role: "tool", Content: "tool output"},
	})
	if err != nil {
		t.Fatalf("a role the API cannot carry should not fail the turn: %v", err)
	}
	if len(fake.appended) != 1 {
		t.Errorf("appended %d events, want 1", len(fake.appended))
	}
}

// TestWithSession_RefusesWhenStorageIsMissing covers the case where the runtime
// was never told which engine it is.
//
// Answering the request as a one-off would look to the caller like an agent
// that forgets, which is the failure this feature exists to remove — so it is
// refused with the reason instead.
func TestWithSession_RefusesWhenStorageIsMissing(t *testing.T) {
	_, err := withSession(nil, nil, "s1")
	if err == nil {
		t.Fatal("expected an error when a session is named but unavailable")
	}
	if !strings.Contains(err.Error(), envEngineID) {
		t.Errorf("error should name the missing variable, got %v", err)
	}
}

// TestWithSession_NoSessionKeepsTodaysBehaviour pins the default: a request
// that does not ask for continuity behaves exactly as it did before.
func TestWithSession_NoSessionKeepsTodaysBehaviour(t *testing.T) {
	opts, err := withSession(nil, nil, "")
	if err != nil {
		t.Fatalf("a request without a session should not fail: %v", err)
	}
	if len(opts) != 0 {
		t.Errorf("no session options should be added, got %d", len(opts))
	}
}

// TestExtractSession covers the accepted spellings.
func TestExtractSession(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"snake case", map[string]any{"session_id": "a"}, "a"},
		{"camel case", map[string]any{"sessionId": "b"}, "b"},
		{"bare", map[string]any{"session": "c"}, "c"},
		{"absent", map[string]any{"message": "hi"}, ""},
		{"empty is absent", map[string]any{"session_id": ""}, ""},
		{"wrong type is absent", map[string]any{"session_id": 42}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractSession(tt.input); got != tt.want {
				t.Errorf("extractSession = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestEngineName covers composing the resource name Agent Runtime never hands
// over whole.
func TestEngineName(t *testing.T) {
	for _, tt := range []struct {
		name string
		cfg  runtimeConfig
		want string
	}{
		{
			"all three pieces",
			runtimeConfig{Project: "p", Location: "us-central1", EngineID: "123"},
			"projects/p/locations/us-central1/reasoningEngines/123",
		},
		{"no engine id", runtimeConfig{Project: "p", Location: "us-central1"}, ""},
		{"no project", runtimeConfig{Location: "us-central1", EngineID: "123"}, ""},
		{"no location", runtimeConfig{Project: "p", EngineID: "123"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.engineName(); got != tt.want {
				t.Errorf("engineName = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSessionStore_PersistsMessagesCarryingTextInParts covers the half of a
// conversation that was silently dropped.
//
// A user turn arrives with Content empty and its text in Parts, which takes
// precedence. Reading Content alone stored only the model's replies, so a
// conversation carried whatever the model happened to repeat back — memory
// that worked or not depending on how the model phrased its acknowledgement.
func TestSessionStore_PersistsMessagesCarryingTextInParts(t *testing.T) {
	fake := newFakeSessionClient()
	store := NewSessionStore(fake, testEngine)
	ctx := context.Background()

	text := "Remember this number: 8675309."
	err := store.AppendMessages(ctx, "s4", []types.Message{
		{Role: roleUser, Parts: []types.ContentPart{{Type: "text", Text: &text}}},
		{Role: roleAssistant, Content: "Acknowledged."},
	})
	if err != nil {
		t.Fatalf("AppendMessages: %v", err)
	}

	state, err := store.Load(ctx, "s4")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(state.Messages) != 2 {
		t.Fatalf("got %d messages, want both halves of the turn: %+v",
			len(state.Messages), state.Messages)
	}
	if !strings.Contains(state.Messages[0].Content, "8675309") {
		t.Errorf("the user turn did not persist: %+v", state.Messages[0])
	}
}

// TestMessageText covers where a message keeps its text.
func TestMessageText(t *testing.T) {
	part := "from parts"
	empty := ""
	for _, tt := range []struct {
		name string
		msg  types.Message
		want string
	}{
		{"content only", types.Message{Content: "from content"}, "from content"},
		{
			"parts win over content",
			types.Message{
				Content: "from content",
				Parts:   []types.ContentPart{{Type: "text", Text: &part}},
			},
			"from parts",
		},
		{
			"empty parts fall back to content",
			types.Message{
				Content: "from content",
				Parts:   []types.ContentPart{{Type: "text", Text: &empty}},
			},
			"from content",
		},
		{
			"non-text parts fall back to content",
			types.Message{
				Content: "from content",
				Parts:   []types.ContentPart{{Type: "image"}},
			},
			"from content",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := messageText(&tt.msg); got != tt.want {
				t.Errorf("messageText = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSessionStore_ConcurrentFirstWritesBothSucceed covers the create race.
//
// A turn saves more than once and check-then-create is not atomic, so two
// writes to a new conversation can both find nothing and both try to create
// it. If the loser treats AlreadyExists as a failure, that turn is lost and
// every later read returns an empty conversation — which reads as an agent
// that forgot, at random.
func TestSessionStore_ConcurrentFirstWritesBothSucceed(t *testing.T) {
	fake := newFakeSessionClient()
	fake.createErr = status.Error(codes.AlreadyExists, "session already exists")
	store := NewSessionStore(fake, testEngine)

	err := store.AppendMessages(context.Background(), "race",
		[]types.Message{{Role: roleUser, Content: "hello"}})
	if err != nil {
		t.Fatalf("losing a create race should not fail the turn: %v", err)
	}
}
