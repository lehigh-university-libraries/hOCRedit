package pdfexport

import (
	"archive/tar"
	"bytes"
	"context"
	"embed"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/*.hocr
var fixtures embed.FS

func TestExtractBundleRejectsUnsafeOrIncompleteInput(t *testing.T) {
	for _, test := range []struct {
		name string
		kind byte
		size int64
	}{
		{"../escape.hocr", tar.TypeReg, 1},
		{"/tmp/page-000001.hocr", tar.TypeReg, 1},
		{"page-000001.hocr", tar.TypeSymlink, 0},
		{"page-000001.hocr", tar.TypeReg, MaxHOCRBytes + 1},
		{"page-000002.hocr", tar.TypeReg, 1},
		{"page-000001.hocr", tar.TypeReg, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var input bytes.Buffer
			archive := tar.NewWriter(&input)
			if err := archive.WriteHeader(&tar.Header{Name: test.name, Typeflag: test.kind, Size: test.size, Linkname: "/etc/passwd"}); err != nil {
				t.Fatal(err)
			}
			if test.size == 1 {
				_, _ = archive.Write([]byte("x"))
			}
			_ = archive.Close()
			if _, err := extractBundle(&input, t.TempDir()); err == nil {
				t.Fatal("unsafe or incomplete PDF bundle was accepted")
			}
		})
	}
}

func TestPDFWorkspaceRecoveryKeepsActiveAndUnrelatedDirectories(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	for _, name := range []string{"scribe-pdf-abandoned", "scribe-pdf-active", "unrelated"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-3 * time.Minute)
	if err := os.Chtimes(filepath.Join(root, "scribe-pdf-abandoned"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := removeExpiredWorkspaces(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 2 || entries[0].Name() != "scribe-pdf-active" || entries[1].Name() != "unrelated" {
		t.Fatalf("workspace recovery removed active/unrelated data or retained abandoned data: %v", err)
	}
}

func TestPDFRuntimePreservesCorrectedUnicodeImagesAndPageOrder(t *testing.T) {
	if os.Getenv("SCRIBE_PDF_RUNTIME_TEST") != "true" {
		t.Skip("make pdf-export-smoke runs this in the packaged runtime")
	}
	var input bytes.Buffer
	archive := tar.NewWriter(&input)
	var sourceImages [][]byte
	for index, fill := range []color.RGBA{{R: 240, A: 255}, {B: 240, A: 255}} {
		page := image.NewRGBA(image.Rect(0, 0, 600, 400))
		draw.Draw(page, page.Bounds(), image.NewUniform(fill), image.Point{}, draw.Src)
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, page, nil); err != nil {
			t.Fatal(err)
		}
		sourceImages = append(sourceImages, encoded.Bytes())
		hocr, err := fixtures.ReadFile("testdata/" + PageName(index+1, "hocr"))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range []struct {
			extension string
			content   []byte
		}{{"hocr", hocr}, {"jpg", encoded.Bytes()}} {
			if err := archive.WriteHeader(&tar.Header{Name: PageName(index+1, entry.extension), Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(entry.content))}); err != nil {
				t.Fatal(err)
			}
			if _, err := archive.Write(entry.content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	service := exec.CommandContext(ctx, "/app/scyllaridae")
	service.Env = append(os.Environ(), "SCYLLARIDAE_YML_PATH=/app/scyllaridae.yml", "SCYLLARIDAE_PORT=8080", "SCYLLARIDAE_LOG_LEVEL=ERROR")
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = service.Process.Kill()
		_ = service.Wait()
	}()
	client := &http.Client{Timeout: 10 * time.Second}
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		response, err := client.Get("http://127.0.0.1:8080/healthcheck")
		if err == nil {
			_ = response.Body.Close()
			ready = response.StatusCode == http.StatusOK
			if ready {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("Scyllaridae did not become ready")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8080/", &input)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-tar")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	pdf, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("Scyllaridae PDF response = %d/%v", response.StatusCode, err)
	}
	dir := t.TempDir()
	filename := filepath.Join(dir, "output.pdf")
	if err := os.WriteFile(filename, pdf, 0600); err != nil {
		t.Fatal(err)
	}
	text, err := exec.CommandContext(ctx, "/usr/bin/pdftotext", "-enc", "UTF-8", filename, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	pages := strings.Split(string(text), "\f")
	if len(pages) != 3 || strings.Join(strings.Fields(pages[0]), " ") != "café 東京" || strings.Join(strings.Fields(pages[1]), " ") != "Second corrected page" {
		t.Fatalf("PDF lost or reordered corrected Unicode text: %q", text)
	}
	if err := exec.CommandContext(ctx, "/usr/bin/pdfimages", "-j", filename, filepath.Join(dir, "image")).Run(); err != nil {
		t.Fatal(err)
	}
	for index, expected := range sourceImages {
		name := []string{"image-000.jpg", "image-001.jpg"}[index]
		actual, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("PDF changed or reordered image %d: %v", index+1, err)
		}
	}
}
