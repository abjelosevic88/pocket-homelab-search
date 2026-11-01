package instant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Online answers what needs the outside world: currency rates, weather.
// One per server; it keeps rates six hours and weather fifteen minutes.
type Online struct {
	HTTP *http.Client
	UA   string
	// base URLs, so tests can point them at a fake
	RatesURL, GeoURL, WeatherURL string

	mu      sync.Mutex
	rates   map[string]float64 // per 1 EUR
	ratesAt time.Time
	weather map[string]cachedWeather
}

type cachedWeather struct {
	a  *Answer
	at time.Time
}

// NewOnline returns an Online with the public endpoints.
func NewOnline(userAgent string) *Online {
	return &Online{HTTP: &http.Client{Timeout: 4 * time.Second}, UA: userAgent,
		RatesURL: "https://open.er-api.com/v6/latest/EUR", GeoURL: "https://geocoding-api.open-meteo.com/v1/search",
		WeatherURL: "https://api.open-meteo.com/v1/forecast", weather: map[string]cachedWeather{}}
}

func (o *Online) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", o.UA)
	req.Header.Set("Accept", "application/json")
	res, err := o.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(v)
}

// ---- currency ----

var symbols = map[string]string{"€": "EUR", "$": "USD", "£": "GBP", "¥": "JPY", "km": "BAM", "km.": "BAM", "din": "RSD", "dinar": "RSD", "dinara": "RSD",
	"euro": "EUR", "euros": "EUR", "eur": "EUR", "dollar": "USD", "dollars": "USD", "pound": "GBP", "pounds": "GBP", "franc": "CHF", "francs": "CHF",
	"kuna": "HRK", "marka": "BAM", "marke": "BAM", "maraka": "BAM", "bam": "BAM", "konvertibilna marka": "BAM", "kn": "HRK", "zł": "PLN", "zloty": "PLN",
	"forint": "HUF", "ft": "HUF", "lira": "TRY", "rub": "RUB", "ruble": "RUB", "rubles": "RUB", "yen": "JPY", "yuan": "CNY", "rmb": "CNY", "btc": "BTC"}

var reCurrency = regexp.MustCompile(`^\s*([0-9]+(?:[.,][0-9]+)?)?\s*([a-z€$£¥.ł ]{1,22}?)\s*(?:in|to|into|=|->|→|u)\s+([a-z€$£¥.ł ]{1,22}?)\s*\??\s*$`)

func currencyCode(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if c, ok := symbols[s]; ok {
		return c
	}
	if len(s) == 3 && s[0] >= 'a' && s[0] <= 'z' {
		return strings.ToUpper(s)
	}
	return ""
}

func (o *Online) loadRates(ctx context.Context) (map[string]float64, error) {
	o.mu.Lock()
	if o.rates != nil && time.Since(o.ratesAt) < 6*time.Hour {
		r := o.rates
		o.mu.Unlock()
		return r, nil
	}
	o.mu.Unlock()
	var raw struct {
		Result string             `json:"result"`
		Rates  map[string]float64 `json:"rates"`
	}
	if err := o.getJSON(ctx, o.RatesURL, &raw); err != nil {
		return nil, err
	}
	if len(raw.Rates) == 0 {
		return nil, fmt.Errorf("no rates")
	}
	// the convertible mark is pegged to the euro
	raw.Rates["BAM"] = 1.95583
	raw.Rates["EUR"] = 1
	o.mu.Lock()
	o.rates, o.ratesAt = raw.Rates, time.Now()
	o.mu.Unlock()
	return raw.Rates, nil
}

// Currency answers "100 eur to bam", "50 $ in km", "usd to rsd".
func (o *Online) Currency(ctx context.Context, q string) *Answer {
	m := reCurrency.FindStringSubmatch(strings.ToLower(q))
	if m == nil {
		return nil
	}
	from, to := currencyCode(m[2]), currencyCode(m[3])
	if from == "" || to == "" || from == to {
		return nil
	}
	rates, err := o.loadRates(ctx)
	if err != nil {
		return nil
	}
	rf, ok1 := rates[from]
	rt, ok2 := rates[to]
	if !ok1 || !ok2 || rf == 0 {
		return nil
	}
	amount := 1.0
	if m[1] != "" {
		if v, err := num(m[1]); err == nil {
			amount = v
		}
	}
	out := amount / rf * rt
	return &Answer{Kind: "currency", Title: fmtNum(amount) + " " + from + " in " + to, Value: money(out) + " " + to,
		Detail: "1 " + from + " = " + money4(rt/rf) + " " + to + " · 1 " + to + " = " + money4(rf/rt) + " " + from,
		Source: "Exchange rates via open.er-api.com, updated daily"}
}

