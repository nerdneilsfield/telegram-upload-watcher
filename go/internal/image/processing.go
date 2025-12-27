package imageutil

import (
	"bytes"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
)

type Result struct {
	Data     []byte
	Filename string
}

func Prepare(data []byte, filename string, maxDimension int, maxBytes int, pngStartLevel int) (*Result, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	img = resizeIfNeeded(img, maxDimension)

	encoded, outName, err := encodeOriginal(img, format, filename)
	if err != nil {
		return nil, err
	}
	if len(encoded) <= maxBytes {
		return &Result{Data: encoded, Filename: outName}, nil
	}

	pngBytes, _, err := compressPNGGready(img, maxBytes, pngStartLevel)
	if err != nil {
		return nil, err
	}
	return &Result{Data: pngBytes, Filename: toPNGName(filename)}, nil
}

func resizeIfNeeded(img image.Image, maxDimension int) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if maxDimension <= 0 || max(width, height) <= maxDimension {
		return img
	}
	scale := float64(maxDimension) / float64(max(width, height))
	newW := int(float64(width) * scale)
	newH := int(float64(height) * scale)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	return imaging.Resize(img, newW, newH, imaging.Lanczos)
}

func encodeOriginal(img image.Image, format string, filename string) ([]byte, string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch format {
	case "jpeg", "jpg":
		return encodeJPEG(img, filename)
	case "png":
		return encodePNG(img, filename, png.DefaultCompression)
	case "gif":
		return encodeGIF(img, filename)
	default:
		if ext == ".jpg" || ext == ".jpeg" {
			return encodeJPEG(img, filename)
		}
		if ext == ".png" {
			return encodePNG(img, filename, png.DefaultCompression)
		}
		return encodePNG(img, filename, png.DefaultCompression)
	}
}

func encodeJPEG(img image.Image, filename string) ([]byte, string, error) {
	buffer := &bytes.Buffer{}
	err := jpeg.Encode(buffer, img, &jpeg.Options{Quality: 90})
	return buffer.Bytes(), filename, err
}

func encodePNG(img image.Image, filename string, level png.CompressionLevel) ([]byte, string, error) {
	buffer := &bytes.Buffer{}
	encoder := png.Encoder{CompressionLevel: level}
	err := encoder.Encode(buffer, img)
	return buffer.Bytes(), ensureExt(filename, ".png"), err
}

func encodeGIF(img image.Image, filename string) ([]byte, string, error) {
	buffer := &bytes.Buffer{}
	err := gif.Encode(buffer, img, nil)
	return buffer.Bytes(), ensureExt(filename, ".gif"), err
}

func ensureExt(filename string, ext string) string {
	if strings.EqualFold(filepath.Ext(filename), ext) {
		return filename
	}
	return strings.TrimSuffix(filename, filepath.Ext(filename)) + ext
}

func toPNGName(filename string) string {
	return strings.TrimSuffix(filename, filepath.Ext(filename)) + ".png"
}

func compressPNGGready(img image.Image, maxBytes int, startLevel int) ([]byte, int, error) {
	startLevel = clamp(startLevel, 0, 9)
	encode := func(level int) ([]byte, error) {
		buffer := &bytes.Buffer{}
		encoder := png.Encoder{CompressionLevel: png.CompressionLevel(level)}
		if err := encoder.Encode(buffer, img); err != nil {
			return nil, err
		}
		return buffer.Bytes(), nil
	}

	data, err := encode(startLevel)
	if err != nil {
		return nil, startLevel, err
	}

	if len(data) > maxBytes {
		best := data
		bestLevel := startLevel
		for level := startLevel + 1; level <= 9; level++ {
			data, err = encode(level)
			if err != nil {
				continue
			}
			best = data
			bestLevel = level
			if len(data) <= maxBytes {
				return data, level, nil
			}
		}
		return best, bestLevel, nil
	}

	best := data
	bestLevel := startLevel
	for level := startLevel - 1; level >= 0; level-- {
		data, err = encode(level)
		if err != nil {
			break
		}
		if len(data) <= maxBytes {
			best = data
			bestLevel = level
			continue
		}
		break
	}
	return best, bestLevel, nil
}

func clamp(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
