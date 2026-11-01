package instant

import (
	"regexp"
	"strings"
)

// unit is one unit: how many base units one of it is, in its kind.
type unit struct {
	kind   string
	factor float64
	name   string // shown in the answer
}

// units by every name people type. Base units: metre, kilogram, litre,
// metre/second, byte, second, square metre; temperature is special.
var units = map[string]unit{}

func addUnit(kind string, factor float64, name string, aliases ...string) {
	for _, a := range append(aliases, name) {
		units[a] = unit{kind, factor, name}
	}
}

func init() {
	// length
	addUnit("length", 1e-3, "mm", "millimeter", "millimeters", "millimetre", "millimetres")
	addUnit("length", 1e-2, "cm", "centimeter", "centimeters", "centimetre", "centimetres")
	addUnit("length", 1, "m", "meter", "meters", "metre", "metres")
	addUnit("length", 1000, "km", "kilometer", "kilometers", "kilometre", "kilometres", "kms")
	addUnit("length", 0.0254, "in", "inch", "inches", "\"")
	addUnit("length", 0.3048, "ft", "foot", "feet", "'")
	addUnit("length", 0.9144, "yd", "yard", "yards")
	addUnit("length", 1609.344, "mi", "mile", "miles")
	addUnit("length", 1852, "nmi", "nautical mile", "nautical miles")
	// mass
	addUnit("mass", 1e-3, "g", "gram", "grams", "gr")
	addUnit("mass", 1, "kg", "kilogram", "kilograms", "kilo", "kilos")
	addUnit("mass", 1000, "t", "tonne", "tonnes", "ton", "tons")
	addUnit("mass", 0.45359237, "lb", "lbs", "pound", "pounds")
	addUnit("mass", 0.028349523125, "oz", "ounce", "ounces")
	addUnit("mass", 6.35029318, "st", "stone", "stones")
	// volume
	addUnit("volume", 1e-3, "ml", "milliliter", "milliliters", "millilitre", "millilitres")
	addUnit("volume", 1e-2, "cl", "centiliter", "centilitre")
	addUnit("volume", 0.1, "dl", "deciliter", "decilitre")
	addUnit("volume", 1, "l", "liter", "liters", "litre", "litres")
	addUnit("volume", 3.785411784, "gal", "gallon", "gallons")
	addUnit("volume", 0.946352946, "qt", "quart", "quarts")
	addUnit("volume", 0.473176473, "pt", "pint", "pints")
	addUnit("volume", 0.2365882365, "cup", "cups")
	addUnit("volume", 0.0295735295625, "fl oz", "fluid ounce", "fluid ounces")
	addUnit("volume", 0.01478676478125, "tbsp", "tablespoon", "tablespoons")
	addUnit("volume", 0.00492892159375, "tsp", "teaspoon", "teaspoons")
	// speed (base m/s)
	addUnit("speed", 1, "m/s", "mps")
	addUnit("speed", 1/3.6, "km/h", "kmh", "kph", "kmph")
	addUnit("speed", 0.44704, "mph")
	addUnit("speed", 0.514444, "kn", "knot", "knots")
	// data (base byte)
	addUnit("data", 1, "B", "byte", "bytes")
	addUnit("data", 1e3, "KB", "kb", "kilobyte", "kilobytes")
	addUnit("data", 1e6, "MB", "mb", "megabyte", "megabytes")
	addUnit("data", 1e9, "GB", "gb", "gigabyte", "gigabytes")
	addUnit("data", 1e12, "TB", "tb", "terabyte", "terabytes")
	addUnit("data", 1024, "KiB", "kib")
	addUnit("data", 1024*1024, "MiB", "mib")
	addUnit("data", 1024*1024*1024, "GiB", "gib")
	addUnit("data", 1024*1024*1024*1024, "TiB", "tib")
	// time (base second)
	addUnit("time", 1, "s", "sec", "second", "seconds")
	addUnit("time", 60, "min", "minute", "minutes", "mins")
	addUnit("time", 3600, "h", "hr", "hour", "hours", "hrs")
	addUnit("time", 86400, "day", "days", "d")
	addUnit("time", 604800, "week", "weeks", "wk")
	// area (base m²)
	addUnit("area", 1, "m²", "m2", "sqm", "square meter", "square meters", "square metre", "square metres")
	addUnit("area", 1e6, "km²", "km2", "square kilometer", "square kilometers")
	addUnit("area", 1e4, "ha", "hectare", "hectares")
	addUnit("area", 4046.8564224, "acre", "acres")
	addUnit("area", 0.09290304, "ft²", "ft2", "sqft", "square foot", "square feet")
	// energy & power
	addUnit("energy", 4184, "kcal", "kilocalorie", "kilocalories", "calories", "cal")
	addUnit("energy", 1000, "kJ", "kj", "kilojoule", "kilojoules")
	addUnit("energy", 3.6e6, "kWh", "kwh")
	addUnit("power", 1000, "kW", "kw", "kilowatt", "kilowatts")
	addUnit("power", 745.699872, "hp", "horsepower", "ks")
	addUnit("power", 1, "W", "w", "watt", "watts")
	// temperature (special)
	for _, a := range []string{"c", "°c", "celsius", "degc", "degrees c", "degrees celsius"} {
		units[a] = unit{"temp", 0, "°C"}
	}
	for _, a := range []string{"f", "°f", "fahrenheit", "degf", "degrees f", "degrees fahrenheit"} {
		units[a] = unit{"temp", 1, "°F"}
	}
	for _, a := range []string{"k", "kelvin"} {
		units[a] = unit{"temp", 2, "K"}
	}
}