func money(v float64) string {
	if v >= 100 {
		return fmtNum(round(v, 2))
	}
	return fmtNum(round(v, 4))
}

func money4(v float64) string { return fmtNum(round(v, 4)) }

// ---- the time in a city ----

var reTimeIn = regexp.MustCompile(`^\s*(?:what(?:'s| is)?\s+(?:the\s+)?)?(?:current\s+)?time\s+(?:in|at)\s+(.+?)\s*\??\s*$|^\s*(.+?)\s+time\s*\??\s*$|^\s*koliko\s+je\s+sati\s+u\s+(.+?)\s*\??\s*$`)

// cities people ask about, by name → IANA zone (any "Region/City" name works too).
var cities = map[string]string{
	"sarajevo": "Europe/Sarajevo", "banja luka": "Europe/Sarajevo", "mostar": "Europe/Sarajevo", "tuzla": "Europe/Sarajevo", "zenica": "Europe/Sarajevo",
	"belgrade": "Europe/Belgrade", "beograd": "Europe/Belgrade", "novi sad": "Europe/Belgrade", "nis": "Europe/Belgrade", "zagreb": "Europe/Zagreb",
	"split": "Europe/Zagreb", "ljubljana": "Europe/Ljubljana", "podgorica": "Europe/Podgorica", "skopje": "Europe/Skopje", "vienna": "Europe/Vienna",
	"wien": "Europe/Vienna", "berlin": "Europe/Berlin", "munich": "Europe/Berlin", "münchen": "Europe/Berlin", "frankfurt": "Europe/Berlin",
	"london": "Europe/London", "paris": "Europe/Paris", "rome": "Europe/Rome", "milan": "Europe/Rome", "madrid": "Europe/Madrid", "barcelona": "Europe/Madrid",
	"lisbon": "Europe/Lisbon", "amsterdam": "Europe/Amsterdam", "brussels": "Europe/Brussels", "zurich": "Europe/Zurich", "zürich": "Europe/Zurich",
	"stockholm": "Europe/Stockholm", "oslo": "Europe/Oslo", "copenhagen": "Europe/Copenhagen", "helsinki": "Europe/Helsinki", "warsaw": "Europe/Warsaw",
	"prague": "Europe/Prague", "budapest": "Europe/Budapest", "athens": "Europe/Athens", "istanbul": "Europe/Istanbul", "moscow": "Europe/Moscow",
	"kyiv": "Europe/Kyiv", "kiev": "Europe/Kyiv", "dublin": "Europe/Dublin", "bucharest": "Europe/Bucharest", "sofia": "Europe/Sofia",
	"new york": "America/New_York", "nyc": "America/New_York", "boston": "America/New_York", "washington": "America/New_York", "miami": "America/New_York",
	"chicago": "America/Chicago", "dallas": "America/Chicago", "houston": "America/Chicago", "denver": "America/Denver", "phoenix": "America/Phoenix",
	"los angeles": "America/Los_Angeles", "la": "America/Los_Angeles", "san francisco": "America/Los_Angeles", "seattle": "America/Los_Angeles",
	"toronto": "America/Toronto", "vancouver": "America/Vancouver", "mexico city": "America/Mexico_City", "sao paulo": "America/Sao_Paulo",
	"são paulo": "America/Sao_Paulo", "buenos aires": "America/Argentina/Buenos_Aires", "tokyo": "Asia/Tokyo", "seoul": "Asia/Seoul",
	"beijing": "Asia/Shanghai", "shanghai": "Asia/Shanghai", "hong kong": "Asia/Hong_Kong", "singapore": "Asia/Singapore", "bangkok": "Asia/Bangkok",
	"delhi": "Asia/Kolkata", "new delhi": "Asia/Kolkata", "mumbai": "Asia/Kolkata", "bangalore": "Asia/Kolkata", "dubai": "Asia/Dubai",
	"abu dhabi": "Asia/Dubai", "doha": "Asia/Qatar", "tel aviv": "Asia/Jerusalem", "jerusalem": "Asia/Jerusalem", "cairo": "Africa/Cairo",
	"johannesburg": "Africa/Johannesburg", "nairobi": "Africa/Nairobi", "lagos": "Africa/Lagos", "sydney": "Australia/Sydney",
	"melbourne": "Australia/Melbourne", "brisbane": "Australia/Brisbane", "perth": "Australia/Perth", "auckland": "Pacific/Auckland",
	"honolulu": "Pacific/Honolulu", "hawaii": "Pacific/Honolulu", "utc": "UTC", "gmt": "UTC",
}

