// Package anpr defines the contract for Automatic Number Plate Recognition
// engines. Real adapters (OpenALPR, PlateRecognizer, in-house CV models)
// are interchangeable per tenant.
package anpr

import (
	"context"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

type Image struct {
	Bytes       []byte
	ContentType string // image/jpeg, image/png
	CapturedAt  time.Time
	Lat, Lng    float64
	Accuracy    float32
}

type Read struct {
	Plate      string
	Confidence float32 // 0..1
	Region     string  // ISO country/region the plate matches
	BBox       *BBox   // optional pixel bounding box
}

type BBox struct{ X, Y, W, H int }

type RecognizeOpts struct {
	TenantID  string
	Country   string // hint
	MinConf   float32
}

type Recognizer interface {
	Info() contracts.AdapterInfo
	Recognize(ctx context.Context, img Image, opts RecognizeOpts) ([]Read, error)
}
