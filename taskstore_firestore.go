package main

// taskstore_firestore.go — l04.5: Firestore-backed A2A task store.
//
// Implements taskstore.Store using the mithlond-services Firestore database
// (the same instance as authorized_users and mcp_auth_codes), enabling task
// persistence across Cloud Run instances and restarts.
//
// Collection: "a2a_tasks"
// Document ID: the task UUID (a2a.TaskID)
// Document schema: see a2aTaskDoc below.
//
// Concurrency: optimistic, via Firestore transactions on Update.
// TTL: 7 days (Firestore TTL policy on the expiresAt field).
// List: filters by user for per-user isolation; in-memory secondary filters.
// Pagination: first-page only (no cursor); full pagination is a follow-up.
//
// Falls back to taskstore.InMemory when firestoreClient is nil (local dev
// without Firebase credentials, AUTH_BYPASS mode).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	a2aTasksCollection = "a2a_tasks"
	taskTTL            = 7 * 24 * time.Hour
	defaultListSize    = 50
)

// a2aTaskDoc is the Firestore document stored for each A2A task.
// The full task is JSON-serialised into TaskJSON to avoid mapping the
// discriminated-union Part.Content type to Firestore's map model.
type a2aTaskDoc struct {
	TaskJSON  string    `firestore:"taskJSON"`
	Version   int64     `firestore:"version"`
	User      string    `firestore:"user"`
	UpdatedAt time.Time `firestore:"updatedAt"`
	ExpiresAt time.Time `firestore:"expiresAt"` // TTL policy target field
}

// FirestoreTaskStore implements taskstore.Store using Cloud Firestore.
type FirestoreTaskStore struct {
	client        *firestore.Client
	authenticator taskstore.Authenticator
}

var _ taskstore.Store = (*FirestoreTaskStore)(nil)

// NewFirestoreTaskStore creates a Firestore-backed task store.
// client must be the already-initialised firestoreClient from oauth.go.
// authenticator should be a2asrv.NewTaskStoreAuthenticator() so that
// task ownership is tied to the CallContext.User set by claimsInterceptor.
func NewFirestoreTaskStore(client *firestore.Client, auth taskstore.Authenticator) *FirestoreTaskStore {
	return &FirestoreTaskStore{client: client, authenticator: auth}
}

func (s *FirestoreTaskStore) coll() *firestore.CollectionRef {
	return s.client.Collection(a2aTasksCollection)
}

// isNotFound returns true for a gRPC NOT_FOUND status error from Firestore.
func isFirestoreNotFound(err error) bool {
	return grpcstatus.Code(err) == codes.NotFound
}

// marshalTask JSON-serialises a *a2a.Task.
func marshalTask(task *a2a.Task) (string, error) {
	b, err := json.Marshal(task)
	if err != nil {
		return "", fmt.Errorf("marshal task %s: %w", task.ID, err)
	}
	return string(b), nil
}

// unmarshalTask deserialises a *a2a.Task from JSON.
func unmarshalTask(js string) (*a2a.Task, error) {
	var task a2a.Task
	if err := json.Unmarshal([]byte(js), &task); err != nil {
		return nil, fmt.Errorf("unmarshal task: %w", err)
	}
	return &task, nil
}

// newDoc builds an a2aTaskDoc for a fresh or updated task.
func newDoc(taskJSON string, version int64, user string) a2aTaskDoc {
	now := time.Now()
	return a2aTaskDoc{
		TaskJSON:  taskJSON,
		Version:   version,
		User:      user,
		UpdatedAt: now,
		ExpiresAt: now.Add(taskTTL),
	}
}

// ── Create ────────────────────────────────────────────────────────────────────

// Create implements taskstore.Store.
// Uses a Firestore transaction to guarantee atomicity against concurrent
// creates with the same task ID.
func (s *FirestoreTaskStore) Create(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
	user, err := s.authenticator(ctx)
	if err != nil {
		return taskstore.TaskVersionMissing, fmt.Errorf("taskstore auth: %w", err)
	}

	taskJSON, err := marshalTask(task)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}

	docRef := s.coll().Doc(string(task.ID))

	txErr := s.client.RunTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(docRef)
		if err == nil && snap.Exists() {
			return taskstore.ErrTaskAlreadyExists
		}
		if err != nil && !isFirestoreNotFound(err) {
			return err
		}
		return tx.Set(docRef, newDoc(taskJSON, 1, user))
	})
	if txErr != nil {
		return taskstore.TaskVersionMissing, txErr
	}

	log.Printf("[Taskstore] Created task %s (user=%q)", task.ID, user)
	return taskstore.TaskVersion(1), nil
}

