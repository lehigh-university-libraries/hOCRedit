# Exports

Plain text, hOCR, PAGE XML, and ALTO XML are generated from a committed canonical
AnnotationPage. They are views, not editable persistence stores.

The Library groups the formats under each item's **Download** control. Open it
to choose hOCR, PAGE XML, ALTO XML, plain text, or a searchable PDF.

`AnnotationService.ExportAnnotationPage` requires both the tenant-scoped
`item_image_id` and the exact committed `expected_revision`. It returns that
revision with the media type, filename, and bytes. A stale revision fails with
Connect `aborted`; export requests never accept annotation JSON supplied by the
caller.

For a multi-page item, `ItemService.GetItem` returns the ordered canonical
revision vector with the item using one bounded database query. The bundled app
verifies that every image has exactly one revision and calls
`ItemService.PrepareItemExport` with that complete vector. The response is a
short-lived, workspace-bound, signed download URL. `/v1/item-exports/{token}`
checks the metadata digest before loading page payloads, fails if any canonical
revision changed, and then creates one bounded text or ZIP response in an
immediately unlinked temporary file. A process crash therefore cannot leave
transcription plaintext in the container filesystem.

Private IIIF Canvas `seeAlso` links use
`/v1/item-images/{item_image_id}/annotations/revisions/{revision}/hocr` so the
linked hOCR is revision-specific. Page, item, prepared-download, and hOCR
exports require `annotations:read`; `items:read` alone never exposes canonical
transcription text.

Exports have dedicated global and per-workspace concurrency limits. A canonical
page may emit at most 32 MiB, one item may read at most 64 MiB of canonical
source and stage at most 128 MiB of derived output, generation has a 90-second
work deadline plus a bounded response-write grace period, and prepared URLs
expire after five minutes.

## Searchable PDF

`ANNOTATION_EXPORT_FORMAT_PDF` is supported by `ItemService.PrepareItemExport`,
including for a single-image item. The page-only export RPC continues to serve
the four text formats. PDF uses the same complete revision vector, signed URL,
authorization, and stale-revision rejection as the other item exports.

The API renders each committed page as hOCR, fetches its full JPEG through the
existing bounded image path, verifies image dimensions, and sends ordered
image/hOCR pairs to the server-configured Scyllaridae service. No image URLs,
workspace credentials, or caller-supplied annotation data reach that service.
The packaged `hocr-pdf` converter combines the existing text with the images;
it does not run OCR again. `pdfinfo` must accept the output and confirm the
page count before any PDF bytes are returned.

A PDF request is limited to 1,000 pages, 250 million aggregate image pixels,
128 MiB of image/hOCR input, and 128 MiB of output. The converter has an
85-second deadline inside the API's 90-second export deadline. Images retain
their pixel dimensions and use their JPEG resolution metadata, or 300 DPI when
the metadata is absent. Downloads contain one PDF, including for multi-page
items. See [PDF runtime operations](../operations/configuration.md#pdf-exports).

`make pdf-export-smoke` calls the packaged Scyllaridae HTTP service with two
colored images and corrected hOCR, checks Unicode text with `pdftotext`, and
uses `pdfimages` to prove image bytes and page order survived. The synthetic
images contain no text to recognize. The same test runs in the CI test group.

## Text format validation

Golden fixtures exercise the production renderer for each format. PAGE output
conforms to the pinned PRImA PAGE Content 2019-07-15 schema, and ALTO output
conforms to the pinned Library of Congress ALTO 4.4 schema. The backend test
gate verifies the committed schemas by checksum and validates every PAGE and
ALTO golden offline with `xmllint`.
PAGE's required `imageFilename` is the deterministic derivation marker
`source-image.png`; authoritative image provenance remains the target Canvas in
the canonical IIIF AnnotationPage.

When an annotation mutation changes export semantics, update the relevant
implementation and intentionally regenerate the golden file with review of the
diff and schema result.
