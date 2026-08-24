package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Authors an Agent Runtime session event can carry. The API models a
// conversation as events from named authors; PromptKit models it as messages
// with roles, and these are the two ends of that translation.
const (
	authorUser  = "user"
	authorModel = "model"

	// PromptKit's own role names, which the session API does not share.
	roleUser      = "user"
	roleAssistant = "assistant"

	// sessionUserID owns the sessions this runtime creates. The API requires
	// one and it is immutable; the runtime authenticates callers no further
	// than the session id they present, so there is no finer identity to use.
	sessionUserID = "promptarena"
)

// sessionRoles maps between the two vocabularies.
var (
	roleForAuthor = map[string]string{
		authorUser:  roleUser,
		authorModel: roleAssistant,
	}
	authorForRole = map[string]string{
		roleUser:      authorUser,
		roleAssistant: authorModel,
	}
)

// sessionClient is the part of the Agent Runtime session API this store uses.
// An interface so the store can be tested without a project.
type sessionClient interface {
	GetSession(ctx context.Context, req *aiplatformpb.GetSessionRequest,
		opts ...gaxCallOption) (*aiplatformpb.Session, error)
	ListEvents(ctx context.Context, req *aiplatformpb.ListEventsRequest,
		opts ...gaxCallOption) eventIterator
	AppendEvent(ctx context.Context, req *aiplatformpb.AppendEventRequest,
		opts ...gaxCallOption) (*aiplatformpb.AppendEventResponse, error)
	CreateSession(ctx context.Context, req *aiplatformpb.CreateSessionRequest,
		opts ...gaxCallOption) (createSessionOp, error)
}

// SessionStore persists conversation state in Agent Runtime sessions.
//
// The engine already provides this storage, and every turn opening a fresh
// conversation is what made multi-turn context the caller's problem. Sessions
// hang off the engine, so the store is scoped to one.
type SessionStore struct {
	client sessionClient
	engine string
	log    *slog.Logger
}

// NewSessionStore returns a store writing to sessions under engine.
func NewSessionStore(client sessionClient, engine string) *SessionStore {
	return &SessionStore{client: client, engine: engine, log: slog.Default()}
}

// fail logs the whole error before returning it.
//
// The pipeline reports a stage failure without the cause, and a conversation
// that silently stops persisting looks exactly like one that was never
// configured to.
func (s *SessionStore) fail(op, id string, err error) error {
	wrapped := fmt.Errorf("%s for session %q: %w", op, id, err)
	s.log.Error("session storage failed", "op", op, "session", id, "error", err)
	return wrapped
}

// sessionName is the full resource name of one session.
func (s *SessionStore) sessionName(id string) string {
	return s.engine + "/sessions/" + id
}

// Load reads a conversation back from its session.
//
// A session that does not exist yet is not an error: the first turn of a new
// conversation names a session nobody has written to, and starting empty is
// the whole of what should happen.
func (s *SessionStore) Load(ctx context.Context, id string) (*statestore.ConversationState, error) {
	messages, err := s.loadMessages(ctx, id)
	if err != nil {
		return nil, err
	}
	s.log.Debug("session loaded", "session", id, "messages", len(messages))
	return &statestore.ConversationState{ID: id, Messages: messages}, nil
}

// loadMessages replays a session's events as messages, oldest first.
func (s *SessionStore) loadMessages(ctx context.Context, id string) ([]types.Message, error) {
	it := s.client.ListEvents(ctx, &aiplatformpb.ListEventsRequest{
		Parent: s.sessionName(id),
	})

	var messages []types.Message
	for {
		event, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			if isNotFoundErr(err) {
				// No session yet: a conversation that has not started.
				return nil, nil
			}
			return nil, s.fail("list events", id, err)
		}
		if msg, ok := messageFromEvent(event); ok {
			messages = append(messages, msg)
		}
	}
	return messages, nil
}

// AppendMessages writes new turns to the session.
//
// The session is created on first write rather than at conversation open, so a
// caller who names a session and never sends anything leaves nothing behind.
func (s *SessionStore) AppendMessages(
	ctx context.Context, id string, messages []types.Message,
) error {
	if len(messages) == 0 {
		return nil
	}
	if err := s.ensureSession(ctx, id); err != nil {
		return err
	}

	// One invocation id for the whole batch: the API groups the events of a
	// single turn by it, and a turn is what this call receives.
	invocation := fmt.Sprintf("inv-%d", time.Now().UnixNano())

	var written int
	for i := range messages {
		event, ok := eventFromMessage(&messages[i], invocation)
		if !ok {
			// A role the session API has no author for — a tool result, say.
			// Skipped rather than failed: losing it costs replay fidelity,
			// while failing costs the turn.
			continue
		}
		if _, err := s.client.AppendEvent(ctx, &aiplatformpb.AppendEventRequest{
			Name:  s.sessionName(id),
			Event: event,
		}); err != nil {
			return s.fail("append event", id, err)
		}
		written++
	}
	s.log.Debug("session appended", "session", id, "events", written, "of", len(messages))
	return nil
}