// ── Update ────────────────────────────────────────────────────────────────────

// Update implements taskstore.Store.
// Uses a Firestore transaction to enforce optimistic concurrency:
// if req.PrevVersion doesn't match the stored version,
// ErrConcurrentModification is returned.
func (s *FirestoreTaskStore) Update(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
	taskJSON, err := marshalTask(req.Task)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}

	docRef := s.coll().Doc(string(req.Task.ID))
	var newVersion taskstore.TaskVersion

	txErr := s.client.RunTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(docRef)
		if err != nil {
			if isFirestoreNotFound(err) {
				return a2a.ErrTaskNotFound
			}
			return err
		}

		var stored a2aTaskDoc
		if err = snap.DataTo(&stored); err != nil {
			return fmt.Errorf("decode task doc: %w", err)
		}

		current := taskstore.TaskVersion(stored.Version)
		if req.PrevVersion != taskstore.TaskVersionMissing && current != req.PrevVersion {
			return taskstore.ErrConcurrentModification
		}

		newVersion = current + 1
		// Preserve original user; only the task payload and version change.
		return tx.Set(docRef, newDoc(taskJSON, int64(newVersion), stored.User))
	})
	if txErr != nil {
		return taskstore.TaskVersionMissing, txErr
	}
	return newVersion, nil
}

// ── Get ───────────────────────────────────────────────────────────────────────

// Get implements taskstore.Store.
func (s *FirestoreTaskStore) Get(ctx context.Context, taskID a2a.TaskID) (*taskstore.StoredTask, error) {
	snap, err := s.coll().Doc(string(taskID)).Get(ctx)
	if err != nil {
		if isFirestoreNotFound(err) {
			return nil, a2a.ErrTaskNotFound
		}
		return nil, err
	}

	var doc a2aTaskDoc
	if err = snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("decode task doc %s: %w", taskID, err)
	}

	task, err := unmarshalTask(doc.TaskJSON)
	if err != nil {
		return nil, err
	}
	return &taskstore.StoredTask{
		Task:    task,
		Version: taskstore.TaskVersion(doc.Version),
	}, nil
}

// ── List ──────────────────────────────────────────────────────────────────────

// List implements taskstore.Store.
// Returns tasks owned by the authenticated user, ordered newest-first.
// ContextID and Status filters are applied in-memory after the Firestore
// query (adding Firestore composite indexes for these is a follow-up).
// Full cursor-based pagination is not yet implemented; NextPageToken is
// always empty (the first page is returned).
func (s *FirestoreTaskStore) List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	user, err := s.authenticator(ctx)
	if err != nil || user == "" {
		return nil, a2a.ErrUnauthenticated
	}

	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = defaultListSize
	}

	// Over-fetch to allow in-memory filtering to reach pageSize results.
	query := s.coll().
		Where("user", "==", user).
		OrderBy("updatedAt", firestore.Desc).
		Limit(pageSize * 4)

	it := query.Documents(ctx)
	defer it.Stop()

	var tasks []*a2a.Task
	for {
		snap, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list tasks query: %w", err)
		}

		var doc a2aTaskDoc
		if err = snap.DataTo(&doc); err != nil {
			continue // skip malformed docs
		}
		task, err := unmarshalTask(doc.TaskJSON)
		if err != nil {
			continue
		}

		// Secondary filters (in-memory).
		if req.ContextID != "" && task.ContextID != req.ContextID {
			continue
		}
		if req.Status != a2a.TaskStateUnspecified && task.Status.State != req.Status {
			continue
		}
		if req.StatusTimestampAfter != nil && task.Status.Timestamp != nil &&
			task.Status.Timestamp.Before(*req.StatusTimestampAfter) {
			continue
		}

		tasks = append(tasks, task)
		if len(tasks) >= pageSize {
			break
		}
	}

	// Shape each task per the request options.
	result := make([]*a2a.Task, len(tasks))
	for i, t := range tasks {
		shaped := *t
		if !req.IncludeArtifacts {
			shaped.Artifacts = nil
		}
		if req.HistoryLength != nil {
			hl := *req.HistoryLength
			if hl == 0 {
				shaped.History = nil
			} else if len(shaped.History) > hl {
				shaped.History = shaped.History[len(shaped.History)-hl:]
			}
		}
		result[i] = &shaped
	}

	return &a2a.ListTasksResponse{
		Tasks:     result,
		TotalSize: len(result),
		PageSize:  pageSize,
		// NextPageToken: ""  — cursor pagination is a future enhancement
	}, nil
}
