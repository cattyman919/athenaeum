package model

// Cover is a cached cover image (docs/02 §book_covers), keyed by its upstream URL.
type Cover struct {
	SourceURL   string
	ContentType string
	Bytes       []byte
	SHA256      string // hex digest; ETag and ?v= cache-buster
}
