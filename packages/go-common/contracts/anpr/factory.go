package anpr

import (
	"log/slog"
	"os"
)

// NewFromEnv selects an ANPR provider from environment variables.
// The selection rule is intentionally simple so operators see exactly
// what's running:
//
//	ANPR_PROVIDER=openalpr  + OPENALPR_SECRET_KEY required → OpenALPR
//	ANPR_PROVIDER=dev-stub or unset                       → DevStub
//
// Phase-3 ships OpenALPR as the only real adapter; new ones (PlateRecognizer,
// in-house CV models) plug in here without touching service code.
//
// The returned Recognizer is also reflected in /healthz via Info() so
// operations can confirm which provider is live per replica.
func NewFromEnv(log *slog.Logger) Recognizer {
	switch os.Getenv("ANPR_PROVIDER") {
	case "openalpr":
		key := os.Getenv("OPENALPR_SECRET_KEY")
		if key == "" {
			if log != nil {
				log.Warn("ANPR_PROVIDER=openalpr but OPENALPR_SECRET_KEY is empty; falling back to dev-stub")
			}
			return NewDevStub()
		}
		return &OpenALPR{
			BaseURL:   os.Getenv("OPENALPR_BASE_URL"),
			SecretKey: key,
			Country:   os.Getenv("OPENALPR_COUNTRY"),
		}
	default:
		return NewDevStub()
	}
}
