package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/repository"
	"github.com/cattyman919/athenaeum/api/internal/service/admin"
)

func adminSvc(t *testing.T) (*pgxpool.Pool, *admin.Service, uuid.UUID) {
	t.Helper()
	pool, cat := seededCatalog(t)
	svc := admin.New(repository.NewAdminRepository(pool), cat, repository.NewBuildingRepository(pool))
	actor := newUser(t, pool, "archivist@athenaeum.dev")
	if _, err := pool.Exec(context.Background(), `UPDATE users SET role='admin', name='Archivist Q' WHERE id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	return pool, svc, actor
}

// wantCode asserts the domain error code and returns its extra envelope keys.
func wantCode(t *testing.T, err error, code model.Code) map[string]any {
	t.Helper()
	de, ok := model.AsError(err)
	if !ok || de.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
	return de.Extra
}

func TestAdminKPIsAndTree(t *testing.T) {
	ctx := context.Background()
	_, svc, _ := adminSvc(t)

	k, err := svc.Data(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if k.TitlesInCatalog != 40 || k.Copies != (model.CopyCounts{Total: 60, Available: 48, CheckedOut: 12}) {
		t.Fatalf("kpis %+v", k)
	}
	if k.ShelfCapacity != (model.Occupancy{Occupied: 60, Total: 40 * model.ShelfSlots}) || k.ComputedAt.IsZero() {
		t.Fatalf("capacity %+v", k)
	}

	tree, err := svc.Tree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Code != "MAIN" || len(tree.Floors) != 3 {
		t.Fatalf("tree %+v", tree)
	}
	shelves, occupied := 0, 0
	var s2 *model.TreeShelf
	for _, f := range tree.Floors {
		for _, se := range f.Sections {
			for _, a := range se.Aisles {
				for i := range a.Shelves {
					sh := &a.Shelves[i]
					shelves++
					occupied += sh.Occupied
					if sh.Path == "MAIN/3F/SF/A-1/S-2" {
						s2 = sh
					}
				}
			}
		}
	}
	if shelves != 40 || occupied != 60 || s2 == nil || s2.Occupied != 4 || s2.Total != 15 || s2.HeightTier == "" {
		t.Fatalf("shelves=%d occupied=%d s2=%+v", shelves, occupied, s2)
	}

	insp, err := svc.Shelf(ctx, s2.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if insp.Code != "S-2" || insp.Aisle != "A-1" || insp.Section.Code != "SF" || insp.Floor != 3 ||
		insp.Path != "MAIN/3F/SF/A-1/S-2" || insp.Occupancy != (model.Occupancy{Occupied: 4, Total: 15}) || len(insp.Copies) != 4 {
		t.Fatalf("inspector %+v", insp)
	}
	if insp.Copies[0].Barcode != "ATH-002941-C01" || insp.Copies[0].Title != "Dune" {
		t.Fatalf("first slot %+v", insp.Copies[0])
	}
	if len(insp.Adjacent) != 3 || insp.Adjacent[0].Code != "S-1" || insp.Adjacent[1].Code != "S-3" || insp.Adjacent[1].Occupancy.Occupied != 3 {
		t.Fatalf("adjacent %+v", insp.Adjacent)
	}
	_, err = svc.Shelf(ctx, uuid.NewString())
	wantCode(t, err, model.CodeNotFound)
}

func TestAdminBookAndCopyCRUD(t *testing.T) {
	ctx := context.Background()
	pool, svc, actor := adminSvc(t)

	// Create with two auto-barcoded copies: the accession counter continues after book #40.
	b, err := svc.CreateBook(ctx, actor, admin.CreateBookRequest{
		Fields: model.BookFields{
			Title: " Solaris ", Authors: []string{"Stanisław Lem", " "}, ISBN13: str("9780156027601"),
			PublishedYear: intp(1961), OpenLibraryKey: str("/works/OL1W"),
		},
		Section: "SF",
		Copies:  []admin.CopyInput{{ShelfPath: "MAIN/3F/SF/A-4/S-2"}, {Status: "checked_out", Condition: "fair"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Solaris" || len(b.Authors) != 1 || b.Section == nil || b.Section.Code != "SF" ||
		b.OpenLibraryKey == nil || b.UpdatedAt.IsZero() || len(b.Copies) != 2 {
		t.Fatalf("created %+v", b)
	}
	if b.Copies[0].Barcode != "ATH-002981-C01" || b.Copies[1].Barcode != "ATH-002981-C02" {
		t.Fatalf("barcodes %s %s", b.Copies[0].Barcode, b.Copies[1].Barcode)
	}
	if b.Copies[0].Location == nil || b.Copies[0].Location.Path != "MAIN/3F/SF/A-4/S-2" || b.Copies[1].Location != nil {
		t.Fatalf("locations %+v", b.Copies)
	}
	if b.Availability.TotalCopies != 2 || b.Availability.AvailableCopies != 1 || b.Wayfinding == nil {
		t.Fatalf("availability %+v", b.Availability)
	}

	// Conflicts & validation.
	_, err = svc.CreateBook(ctx, actor, admin.CreateBookRequest{Fields: model.BookFields{Title: "Dup", ISBN13: str("9780441013593")}})
	if extra := wantCode(t, err, model.CodeISBNConflict); extra["book_id"] != bookID(t, pool, "Dune") {
		t.Fatalf("conflict extra %v", extra)
	}
	_, err = svc.CreateBook(ctx, actor, admin.CreateBookRequest{Fields: model.BookFields{Title: "X", ISBN13: str("9780441013590")}})
	wantCode(t, err, model.CodeInvalidISBN)
	_, err = svc.CreateBook(ctx, actor, admin.CreateBookRequest{Fields: model.BookFields{Title: "X"},
		Copies: []admin.CopyInput{{ShelfPath: "MAIN/9F/SF/A-1/S-2"}}})
	wantCode(t, err, model.CodeNotFound)
	_, err = svc.CreateBook(ctx, actor, admin.CreateBookRequest{Fields: model.BookFields{Title: "X"},
		Copies: []admin.CopyInput{{Barcode: "ATH-002941-C01"}}})
	wantCode(t, err, model.CodeConflict)
	_, err = svc.CreateBook(ctx, actor, admin.CreateBookRequest{Fields: model.BookFields{Title: "X"}, Section: "ZZ"})
	wantCode(t, err, model.CodeNotFound)

	// Add copies: existing accession continues; a seeded zero-copy book gets a fresh accession.
	dune := bookID(t, pool, "Dune")
	c, err := svc.AddCopy(ctx, actor, admin.AddCopyRequest{BookID: dune.String(),
		Copy: admin.CopyInput{ShelfPath: "MAIN/3F/SF/A-1/S-2", Condition: "fair"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Barcode != "ATH-002941-C04" || c.Location == nil || c.Location.Parts.ShelfCode != "S-2" || c.Status != "available" {
		t.Fatalf("add copy %+v", c)
	}
	oxford := bookID(t, pool, "The Oxford Guide to Library Research")
	c2, err := svc.AddCopy(ctx, actor, admin.AddCopyRequest{BookID: oxford.String()})
	if err != nil || c2.Barcode != "ATH-002982-C01" || c2.ShelfID != nil {
		t.Fatalf("zero-copy add %+v %v", c2, err)
	}
	_, err = svc.AddCopy(ctx, actor, admin.AddCopyRequest{BookID: uuid.NewString()})
	wantCode(t, err, model.CodeNotFound)
	_, err = svc.AddCopy(ctx, actor, admin.AddCopyRequest{BookID: dune.String(), Copy: admin.CopyInput{Barcode: "ATH-1-C1"}})
	wantCode(t, err, model.CodeValidationFailed)

	// Patch copy: checked_out → available by barcode reference, audited with actor.
	before := time.Now().Add(-time.Second)
	avail := "available"
	p, err := svc.PatchCopy(ctx, actor, "ATH-002941-C01", model.CopyPatch{Status: &avail})
	if err != nil || p.Status != "available" || !p.LastStateChange.After(before) {
		t.Fatalf("patch %+v %v", p, err)
	}
	ev, err := svc.CopyEvents(ctx, before.Format(time.RFC3339Nano), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) == 0 || ev[0].Barcode != "ATH-002941-C01" || ev[0].FromStatus == nil || *ev[0].FromStatus != "checked_out" ||
		ev[0].ToStatus != "available" || ev[0].Source != "admin_patch" || ev[0].Actor == nil || ev[0].Actor.Name != "Archivist Q" ||
		ev[0].BookTitle != "Dune" {
		t.Fatalf("events %+v", ev)
	}
	// Creates were audited too (2 Solaris + 2 added copies + the patch).
	if n := adminEvents(ev); n != 5 {
		t.Fatalf("want 5 admin events, got %d", n)
	}
	// Condition-only patch: no event; unknown shelf → 404; unshelve with null.
	fair := "worn"
	if _, err := svc.PatchCopy(ctx, actor, c.ID.String(), model.CopyPatch{Condition: &fair}); err != nil {
		t.Fatal(err)
	}
	if ev2, _ := svc.CopyEvents(ctx, before.Format(time.RFC3339Nano), 0); adminEvents(ev2) != 5 {
		t.Fatalf("condition change must not append an event: %d", adminEvents(ev2))
	}
	_, err = svc.PatchCopy(ctx, actor, c.ID.String(), model.CopyPatch{ShelfID: model.Optional[uuid.UUID]{Set: true, Value: uuid.New()}})
	wantCode(t, err, model.CodeNotFound)
	p, err = svc.PatchCopy(ctx, actor, c.ID.String(), model.CopyPatch{ShelfID: model.Optional[uuid.UUID]{Set: true, Null: true}})
	if err != nil || p.ShelfID != nil || p.Location != nil {
		t.Fatalf("unshelve %+v %v", p, err)
	}
	bad := "lost"
	_, err = svc.PatchCopy(ctx, actor, c.ID.String(), model.CopyPatch{Status: &bad})
	wantCode(t, err, model.CodeValidationFailed)
	_, err = svc.PatchCopy(ctx, actor, uuid.NewString(), model.CopyPatch{Status: &avail})
	wantCode(t, err, model.CodeNotFound)

	// reserved_waitlist promotes the oldest waiting patron.
	patron := newUser(t, pool, "waiter@athenaeum.dev")
	if _, err := pool.Exec(ctx, `INSERT INTO waitlist (id, book_id, user_id) VALUES ($1,$2,$3)`, uuid.New(), dune, patron); err != nil {
		t.Fatal(err)
	}
	reserved := "reserved_waitlist"
	if _, err := svc.PatchCopy(ctx, actor, "ATH-002941-C02", model.CopyPatch{Status: &reserved}); err != nil {
		t.Fatal(err)
	}
	var wstatus string
	var expires *time.Time
	_ = pool.QueryRow(ctx, `SELECT status, expires_at FROM waitlist WHERE user_id=$1`, patron).Scan(&wstatus, &expires)
	if wstatus != "ready" || expires == nil {
		t.Fatalf("waitlist %s %v", wstatus, expires)
	}

	// Patch book: partial update, section change, ISBN conflict, empty title.
	pb, err := svc.PatchBook(ctx, b.ID.String(), admin.BookPatchRequest{
		Patch:   model.BookPatch{Description: model.Optional[string]{Set: true, Value: "Ocean planet."}, Subtitle: model.Optional[string]{Set: true, Null: true}},
		Section: model.Optional[string]{Set: true, Value: "SCI"},
	})
	if err != nil || pb.Description == nil || *pb.Description != "Ocean planet." || pb.Section.Code != "SCI" || pb.Title != "Solaris" {
		t.Fatalf("patch book %+v %v", pb, err)
	}
	_, err = svc.PatchBook(ctx, b.ID.String(), admin.BookPatchRequest{Patch: model.BookPatch{ISBN13: model.Optional[string]{Set: true, Value: "9780441013593"}}})
	wantCode(t, err, model.CodeISBNConflict)
	empty := " "
	_, err = svc.PatchBook(ctx, b.ID.String(), admin.BookPatchRequest{Patch: model.BookPatch{Title: &empty}})
	wantCode(t, err, model.CodeValidationFailed)
	_, err = svc.PatchBook(ctx, uuid.NewString(), admin.BookPatchRequest{})
	wantCode(t, err, model.CodeNotFound)

	// List: search, shelf filter (path or id), sort, pagination.
	page, err := svc.ListBooks(ctx, admin.BookListRequest{Search: "solar"})
	if err != nil || page.Meta.Total != 1 || page.Items[0].ID != b.ID {
		t.Fatalf("search %+v %v", page.Meta, err)
	}
	page, err = svc.ListBooks(ctx, admin.BookListRequest{Shelf: "MAIN/3F/SF/A-1/S-2", Sort: "title:asc"})
	if err != nil || page.Meta.Total != 2 || page.Items[0].Title != "Dune" || page.Items[1].Title != "Neuromancer" {
		t.Fatalf("shelf filter %+v %v", page.Meta, err)
	}
	page, err = svc.ListBooks(ctx, admin.BookListRequest{PerPage: 5})
	if err != nil || page.Meta.Total != 41 || page.Meta.TotalPages != 9 || len(page.Items) != 5 || page.Items[0].ID != b.ID {
		t.Fatalf("default sort updated_at:desc %+v %v", page.Meta, err)
	}
	_, err = svc.ListBooks(ctx, admin.BookListRequest{Sort: "isbn:asc"})
	wantCode(t, err, model.CodeValidationFailed)

	// Deletes.
	if err := svc.DeleteCopy(ctx, c2.ID.String()); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.DeleteCopy(ctx, c2.ID.String()), model.CodeNotFound)
	wantCode(t, svc.DeleteBook(ctx, b.ID.String()), model.CodeConflict)
	if _, err := svc.PatchCopy(ctx, actor, "ATH-002981-C02", model.CopyPatch{Status: &avail}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBook(ctx, b.ID.String()); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM book_copies WHERE barcode LIKE 'ATH-002981-%'`).Scan(&n)
	if n != 0 {
		t.Fatal("copies must cascade")
	}
	wantCode(t, svc.DeleteBook(ctx, b.ID.String()), model.CodeNotFound)
}