// TimeIn answers "time in tokyo", "new york time", "koliko je sati u beču".
func TimeIn(q string, now time.Time) *Answer {
	m := reTimeIn.FindStringSubmatch(strings.ToLower(q))
	if m == nil {
		return nil
	}
	place := strings.TrimSpace(m[1] + m[2] + m[3])
	zone, ok := cities[place]
	if !ok {
		// "Region/City" names and their last part ("tokyo" → Asia/Tokyo is above; "lima" → America/Lima)
		zone = guessZone(place)
	}
	if zone == "" {
		return nil
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil
	}
	t := now.In(loc)
	diff := zoneOffset(t) - zoneOffset(now)
	rel := "same time as here"
	if diff != 0 {
		h := float64(diff) / 3600
		rel = fmt.Sprintf("%+g h from here", h)
	}
	name := strings.ToUpper(place[:1]) + place[1:]
	return &Answer{Kind: "time", Title: "Time in " + name, Value: t.Format("15:04"), Detail: t.Format("Monday, 2 January") + " · " + t.Format("MST") + " · " + rel}
}

func zoneOffset(t time.Time) int { _, off := t.Zone(); return off }

var zoneNames = []string{"America/Lima", "America/Bogota", "America/Santiago", "America/Caracas", "America/Havana", "America/Anchorage",
	"Asia/Manila", "Asia/Jakarta", "Asia/Karachi", "Asia/Tehran", "Asia/Riyadh", "Asia/Baghdad", "Asia/Kathmandu", "Asia/Dhaka",
	"Asia/Ho_Chi_Minh", "Asia/Kuala_Lumpur", "Asia/Taipei", "Africa/Casablanca", "Africa/Algiers", "Africa/Tunis", "Atlantic/Reykjavik",
	"Europe/Riga", "Europe/Vilnius", "Europe/Tallinn", "Europe/Minsk", "Europe/Bratislava", "Europe/Luxembourg", "Europe/Monaco", "Europe/Malta"}

func guessZone(place string) string {
	if strings.Contains(place, "/") {
		return place
	}
	key := strings.ReplaceAll(place, " ", "_")
	for _, z := range zoneNames {
		if strings.EqualFold(z[strings.LastIndex(z, "/")+1:], key) {
			return z
		}
	}
	return ""
}

// ---- weather ----

var reWeather = regexp.MustCompile(`^\s*(?:weather|forecast|vrijeme|prognoza)(?:\s+(?:in|for|u|za))?\s+(.+?)\s*\??\s*$|^\s*(.+?)\s+(?:weather|forecast|vrijeme|prognoza)\s*\??\s*$`)

