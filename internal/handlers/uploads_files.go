package handlers

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
)

// maxDecodePixels caps the canvas an image may declare before it is decoded.
// A few hundred bytes of PNG header can claim a canvas of millions of pixels,
// and decoding it allocates width*height*4 bytes: the cap keeps the worst case
// around 200 MB. It sits far above any real camera (a 48 MP phone sensor
// outputs roughly 8000x6000, i.e. 48 million pixels).
const maxDecodePixels = 50_000_000

// jpegQuality is the quality used when re-encoding a jpeg upload. 85 is the
// usual "visually lossless" point for photos.
const jpegQuality = 85

// Errors that stripImageMetadata reports so the handler can map them to a
// status without leaking decoder internals to the client.
var (
	errImageTooLarge    = errors.New("image dimensions too large")
	errInvalidImageData = errors.New("invalid image data")
)

// uploadExtByType maps sniffed content types to the file extension served.
var uploadExtByType = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// stripImageMetadata re-encodes a jpeg or png from its decoded pixels. The
// stdlib encoders write pixel data only, so the result carries no EXIF (where
// a phone camera records the GPS position of, say, the seller's home) and no
// ancillary chunks such as tEXt.
func stripImageMetadata(data []byte, ext string) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errInvalidImageData
	}
	// int64 keeps the product meaningful on 32-bit builds, where the declared
	// dimensions would overflow an int.
	if int64(cfg.Width)*int64(cfg.Height) > maxDecodePixels {
		return nil, errImageTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errInvalidImageData
	}

	var buf bytes.Buffer
	if ext == ".jpg" {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality})
	} else {
		err = png.Encode(&buf, img)
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// dirUsageBytes sums the size of the regular files in dir. A missing
// directory counts as empty: nothing has been uploaded yet.
func dirUsageBytes(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed underneath us; it does not count
		}
		total += info.Size()
	}
	return total, nil
}
