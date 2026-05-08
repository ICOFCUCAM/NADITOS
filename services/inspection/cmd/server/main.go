// Roadworthiness / EU inspection service — Phase-1 scaffold.
package main

import (
	"context"
	"net/http"

	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
)

func main() {
	cfg := config.MustLoad("inspection", 8005)
	log := logger.New(cfg.LogLevel)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/inspection/verify", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, 200, map[string]any{
			"status":  "stub",
			"message": "Phase-2: connect to national inspection station network",
		})
	})
	if err := server.Run(context.Background(), log, cfg.Port, mux); err != nil {
		log.Error("server exited", "err", err)
	}
}
