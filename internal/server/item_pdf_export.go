package server

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/lehigh-university-libraries/scribe/internal/pdfexport"
	"github.com/lehigh-university-libraries/scribe/internal/servicehttp"
	"github.com/lehigh-university-libraries/scribe/internal/uploadlimits"
)

// stagePDFItemExport sends only server-rendered hOCR and bounded JPEG bytes to
// the configured converter. The converter receives no image URLs or credentials.
func (h *Handler) stagePDFItemExport(ctx context.Context, plan canonicalItemExportPlan) (staged *stagedCanonicalItemExport, cleanup func(), err error) {
	if h.pdfExportURL == "" {
		return nil, nil, fmt.Errorf("PDF export service is not configured")
	}
	input, err := newItemExportFile()
	if err != nil {
		return nil, nil, err
	}
	defer input.Close()
	archive := tar.NewWriter(&boundedExportWriter{destination: input, maximum: pdfexport.MaxInputBytes})
	var pixels int64
	for index, page := range plan.Pages {
		width, height := int64(page.Image.Width), int64(page.Image.Height)
		if width <= 0 || height <= 0 || width > uploadlimits.MaxImageDimension || height > uploadlimits.MaxImageDimension || width*height > uploadlimits.MaxImagePixels {
			return nil, nil, errItemExportInvalid
		}
		pixels += width * height
		if pixels > pdfexport.MaxPixels {
			return nil, nil, errItemExportOutputLimit
		}
		if err := h.writePDFExportPage(ctx, archive, page, index+1); err != nil {
			return nil, nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, nil, err
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.pdfExportURL, input)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid PDF export endpoint")
	}
	req.Header.Set("Content-Type", "application/x-tar")
	response, err := servicehttp.NewClient(maxPreparedExportDuration).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, fmt.Errorf("PDF export service unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("PDF export service returned status %d", response.StatusCode)
	}
	output, err := newItemExportFile()
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			_ = output.Close()
		}
	}()
	bounded := &boundedExportWriter{destination: output, maximum: pdfexport.MaxOutputBytes}
	size, err := copyExportContent(ctx, bounded, response.Body)
	if err != nil {
		return nil, nil, err
	}
	var header [5]byte
	var trailer [16]byte
	if size < int64(len(trailer)) {
		return nil, nil, errItemExportInvalid
	}
	if _, err := output.ReadAt(header[:], 0); err != nil {
		return nil, nil, err
	}
	if _, err := output.ReadAt(trailer[:], size-int64(len(trailer))); err != nil {
		return nil, nil, err
	}
	if string(header[:]) != "%PDF-" || !bytes.HasSuffix(bytes.TrimSpace(trailer[:]), []byte("%%EOF")) {
		return nil, nil, errItemExportInvalid
	}
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		return nil, nil, err
	}
	return &stagedCanonicalItemExport{File: output, Size: size}, func() { _ = output.Close() }, nil
}

func (h *Handler) writePDFExportPage(ctx context.Context, archive *tar.Writer, page canonicalExportPage, sequence int) error {
	hocr, _, _, err := renderCanonicalExportPage(page, "hocr")
	if err != nil {
		return err
	}
	fetch := h.imageRegionFetcher
	if fetch == nil {
		fetch = fetchImageRegionToTemp
	}
	path, cleanup, err := fetch(ctx, page.Image.ImageURL, 0, 0, int(page.Image.Width), int(page.Image.Height))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("PDF source image could not be loaded")
	}
	defer cleanup()
	file, err := os.Open(path) // #nosec G304 -- fetch creates this private temporary file; request input never supplies the path.
	if err != nil {
		return errItemExportStaging
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxRemoteImageBytes {
		return errItemExportInvalid
	}
	image, err := jpeg.DecodeConfig(file)
	if err != nil || int64(image.Width) != int64(page.Image.Width) || int64(image.Height) != int64(page.Image.Height) {
		return errItemExportInvalid
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := archive.WriteHeader(&tar.Header{Name: pdfexport.PageName(sequence, "hocr"), Mode: 0600, Size: int64(len(hocr)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := copyExportContent(ctx, archive, strings.NewReader(hocr)); err != nil {
		return err
	}
	if err := archive.WriteHeader(&tar.Header{Name: pdfexport.PageName(sequence, "jpg"), Mode: 0600, Size: info.Size(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err = copyExportContent(ctx, archive, file)
	return err
}
