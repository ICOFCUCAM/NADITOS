// Notifications service — Phase-1 scaffold (SMS / email / push).
// Phase-2: integrate Twilio / Vonage / Sendgrid / sovereign providers.
package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
)

func main() {
	cfg := config.MustLoad("notifications", 8009)
	log := logger.New(cfg.LogLevel)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/notify", func(w http.ResponseWriter, r *http.Request) {
		type req struct {
			Channel string `json:"channel"` // sms|email|push
			To      string `json:"to"`
			Subject string `json:"subject,omitempty"`
			Body    string `json:"body"`
		}
		var in req
		if err := httpx.ReadJSON(r, &in); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		log.Info("notify (stub)",
			slog.String("channel", in.Channel),
			slog.String("to", in.To),
			slog.String("subject", in.Subject),
		)
		httpx.WriteJSON(w, 202, map[string]string{"status": "queued"})
	})
	if err := server.Run(context.Background(), log, "notifications", cfg.Port, mux); err != nil {
		log.Error("server exited", "err", err)
	}
}
