package main

// taskstore_test.go — l04.5 taskstore tests.
//
// TestTaskstoreInMemoryIsolation — pure unit test (no external deps).
//   Proves that two separate InMemory stores do not share state, i.e.
//   tasks created in one instance are invisible to another. This is the
//   baseline that motivates the Firestore store.
//
// TestTaskstoreFirestoreRoundtrip — integration test (requires Firebase).
//   Skipped when FIREBASE_PROJECT_ID is unset. Proves that a task created
//   via one FirestoreTaskStore instance is visible to a second instance
//   (simulating a Cloud Run restart or scale-out event).
//
// TestTaskstoreFirestorePerUserIsolation — integration test (requires Firebase).
//   Proves that List only returns tasks owned by the requesting user.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/ghchinoy/eldamoapi/data"
	"github.com/ghchinoy/eldamoapi/index"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// staticAuthenticator returns a taskstore.Authenticator that always reports
// the given user — useful for unit tests where there is no CallContext.
func staticAuthenticator(user string) taskstore.Authenticator {
	return func(_ context.Context) (string, error) {
		return user, nil
	}
}

// makeTask creates a minimal *a2a.Task for test use.
func makeTask(id string) *a2a.Task {
	return &a2a.Task{
		ID:        a2a.TaskID(id),
		ContextID: "ctx-" + id,
		Status:    a2a.TaskStatus{State: a2a.TaskStateSubmitted},
	}
}

// ── TestTaskstoreInMemoryIsolation ────────────────────────────────────────────

// TestTaskstoreInMemoryIsolation proves the in-memory store does not share
// state across instances — tasks created in store1 are not visible in store2.
// This is the explicit regression baseline for the Firestore store.
func TestTaskstoreInMemoryIsolation(t *testing.T) {
	ctx := context.Background()

	store1 := taskstore.NewInMemory(nil)
	store2 := taskstore.NewInMemory(nil) // separate instance == simulated restart

	task := makeTask("isolation-test-task-1")
	if _, err := store1.Create(ctx, task); err != nil {
		t.Fatalf("store1.Create: %v", err)
	}

	// store1 can retrieve it.
	stored, err := store1.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("store1.Get: %v", err)
	}
	if stored.Task.ID != task.ID {
		t.Errorf("store1.Get: want ID %s, got %s", task.ID, stored.Task.ID)
	}

	// store2 (the "restarted" instance) cannot see it.
	_, err = store2.Get(ctx, task.ID)
	if !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Errorf("store2.Get should return ErrTaskNotFound across instances, got: %v", err)
	}
	t.Log("✓ In-memory store correctly isolates tasks across instances (expected limitation)")
}

// ── TestTaskstoreFirestoreRoundtrip ──────────────────────────────────────────

// TestTaskstoreFirestoreRoundtrip verifies Create → Get across two separate
// FirestoreTaskStore instances, simulating a Cloud Run restart. Skipped when
// Firebase credentials are not available.
func TestTaskstoreFirestoreRoundtrip(t *testing.T) {
	if os.Getenv("FIREBASE_PROJECT_ID") == "" {
		t.Skip("FIREBASE_PROJECT_ID not set; skipping Firestore integration test")
	}
	ensureFirestoreForTest(t)

	ctx := context.Background()
	user := "test-taskstore-user"
	auth := staticAuthenticator(user)

	store1 := NewFirestoreTaskStore(firestoreClient, auth)
	store2 := NewFirestoreTaskStore(firestoreClient, auth) // separate instance

	taskID := a2a.TaskID("fs-roundtrip-" + time.Now().Format("20060102-150405"))
	task := makeTask(string(taskID))

	// Create via store1.
	v1, err := store1.Create(ctx, task)
	if err != nil {
		t.Fatalf("store1.Create: %v", err)
	}
	if v1 != 1 {
		t.Errorf("Create: want version 1, got %d", v1)
	}
	t.Cleanup(func() {
		// Best-effort cleanup: delete the test document.
		_, _ = firestoreClient.Collection(a2aTasksCollection).Doc(string(taskID)).Delete(ctx)
	})

	// Get via store2 — the "restarted" instance must find it.
	stored, err := store2.Get(ctx, taskID)
	if err != nil {
		t.Fatalf("store2.Get (cross-instance): %v", err)
	}
	if stored.Task.ID != taskID {
		t.Errorf("cross-instance Get: want ID %s, got %s", taskID, stored.Task.ID)
	}
	if stored.Version != 1 {
		t.Errorf("cross-instance Get: want version 1, got %d", stored.Version)
	}
	t.Log("✓ Firestore store persists tasks across instances")

	// Update via store2 with OCC.
	task.Status.State = a2a.TaskStateWorking
	v2, err := store2.Update(ctx, &taskstore.UpdateRequest{
		Task:        task,
		PrevVersion: v1,
	})
	if err != nil {
		t.Fatalf("store2.Update: %v", err)
	}
	if v2 != 2 {
		t.Errorf("Update: want version 2, got %d", v2)
	}

	// Stale update (wrong PrevVersion) must be rejected.
	_, err = store1.Update(ctx, &taskstore.UpdateRequest{
		Task:        task,
		PrevVersion: v1, // stale — store2 already moved to v2
	})
	if !errors.Is(err, taskstore.ErrConcurrentModification) {
		t.Errorf("stale Update: want ErrConcurrentModification, got %v", err)
	}
	t.Log("✓ Optimistic concurrency correctly rejects stale updates")
}