// Weather answers "weather sarajevo", "vienna forecast" with today and the next days.
func (o *Online) Weather(ctx context.Context, q string) *Answer {
	m := reWeather.FindStringSubmatch(strings.ToLower(q))
	if m == nil {
		return nil
	}
	place := strings.TrimSpace(m[1] + m[2])
	if place == "" || len(place) > 60 {
		return nil
	}
	o.mu.Lock()
	if c, ok := o.weather[place]; ok && time.Since(c.at) < 15*time.Minute {
		o.mu.Unlock()
		return c.a
	}
	o.mu.Unlock()
	var geo struct {
		Results []struct {
			Name      string  `json:"name"`
			Country   string  `json:"country"`
			Admin1    string  `json:"admin1"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"results"`
	}
	if err := o.getJSON(ctx, o.GeoURL+"?count=1&language=en&name="+url.QueryEscape(place), &geo); err != nil || len(geo.Results) == 0 {
		return nil
	}
	g := geo.Results[0]
	var w struct {
		Current struct {
			Temp     float64 `json:"temperature_2m"`
			Feels    float64 `json:"apparent_temperature"`
			Code     int     `json:"weather_code"`
			Wind     float64 `json:"wind_speed_10m"`
			Humidity float64 `json:"relative_humidity_2m"`
		} `json:"current"`
		Daily struct {
			Time []string  `json:"time"`
			Code []int     `json:"weather_code"`
			Max  []float64 `json:"temperature_2m_max"`
			Min  []float64 `json:"temperature_2m_min"`
			Rain []float64 `json:"precipitation_probability_max"`
		} `json:"daily"`
	}
	v := url.Values{"latitude": {fmt.Sprint(g.Latitude)}, "longitude": {fmt.Sprint(g.Longitude)}, "timezone": {"auto"}, "forecast_days": {"4"},
		"current": {"temperature_2m,apparent_temperature,weather_code,wind_speed_10m,relative_humidity_2m"},
		"daily":   {"weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max"}}
	if err := o.getJSON(ctx, o.WeatherURL+"?"+v.Encode(), &w); err != nil {
		return nil
	}
	where := g.Name
	if g.Country != "" {
		where += ", " + g.Country
	}
	a := &Answer{Kind: "weather", Title: "Weather in " + where, Value: fmt.Sprintf("%.0f °C · %s", w.Current.Temp, weatherText(w.Current.Code)),
		Detail: fmt.Sprintf("Feels like %.0f °C · wind %.0f km/h · humidity %.0f%%", w.Current.Feels, w.Current.Wind, w.Current.Humidity),
		Source: "Open-Meteo", URL: fmt.Sprintf("https://open-meteo.com/en/docs#latitude=%.4f&longitude=%.4f", g.Latitude, g.Longitude)}
	for i, d := range w.Daily.Time {
		if i >= len(w.Daily.Code) || i >= len(w.Daily.Max) || i >= len(w.Daily.Min) {
			break
		}
		day := d
		if t, err := time.Parse("2006-01-02", d); err == nil {
			day = t.Format("Mon 2 Jan")
			if i == 0 {
				day = "Today"
			}
		}
		rain := ""
		if i < len(w.Daily.Rain) && w.Daily.Rain[i] >= 20 {
			rain = fmt.Sprintf(" · rain %.0f%%", w.Daily.Rain[i])
		}
		a.Rows = append(a.Rows, Row{Label: day, Value: fmt.Sprintf("%s · %.0f° / %.0f°%s", weatherText(w.Daily.Code[i]), w.Daily.Max[i], w.Daily.Min[i], rain)})
	}
	o.mu.Lock()
	o.weather[place] = cachedWeather{a, time.Now()}
	if len(o.weather) > 200 {
		keys := make([]string, 0, len(o.weather))
		for k := range o.weather {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys[:100] {
			delete(o.weather, k)
		}
	}
	o.mu.Unlock()
	return a
}

// weatherText is a WMO weather code in words.
func weatherText(code int) string {
	switch {
	case code == 0:
		return "Clear"
	case code <= 2:
		return "Partly cloudy"
	case code == 3:
		return "Overcast"
	case code == 45 || code == 48:
		return "Fog"
	case code >= 51 && code <= 57:
		return "Drizzle"
	case code >= 61 && code <= 67:
		return "Rain"
	case code >= 71 && code <= 77:
		return "Snow"
	case code >= 80 && code <= 82:
		return "Showers"
	case code == 85 || code == 86:
		return "Snow showers"
	case code >= 95:
		return "Thunderstorm"
	}
	return "—"
}

// Answer tries every kind, the ones that need no network first.
func (o *Online) Answer(ctx context.Context, q string, now time.Time) *Answer {
	if a := Convert(q); a != nil {
		return a
	}
	if a := Calc(q); a != nil {
		return a
	}
	if a := TimeIn(q, now); a != nil {
		return a
	}
	if a := o.Currency(ctx, q); a != nil {
		return a
	}
	return o.Weather(ctx, q)
}
