package scraper

import (
	"net/url"
	"strings"

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

// adDomains is a set of well-known ad and tracking domains to block
// when BlockAds is enabled.
var adDomains = map[string]struct{}{
	"doubleclick.net":                {},
	"googlesyndication.com":          {},
	"googleadservices.com":           {},
	"google-analytics.com":           {},
	"googletagmanager.com":           {},
	"googletagservices.com":          {},
	"facebook.net":                   {},
	"connect.facebook.net":           {},
	"facebook.com":                   {},
	"fbcdn.net":                      {},
	"adnxs.com":                      {},
	"adsrvr.org":                     {},
	"amazon-adsystem.com":            {},
	"criteo.com":                     {},
	"criteo.net":                     {},
	"outbrain.com":                   {},
	"taboola.com":                    {},
	"moatads.com":                    {},
	"pubmatic.com":                   {},
	"rubiconproject.com":             {},
	"scorecardresearch.com":          {},
	"quantserve.com":                 {},
	"hotjar.com":                     {},
	"mixpanel.com":                   {},
	"segment.io":                     {},
	"segment.com":                    {},
	"analytics.twitter.com":          {},
	"ads-twitter.com":                {},
	"static.ads-twitter.com":         {},
	"chartbeat.com":                  {},
	"chartbeat.net":                  {},
	"optimizely.com":                 {},
	"zedo.com":                       {},
	"media.net":                      {},
	"contextweb.com":                 {},
	"bidswitch.net":                  {},
	"openx.net":                      {},
	"casalemedia.com":                {},
	"demdex.net":                     {},
	"krxd.net":                       {},
	"bluekai.com":                    {},
	"exelator.com":                   {},
	"turn.com":                       {},
	"mathtag.com":                    {},
	"serving-sys.com":                {},
	"eyeota.net":                     {},
	"agkn.com":                       {},
	"rlcdn.com":                      {},
	"sharethis.com":                  {},
	"addthis.com":                    {},
	"consensu.org":                   {},
}

// isAdDomain checks if a hostname (or any parent domain) is in the ad blocklist.
func isAdDomain(host string) bool {
	host = strings.ToLower(host)
	// Check exact match first.
	if _, ok := adDomains[host]; ok {
		return true
	}
	// Check parent domains (e.g., "pagead2.googlesyndication.com" → "googlesyndication.com").
	for {
		idx := strings.IndexByte(host, '.')
		if idx < 0 {
			break
		}
		host = host[idx+1:]
		if _, ok := adDomains[host]; ok {
			return true
		}
	}
	return false
}

// setupHijack installs a request interceptor on the page that blocks
// the specified resource types (images, CSS, fonts, media) and optionally
// blocks requests to known ad/tracking domains.
//
// Returns the running HijackRouter so the caller can defer router.Stop().
// Returns nil if there is nothing to block.
func setupHijack(page *rod.Page, blockedTypes []string, blockAds bool) *rod.HijackRouter {
	// Build O(1) lookup set from config strings
	blocked := make(map[proto.NetworkResourceType]struct{}, len(blockedTypes))
	for _, name := range blockedTypes {
		if rt, ok := configToProto[name]; ok {
			blocked[rt] = struct{}{}
		}
	}
	if len(blocked) == 0 && !blockAds {
		return nil
	}

	router := page.HijackRequests()

	// Pattern "*" + empty resourceType = intercept ALL requests, then
	// decide per-request whether to block or continue.
	_ = router.Add("*", "", func(ctx *rod.Hijack) {
		// Block by resource type.
		if _, shouldBlock := blocked[ctx.Request.Type()]; shouldBlock {
			ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
			return
		}

		// Block by ad domain.
		if blockAds {
			if u, err := url.Parse(ctx.Request.URL().String()); err == nil {
				if isAdDomain(u.Hostname()) {
					ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
					return
				}
			}
		}

		ctx.ContinueRequest(&proto.FetchContinueRequest{})
	})

	// router.Run() blocks, so it must live in its own goroutine.
	// It will exit when router.Stop() is called.
	go router.Run()

	return router
}
