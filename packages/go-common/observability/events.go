package observability

import "github.com/icofcucam/naditos/packages/go-common/events"

func init() {
	events.RegisterTraceExtractor(func(ctx events.ContextLike) (string, bool) {
		v := ctx.Value(keyTraceID)
		if v == nil {
			return "", false
		}
		s, ok := v.(string)
		return s, ok && s != ""
	})
}
