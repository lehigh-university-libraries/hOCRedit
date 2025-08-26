# hOCRedit

A web-based hOCR editor with visual overlay editing and intelligent OCR processing optimized for handwritten text.

![Demo](./docs/assets/example.gif)

## Overview

hOCRedit uses a custom word bounding box algorithm specifically designed for better handwritten text detection, combined with ChatGPT transcription to provide high-quality OCR results. Features a visual interface for correcting text directly on source images with real-time accuracy metrics.

## Features

- Visual text editing with bounding box overlays
- Custom word bounding box algorithm optimized for handwritten text
- Hybrid OCR: word detection + ChatGPT transcription
- Line-based editing and drawing mode for new regions
- Islandora integration

## Quick Start

```bash
docker run \
  -p 8888:8888 \
  -e OPENAI_API_KEY=your-key \
  ghcr.io/lehigh-university-libraries/hocredit:main
```

## Configuration

See `[sample.env](./sample.env)`

## Usage

1. Upload images, provide URLs, or Islandora node ID
2. Review OCR results with visual overlays
3. Click text regions to edit content
4. Use drawing mode to create new text regions
5. Monitor accuracy metrics in real-time
6. Export corrected hOCR or save to repositories


## Support

This project was sponsered thanks to a [Lyrasis Catalyst Fund](https://lyrasis.org/catalyst-fund/) grant awarded to Lehigh University.

## License

Apache 2.0

