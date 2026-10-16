// Package admin implements the staff endpoints of docs/03 §8: KPIs, the copy audit
// stream, book/copy CRUD with barcode allocation, the building tree, the shelf
// inspector and user role management.
package admin

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/checkdigit"
	"github.com/cattyman919/athenaeum/api/internal/service/building"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
)

// Repository is the persistence the admin service needs (repository.AdminRepository).
type Repository interface {
	KPIs(ctx context.Context, now time.Time) (model.AdminKPIs, error)
	CopyEvents(ctx context.Context, since *time.Time, limit int) ([]model.CopyEvent, error)
	ListBooks(ctx context.Context, f model.AdminBookFilter, page, perPage int) ([]model.AdminBookRow, int, error)
	BookMeta(ctx context.Context, id uuid.UUID) (model.BookMeta, error)
	BookIDByISBN(ctx context.Context, isbn13 string) (uuid.UUID, bool, error)
	SectionByCode(ctx context.Context, code string) (uuid.UUID, error)
	CreateBook(ctx context.Context, actor uuid.UUID, b model.BookFields, copies []model.NewCopy) (uuid.UUID, error)
	PatchBook(ctx context.Context, id uuid.UUID, p model.BookPatch) error
	DeleteBook(ctx context.Context, id uuid.UUID) error
	AddCopy(ctx context.Context, actor, bookID uuid.UUID, c model.NewCopy) (model.AdminCopy, *model.LocationRow, error)
	PatchCopy(ctx context.Context, actor, id uuid.UUID, p model.CopyPatch, now time.Time) (model.AdminCopy, *model.LocationRow, error)
	DeleteCopy(ctx context.Context, id uuid.UUID) error
	CopyIDByBarcode(ctx context.Context, barcode string) (uuid.UUID, error)
	Tree(ctx context.Context) (model.TreeBuilding, error)
	Shelf(ctx context.Context, id uuid.UUID) (model.ShelfInspector, error)
	Users(ctx context.Context, page, perPage int) ([]model.AdminUser, int, error)
	SetRole(ctx context.Context, id uuid.UUID, role string) (model.User, error)
}

// Details assembles the book detail schema (catalog.Service).
type Details interface {
	Detail(ctx context.Context, id string, userID uuid.UUID) (model.BookDetail, error)
}

// IndexNotifier wakes the auto-index worker after a RAG-relevant write (index.Worker).
type IndexNotifier interface {
	Notify()
}

// IndexStatusSource reports the auto-index backlog (repository.IndexStore).
type IndexStatusSource interface {
	PendingStatus(ctx context.Context, limit int) (model.IndexStatus, error)
}

// MaxIndexStatusIDs caps pending_book_ids in GET /api/admin/index-status (docs/03 §8.1).
const MaxIndexStatusIDs = 200

// Service is the admin use-case layer.
type Service struct {
	repo        Repository
	details     Details
	paths       building.Lookup
	indexer     IndexNotifier
	indexStatus IndexStatusSource
	now         func() time.Time
}

// New builds the service.
func New(repo Repository, details Details, paths building.Lookup) *Service {
	return &Service{repo: repo, details: details, paths: paths, now: time.Now}
}

// WithIndexNotifier wakes n after book writes that mark embeddings dirty (docs/06 §2).
func (s *Service) WithIndexNotifier(n IndexNotifier) *Service { s.indexer = n; return s }

// WithIndexStatus sets the backlog source behind GET /api/admin/index-status.
func (s *Service) WithIndexStatus(src IndexStatusSource) *Service { s.indexStatus = src; return s }

// IndexStatus returns the auto-index backlog (GET /api/admin/index-status).
func (s *Service) IndexStatus(ctx context.Context) (model.IndexStatus, error) {
	if s.indexStatus == nil {
		return model.IndexStatus{}, model.New(model.CodeUnexpected, "Index status is not configured.")
	}
	st, err := s.indexStatus.PendingStatus(ctx, MaxIndexStatusIDs)
	if err != nil {
		return model.IndexStatus{}, err
	}
	if st.PendingBookIDs == nil {
		st.PendingBookIDs = []string{}
	}
	return st, nil
}

func (s *Service) notifyIndex() {
	if s.indexer != nil {
		s.indexer.Notify()
	}
}