// Save replaces a conversation's state.
//
// Sessions are append-only, so this appends what is not already there rather
// than rewriting. It exists for the BulkWriter paths; the hot path uses
// AppendMessages.
func (s *SessionStore) Save(ctx context.Context, state *statestore.ConversationState) error {
	if state == nil {
		return nil
	}
	existing, err := s.loadMessages(ctx, state.ID)
	if err != nil {
		return err
	}
	if len(state.Messages) <= len(existing) {
		return nil
	}
	return s.AppendMessages(ctx, state.ID, state.Messages[len(existing):])
}

// Fork copies a conversation into a new session.
func (s *SessionStore) Fork(ctx context.Context, sourceID, newID string) error {
	messages, err := s.loadMessages(ctx, sourceID)
	if err != nil {
		return err
	}
	if len(messages) == 0 {
		return statestore.ErrNotFound
	}
	return s.AppendMessages(ctx, newID, messages)
}

// ensureSession creates the session when it is not already there.
func (s *SessionStore) ensureSession(ctx context.Context, id string) error {
	_, err := s.client.GetSession(ctx, &aiplatformpb.GetSessionRequest{
		Name: s.sessionName(id),
	})
	if err == nil {
		return nil
	}
	if !isNotFoundErr(err) {
		return s.fail("get session", id, err)
	}

	// SessionId, not DisplayName: it becomes the last component of the
	// session's resource name, which is how every later call addresses it.
	// Leaving it unset makes Vertex generate one, and the store then reads
	// and writes a name that does not exist.
	//
	// UserId is required and immutable. Sessions are keyed per user, and the
	// caller's session id is the only identity this runtime has.
	op, err := s.client.CreateSession(ctx, &aiplatformpb.CreateSessionRequest{
		Parent:    s.engine,
		SessionId: id,
		Session: &aiplatformpb.Session{
			DisplayName: id,
			UserId:      sessionUserID,
		},
	})
	if err != nil {
		if isAlreadyExistsErr(err) {
			return nil
		}
		return s.fail("create session", id, err)
	}
	if _, err := op.Wait(ctx); err != nil {
		if isAlreadyExistsErr(err) {
			return nil
		}
		return s.fail("wait for session", id, err)
	}
	return nil
}

// isAlreadyExistsErr reports whether err is the API saying the resource is
// already there.
//
// Check-then-create is not atomic, and a turn saves more than once, so two
// writes for a new conversation can both find nothing and both try to create
// it. The loser's error means the session exists, which is what the caller
// wanted — treating it as a failure loses that turn and every later read
// returns an empty conversation.
func isAlreadyExistsErr(err error) bool {
	return status.Code(err) == codes.AlreadyExists
}

// isNotFoundErr reports whether err is the API saying a resource is absent.
//
// A session that has never been written to is the ordinary first turn of a
// conversation, not a failure, so this separates the two.
func isNotFoundErr(err error) bool {
	return status.Code(err) == codes.NotFound
}

// messageFromEvent converts a session event into a message.
func messageFromEvent(event *aiplatformpb.SessionEvent) (types.Message, bool) {
	role, ok := roleForAuthor[event.GetAuthor()]
	if !ok {
		return types.Message{}, false
	}

	var text strings.Builder
	for _, part := range event.GetContent().GetParts() {
		text.WriteString(part.GetText())
	}
	if text.Len() == 0 {
		return types.Message{}, false
	}
	return types.Message{Role: role, Content: text.String()}, true
}

// messageText is a message's text, from wherever it holds it.
//
// Parts takes precedence over Content and the user turn arrives with Content
// empty, so reading Content alone silently dropped every user message and
// stored only the model's replies. The conversation then carried whatever the
// model happened to repeat back and nothing else — which looked like
// intermittent memory rather than a missing half.
func messageText(msg *types.Message) string {
	if len(msg.Parts) > 0 {
		var b strings.Builder
		for i := range msg.Parts {
			if msg.Parts[i].Text != nil {
				b.WriteString(*msg.Parts[i].Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return msg.Content
}

// eventFromMessage converts a message into a session event.
func eventFromMessage(msg *types.Message, invocation string) (*aiplatformpb.SessionEvent, bool) {
	author, ok := authorForRole[msg.Role]
	if !ok {
		return nil, false
	}
	text := messageText(msg)
	if text == "" {
		return nil, false
	}
	// Author, InvocationId and Timestamp are all required; an event missing
	// any of them is rejected, and the turn is lost with it.
	return &aiplatformpb.SessionEvent{
		Author:       author,
		InvocationId: invocation,
		Timestamp:    timestamppb.New(time.Now()),
		Content: &aiplatformpb.Content{
			Role:  author,
			Parts: []*aiplatformpb.Part{{Data: &aiplatformpb.Part_Text{Text: text}}},
		},
	}, true
}
