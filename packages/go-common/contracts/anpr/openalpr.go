// OpenALPR Cloud API adapter.
//
// Wire format (v3):
//
//	POST {BaseURL}/v3/recognize_bytes
//	  application/x-www-form-urlencoded
//	    secret_key   = …
//	    country      = us | eu | gb | …
//	    image_bytes  = base64(<raw image>)
//	    recognize_vehicle = 0
//	    topn         = 5
//
//	200 application/json
//	  { "results": [
//	      { "plate":"ABC123",
//	        "confidence":92.3,           // 0..100
//	        "region":"us-ca",
//	        "coordinates":[{"x":..,"y":..}, …]   // four corners
//	      },
//	      … (one per detection in the frame)
//	    ]
//	  }
//
// Failure modes we explicitly map:
//   - 401 → ErrUnauthorized
//   - 429 → wraps with the body so the caller can surface the rate-limit reason
//   - 5xx → wraps with status + body
//   - network / context errors → wrapped with %w
//
// The adapter re-scales OpenALPR's 0-100 confidence into the contract's
// 0-1 range. Multiple results are returned sorted by confidence
// descending; results below opts.MinConf are dropped.
package anpr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

// ErrUnauthorized is returned when OpenALPR rejects the secret_key.
// Callers should mark the provider down via the connectors.HealthMonitor
// so subsequent scans don't burn the quota.
var ErrUnauthorized = errors.New("openalpr: unauthorized (check secret_key)")

type OpenALPR struct {
	// BaseURL defaults to https://api.openalpr.com when empty. Tests
	// override to point at httptest.NewServer.
	BaseURL string

	// SecretKey is the Cloud API key from openalpr.com.
	SecretKey string

	// Country is the 2-letter region hint (us, eu, gb, fr, …). Per-call
	// RecognizeOpts.Country wins when set.
	Country string

	// HTTPClient is overridable for testing. When nil a default client
	// with an 8-second timeout is used.
	HTTPClient *http.Client
}

func (*OpenALPR) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "anpr", Provider: "openalpr"}
}

// Recognize sends one frame to OpenALPR and returns every detected
// plate sorted by confidence. RecognizeOpts.MinConf gates the cutoff
// (default 0.0 means "return everything").
func (o *OpenALPR) Recognize(ctx context.Context, img Image, opts RecognizeOpts) ([]Read, error) {
	base := o.BaseURL
	if base == "" {
		base = "https://api.openalpr.com"
	}
	country := opts.Country
	if country == "" {
		country = o.Country
	}
	if country == "" {
		country = "us"
	}

	form := url.Values{}
	form.Set("secret_key", o.SecretKey)
	form.Set("country", country)
	form.Set("image_bytes", base64.StdEncoding.EncodeToString(img.Bytes))
	form.Set("recognize_vehicle", "0")
	form.Set("topn", "5")

	req, err := http.NewRequestWithContext(ctx, "POST",
		base+"/v3/recognize_bytes", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	cl := o.HTTPClient
	if cl == nil {
		cl = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openalpr: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openalpr: read body: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("openalpr: %d: %s", resp.StatusCode, snippet(body))
	}

	var raw struct {
		Results []struct {
			Plate       string  `json:"plate"`
			Confidence  float64 `json:"confidence"`
			Region      string  `json:"region"`
			Coordinates []struct {
				X int `json:"x"`
				Y int `json:"y"`
			} `json:"coordinates"`
		} `json:"results"`
		// Some error responses carry no `results` key but { "error": "…" }.
		ErrorMessage string `json:"error"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("openalpr: decode response: %w (body=%s)", err, snippet(body))
	}
	if raw.ErrorMessage != "" {
		return nil, fmt.Errorf("openalpr: %s", raw.ErrorMessage)
	}

	out := make([]Read, 0, len(raw.Results))
	for _, r := range raw.Results {
		conf := float32(r.Confidence / 100.0)
		if conf < opts.MinConf {
			continue
		}
		out = append(out, Read{
			Plate:      r.Plate,
			Confidence: conf,
			Region:     r.Region,
			BBox:       coordsToBBox(r.Coordinates),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Confidence > out[j].Confidence
	})
	return out, nil
}

func coordsToBBox(coords []struct {
	X int `json:"x"`
	Y int `json:"y"`
}) *BBox {
	if len(coords) == 0 {
		return nil
	}
	minX, minY := coords[0].X, coords[0].Y
	maxX, maxY := minX, minY
	for _, c := range coords[1:] {
		if c.X < minX {
			minX = c.X
		}
		if c.X > maxX {
			maxX = c.X
		}
		if c.Y < minY {
			minY = c.Y
		}
		if c.Y > maxY {
			maxY = c.Y
		}
	}
	return &BBox{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

func snippet(b []byte) string {
	if len(b) <= 200 {
		return string(b)
	}
	return string(b[:200]) + "…"
}
