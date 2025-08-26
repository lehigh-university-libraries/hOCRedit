package ocr

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lehigh-university-libraries/hocr-edit/internal/models"
	gosseract "github.com/otiai10/gosseract/v2"
)

type Service struct{}

func New() *Service {
	slog.Info("Initializing OCR service (Tesseract word detection + ChatGPT transcription)")
	return &Service{}
}

func (s *Service) ProcessImage(imagePath string) (models.OCRResponse, error) {
	return s.detectWordBoundariesWithTesseract(imagePath)
}

func (s *Service) ProcessImageToHOCR(imagePath string) (string, error) {
	var ocrResponse models.OCRResponse
	var err error
	if os.Getenv("TESSERACT_WORD_BOUNDARIES") != "" {
		ocrResponse, err = s.detectWordBoundariesWithTesseract(imagePath)
		if err != nil {
			return "", fmt.Errorf("failed to detect word boundaries with both methods: %w", err)
		}
	} else {
		ocrResponse, err = s.detectWordBoundariesCustom(imagePath)
		if err != nil {
			return "", fmt.Errorf("failed to detect word boundaries with both methods: %w", err)
		}
	}

	slog.Info("Detected word boundaries with Tesseract", "word_count", s.countWords(ocrResponse))

	// Step 2: Create stitched image with hOCR markup overlaid
	stitchedImagePath, err := s.createStitchedImageWithHOCRMarkup(imagePath, ocrResponse)
	if err != nil {
		slog.Warn("Failed to create stitched image, using Tesseract output only", "error", err)
		return s.convertToBasicHOCR(ocrResponse), nil
	}
	// defer os.Remove(stitchedImagePath)

	slog.Info("Created stitched image with hOCR markup", "path", stitchedImagePath)

	// Step 3: Have ChatGPT transcribe the hOCR markup from the stitched image
	hocrResult, err := s.transcribeWithChatGPT(stitchedImagePath)
	if err != nil {
		slog.Warn("ChatGPT transcription failed, using Tesseract output only", "error", err)
		return s.convertToBasicHOCR(ocrResponse), nil
	}

	slog.Info("ChatGPT transcription completed", "result_length", len(hocrResult))

	// Step 4: Wrap the result in a complete hOCR document
	return s.wrapInHOCRDocument(hocrResult), nil
}

func (s *Service) GetDetectionMethod() string {
	return "tesseract_with_custom_fallback_and_chatgpt"
}

// detectWordBoundariesWithTesseract uses Tesseract to find word boundaries
func (s *Service) detectWordBoundariesWithTesseract(imagePath string) (models.OCRResponse, error) {
	client := gosseract.NewClient()
	defer client.Close()

	// Set the image
	err := client.SetImage(imagePath)
	if err != nil {
		return models.OCRResponse{}, fmt.Errorf("failed to set image in Tesseract: %w", err)
	}

	// Get bounding boxes for words
	boxes, err := client.GetBoundingBoxes(gosseract.RIL_TEXTLINE)
	if err != nil {
		return models.OCRResponse{}, fmt.Errorf("failed to get bounding boxes from Tesseract: %w", err)
	}

	// Get image dimensions
	width, height, err := s.getImageDimensions(imagePath)
	if err != nil {
		return models.OCRResponse{}, fmt.Errorf("failed to get image dimensions: %w", err)
	}

	slog.Info("Tesseract detected words", "count", len(boxes), "image_size", fmt.Sprintf("%dx%d", width, height))

	// Convert Tesseract boxes to our OCR response format
	return s.convertTesseractBoxesToOCRResponse(boxes, width, height), nil
}

func (s *Service) getImageDimensions(imagePath string) (int, int, error) {
	// Use ImageMagick to get dimensions
	cmd := exec.Command("magick", "identify", "-format", "%w %h", imagePath)
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get image dimensions: %w", err)
	}

	var width, height int
	_, err = fmt.Sscanf(strings.TrimSpace(string(output)), "%d %d", &width, &height)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse dimensions: %w", err)
	}

	return width, height, nil
}

