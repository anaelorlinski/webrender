package document

import (
	"math"

	pr "github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/logger"
	"github.com/benoitkugler/webrender/utils"
)

// Transform a HSV color to a RGB color.
func hsv2rgb(hue, saturation, value fl) (r, g, b fl) {
	c := value * saturation
	x := c * fl(1-math.Abs(float64(utils.FloatModulo(hue/60, 2))-1))
	m := value - c
	switch {
	case 0 <= hue && hue < 60:
		return c + m, x + m, m
	case 60 <= hue && hue < 120:
		return x + m, c + m, m
	case 120 <= hue && hue < 180:
		return m, c + m, x + m
	case 180 <= hue && hue < 240:
		return m, x + m, c + m
	case 240 <= hue && hue < 300:
		return x + m, m, c + m
	case 300 <= hue && hue < 360:
		return c + m, m, x + m
	default:
		logger.WarningLogger.Printf("invalid hue %f", hue)
		return 0, 0, 0
	}
}

// Transform a RGB color to a HSV color.
func rgb2hsv(red, green, blue fl) (h, s, c fl) {
	cmax := utils.Maxs(red, green, blue)
	cmin := utils.Mins(red, green, blue)
	delta := cmax - cmin
	var hue fl
	if delta == 0 {
		hue = 0
	} else if cmax == red {
		hue = 60 * utils.FloatModulo((green-blue)/delta, 6)
	} else if cmax == green {
		hue = 60 * ((blue-red)/delta + 2)
	} else if cmax == blue {
		hue = 60 * ((red-green)/delta + 4)
	}
	var saturation fl
	if delta != 0 {
		saturation = delta / cmax
	}
	return hue, saturation, cmax
}

// Return a darker color.
func darken(color Color) Color {
	hue, saturation, value := rgb2hsv(color.R, color.G, color.B)
	value /= 1.5
	saturation /= 1.25
	r, g, b := hsv2rgb(hue, saturation, value)
	return Color{R: r, G: g, B: b, A: color.A}
}

// Return a lighter color.
func lighten(color Color) Color {
	hue, saturation, value := rgb2hsv(color.R, color.G, color.B)
	value = 1 - (1-value)/1.5
	if saturation != 0 {
		saturation = 1 - (1-saturation)/1.25
	}
	r, g, b := hsv2rgb(hue, saturation, value)
	return Color{R: r, G: g, B: b, A: color.A}
}

func styledColor(style pr.String, color Color, side pr.KnownProp) [2]Color {
	if style == "inset" || style == "outset" {
		doLighten := (side == top || side == left) != (style == "inset")
		if doLighten {
			return [2]Color{lighten(color)}
		}
		return [2]Color{darken(color)}
	} else if style == "ridge" || style == "groove" {
		if (side == top || side == left) != (style == "ridge") {
			return [2]Color{lighten(color), darken(color)}
		} else {
			return [2]Color{darken(color), lighten(color)}
		}
	}
	return [2]Color{color}
}
