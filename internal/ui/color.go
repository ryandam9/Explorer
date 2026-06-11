package ui

import (
	"fmt"
	"strings"
)

// Color math for the settings console's HUE/SAT/LUM sliders. All functions
// work on "#rgb" / "#rrggbb" hex strings — the only color format the built-in
// themes use. Hue is in degrees [0,360); saturation and luminance in [0,100].

// parseHexColor parses "#rgb" or "#rrggbb" into 8-bit channels.
func parseHexColor(s string) (r, g, b int, ok bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		return 0, 0, 0, false
	}
	hexDigit := func(c byte) (int, bool) {
		switch {
		case c >= '0' && c <= '9':
			return int(c - '0'), true
		case c >= 'a' && c <= 'f':
			return int(c-'a') + 10, true
		case c >= 'A' && c <= 'F':
			return int(c-'A') + 10, true
		}
		return 0, false
	}
	switch len(s) {
	case 4: // #rgb
		var v [3]int
		for i := 0; i < 3; i++ {
			d, ok := hexDigit(s[i+1])
			if !ok {
				return 0, 0, 0, false
			}
			v[i] = d*16 + d
		}
		return v[0], v[1], v[2], true
	case 7: // #rrggbb
		var v [3]int
		for i := 0; i < 3; i++ {
			hi, ok1 := hexDigit(s[1+i*2])
			lo, ok2 := hexDigit(s[2+i*2])
			if !ok1 || !ok2 {
				return 0, 0, 0, false
			}
			v[i] = hi*16 + lo
		}
		return v[0], v[1], v[2], true
	}
	return 0, 0, 0, false
}

// hexToHSL converts a hex color to hue [0,360), saturation and luminance
// [0,100]. ok is false when the string is not a parseable hex color.
func hexToHSL(hex string) (h, s, l float64, ok bool) {
	ri, gi, bi, ok := parseHexColor(hex)
	if !ok {
		return 0, 0, 0, false
	}
	r, g, b := float64(ri)/255, float64(gi)/255, float64(bi)/255

	maxC := max(r, max(g, b))
	minC := min(r, min(g, b))
	l = (maxC + minC) / 2

	if maxC == minC {
		return 0, 0, l * 100, true // achromatic
	}

	d := maxC - minC
	if l > 0.5 {
		s = d / (2 - maxC - minC)
	} else {
		s = d / (maxC + minC)
	}
	switch maxC {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	return h, s * 100, l * 100, true
}

// hslToHex converts hue [0,360), saturation and luminance [0,100] back to a
// "#rrggbb" hex string.
func hslToHex(h, s, l float64) string {
	h = h - 360*float64(int(h)/360)
	if h < 0 {
		h += 360
	}
	s = clampF(s, 0, 100) / 100
	l = clampF(l, 0, 100) / 100

	if s == 0 {
		v := int(l*255 + 0.5)
		return fmt.Sprintf("#%02x%02x%02x", v, v, v)
	}

	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	hk := h / 360

	channel := func(t float64) int {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		var v float64
		switch {
		case t < 1.0/6:
			v = p + (q-p)*6*t
		case t < 1.0/2:
			v = q
		case t < 2.0/3:
			v = p + (q-p)*(2.0/3-t)*6
		default:
			v = p
		}
		return int(v*255 + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", channel(hk+1.0/3), channel(hk), channel(hk-1.0/3))
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
