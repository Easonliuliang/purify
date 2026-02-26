package scraper

import (
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// configToProto maps human-readable config strings to Rod protocol resource types.
var configToProto = map[string]proto.NetworkResourceType{
	"Image":      proto.NetworkResourceTypeImage,
	"Stylesheet": proto.NetworkResourceTypeStylesheet,
	"Font":       proto.NetworkResourceTypeFont,
	"Media":      proto.NetworkResourceTypeMedia,
	"Script":     proto.NetworkResourceTypeScript,
}

// setupHijack installs a request interceptor on the page that blocks
// the specified resource types (images, CSS, fonts, media) to:
//   - slash bandwidth consumption by ~60-80%
//   - accelerate DOM rendering (no image decode, no layout reflow from CSS)
//
// Returns the running HijackRouter so the caller can defer router.Stop().
// Returns nil if there is nothing to block.
func setupHijack(page *rod.Page, blockedTypes []string) *rod.HijackRouter {
	// Build O(1) lookup set from config strings
	blocked := make(map[proto.NetworkResourceType]struct{}, len(blockedTypes))
	for _, name := range blockedTypes {
		if rt, ok := configToProto[name]; ok {
			blocked[rt] = struct{}{}
		}
	}
	if len(blocked) == 0 {
		return nil
	}

	router := page.HijackRequests()

	// Pattern "*" + empty resourceType = intercept ALL requests, then
	// decide per-request whether to block or continue.
	_ = router.Add("*", "", func(ctx *rod.Hijack) {
		if _, shouldBlock := blocked[ctx.Request.Type()]; shouldBlock {
			ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
			return
		}
		ctx.ContinueRequest(&proto.FetchContinueRequest{})
	})

	// router.Run() blocks, so it must live in its own goroutine.
	// It will exit when router.Stop() is called.
	go router.Run()

	return router
}