// WithClock overrides the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// Limits (docs/03 §1, §8.1).
const (
	MaxPerPage         = 50
	DefaultEventLimit  = 100
	MaxEventLimit      = 500
	MaxCopiesPerCreate = 50
	maxConditionLen    = 40
	maxYear            = 9999
)

var (
	barcodeRe   = regexp.MustCompile(`^ATH-[0-9]{6}-C[0-9]{2}$`)
	copyStatus  = map[string]bool{model.CopyAvailable: true, model.CopyCheckedOut: true, model.CopyReservedWaitlist: true, model.CopyMissingLost: true}
	sortColumns = map[string]bool{"updated_at": true, "created_at": true, "title": true, "published_year": true}
)

func invalid(format string, a ...any) error {
	return model.New(model.CodeValidationFailed, fmt.Sprintf(format, a...))
}

func parseID(id, what string) (uuid.UUID, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, model.New(model.CodeNotFound, what+" not found.")
	}
	return u, nil
}

func pageArgs(page, perPage int) (int, int, error) {
	if page == 0 {
		page = 1
	}
	if perPage == 0 {
		perPage = 20
	}
	if page < 1 || perPage < 1 || perPage > MaxPerPage {
		return 0, 0, invalid("page must be >= 1 and per_page between 1 and %d.", MaxPerPage)
	}
	return page, perPage, nil
}

func meta(page, perPage, total int) model.PageMeta {
	return model.PageMeta{Page: page, PerPage: perPage, Total: total, TotalPages: (total + perPage - 1) / perPage}
}

// ---- KPIs & audit ----------------------------------------------------------

// Data returns the KPI snapshot (GET /api/admin/data).
func (s *Service) Data(ctx context.Context) (model.AdminKPIs, error) {
	return s.repo.KPIs(ctx, s.now())
}

// CopyEvents returns the audit stream (GET /api/admin/copy-events?since=&limit=).
func (s *Service) CopyEvents(ctx context.Context, since string, limit int) ([]model.CopyEvent, error) {
	var sp *time.Time
	if since != "" {
		t, err := time.Parse(time.RFC3339Nano, since)
		if err != nil {
			return nil, invalid("since must be an RFC3339 timestamp.")
		}
		sp = &t
	}
	if limit == 0 {
		limit = DefaultEventLimit
	}
	if limit < 1 || limit > MaxEventLimit {
		return nil, invalid("limit must be between 1 and %d.", MaxEventLimit)
	}
	return s.repo.CopyEvents(ctx, sp, limit)
}

// ---- books -----------------------------------------------------------------

// BookListRequest is GET /api/admin/books input.
type BookListRequest struct {
	Search, Shelf, Sort string
	Page, PerPage       int
}

// BookPage is the GET /api/admin/books response.
type BookPage struct {
	Items []model.AdminBook `json:"items"`
	Meta  model.PageMeta    `json:"meta"`
}

func parseSort(sort string) (string, bool, error) {
	if sort == "" {
		return "updated_at", true, nil
	}
	col, dir, found := strings.Cut(sort, ":")
	if !sortColumns[col] || (found && dir != "asc" && dir != "desc") {
		return "", false, invalid("sort must be <updated_at|created_at|title|published_year>:<asc|desc>.")
	}
	return col, dir == "desc", nil
}

// ListBooks pages the admin book list. shelf accepts a shelf id or a path token.
func (s *Service) ListBooks(ctx context.Context, req BookListRequest) (BookPage, error) {
	page, perPage, err := pageArgs(req.Page, req.PerPage)
	if err != nil {
		return BookPage{}, err
	}
	col, desc, err := parseSort(req.Sort)
	if err != nil {
		return BookPage{}, err
	}
	f := model.AdminBookFilter{Search: req.Search, SortCol: col, Desc: desc}
	if req.Shelf != "" {
		id, err := uuid.Parse(req.Shelf)
		if err != nil {
			if id, err = building.ResolvePath(ctx, s.paths, req.Shelf); err != nil {
				return BookPage{}, err
			}
		}
		f.ShelfID = &id
	}
	rows, total, err := s.repo.ListBooks(ctx, f, page, perPage)
	if err != nil {
		return BookPage{}, err
	}
	items := make([]model.AdminBook, 0, len(rows))
	for _, r := range rows {
		d, err := s.details.Detail(ctx, r.ID.String(), uuid.Nil)
		if err != nil {
			return BookPage{}, err
		}
		items = append(items, model.AdminBookFromDetail(d, r.Meta.OpenLibraryKey, r.Meta.UpdatedAt))
	}
	return BookPage{Items: items, Meta: meta(page, perPage, total)}, nil
}

