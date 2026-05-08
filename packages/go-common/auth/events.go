package auth

import "github.com/icofcucam/naditos/packages/go-common/events"

func init() {
	events.RegisterActorExtractor(func(ctx events.ContextLike) (string, string, bool) {
		// Claims is stored under ctxKey{} in this package; use the same key.
		v := ctx.Value(ctxKey{})
		if v == nil {
			return "", "", false
		}
		c, ok := v.(*Claims)
		if !ok || c == nil {
			return "", "", false
		}
		return c.Subject, c.Role, true
	})
}
