package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
)

// ToolTimeout bounds each tool invocation (docs/06 §3).
const ToolTimeout = 5 * time.Second

// toolTimeout is ToolTimeout, overridable in tests.
var toolTimeout = ToolTimeout

// Catalog is the slice of the catalog service the tools use (no duplicate SQL, docs/06 §4).
type Catalog interface {
	Search(ctx context.Context, req catalog.Request) (model.SearchPage, error)
	Detail(ctx context.Context, id string, userID uuid.UUID) (model.BookDetail, error)
}

// Tool names (docs/06 §4).
const (
	ToolSearchCatalog     = "search_catalog"
	ToolGetBookLocation   = "get_book_location"
	ToolCheckAvailability = "check_availability"
	ToolRecommendSimilar  = "recommend_similar"
)

// JSON-schema args verbatim from docs/06 §4.
var toolSchemas = map[string]string{
	ToolSearchCatalog: `{"type":"object","properties":{"query":{"type":"string"},
		"mode":{"type":"string","enum":["keyword","semantic","hybrid"]},
		"top_k":{"type":"integer","minimum":1,"maximum":10}},"required":["query"]}`,
	ToolGetBookLocation:   `{"type":"object","properties":{"book_id":{"type":"string","format":"uuid"}},"required":["book_id"]}`,
	ToolCheckAvailability: `{"type":"object","properties":{"book_id":{"type":"string","format":"uuid"}},"required":["book_id"]}`,
	ToolRecommendSimilar: `{"type":"object","properties":{"book_id":{"type":"string","format":"uuid"},
		"top_k":{"type":"integer","minimum":1,"maximum":5}},"required":["book_id"]}`,
}

var toolDescs = map[string]string{
	ToolSearchCatalog:     "Search the Athenaeum catalog. Returns matching books (id, title, authors, availability state, shelf path). Use English queries.",
	ToolGetBookLocation:   "Get the shelf location of a catalog book: path, wayfinding ladder and section.",
	ToolCheckAvailability: "Check a catalog book's availability state and per-copy statuses.",
	ToolRecommendSimilar:  "Recommend books shelved near a catalog book (shelf/section neighbors) with deep links.",
}

func toolInfo(name string) (*schema.ToolInfo, error) {
	var s jsonschema.Schema
	if err := json.Unmarshal([]byte(toolSchemas[name]), &s); err != nil {
		return nil, fmt.Errorf("tool %s schema: %w", name, err)
	}
	return &schema.ToolInfo{Name: name, Desc: toolDescs[name], ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&s)}, nil
}

// --- tool inputs/outputs ---

type searchIn struct {
	Query string `json:"query"`
	Mode  string `json:"mode"`
	TopK  int    `json:"top_k"`
}

type bookIn struct {
	BookID string `json:"book_id"`
	TopK   int    `json:"top_k"`
}

type searchBook struct {
	ID           uuid.UUID         `json:"id"`
	Title        string            `json:"title"`
	Authors      []string          `json:"authors"`
	Availability map[string]string `json:"availability"`
	Path         *string           `json:"path"`
}

type searchOut struct {
	Books []searchBook `json:"books"`
}

type locationOut struct {
	BookID      uuid.UUID         `json:"book_id"`
	Title       string            `json:"title"`
	Path        *string           `json:"path"`
	Ladder      string            `json:"ladder,omitempty"`
	LadderSteps []string          `json:"ladder_steps,omitempty"`
	Section     *model.SectionRef `json:"section"`
}

type copyOut struct {
	Barcode string  `json:"barcode"`
	Status  string  `json:"status"`
	Path    *string `json:"path"`
}

type availabilityOut struct {
	BookID          uuid.UUID `json:"book_id"`
	Title           string    `json:"title"`
	State           string    `json:"state"`
	TotalCopies     int       `json:"total_copies"`
	AvailableCopies int       `json:"available_copies"`
	Copies          []copyOut `json:"copies"`
}

type recommendation struct {
	ID      uuid.UUID `json:"id"`
	Title   string    `json:"title"`
	Authors []string  `json:"authors"`
	Link    string    `json:"link"`
}