func (s *Service) convertTesseractBoxesToOCRResponse(boxes []gosseract.BoundingBox, width, height int) models.OCRResponse {
	var paragraphs []models.Paragraph

	// Convert each Tesseract word box to a paragraph containing a single word
	for _, box := range boxes {
		// Skip empty words
		if strings.TrimSpace(box.Word) == "" {
			continue
		}

		word := models.Word{
			BoundingBox: models.BoundingPoly{
				Vertices: []models.Vertex{
					{X: box.Box.Min.X, Y: box.Box.Min.Y},
					{X: box.Box.Max.X, Y: box.Box.Min.Y},
					{X: box.Box.Max.X, Y: box.Box.Max.Y},
					{X: box.Box.Min.X, Y: box.Box.Max.Y},
				},
			},
			Symbols: []models.Symbol{
				{
					BoundingBox: models.BoundingPoly{
						Vertices: []models.Vertex{
							{X: box.Box.Min.X, Y: box.Box.Min.Y},
							{X: box.Box.Max.X, Y: box.Box.Min.Y},
							{X: box.Box.Max.X, Y: box.Box.Max.Y},
							{X: box.Box.Min.X, Y: box.Box.Max.Y},
						},
					},
					Text: box.Word, // Use actual text from Tesseract
				},
			},
		}

		paragraph := models.Paragraph{
			BoundingBox: models.BoundingPoly{
				Vertices: []models.Vertex{
					{X: box.Box.Min.X, Y: box.Box.Min.Y},
					{X: box.Box.Max.X, Y: box.Box.Min.Y},
					{X: box.Box.Max.X, Y: box.Box.Max.Y},
					{X: box.Box.Min.X, Y: box.Box.Max.Y},
				},
			},
			Words: []models.Word{word},
		}
		paragraphs = append(paragraphs, paragraph)
	}

	block := models.Block{
		BoundingBox: models.BoundingPoly{
			Vertices: []models.Vertex{
				{X: 0, Y: 0},
				{X: width, Y: 0},
				{X: width, Y: height},
				{X: 0, Y: height},
			},
		},
		BlockType:  "TEXT",
		Paragraphs: paragraphs,
	}

	page := models.Page{
		Width:  width,
		Height: height,
		Blocks: []models.Block{block},
	}

	return models.OCRResponse{
		Responses: []models.Response{
			{
				FullTextAnnotation: &models.FullTextAnnotation{
					Pages: []models.Page{page},
					Text:  "Tesseract word detection with ChatGPT transcription",
				},
			},
		},
	}
}

// detectWordBoundariesCustom uses our own image processing algorithm to find word boundaries
func (s *Service) detectWordBoundariesCustom(imagePath string) (models.OCRResponse, error) {
	// Get image dimensions first
	width, height, err := s.getImageDimensions(imagePath)
	if err != nil {
		return models.OCRResponse{}, fmt.Errorf("failed to get image dimensions: %w", err)
	}

	// Step 1: Detect individual words using image processing
	words, err := s.detectWords(imagePath, width, height)
	if err != nil {
		return models.OCRResponse{}, fmt.Errorf("failed to detect words: %w", err)
	}

	slog.Info("Custom word detection completed", "word_count", len(words), "image_size", fmt.Sprintf("%dx%d", width, height))

	// Step 2: Group words into lines based on coordinates
	lines := s.groupWordsIntoLines(words)
	slog.Info("Grouped words into lines", "line_count", len(lines))

	// Step 3: Convert to OCR response format
	return s.convertWordsAndLinesToOCRResponse(lines, width, height), nil
}

// WordBox represents a detected word with its bounding box
type WordBox struct {
	X, Y, Width, Height int
	Text                string // Placeholder text for custom detection
}

// LineBox represents a line of text containing multiple words
type LineBox struct {
	Words               []WordBox
	X, Y, Width, Height int // Bounding box of the entire line
}

// detectWords finds individual word regions using image processing
func (s *Service) detectWords(imagePath string, imgWidth, imgHeight int) ([]WordBox, error) {
	// Preprocess the image
	processedPath, err := s.preprocessImageForWordDetection(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to preprocess image: %w", err)
	}
	defer os.Remove(processedPath)

	// Load processed image
	file, err := os.Open(processedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open processed image: %w", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("failed to decode processed image: %w", err)
	}

	// Find connected components (potential words)
	components := s.findWordComponents(img)

	// Filter and refine components to get word boxes
	wordBoxes := s.refineComponentsToWords(components, imgWidth, imgHeight)

	return wordBoxes, nil
}

