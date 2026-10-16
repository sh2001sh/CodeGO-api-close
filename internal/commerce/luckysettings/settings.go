// Package luckysettings retains only the fixed defaults needed to replay the
// historical blind-box draw-window migration. It registers no runtime options.
package luckysettings

import "time"

type Setting struct {
	Timezone   string
	DrawHour   int
	DrawMinute int
}

func Get() Setting {
	return Setting{Timezone: "Asia/Shanghai", DrawHour: 20}
}

func (value Setting) Location() (*time.Location, error) {
	return time.LoadLocation(value.Timezone)
}
