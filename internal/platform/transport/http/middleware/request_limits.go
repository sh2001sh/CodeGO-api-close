package middleware

import "github.com/sh2001sh/new-api/constant"

const defaultAnonymousRequestBodyLimitKB = 512

func getAnonymousRequestBodyLimitBytes() int64 {
	limitKB := constant.AnonymousRequestBodyLimitKB
	if limitKB < 0 {
		limitKB = defaultAnonymousRequestBodyLimitKB
	}
	return int64(limitKB) << 10
}