// preprocessImageForWordDetection preprocesses the image for better word detection
func (s *Service) preprocessImageForWordDetection(imagePath string) (string, error) {
	tempDir := "/tmp"
	baseName := strings.TrimSuffix(filepath.Base(imagePath), filepath.Ext(imagePath))
	processedPath := filepath.Join(tempDir, fmt.Sprintf("processed_words_%s_%d.jpg", baseName, time.Now().Unix()))

	// Preprocess: grayscale, enhance contrast, sharpen, threshold
	cmd := exec.Command("magick", imagePath,
		"-colorspace", "Gray", // Convert to grayscale
		"-contrast-stretch", "0.15x0.05%", // Enhance contrast
		"-sharpen", "0x1", // Sharpen slightly
		"-morphology", "close", "rectangle:2x1", // Close small gaps horizontally
		"-threshold", "75%", // Apply threshold
		processedPath)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("imagemagick preprocessing failed: %w", err)
	}

	return processedPath, nil
}

// findWordComponents finds connected components that could be words
func (s *Service) findWordComponents(img image.Image) []WordBox {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	visited := make([][]bool, height)
	for i := range visited {
		visited[i] = make([]bool, width)
	}

	var components []WordBox

	// Find all connected components using flood fill
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !visited[y][x] && s.isTextPixel(img.At(x, y)) {
				minX, minY, maxX, maxY := x, y, x, y
				s.floodFillComponent(img, visited, x, y, &minX, &minY, &maxX, &maxY)

				// Filter by size to get potential words
				w := maxX - minX + 1
				h := maxY - minY + 1
				if s.isValidWordSize(w, h, width, height) {
					components = append(components, WordBox{
						X:      minX,
						Y:      minY,
						Width:  w,
						Height: h,
						Text:   fmt.Sprintf("word_%d", len(components)+1),
					})
				}
			}
		}
	}

	return components
}

// floodFillComponent performs flood fill to find connected text pixels
func (s *Service) floodFillComponent(img image.Image, visited [][]bool, x, y int, minX, minY, maxX, maxY *int) {
	bounds := img.Bounds()
	if x < 0 || x >= bounds.Dx() || y < 0 || y >= bounds.Dy() || visited[y][x] || !s.isTextPixel(img.At(x, y)) {
		return
	}

	visited[y][x] = true

	// Update bounding box
	if x < *minX {
		*minX = x
	}
	if x > *maxX {
		*maxX = x
	}
	if y < *minY {
		*minY = y
	}
	if y > *maxY {
		*maxY = y
	}

	// Check 8 neighbors
	directions := [][]int{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}
	for _, dir := range directions {
		s.floodFillComponent(img, visited, x+dir[0], y+dir[1], minX, minY, maxX, maxY)
	}
}

// isTextPixel determines if a pixel is likely part of text (dark pixel)
func (s *Service) isTextPixel(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	gray := (r + g + b) / 3
	return gray < 32768 // Dark pixels are considered text
}

// isValidWordSize checks if a component size is reasonable for a word
func (s *Service) isValidWordSize(w, h, imgWidth, imgHeight int) bool {
	// Filter by reasonable word dimensions
	minWidth, minHeight := 8, 10 // Minimum size for a word
	maxWidth := imgWidth / 2     // Words shouldn't be more than half the image width
	maxHeight := imgHeight / 5   // Words shouldn't be more than 1/5 the image height

	return w >= minWidth && h >= minHeight && w <= maxWidth && h <= maxHeight
}

// refineComponentsToWords refines detected components into word boxes
func (s *Service) refineComponentsToWords(components []WordBox, imgWidth, imgHeight int) []WordBox {
	if len(components) == 0 {
		return components
	}

	// Sort components for processing (top to bottom, left to right)
	sort.Slice(components, func(i, j int) bool {
		if abs(components[i].Y-components[j].Y) < 10 { // Same line threshold
			return components[i].X < components[j].X
		}
		return components[i].Y < components[j].Y
	})

	// Merge nearby components that likely belong to the same word
	mergedWords := s.mergeNearbyComponents(components)

	return mergedWords
}