// Book loads one book in the admin schema.
func (s *Service) Book(ctx context.Context, id uuid.UUID) (model.AdminBook, error) {
	m, err := s.repo.BookMeta(ctx, id)
	if err != nil {
		return model.AdminBook{}, err
	}
	d, err := s.details.Detail(ctx, id.String(), uuid.Nil)
	if err != nil {
		return model.AdminBook{}, err
	}
	return model.AdminBookFromDetail(d, m.OpenLibraryKey, m.UpdatedAt), nil
}

// CopyInput is one requested copy (POST /api/admin/books copies[], POST /api/admin/copies).
type CopyInput struct {
	Barcode   string
	Status    string
	Condition string
	ShelfPath string
}

// CreateBookRequest is POST /api/admin/books input; Section is a section code.
type CreateBookRequest struct {
	Fields  model.BookFields
	Section string
	Copies  []CopyInput
}

func trimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}

func cleanAuthors(in []string) []string {
	out := []string{}
	for _, a := range in {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

func checkISBN(isbn *string) error {
	if isbn != nil && !checkdigit.ValidISBN13(*isbn) {
		return model.New(model.CodeInvalidISBN, "ISBN-13 checksum is invalid.")
	}
	return nil
}

func checkYear(y *int) error {
	if y != nil && (*y < 0 || *y > maxYear) {
		return invalid("published_year must be between 0 and %d.", maxYear)
	}
	return nil
}

func (s *Service) isbnConflict(ctx context.Context, isbn string, self uuid.UUID) error {
	id, found, err := s.repo.BookIDByISBN(ctx, isbn)
	if err != nil {
		return err
	}
	if found && id != self {
		return model.New(model.CodeISBNConflict, "A book with this ISBN already exists.").
			WithExtra(map[string]any{"book_id": id})
	}
	return nil
}

func (s *Service) newCopy(ctx context.Context, in CopyInput) (model.NewCopy, error) {
	c := model.NewCopy{
		Barcode:   strings.ToUpper(strings.TrimSpace(in.Barcode)),
		Status:    strings.TrimSpace(in.Status),
		Condition: strings.TrimSpace(in.Condition),
	}
	if c.Barcode != "" && !barcodeRe.MatchString(c.Barcode) {
		return model.NewCopy{}, invalid("barcode must match ATH-XXXXXX-CNN (e.g. ATH-002941-C01).")
	}
	if c.Status == "" {
		c.Status = model.CopyAvailable
	}
	if !copyStatus[c.Status] {
		return model.NewCopy{}, invalid("status must be available, checked_out, reserved_waitlist or missing_lost.")
	}
	if c.Condition == "" {
		c.Condition = "good"
	}
	if len(c.Condition) > maxConditionLen {
		return model.NewCopy{}, invalid("condition must be at most %d characters.", maxConditionLen)
	}
	if p := strings.TrimSpace(in.ShelfPath); p != "" {
		id, err := building.ResolvePath(ctx, s.paths, p)
		if err != nil {
			return model.NewCopy{}, err
		}
		c.ShelfID = &id
	}
	return c, nil
}

// CreateBook creates a book with its copies (POST /api/admin/books → 201 full book).
func (s *Service) CreateBook(ctx context.Context, actor uuid.UUID, req CreateBookRequest) (model.AdminBook, error) {
	f := req.Fields
	f.Title = strings.TrimSpace(f.Title)
	if f.Title == "" {
		return model.AdminBook{}, invalid("title is required.")
	}
	f.Authors = cleanAuthors(f.Authors)
	for _, p := range []**string{&f.Subtitle, &f.ISBN13, &f.ISBN10, &f.Publisher, &f.Description, &f.CoverURL,
		&f.OpenLibraryKey, &f.DeweyNum, &f.LOCNum} {
		*p = trimPtr(*p)
	}
	if err := checkISBN(f.ISBN13); err != nil {
		return model.AdminBook{}, err
	}
	if err := checkYear(f.PublishedYear); err != nil {
		return model.AdminBook{}, err
	}
	if len(req.Copies) > MaxCopiesPerCreate {
		return model.AdminBook{}, invalid("at most %d copies per request.", MaxCopiesPerCreate)
	}
	copies := make([]model.NewCopy, 0, len(req.Copies))
	seen := map[string]bool{}
	for _, in := range req.Copies {
		c, err := s.newCopy(ctx, in)
		if err != nil {
			return model.AdminBook{}, err
		}
		if c.Barcode != "" {
			if seen[c.Barcode] {
				return model.AdminBook{}, invalid("duplicate barcode %s in request.", c.Barcode)
			}
			seen[c.Barcode] = true
		}
		copies = append(copies, c)
	}
	if f.ISBN13 != nil {
		if err := s.isbnConflict(ctx, *f.ISBN13, uuid.Nil); err != nil {
			return model.AdminBook{}, err
		}
	}
	if code := strings.TrimSpace(req.Section); code != "" {
		id, err := s.repo.SectionByCode(ctx, code)
		if err != nil {
			return model.AdminBook{}, err
		}
		f.SectionID = &id
	}
	id, err := s.repo.CreateBook(ctx, actor, f, copies)
	if err != nil {
		return model.AdminBook{}, err
	}
	s.notifyIndex()
	return s.Book(ctx, id)
}

// BookPatchRequest is PATCH /api/admin/books/{id} input; Section is a code (null clears).
type BookPatchRequest struct {
	Patch   model.BookPatch
	Section model.Optional[string]
}

func trimOpt(o *model.Optional[string]) {
	if o.Set && !o.Null {
		o.Value = strings.TrimSpace(o.Value)
		if o.Value == "" {
			o.Null = true
		}
	}
}

// PatchBook partially updates a book and returns it.
func (s *Service) PatchBook(ctx context.Context, id string, req BookPatchRequest) (model.AdminBook, error) {
	bid, err := parseID(id, "Book")
	if err != nil {
		return model.AdminBook{}, err
	}
	p := req.Patch
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		if t == "" {
			return model.AdminBook{}, invalid("title must not be empty.")
		}
		p.Title = &t
	}
	if p.Authors != nil {
		a := cleanAuthors(*p.Authors)
		p.Authors = &a
	}
	for _, o := range []*model.Optional[string]{&p.Subtitle, &p.ISBN13, &p.ISBN10, &p.Publisher, &p.Description,
		&p.CoverURL, &p.OpenLibraryKey, &p.DeweyNum, &p.LOCNum} {
		trimOpt(o)
	}
	if p.ISBN13.Set && !p.ISBN13.Null {
		if err := checkISBN(&p.ISBN13.Value); err != nil {
			return model.AdminBook{}, err
		}
		if err := s.isbnConflict(ctx, p.ISBN13.Value, bid); err != nil {
			return model.AdminBook{}, err
		}
	}
	if p.PublishedYear.Set && !p.PublishedYear.Null {
		if err := checkYear(&p.PublishedYear.Value); err != nil {
			return model.AdminBook{}, err
		}
	}
	sec := req.Section
	trimOpt(&sec)
	if sec.Set {
		if sec.Null {
			p.SectionID = model.Optional[uuid.UUID]{Set: true, Null: true}
		} else {
			sid, err := s.repo.SectionByCode(ctx, sec.Value)
			if err != nil {
				return model.AdminBook{}, err
			}
			p.SectionID = model.Optional[uuid.UUID]{Set: true, Value: sid}
		}
	}
	if err := s.repo.PatchBook(ctx, bid, p); err != nil {
		return model.AdminBook{}, err
	}
	if p.TouchesRAG() {
		s.notifyIndex()
	}
	return s.Book(ctx, bid)
}

// DeleteBook removes a book (cascades per docs/02 FKs).
func (s *Service) DeleteBook(ctx context.Context, id string) error {
	bid, err := parseID(id, "Book")
	if err != nil {
		return err
	}
	return s.repo.DeleteBook(ctx, bid)
}

// ---- copies ----------------------------------------------------------------

func withLocation(c model.AdminCopy, loc *model.LocationRow) model.AdminCopy {
	if loc != nil {
		c.Location = &model.CopyLocation{Path: loc.Path, Ladder: catalog.Ladder(*loc), Parts: catalog.Parts(*loc)}
	}
	return c
}

// AddCopyRequest is POST /api/admin/copies input.
type AddCopyRequest struct {
	BookID string
	Copy   CopyInput
}

// AddCopy registers a copy; the barcode is allocated when omitted (docs/08 §5).
func (s *Service) AddCopy(ctx context.Context, actor uuid.UUID, req AddCopyRequest) (model.AdminCopy, error) {
	bid, err := parseID(req.BookID, "Book")
	if err != nil {
		return model.AdminCopy{}, err
	}
	c, err := s.newCopy(ctx, req.Copy)
	if err != nil {
		return model.AdminCopy{}, err
	}
	cp, loc, err := s.repo.AddCopy(ctx, actor, bid, c)
	if err != nil {
		return model.AdminCopy{}, err
	}
	return withLocation(cp, loc), nil
}

// copyRef resolves a copy id, or a registered barcode (shelf inspector quick-assign).
func (s *Service) copyRef(ctx context.Context, ref string) (uuid.UUID, error) {
	if b := strings.ToUpper(strings.TrimSpace(ref)); barcodeRe.MatchString(b) {
		return s.repo.CopyIDByBarcode(ctx, b)
	}
	return parseID(ref, "Copy")
}

// PatchCopy mutates status/condition/shelf; the barcode is immutable. id may be the
// copy id or its barcode.
func (s *Service) PatchCopy(ctx context.Context, actor uuid.UUID, id string, p model.CopyPatch) (model.AdminCopy, error) {
	cid, err := s.copyRef(ctx, id)
	if err != nil {
		return model.AdminCopy{}, err
	}
	if p.Status != nil {
		st := strings.TrimSpace(*p.Status)
		if !copyStatus[st] {
			return model.AdminCopy{}, invalid("status must be available, checked_out, reserved_waitlist or missing_lost.")
		}
		p.Status = &st
	}
	if p.Condition != nil {
		cond := strings.TrimSpace(*p.Condition)
		if cond == "" || len(cond) > maxConditionLen {
			return model.AdminCopy{}, invalid("condition must be 1–%d characters.", maxConditionLen)
		}
		p.Condition = &cond
	}
	cp, loc, err := s.repo.PatchCopy(ctx, actor, cid, p, s.now())
	if err != nil {
		return model.AdminCopy{}, err
	}
	return withLocation(cp, loc), nil
}

// DeleteCopy hard-deletes a copy (id or barcode).
func (s *Service) DeleteCopy(ctx context.Context, id string) error {
	cid, err := s.copyRef(ctx, id)
	if err != nil {
		return err
	}
	return s.repo.DeleteCopy(ctx, cid)
}

// ---- building ----------------------------------------------------------------

// Tree returns the building hierarchy (GET /api/admin/tree).
func (s *Service) Tree(ctx context.Context) (model.TreeBuilding, error) { return s.repo.Tree(ctx) }

// Shelf returns the shelf manifest inspector (GET /api/admin/shelves/{id}).
func (s *Service) Shelf(ctx context.Context, id string) (model.ShelfInspector, error) {
	sid, err := parseID(id, "Shelf")
	if err != nil {
		return model.ShelfInspector{}, err
	}
	return s.repo.Shelf(ctx, sid)
}

// ---- users -------------------------------------------------------------------

// UserPage is the GET /api/admin/users response.
type UserPage struct {
	Items []model.AdminUser `json:"items"`
	Meta  model.PageMeta    `json:"meta"`
}

// Users pages accounts with joined stats.
func (s *Service) Users(ctx context.Context, page, perPage int) (UserPage, error) {
	page, perPage, err := pageArgs(page, perPage)
	if err != nil {
		return UserPage{}, err
	}
	items, total, err := s.repo.Users(ctx, page, perPage)
	if err != nil {
		return UserPage{}, err
	}
	return UserPage{Items: items, Meta: meta(page, perPage, total)}, nil
}

// SetRole changes a user's role; admins cannot demote themselves (400, docs/03 §8.5).
func (s *Service) SetRole(ctx context.Context, actor uuid.UUID, id, role string) (model.User, error) {
	uid, err := parseID(id, "User")
	if err != nil {
		return model.User{}, err
	}
	if role != model.RoleAdmin && role != model.RoleUser {
		return model.User{}, invalid("role must be admin or user.")
	}
	if uid == actor && role != model.RoleAdmin {
		return model.User{}, invalid("You cannot demote yourself.")
	}
	return s.repo.SetRole(ctx, uid, role)
}
