package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cattyman919/athenaeum/api/internal/model"
)

// Detail loads a book with its copies, shelf locations, neighbors and the caller's
// library entry. A missing book yields NOT_FOUND.
func (r *CatalogRepository) Detail(ctx context.Context, id uuid.UUID, userID uuid.UUID) (model.BookDetailData, error) {
	var d model.BookDetail
	var authors []byte
	var secCode, secName, secColor *string
	err := r.pool.QueryRow(ctx, `
		SELECT b.id, b.title, b.subtitle, b.authors, b.isbn13, b.isbn10, b.publisher, b.published_year,
		       b.description, b.dewey_num, b.loc_num, `+coverURLSQL("b")+`, se.code, se.name, se.color
		FROM books b LEFT JOIN sections se ON se.id = b.section_id
		WHERE b.id = $1`, id).Scan(&d.ID, &d.Title, &d.Subtitle, &authors, &d.ISBN13, &d.ISBN10, &d.Publisher,
		&d.PublishedYear, &d.Description, &d.DeweyNum, &d.LOCNum, &d.CoverURL, &secCode, &secName, &secColor)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.BookDetailData{}, model.New(model.CodeNotFound, "Book not found.")
	}
	if err != nil {
		return model.BookDetailData{}, fmt.Errorf("book detail: %w", err)
	}
	if err := json.Unmarshal(authors, &d.Authors); err != nil {
		return model.BookDetailData{}, fmt.Errorf("authors: %w", err)
	}
	if d.Authors == nil {
		d.Authors = []string{}
	}
	if secCode != nil {
		d.Section = &model.SectionRef{Code: *secCode, Name: deref(secName), Color: deref(secColor)}
	}

	data := model.BookDetailData{Locations: map[uuid.UUID]model.LocationRow{}}
	d.Copies = []model.CopyDetail{}
	rows, err := r.pool.Query(ctx, `
		SELECT bc.id, bc.barcode, bc.status, bc.condition, bc.last_state_change,
		       bl.path, bl.building_name, bl.floor_number, bl.floor_name, bl.section_name,
		       bl.aisle_code, bl.shelf_code, bl.row_level, bl.height_tier, bl.building_code, bl.section_code
		FROM book_copies bc LEFT JOIN book_locations bl ON bl.copy_id = bc.id
		WHERE bc.book_id = $1 ORDER BY bc.barcode`, id)
	if err != nil {
		return model.BookDetailData{}, fmt.Errorf("copies: %w", err)
	}
	defer rows.Close()
	statuses := map[string]bool{}
	for rows.Next() {
		var c model.CopyDetail
		var path, bName, fName, sName, aisle, shelf, tier, bCode, sCode *string
		var floorN, rowLevel *int
		if err := rows.Scan(&c.ID, &c.Barcode, &c.Status, &c.Condition, &c.LastStateChange,
			&path, &bName, &floorN, &fName, &sName, &aisle, &shelf, &rowLevel, &tier, &bCode, &sCode); err != nil {
			return model.BookDetailData{}, err
		}
		d.Availability.TotalCopies++
		if c.Status == "available" {
			d.Availability.AvailableCopies++
		}
		statuses[c.Status] = true
		if path != nil {
			data.Locations[c.ID] = model.LocationRow{
				CopyID: c.ID, Barcode: c.Barcode, BuildingName: deref(bName), FloorNumber: derefInt(floorN),
				FloorName: deref(fName), SectionName: deref(sName), AisleCode: deref(aisle), ShelfCode: deref(shelf),
				RowLevel: derefInt(rowLevel), HeightTier: deref(tier), Path: *path,
				BuildingCode: deref(bCode), SectionCode: deref(sCode),
			}
		}
		d.Copies = append(d.Copies, c)
	}
	if err := rows.Err(); err != nil {
		return model.BookDetailData{}, err
	}
	for s := range statuses {
		data.Statuses = append(data.Statuses, s)
	}

	d.Neighbors, err = r.neighbors(ctx, id)
	if err != nil {
		return model.BookDetailData{}, err
	}

	// Load ISBNs from book_isbns (primary first).
	isbnRows, err := r.pool.Query(ctx,
		`SELECT isbn13, is_primary FROM book_isbns WHERE book_id = $1 ORDER BY is_primary DESC, isbn13`, id)
	if err != nil {
		return model.BookDetailData{}, fmt.Errorf("book_isbns: %w", err)
	}
	data.Isbns = []model.ISBNRef{}
	for isbnRows.Next() {
		var ref model.ISBNRef
		if err := isbnRows.Scan(&ref.ISBN13, &ref.IsPrimary); err != nil {
			isbnRows.Close()
			return model.BookDetailData{}, err
		}
		data.Isbns = append(data.Isbns, ref)
	}
	isbnRows.Close()
	if err := isbnRows.Err(); err != nil {
		return model.BookDetailData{}, err
	}

	if userID != uuid.Nil {
		var e model.LibraryEntryRef
		err := r.pool.QueryRow(ctx, `SELECT status, is_favorite, rating FROM user_library WHERE user_id = $1 AND book_id = $2`,
			userID, id).Scan(&e.Status, &e.IsFavorite, &e.Rating)
		switch {
		case err == nil:
			d.MyLibraryEntry = &e
		case !errors.Is(err, pgx.ErrNoRows):
			return model.BookDetailData{}, fmt.Errorf("library entry: %w", err)
		}
	}
	data.Detail = d
	return data, nil
}

// neighbors: other books with an available copy sharing the shelf, then the section.
func (r *CatalogRepository) neighbors(ctx context.Context, id uuid.UUID) ([]model.Neighbor, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT b2.id, b2.title, b2.authors, `+coverURLSQL("b2")+`
		FROM books b2, books b
		WHERE b.id = $1 AND b2.id <> b.id
		  AND EXISTS (SELECT 1 FROM book_copies c WHERE c.book_id = b2.id AND c.status = 'available')
		  AND (b2.section_id = b.section_id
		       OR EXISTS (SELECT 1 FROM book_copies c2 JOIN book_copies c1 ON c1.shelf_id = c2.shelf_id
		                  WHERE c2.book_id = b2.id AND c1.book_id = b.id AND c1.shelf_id IS NOT NULL))
		ORDER BY (EXISTS (SELECT 1 FROM book_copies c2 JOIN book_copies c1 ON c1.shelf_id = c2.shelf_id
		                  WHERE c2.book_id = b2.id AND c1.book_id = b.id AND c1.shelf_id IS NOT NULL)) DESC,
		         b2.title
		LIMIT 8`, id)
	if err != nil {
		return nil, fmt.Errorf("neighbors: %w", err)
	}
	defer rows.Close()
	out := []model.Neighbor{}
	for rows.Next() {
		var n model.Neighbor
		var authors []byte
		if err := rows.Scan(&n.ID, &n.Title, &authors, &n.CoverURL); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(authors, &n.Authors); err != nil {
			return nil, err
		}
		if n.Authors == nil {
			n.Authors = []string{}
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func derefInt(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}