// mergeNearbyComponents merges components that are close together into single words
func (s *Service) mergeNearbyComponents(components []WordBox) []WordBox {
	if len(components) <= 1 {
		return components
	}

	var mergedWords []WordBox
	currentGroup := []WordBox{components[0]}

	for i := 1; i < len(components); i++ {
		component := components[i]
		lastInGroup := currentGroup[len(currentGroup)-1]

		// Check if this component should be merged with the current group
		if s.shouldMergeComponents(lastInGroup, component) {
			currentGroup = append(currentGroup, component)
		} else {
			// Finish current group and start new one
			mergedWord := s.mergeComponentGroup(currentGroup)
			mergedWords = append(mergedWords, mergedWord)
			currentGroup = []WordBox{component}
		}
	}

	// Don't forget the last group
	if len(currentGroup) > 0 {
		mergedWord := s.mergeComponentGroup(currentGroup)
		mergedWords = append(mergedWords, mergedWord)
	}

	return mergedWords
}

// shouldMergeComponents determines if two components should be merged into one word
func (s *Service) shouldMergeComponents(a, b WordBox) bool {
	// Calculate horizontal and vertical distances
	horizontalGap := b.X - (a.X + a.Width)
	verticalOverlap := !(b.Y+b.Height < a.Y || b.Y > a.Y+a.Height)

	// Merge if components are close horizontally and have vertical overlap
	maxGap := max(a.Height, b.Height) / 3 // Allow gap up to 1/3 of character height
	return horizontalGap >= 0 && horizontalGap <= maxGap && verticalOverlap
}

// mergeComponentGroup merges a group of components into a single word box
func (s *Service) mergeComponentGroup(group []WordBox) WordBox {
	if len(group) == 1 {
		return group[0]
	}

	minX, minY := group[0].X, group[0].Y
	maxX, maxY := group[0].X+group[0].Width, group[0].Y+group[0].Height

	for _, comp := range group[1:] {
		if comp.X < minX {
			minX = comp.X
		}
		if comp.Y < minY {
			minY = comp.Y
		}
		if comp.X+comp.Width > maxX {
			maxX = comp.X + comp.Width
		}
		if comp.Y+comp.Height > maxY {
			maxY = comp.Y + comp.Height
		}
	}

	return WordBox{
		X:      minX,
		Y:      minY,
		Width:  maxX - minX,
		Height: maxY - minY,
		Text:   fmt.Sprintf("merged_word_%d", len(group)),
	}
}

// groupWordsIntoLines groups detected words into text lines based on their coordinates
func (s *Service) groupWordsIntoLines(words []WordBox) []LineBox {
	if len(words) == 0 {
		return nil
	}

	// Sort words by Y coordinate first, then X coordinate
	sort.Slice(words, func(i, j int) bool {
		if abs(words[i].Y-words[j].Y) < words[i].Height/2 { // Same line threshold
			return words[i].X < words[j].X
		}
		return words[i].Y < words[j].Y
	})

	var lines []LineBox
	var currentLineWords []WordBox

	for _, word := range words {
		if len(currentLineWords) == 0 {
			currentLineWords = append(currentLineWords, word)
			continue
		}

		// Check if this word belongs to the current line
		if s.wordsOnSameLine(currentLineWords, word) {
			currentLineWords = append(currentLineWords, word)
		} else {
			// Finish current line and start new one
			if len(currentLineWords) > 0 {
				line := s.createLineFromWords(currentLineWords)
				lines = append(lines, line)
			}
			currentLineWords = []WordBox{word}
		}
	}

	// Don't forget the last line
	if len(currentLineWords) > 0 {
		line := s.createLineFromWords(currentLineWords)
		lines = append(lines, line)
	}

	return lines
}

