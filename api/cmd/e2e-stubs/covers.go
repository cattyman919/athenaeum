package main

import (
	"bytes"
	"hash/fnv"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
)

// coverImage mirrors covers.openlibrary.org GET /b/{isbn|id|olid}/{key}-{S|M|L}.jpg for
// the API's cover cache (docs/08 §6a); point OPENLIBRARY_COVERS_BASE_URL at the stubs.
// Every key gets a deterministic 200×300 gradient JPEG, so seeded books always have a
// cover offline.
func coverImage(w http.ResponseWriter, r *http.Request) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(r.PathValue("kind") + "/" + r.PathValue("key")))
	seed := h.Sum32()
	img := image.NewRGBA(image.Rect(0, 0, 200, 300))
	for y := range 300 {
		for x := range 200 {
			img.Set(x, y, color.RGBA{
				R: uint8(seed) + uint8(x/2), G: uint8(seed>>8) + uint8(y/3), B: uint8(seed>>16) + uint8((x+y)/4), A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	_, _ = w.Write(buf.Bytes())
}