func TestAdminBulkDeleteAndISBNSearch(t *testing.T) {
	ctx := context.Background()
	pool, svc, actor := adminSvc(t)
	one, err := svc.CreateBook(ctx, actor, admin.CreateBookRequest{Fields: model.BookFields{Title: "Bulk removable"}, Copies: []admin.CopyInput{{}}})
	if err != nil {
		t.Fatal(err)
	}
	two, err := svc.CreateBook(ctx, actor, admin.CreateBookRequest{Fields: model.BookFields{Title: "Bulk reserved"}, Copies: []admin.CopyInput{{Status: model.CopyReservedWaitlist}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO book_isbns (isbn13, book_id, is_primary) VALUES ('9781234567897', $1, false)`, one.ID); err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListBooks(ctx, admin.BookListRequest{Search: "34567897"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != one.ID || !page.Items[0].Removable || page.Items[0].ActiveLoans != 0 {
		t.Fatalf("alternate ISBN search: %+v, %v", page, err)
	}
	page, err = svc.ListBooks(ctx, admin.BookListRequest{Search: "13593"})
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Dune" {
		t.Fatalf("primary ISBN substring search: %+v, %v", page, err)
	}
	page, err = svc.ListBooks(ctx, admin.BookListRequest{Search: "Bulk reserved"})
	if err != nil || len(page.Items) != 1 || page.Items[0].Removable || page.Items[0].ActiveLoans != 0 {
		t.Fatalf("reserved search: %+v, %v", page, err)
	}
	ids := []string{one.ID.String(), two.ID.String()}
	wantCode(t, svc.DeleteBooks(ctx, nil), model.CodeValidationFailed)
	wantCode(t, svc.DeleteBooks(ctx, []string{"bad"}), model.CodeValidationFailed)
	wantCode(t, svc.DeleteBooks(ctx, []string{ids[0], ids[0]}), model.CodeValidationFailed)
	wantCode(t, svc.DeleteBooks(ctx, make([]string, 51)), model.CodeValidationFailed)
	wantCode(t, svc.DeleteBooks(ctx, []string{ids[0], uuid.NewString()}), model.CodeNotFound)
	extra := wantCode(t, svc.DeleteBooks(ctx, ids), model.CodeConflict)
	if extra["blocked"] == nil {
		t.Fatal("missing blocked details")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM books WHERE id = ANY($1)`, []uuid.UUID{one.ID, two.ID}).Scan(&count); err != nil || count != 2 {
		t.Fatalf("atomic delete count %d: %v", count, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO loans (id, copy_id, user_id, borrowed_at, due_at) SELECT $1, id, $2, now(), now() + interval '14 days' FROM book_copies WHERE book_id=$3`, uuid.New(), actor, one.ID); err != nil {
		t.Fatal(err)
	}
	page, err = svc.ListBooks(ctx, admin.BookListRequest{Search: "Bulk removable"})
	if err != nil || len(page.Items) != 1 || page.Items[0].Removable || page.Items[0].ActiveLoans != 1 {
		t.Fatalf("open loan search: %+v, %v", page, err)
	}
	wantCode(t, svc.DeleteBook(ctx, one.ID.String()), model.CodeConflict)
	if _, err := pool.Exec(ctx, `UPDATE loans SET returned_at=now() WHERE copy_id IN (SELECT id FROM book_copies WHERE book_id=$1)`, one.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE book_copies SET status='available' WHERE book_id=$1`, two.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBooks(ctx, ids); err != nil {
		t.Fatal(err)
	}
}

func TestAdminDeleteWaitsForConcurrentCheckout(t *testing.T) {
	ctx := context.Background()
	pool, svc, actor := adminSvc(t)
	book, err := svc.CreateBook(ctx, actor, admin.CreateBookRequest{Fields: model.BookFields{Title: "Concurrent checkout"}, Copies: []admin.CopyInput{{}}})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE book_copies SET status='checked_out' WHERE book_id=$1`, book.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- svc.DeleteBook(ctx, book.ID.String()) }()
	select {
	case err := <-result:
		t.Fatalf("delete completed before checkout commit: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		wantCode(t, err, model.CodeConflict)
	case <-time.After(5 * time.Second):
		t.Fatal("delete did not complete after checkout")
	}
}

func TestAdminUsers(t *testing.T) {
	ctx := context.Background()
	pool, svc, actor := adminSvc(t)
	u := newUser(t, pool, "patron@athenaeum.dev")
	if _, err := pool.Exec(ctx, `INSERT INTO user_library (id, user_id, book_id) VALUES ($1,$2,$3)`, uuid.New(), u, bookID(t, pool, "Dune")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES ($1,$2,'h',now()+interval '1 day')`, uuid.New(), u); err != nil {
		t.Fatal(err)
	}

	page, err := svc.Users(ctx, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	var got *model.AdminUser
	for i := range page.Items {
		if page.Items[i].ID == u {
			got = &page.Items[i]
		}
	}
	if got == nil || got.LibraryCount != 1 || got.WaitlistCount != 0 || got.LastLoginAt == nil || got.Role != "user" ||
		page.Meta.Total != len(page.Items) {
		t.Fatalf("users %+v %+v", got, page.Meta)
	}

	pu, err := svc.SetRole(ctx, actor, u.String(), "admin")
	if err != nil || pu.Role != "admin" || pu.Email != "patron@athenaeum.dev" {
		t.Fatalf("promote %+v %v", pu, err)
	}
	_, err = svc.SetRole(ctx, actor, actor.String(), "user")
	wantCode(t, err, model.CodeValidationFailed)
	_, err = svc.SetRole(ctx, actor, u.String(), "root")
	wantCode(t, err, model.CodeValidationFailed)
	_, err = svc.SetRole(ctx, actor, uuid.NewString(), "user")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}

func intp(n int) *int { return &n }

func adminEvents(ev []model.CopyEvent) int {
	n := 0
	for _, e := range ev {
		if e.Source == model.SourceAdminPatch {
			n++
		}
	}
	return n
}