// wordsOnSameLine determines if a word belongs to the current line
func (s *Service) wordsOnSameLine(currentLineWords []WordBox, newWord WordBox) bool {
	if len(currentLineWords) == 0 {
		return true
	}

	// Calculate average height of current line
	avgHeight := 0
	minY, maxY := currentLineWords[0].Y, currentLineWords[0].Y+currentLineWords[0].Height
	for _, word := range currentLineWords {
		avgHeight += word.Height
		if word.Y < minY {
			minY = word.Y
		}
		if word.Y+word.Height > maxY {
			maxY = word.Y + word.Height
		}
	}
	avgHeight /= len(currentLineWords)

	// Check for Y-coordinate overlap with some tolerance
	tolerance := avgHeight / 3
	currentLineBottom := maxY + tolerance
	currentLineTop := minY - tolerance

	return !(newWord.Y+newWord.Height < currentLineTop || newWord.Y > currentLineBottom)
}

// createLineFromWords creates a LineBox from a group of words
func (s *Service) createLineFromWords(words []WordBox) LineBox {
	if len(words) == 0 {
		return LineBox{}
	}

	// Calculate line bounding box
	minX, minY := words[0].X, words[0].Y
	maxX, maxY := words[0].X+words[0].Width, words[0].Y+words[0].Height

	for _, word := range words[1:] {
		if word.X < minX {
			minX = word.X
		}
		if word.Y < minY {
			minY = word.Y
		}
		if word.X+word.Width > maxX {
			maxX = word.X + word.Width
		}
		if word.Y+word.Height > maxY {
			maxY = word.Y + word.Height
		}
	}

	return LineBox{
		Words:  words,
		X:      minX,
		Y:      minY,
		Width:  maxX - minX,
		Height: maxY - minY,
	}
}

// convertWordsAndLinesToOCRResponse converts our custom detection results to OCR response format
// Each line is treated as a single "word" for simplicity
func (s *Service) convertWordsAndLinesToOCRResponse(lines []LineBox, width, height int) models.OCRResponse {
	var paragraphs []models.Paragraph

	// Convert each line to a paragraph containing a single "word" (the entire line)
	for i, line := range lines {
		// Create a single word that represents the entire line
		word := models.Word{
			BoundingBox: models.BoundingPoly{
				Vertices: []models.Vertex{
					{X: line.X, Y: line.Y},
					{X: line.X + line.Width, Y: line.Y},
					{X: line.X + line.Width, Y: line.Y + line.Height},
					{X: line.X, Y: line.Y + line.Height},
				},
			},
			Symbols: []models.Symbol{
				{
					BoundingBox: models.BoundingPoly{
						Vertices: []models.Vertex{
							{X: line.X, Y: line.Y},
							{X: line.X + line.Width, Y: line.Y},
							{X: line.X + line.Width, Y: line.Y + line.Height},
							{X: line.X, Y: line.Y + line.Height},
						},
					},
					Text: fmt.Sprintf("line_%d", i+1), // Placeholder text for the entire line
				},
			},
		}

		paragraph := models.Paragraph{
			BoundingBox: models.BoundingPoly{
				Vertices: []models.Vertex{
					{X: line.X, Y: line.Y},
					{X: line.X + line.Width, Y: line.Y},
					{X: line.X + line.Width, Y: line.Y + line.Height},
					{X: line.X, Y: line.Y + line.Height},
				},
			},
			Words: []models.Word{word}, // Single word per paragraph (line-level detection)
		}
		paragraphs = append(paragraphs, paragraph)
	}

	block := models.Block{
		BoundingBox: models.BoundingPoly{
			Vertices: []models.Vertex{
				{X: 0, Y: 0},
				{X: width, Y: 0},
				{X: width, Y: height},
				{X: 0, Y: height},
			},
		},
		BlockType:  "TEXT",
		Paragraphs: paragraphs,
	}

	page := models.Page{
		Width:  width,
		Height: height,
		Blocks: []models.Block{block},
	}

	return models.OCRResponse{
		Responses: []models.Response{
			{
				FullTextAnnotation: &models.FullTextAnnotation{
					Pages: []models.Page{page},
					Text:  "Custom word detection with line grouping + ChatGPT transcription",
				},
			},
		},
	}
}