type recommendOut struct {
	BookID          uuid.UUID        `json:"book_id"`
	Recommendations []recommendation `json:"recommendations"`
}

// toolSet builds the four tools over the catalog service.
type toolSet struct{ cat Catalog }

func (ts toolSet) search(ctx context.Context, in searchIn) (searchOut, error) {
	if in.Mode == "" {
		in.Mode = model.ModeHybrid
	}
	if in.TopK < 1 || in.TopK > 10 {
		in.TopK = 5
	}
	req := catalog.Request{Query: in.Query, Mode: in.Mode, Page: 1, PerPage: in.TopK}
	page, err := ts.cat.Search(ctx, req)
	if err != nil && in.Mode != model.ModeKeyword && errors.Is(err, model.ErrEmbeddingUnavail) {
		req.Mode = model.ModeKeyword // vectors unavailable: degrade to keyword (docs/06 §5)
		page, err = ts.cat.Search(ctx, req)
	}
	if err != nil {
		return searchOut{}, err
	}
	out := searchOut{Books: []searchBook{}}
	st := turnFrom(ctx)
	for _, b := range page.Items {
		var path *string
		if b.Location != nil {
			p := b.Location.Path
			path = &p
		}
		out.Books = append(out.Books, searchBook{ID: b.ID, Title: b.Title, Authors: b.Authors,
			Availability: map[string]string{"state": b.Availability.State}, Path: path})
		st.addHit(Citation{BookID: b.ID, Title: b.Title, Path: path, Score: b.Score, CoverURL: b.CoverURL})
	}
	return out, nil
}

// homeLocation returns the wayfinding path and the matching copy's English ladder.
func homeLocation(d model.BookDetail) (*string, string, []string) {
	if d.Wayfinding == nil {
		return nil, "", nil
	}
	p := d.Wayfinding.Path
	ladder := ""
	for _, c := range d.Copies {
		if c.Location != nil && c.Location.Path == p {
			ladder = c.Location.Ladder
			break
		}
	}
	return &p, ladder, d.Wayfinding.LadderSteps
}

func (ts toolSet) location(ctx context.Context, in bookIn) (locationOut, error) {
	st := turnFrom(ctx)
	d, err := ts.cat.Detail(ctx, in.BookID, st.userID)
	if err != nil {
		return locationOut{}, err
	}
	path, ladder, steps := homeLocation(d)
	st.addHit(Citation{BookID: d.ID, Title: d.Title, Path: path, CoverURL: d.CoverURL})
	return locationOut{BookID: d.ID, Title: d.Title, Path: path, Ladder: ladder, LadderSteps: steps, Section: d.Section}, nil
}

func (ts toolSet) availability(ctx context.Context, in bookIn) (availabilityOut, error) {
	st := turnFrom(ctx)
	d, err := ts.cat.Detail(ctx, in.BookID, st.userID)
	if err != nil {
		return availabilityOut{}, err
	}
	out := availabilityOut{BookID: d.ID, Title: d.Title, State: d.Availability.State,
		TotalCopies: d.Availability.TotalCopies, AvailableCopies: d.Availability.AvailableCopies, Copies: []copyOut{}}
	for _, c := range d.Copies {
		co := copyOut{Barcode: c.Barcode, Status: c.Status}
		if c.Location != nil {
			p := c.Location.Path
			co.Path = &p
		}
		out.Copies = append(out.Copies, co)
	}
	path, _, _ := homeLocation(d)
	st.addHit(Citation{BookID: d.ID, Title: d.Title, Path: path, CoverURL: d.CoverURL})
	return out, nil
}

func (ts toolSet) recommend(ctx context.Context, in bookIn) (recommendOut, error) {
	st := turnFrom(ctx)
	if in.TopK < 1 || in.TopK > 5 {
		in.TopK = 3
	}
	d, err := ts.cat.Detail(ctx, in.BookID, st.userID)
	if err != nil {
		return recommendOut{}, err
	}
	out := recommendOut{BookID: d.ID, Recommendations: []recommendation{}}
	for _, n := range d.Neighbors {
		if len(out.Recommendations) == in.TopK {
			break
		}
		out.Recommendations = append(out.Recommendations, recommendation{ID: n.ID, Title: n.Title,
			Authors: n.Authors, Link: "/books/" + n.ID.String()})
		st.addHit(Citation{BookID: n.ID, Title: n.Title, CoverURL: n.CoverURL})
	}
	return out, nil
}

