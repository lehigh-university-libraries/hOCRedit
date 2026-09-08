// Package pdfexport defines the bounded image/hOCR bundle consumed by the PDF
// runtime. The HTTP API alone selects the canonical revisions and source images.
package pdfexport

import "fmt"

const (
	// MaxInputBytes bounds the complete uncompressed tar request.
	MaxInputBytes int64 = 128 << 20
	// MaxOutputBytes bounds the generated PDF.
	MaxOutputBytes int64 = 128 << 20
	// MaxPages bounds file count independently of byte size.
	MaxPages = 1000
	// MaxHOCRBytes bounds one canonical hOCR page.
	MaxHOCRBytes int64 = 32 << 20
	// MaxPixels bounds the total decoded image work in a request.
	MaxPixels int64 = 250_000_000
)

// PageName orders pages lexically without accepting caller-selected paths.
func PageName(sequence int, extension string) string {
	return fmt.Sprintf("page-%06d.%s", sequence, extension)
}