func (s *Service) createStitchedImageWithHOCRMarkup(imagePath string, response models.OCRResponse) (string, error) {
	tempDir := "/tmp"
	baseName := strings.TrimSuffix(filepath.Base(imagePath), filepath.Ext(imagePath))
	stitchedPath := filepath.Join(tempDir, fmt.Sprintf("stitched_%s_%d.png", baseName, time.Now().Unix()))

	var componentPaths []string

	if len(response.Responses) == 0 || response.Responses[0].FullTextAnnotation == nil {
		return "", fmt.Errorf("no text annotation in response")
	}

	wordIndex := 0
	for _, page := range response.Responses[0].FullTextAnnotation.Pages {
		for _, block := range page.Blocks {
			for _, paragraph := range block.Paragraphs {
				for _, word := range paragraph.Words {
					if len(word.BoundingBox.Vertices) < 4 {
						continue
					}

					bbox := word.BoundingBox

					// Create hOCR line opening tag
					lineTag := fmt.Sprintf(`<span class='ocrx_line' id='line_%d' title='bbox %d %d %d %d'>`,
						wordIndex+1,
						bbox.Vertices[0].X, bbox.Vertices[0].Y,
						bbox.Vertices[2].X, bbox.Vertices[2].Y)
					lineTagPath, err := s.createTextImage(lineTag, tempDir, fmt.Sprintf("line_%d", wordIndex))
					if err == nil {
						componentPaths = append(componentPaths, lineTagPath)
					}

					// Create hOCR word opening tag
					wordTag := fmt.Sprintf(`<span class='ocrx_word' id='word_%d' title='bbox %d %d %d %d'>`,
						wordIndex+1,
						bbox.Vertices[0].X, bbox.Vertices[0].Y,
						bbox.Vertices[2].X, bbox.Vertices[2].Y)
					wordTagPath, err := s.createTextImage(wordTag, tempDir, fmt.Sprintf("word_%d", wordIndex))
					if err == nil {
						componentPaths = append(componentPaths, wordTagPath)
					}

					// Extract the actual word image
					wordImagePath, err := s.extractWordImage(imagePath, bbox, tempDir, wordIndex)
					if err == nil {
						componentPaths = append(componentPaths, wordImagePath)
					}

					// Create closing tags
					wordClosePath, err := s.createTextImage("</span>", tempDir, fmt.Sprintf("word_close_%d", wordIndex))
					if err == nil {
						componentPaths = append(componentPaths, wordClosePath)
					}

					lineClosePath, err := s.createTextImage("</span>", tempDir, fmt.Sprintf("line_close_%d", wordIndex))
					if err == nil {
						componentPaths = append(componentPaths, lineClosePath)
					}

					wordIndex++
				}
			}
		}
	}

	if len(componentPaths) == 0 {
		return "", fmt.Errorf("no valid components were created")
	}

	// Stitch all components together vertically
	args := append(componentPaths, "-append", stitchedPath)
	cmd := exec.Command("magick", args...)
	err := cmd.Run()

	// Clean up component images
	for _, componentPath := range componentPaths {
		os.Remove(componentPath)
	}

	if err != nil {
		return "", fmt.Errorf("failed to stitch components: %w", err)
	}

	return stitchedPath, nil
}

func (s *Service) createTextImage(text, tempDir, filename string) (string, error) {
	outputPath := filepath.Join(tempDir, fmt.Sprintf("%s_%d.png", filename, time.Now().Unix()))

	cmd := exec.Command("magick",
		"-size", "2000x60",
		"xc:white",
		"-fill", "black",
		"-font", "DejaVu-Sans-Mono",
		"-pointsize", "24",
		"-draw", fmt.Sprintf(`text 10,40 "%s"`, text),
		outputPath)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create text image: %w", err)
	}

	return outputPath, nil
}

func (s *Service) extractWordImage(imagePath string, bbox models.BoundingPoly, tempDir string, wordIndex int) (string, error) {
	if len(bbox.Vertices) < 4 {
		return "", fmt.Errorf("invalid bounding box")
	}

	minX := bbox.Vertices[0].X
	minY := bbox.Vertices[0].Y
	maxX := bbox.Vertices[2].X
	maxY := bbox.Vertices[2].Y

	width := maxX - minX
	height := maxY - minY

	if width <= 0 || height <= 0 {
		return "", fmt.Errorf("invalid dimensions")
	}

	// Add padding
	padding := 3
	cropX := max(0, minX-padding)
	cropY := max(0, minY-padding)
	cropWidth := width + 2*padding
	cropHeight := height + 2*padding

	outputPath := filepath.Join(tempDir, fmt.Sprintf("word_img_%d_%d.png", wordIndex, time.Now().Unix()))

	cmd := exec.Command("magick", imagePath,
		"-crop", fmt.Sprintf("%dx%d+%d+%d", cropWidth, cropHeight, cropX, cropY),
		"+repage",
		outputPath)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to extract word image: %w", err)
	}

	return outputPath, nil
}