// summaries render the short tool_result text (docs/06 §6).
func summarize(name, result string) string {
	switch name {
	case ToolSearchCatalog:
		var o searchOut
		_ = json.Unmarshal([]byte(result), &o)
		if len(o.Books) == 1 {
			return "1 book found"
		}
		return fmt.Sprintf("%d books found", len(o.Books))
	case ToolGetBookLocation:
		var o locationOut
		_ = json.Unmarshal([]byte(result), &o)
		if o.Path == nil {
			return "no shelf location"
		}
		return *o.Path
	case ToolCheckAvailability:
		var o availabilityOut
		_ = json.Unmarshal([]byte(result), &o)
		return fmt.Sprintf("%d of %d copies available", o.AvailableCopies, o.TotalCopies)
	case ToolRecommendSimilar:
		var o recommendOut
		_ = json.Unmarshal([]byte(result), &o)
		return fmt.Sprintf("%d similar books", len(o.Recommendations))
	}
	return "done"
}

// guardedTool wraps an Eino tool: emits tool_call/tool_result, enforces the 5 s budget
// and turns failures into observations so the agent can retry or answer without
// (docs/06 §3).
type guardedTool struct {
	inner   tool.InvokableTool
	name    string
	timeout time.Duration
}

func (g *guardedTool) Info(ctx context.Context) (*schema.ToolInfo, error) { return g.inner.Info(ctx) }

func (g *guardedTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	st := turnFrom(ctx)
	id := compose.GetToolCallID(ctx)
	rawArgs := json.RawMessage(args)
	if !json.Valid(rawArgs) {
		rawArgs, _ = json.Marshal(args)
	}
	st.emit(Event{Name: EventToolCall, Data: ToolCallData{ID: id, Name: g.name, Args: rawArgs}})

	tctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	out, err := g.inner.InvokableRun(tctx, args, opts...)
	var summary string
	if err != nil {
		msg := "tool failed"
		if de, ok := model.AsError(err); ok {
			msg = de.Message
		} else if errors.Is(tctx.Err(), context.DeadlineExceeded) {
			msg = "tool timed out"
		}
		b, _ := json.Marshal(map[string]string{"error": msg})
		out = string(b)
		summary = "error: " + msg
	} else {
		summary = summarize(g.name, out)
	}
	st.emit(Event{Name: EventToolResult, Data: ToolResultData{ID: id, Summary: summary}})
	st.addRecord(model.ToolMeta{ToolCallID: id, Name: g.name, Args: rawArgs, Result: out})
	return out, nil
}

// newTools registers the four tools through utils.NewTool and wraps them.
func newTools(cat Catalog, timeout time.Duration) ([]tool.BaseTool, error) {
	ts := toolSet{cat: cat}
	infos := map[string]*schema.ToolInfo{}
	for name := range toolSchemas {
		info, err := toolInfo(name)
		if err != nil {
			return nil, err
		}
		infos[name] = info
	}
	raw := []struct {
		name string
		t    tool.InvokableTool
	}{
		{ToolSearchCatalog, utils.NewTool(infos[ToolSearchCatalog], ts.search)},
		{ToolGetBookLocation, utils.NewTool(infos[ToolGetBookLocation], ts.location)},
		{ToolCheckAvailability, utils.NewTool(infos[ToolCheckAvailability], ts.availability)},
		{ToolRecommendSimilar, utils.NewTool(infos[ToolRecommendSimilar], ts.recommend)},
	}
	out := make([]tool.BaseTool, 0, len(raw))
	for _, r := range raw {
		out = append(out, &guardedTool{inner: r.t, name: r.name, timeout: timeout})
	}
	return out, nil
}