var reConvert = regexp.MustCompile(`^\s*(-?[0-9]+(?:[.,][0-9]+)?)\s*([a-z°²"'/ ]+?)\s+(?:in|to|into|as|=|->|→)\s+([a-z°²"'/ ]+?)\s*\??\s*$`)

func lookupUnit(s string) (unit, bool) {
	s = strings.TrimSpace(s)
	if u, ok := units[s]; ok {
		return u, true
	}
	u, ok := units[strings.TrimSuffix(s, "s")]
	return u, ok
}

// Convert answers "5 km in miles", "100 f to c", "2 cups in ml".
func Convert(q string) *Answer {
	m := reConvert.FindStringSubmatch(strings.ToLower(q))
	if m == nil {
		return nil
	}
	v, err := num(m[1])
	if err != nil {
		return nil
	}
	from, ok1 := lookupUnit(m[2])
	to, ok2 := lookupUnit(m[3])
	if !ok1 || !ok2 || from.kind != to.kind {
		return nil
	}
	var out float64
	if from.kind == "temp" {
		c := v
		switch from.factor {
		case 1:
			c = (v - 32) * 5 / 9
		case 2:
			c = v - 273.15
		}
		switch to.factor {
		case 0:
			out = c
		case 1:
			out = c*9/5 + 32
		case 2:
			out = c + 273.15
		}
	} else {
		out = v * from.factor / to.factor
	}
	a := &Answer{Kind: "convert", Title: fmtNum(v) + " " + from.name + " in " + to.name, Value: fmtNum(round(out, 6)) + " " + to.name}
	if from.kind != "temp" {
		a.Detail = "1 " + from.name + " = " + fmtNum(round(from.factor/to.factor, 6)) + " " + to.name
	}
	return a
}

func round(v float64, digits int) float64 {
	p := 1.0
	for i := 0; i < digits; i++ {
		p *= 10
	}
	if (v > 1e-3 || v < -1e-3) && v < 1e9 && v > -1e9 {
		return float64(int64(v*p+copysign(0.5, v))) / p
	}
	return v
}

func copysign(a, b float64) float64 {
	if b < 0 {
		return -a
	}
	return a
}