func (s *Service) transcribeWithChatGPT(imagePath string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}

	// Encode image as base64
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to read image: %w", err)
	}
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	// Create ChatGPT request
	request := ChatGPTRequest{
		Model: s.getModel(),
		Messages: []ChatGPTMessage{
			{
				Role: "user",
				Content: []ChatGPTContent{
					{
						Type: "text",
						Text: `Read and transcribe all the hOCR markup overlaid on this image.
You will see hOCR tags like:
<span class='ocrx_line' id='line_X' title='bbox x y w h'>
<span class='ocrx_word' id='word_X' title='bbox x y w h'>
[word image that needs transcription]
</span>
</span>

Transcribe BOTH the hOCR tags AND the text content inside them.
For each word image, read the text and include it between the word tags.
If a word image has no legible text, omit that word's span entirely.
IMPORTANT: If the transcribed text contains special characters like &, <, >, ", or ', 
please replace them with their XML entities: &amp; &lt; &gt; &quot; &#39;
Return only the hOCR markup with transcribed text content.`,
					},
					{
						Type: "image_url",
						ImageURL: &ChatGPTImageURL{
							URL: fmt.Sprintf("data:image/png;base64,%s", imageBase64),
						},
					},
				},
			},
		},
	}

	return s.callChatGPT(request)
}

type ChatGPTRequest struct {
	Model       string           `json:"model"`
	Temperature float64          `json:"temperature,omitempty"`
	Messages    []ChatGPTMessage `json:"messages"`
}

type ChatGPTMessage struct {
	Role    string           `json:"role"`
	Content []ChatGPTContent `json:"content"`
}

type ChatGPTContent struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *ChatGPTImageURL `json:"image_url,omitempty"`
}

type ChatGPTImageURL struct {
	URL string `json:"url"`
}

type ChatGPTResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (s *Service) callChatGPT(request ChatGPTRequest) (string, error) {
	requestBody, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))

	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ChatGPT API returned status %d: %s", resp.StatusCode, string(body))
	}

	var chatGPTResponse ChatGPTResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatGPTResponse); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(chatGPTResponse.Choices) == 0 {
		return "", fmt.Errorf("no response from ChatGPT")
	}

	content := strings.TrimSpace(chatGPTResponse.Choices[0].Message.Content)
	slog.Info("Raw response", "content", content)
	// Clean up the ChatGPT response to ensure valid XML
	content = s.cleanChatGPTResponse(content)
	slog.Info("Clean response", "content", content)

	return content, nil
}

func (s *Service) cleanChatGPTResponse(content string) string {
	// Clean up the ChatGPT response to fix common XML issues
	result := content

	// Handle standalone & characters that aren't part of valid entities
	// Replace & with &amp; unless it's already part of a valid entity
	result = s.fixAmpersands(result)

	// Clean up any other problematic characters in text content
	result = s.escapeTextContent(result)

	return result
}

func (s *Service) fixAmpersands(content string) string {
	// Replace & with &amp; unless it's already part of a valid XML entity
	validEntities := []string{"&amp;", "&lt;", "&gt;", "&quot;", "&apos;", "&#39;"}

	result := content
	lines := strings.Split(result, "\n")
	var cleanLines []string

	for _, line := range lines {
		cleanLine := line

		// Find all & characters
		for i := 0; i < len(cleanLine); i++ {
			if cleanLine[i] == '&' {
				// Check if this is part of a valid entity
				isValidEntity := false
				for _, entity := range validEntities {
					if i+len(entity) <= len(cleanLine) && cleanLine[i:i+len(entity)] == entity {
						isValidEntity = true
						i += len(entity) - 1 // Skip past this entity
						break
					}
				}

				// Check for numeric entities like &#39;
				if !isValidEntity && i+2 < len(cleanLine) && cleanLine[i+1] == '#' {
					// Look for numeric entity pattern &#digits;
					j := i + 2
					for j < len(cleanLine) && cleanLine[j] >= '0' && cleanLine[j] <= '9' {
						j++
					}
					if j < len(cleanLine) && cleanLine[j] == ';' {
						isValidEntity = true
						i = j // Skip past this entity
					}
				}

				if !isValidEntity {
					// Replace this & with &amp;
					cleanLine = cleanLine[:i] + "&amp;" + cleanLine[i+1:]
					i += 4 // Skip past the inserted &amp;
				}
			}
		}

		cleanLines = append(cleanLines, cleanLine)
	}

	return strings.Join(cleanLines, "\n")
}

