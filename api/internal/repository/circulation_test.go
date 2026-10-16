package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
)

func TestReturnCopyToShelf(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	u := newUser(t, pool, "returner@athenaeum.dev")

	var copyID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM book_copies WHERE barcode = 'ATH-002941-C01'`).Scan(&copyID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	d, err := repo.ReturnCopy(ctx, u, copyID, now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Copy.ID != copyID || d.Copy.Status != model.CopyAvailable || !d.Copy.LastStateChange.Equal(now) || d.Hold != nil {
		t.Fatalf("copy %+v hold %+v", d.Copy, d.Hold)
	}
	if d.Location == nil || d.Location.Path != "MAIN/3F/SF/A-1/S-2" || d.Location.SectionCode != "SF" {
		t.Fatalf("location %+v", d.Location)
	}
	if d.Total != 3 || d.Available != 3 || len(d.Statuses) != 1 || d.Statuses[0] != model.CopyAvailable {
		t.Fatalf("availability %d/%d %v", d.Available, d.Total, d.Statuses)
	}
	var from, to, source string
	var actor uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT from_status, to_status, source, actor_id FROM copy_events
		WHERE copy_id = $1 ORDER BY id DESC LIMIT 1`, copyID).Scan(&from, &to, &source, &actor); err != nil {
		t.Fatal(err)
	}
	if from != "checked_out" || to != "available" || source != "self_return" || actor != u {
		t.Fatalf("event %s→%s %s %v", from, to, source, actor)
	}

	// A second return conflicts and reports the current status; nothing is appended.
	_, err = repo.ReturnCopy(ctx, u, copyID, now)
	de, ok := model.AsError(err)
	if !ok || de.Code != model.CodeConflict || de.Extra["current_status"] != "available" {
		t.Fatalf("second return: %v", err)
	}
	var events int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM copy_events WHERE copy_id = $1 AND source = 'self_return'`, copyID).Scan(&events)
	if events != 1 {
		t.Fatalf("self_return events %d", events)
	}

	if _, err := repo.ReturnCopy(ctx, u, uuid.New(), now); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("unknown copy: %v", err)
	}
}

func TestReturnCopyPromotesOldestWaitingHold(t *testing.T) {
	ctx := context.Background()
	pool, _ := seededCatalog(t)
	repo := repository.NewCirculationRepository(pool)
	returner := newUser(t, pool, "desk@athenaeum.dev")
	a, b := newUser(t, pool, "a@athenaeum.dev"), newUser(t, pool, "b@athenaeum.dev")
	book := bookID(t, pool, "Neuromancer")

	var copyID uuid.UUID
	if err := pool.QueryRow(ctx, `UPDATE book_copies SET status = 'checked_out'
		WHERE id = (SELECT id FROM book_copies WHERE book_id = $1 ORDER BY barcode LIMIT 1) RETURNING id`, book).Scan(&copyID); err != nil {
		t.Fatal(err)
	}
	var holdA, holdB uuid.UUID
	base := time.Now().Add(-time.Hour)
	if err := pool.QueryRow(ctx, `INSERT INTO waitlist (id, book_id, user_id, created_at) VALUES ($1,$2,$3,$4) RETURNING id`,
		uuid.New(), book, b, base.Add(time.Minute)).Scan(&holdB); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO waitlist (id, book_id, user_id, created_at) VALUES ($1,$2,$3,$4) RETURNING id`,
		uuid.New(), book, a, base).Scan(&holdA); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	d, err := repo.ReturnCopy(ctx, returner, copyID, now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Copy.Status != model.CopyReservedWaitlist {
		t.Fatalf("copy status %s", d.Copy.Status)
	}
	if d.Hold == nil || d.Hold.ID != holdA || d.Hold.Status != model.WaitReady || !d.Hold.ExpiresAt.Equal(now.Add(48*time.Hour)) {
		t.Fatalf("hold %+v", d.Hold)
	}
	var sa, sb string
	var readyAt time.Time
	_ = pool.QueryRow(ctx, `SELECT status, ready_at FROM waitlist WHERE id = $1`, holdA).Scan(&sa, &readyAt)
	_ = pool.QueryRow(ctx, `SELECT status FROM waitlist WHERE id = $1`, holdB).Scan(&sb)
	if sa != "ready" || !readyAt.Equal(now) || sb != "waiting" {
		t.Fatalf("a=%s (%v) b=%s", sa, readyAt, sb)
	}
	var to, source string
	_ = pool.QueryRow(ctx, `SELECT to_status, source FROM copy_events WHERE copy_id = $1 ORDER BY id DESC LIMIT 1`, copyID).Scan(&to, &source)
	if to != "reserved_waitlist" || source != "self_return" {
		t.Fatalf("event →%s %s", to, source)
	}
}
