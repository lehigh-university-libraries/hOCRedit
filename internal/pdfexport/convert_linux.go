package pdfexport

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lehigh-university-libraries/scribe/internal/uploadlimits"
)

// Convert runs the packaged renderer without recognition, then validates page
// count before emitting any PDF bytes. Its working directory lives on tmpfs.
func Convert(ctx context.Context, input io.Reader, output io.Writer) error {
	if err := removeExpiredWorkspaces(); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "scribe-pdf-")
	if err != nil {
		return fmt.Errorf("create PDF workspace")
	}
	defer os.RemoveAll(dir)
	pages, err := extractBundle(input, dir)
	if err != nil {
		return err
	}
	if err := runConverterCommand(ctx, dir, io.Discard, exec.CommandContext(ctx, "/opt/pdf/bin/hocr-pdf", "--savefile", "output.pdf", ".")); err != nil {
		return err
	}
	file, err := os.OpenInRoot(dir, "output.pdf")
	if err != nil {
		return fmt.Errorf("PDF output missing")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > MaxOutputBytes {
		return fmt.Errorf("PDF output exceeds limits")
	}
	var metadata bytes.Buffer
	if err := runConverterCommand(ctx, dir, &metadata, exec.CommandContext(ctx, "/usr/bin/pdfinfo", "output.pdf")); err != nil {
		return err
	}
	validPageCount := false
	for line := range strings.SplitSeq(metadata.String(), "\n") {
		if strings.HasPrefix(line, "Pages:") {
			count, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Pages:")))
			validPageCount = err == nil && count == pages
		}
	}
	if !validPageCount {
		return fmt.Errorf("PDF page count differs from input")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = io.Copy(output, file)
	return err
}

func extractBundle(input io.Reader, dir string) (int, error) {
	limited := &io.LimitedReader{R: input, N: MaxInputBytes + 1}
	archive := tar.NewReader(limited)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	var pixels int64
	for sequence := 1; sequence <= MaxPages+1; sequence++ {
		for _, extension := range []string{"hocr", "jpg"} {
			header, err := archive.Next()
			if err == io.EOF && extension == "hocr" && sequence > 1 {
				_, err = io.Copy(io.Discard, limited)
				if err != nil || limited.N <= 0 {
					return 0, fmt.Errorf("PDF input exceeds byte limit")
				}
				return sequence - 1, nil
			}
			maximum := MaxHOCRBytes
			if extension == "jpg" {
				maximum = uploadlimits.MaxImageBytes
			}
			name := PageName(sequence, extension)
			if err != nil || sequence > MaxPages || header.Name != name || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maximum || limited.N <= header.Size {
				return 0, fmt.Errorf("invalid PDF image/hOCR bundle")
			}
			file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				return 0, fmt.Errorf("create PDF input file")
			}
			pagePixels, err := extractEntry(archive, file, extension)
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				return 0, fmt.Errorf("invalid PDF input file")
			}
			pixels += pagePixels
			if pixels > MaxPixels {
				return 0, fmt.Errorf("PDF input exceeds pixel limit")
			}
		}
	}
	return 0, fmt.Errorf("PDF input exceeds page limit")
}

func extractEntry(source io.Reader, file *os.File, extension string) (int64, error) {
	if _, err := io.Copy(file, source); err != nil {
		return 0, err
	}
	if extension != "jpg" {
		return 0, nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	image, err := jpeg.DecodeConfig(file)
	if err != nil {
		return 0, err
	}
	if err := uploadlimits.ValidateImageDimensions(image.Width, image.Height); err != nil {
		return 0, err
	}
	return int64(image.Width) * int64(image.Height), nil
}

func runConverterCommand(ctx context.Context, dir string, output io.Writer, cmd *exec.Cmd) error {
	cmd.Dir = dir
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	// Scyllaridae cancels the wrapper on client disconnect. Kill the renderer
	// too, including if the wrapper is killed before it can run its defers.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("PDF converter command failed")
	}
	return nil
}

func removeExpiredWorkspaces() error {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return fmt.Errorf("read PDF workspace directory")
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "scribe-pdf-") {
			continue
		}
		info, err := entry.Info()
		// Every converter command has an 85-second deadline. Only abandoned
		// directories from killed wrappers can be older than two minutes.
		if err == nil && time.Since(info.ModTime()) > 2*time.Minute {
			if err := os.RemoveAll(filepath.Join(os.TempDir(), entry.Name())); err != nil {
				return fmt.Errorf("clean abandoned PDF workspace")
			}
		}
	}
	return nil
}