// ── TestTaskstoreFirestorePerUserIsolation ────────────────────────────────────

// TestTaskstoreFirestorePerUserIsolation proves that List only returns tasks
// belonging to the requesting user and not tasks owned by other users.
func TestTaskstoreFirestorePerUserIsolation(t *testing.T) {
	if os.Getenv("FIREBASE_PROJECT_ID") == "" {
		t.Skip("FIREBASE_PROJECT_ID not set; skipping Firestore integration test")
	}
	ensureFirestoreForTest(t)

	ctx := context.Background()
	ts := time.Now().Format("20060102-150405")
	userA := "test-isolation-user-a"
	userB := "test-isolation-user-b"

	storeA := NewFirestoreTaskStore(firestoreClient, staticAuthenticator(userA))
	storeB := NewFirestoreTaskStore(firestoreClient, staticAuthenticator(userB))

	idA := a2a.TaskID("isolation-a-" + ts)
	idB := a2a.TaskID("isolation-b-" + ts)

	_, err := storeA.Create(ctx, makeTask(string(idA)))
	if err != nil {
		t.Fatalf("storeA.Create: %v", err)
	}
	_, err = storeB.Create(ctx, makeTask(string(idB)))
	if err != nil {
		t.Fatalf("storeB.Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = firestoreClient.Collection(a2aTasksCollection).Doc(string(idA)).Delete(ctx)
		_, _ = firestoreClient.Collection(a2aTasksCollection).Doc(string(idB)).Delete(ctx)
	})

	// userA's list should contain idA but NOT idB.
	respA, err := storeA.List(ctx, &a2a.ListTasksRequest{PageSize: 50})
	if err != nil {
		t.Fatalf("storeA.List: %v", err)
	}
	for _, task := range respA.Tasks {
		if task.ID == idB {
			t.Errorf("userA's List returned task %s owned by userB", idB)
		}
	}
	foundA := false
	for _, task := range respA.Tasks {
		if task.ID == idA {
			foundA = true
		}
	}
	if !foundA {
		t.Errorf("userA's List did not include own task %s", idA)
	}
	t.Log("✓ Firestore List correctly isolates tasks per user")
}

// ── TestTaskstoreFirestoreNotFound ────────────────────────────────────────────

func TestTaskstoreFirestoreNotFound(t *testing.T) {
	if os.Getenv("FIREBASE_PROJECT_ID") == "" {
		t.Skip("FIREBASE_PROJECT_ID not set; skipping Firestore integration test")
	}
	ensureFirestoreForTest(t)

	ctx := context.Background()
	store := NewFirestoreTaskStore(firestoreClient, staticAuthenticator("test-user"))

	_, err := store.Get(ctx, a2a.TaskID("definitely-does-not-exist-xyz-999"))
	if !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Errorf("Get non-existent: want ErrTaskNotFound, got %v", err)
	}
}

// ── test infrastructure ───────────────────────────────────────────────────────

// ensureFirestoreForTest initialises Firebase/Firestore if not already done
// (tests don't call initFirebase as main() does).
func ensureFirestoreForTest(t *testing.T) {
	t.Helper()
	if firestoreClient != nil {
		return
	}
	// Also ensure the lexicon is loaded for any test that might trigger the executor.
	if lexiconIndex == nil {
		raw, err := data.GetJSONL()
		if err == nil {
			lexiconIndex, _ = index.NewIndex(raw)
		}
	}
	initFirebase()
	if firestoreClient == nil {
		t.Fatal("initFirebase() did not produce a Firestore client; check FIREBASE_PROJECT_ID / credentials")
	}
	t.Cleanup(func() {
		if firestoreClient != nil {
			_ = firestoreClient.Close()
			firestoreClient = nil
		}
	})
}