func (s *Service) escapeTextContent(content string) string {
	// This function looks for text content within span tags and escapes any remaining problematic characters
	lines := strings.Split(content, "\n")
	var cleanLines []string

	for _, line := range lines {
		if strings.Contains(line, "<span") && strings.Contains(line, "</span>") {
			// Process span lines to escape text content
			cleaned := s.escapeTextInSpans(line)
			cleanLines = append(cleanLines, cleaned)
		} else {
			cleanLines = append(cleanLines, line)
		}
	}

	return strings.Join(cleanLines, "\n")
}

func (s *Service) escapeTextInSpans(line string) string {
	// Split by </span> to process each span element
	parts := strings.Split(line, "</span>")

	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		lastGT := strings.LastIndex(part, ">")
		if lastGT >= 0 && lastGT < len(part)-1 {
			before := part[:lastGT+1]
			text := part[lastGT+1:]

			// Only escape < and > that aren't already escaped and aren't part of valid entities
			text = strings.ReplaceAll(text, "<", "&lt;")
			text = strings.ReplaceAll(text, ">", "&gt;")

			parts[i] = before + text
		}
	}

	return strings.Join(parts, "</span>")
}

func (s *Service) getModel() string {
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		return "gpt-4o"
	}
	return model
}

func (s *Service) convertToBasicHOCR(response models.OCRResponse) string {
	var lines []string

	if len(response.Responses) == 0 || response.Responses[0].FullTextAnnotation == nil {
		return s.wrapInHOCRDocument("")
	}

	wordIndex := 0
	for _, page := range response.Responses[0].FullTextAnnotation.Pages {
		for _, block := range page.Blocks {
			for _, paragraph := range block.Paragraphs {
				for _, word := range paragraph.Words {
					if len(word.BoundingBox.Vertices) >= 4 && len(word.Symbols) > 0 {
						bbox := word.BoundingBox
						text := html.EscapeString(word.Symbols[0].Text) // Use Tesseract-detected text with XML escaping
						line := fmt.Sprintf(`<span class='ocrx_line' id='line_%d' title='bbox %d %d %d %d'><span class='ocrx_word' id='word_%d' title='bbox %d %d %d %d'>%s</span></span>`,
							wordIndex+1,
							bbox.Vertices[0].X, bbox.Vertices[0].Y,
							bbox.Vertices[2].X, bbox.Vertices[2].Y,
							wordIndex+1,
							bbox.Vertices[0].X, bbox.Vertices[0].Y,
							bbox.Vertices[2].X, bbox.Vertices[2].Y,
							text)
						lines = append(lines, line)
						wordIndex++
					}
				}
			}
		}
	}

	return s.wrapInHOCRDocument(strings.Join(lines, "\n"))
}

func (s *Service) wrapInHOCRDocument(content string) string {
	return fmt.Sprintf(`<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="en" lang="en">
<head>
<title></title>
<meta http-equiv="Content-Type" content="text/html;charset=utf-8" />
<meta name='ocr-system' content='tesseract-with-chatgpt' />
</head>
<body>
<div class='ocr_page' id='page_1'>
%s
</div>
</body>
</html>`, content)
}

func (s *Service) countWords(response models.OCRResponse) int {
	count := 0
	if len(response.Responses) == 0 || response.Responses[0].FullTextAnnotation == nil {
		return count
	}
	for _, page := range response.Responses[0].FullTextAnnotation.Pages {
		for _, block := range page.Blocks {
			for _, paragraph := range block.Paragraphs {
				count += len(paragraph.Words)
			}
		}
	}
	return count
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
