package instant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCalc(t *testing.T) {
	for q, want := range map[string]string{
		"15% of 240": "36", "(3+4)*2^3": "56", "120 / 8": "15", "200 + 10%": "220", "100 - 25%": "75",
		"2^10": "1024", "1234567 * 2": "2 469 134", "sqrt(16)": "4", "10 / 4": "2.5", "= 3,5 * 2": "7", "what is 7*6": "42",
	} {
		a := Calc(q)
		if a == nil || a.Value != want {
			t.Errorf("Calc(%q) = %+v, want %s", q, a, want)
		}
	}
	for _, q := range []string{"nikola tesla", "2026-11-01", "-5", "iphone 15", "covid-19", "1/0", "5 km in miles", "top 10 movies"} {
		if a := Calc(q); a != nil {
			t.Errorf("Calc(%q) = %+v, want nothing", q, a)
		}
	}
}

func TestConvert(t *testing.T) {
	for q, want := range map[string]string{
		"5 km in miles": "3.106856 mi", "100 f to c": "37.777778 °C", "2 cups in ml": "473.176473 ml", "1 GB to MB": "1000 MB",
		"60 mph to km/h": "96.56064 km/h", "10 kg in lbs": "22.046226 lb", "0 c to f": "32 °F",
	} {
		a := Convert(q)
		if a == nil || a.Value != want {
			t.Errorf("Convert(%q) = %+v, want %s", q, a, want)
		}
	}
	if a := Convert("5 km in kg"); a != nil {
		t.Errorf("km to kg converted: %+v", a)
	}
}

func TestTimeIn(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	a := TimeIn("time in tokyo", now)
	if a == nil || a.Value != "21:00" || !strings.Contains(a.Detail, "Sunday") {
		t.Fatalf("tokyo = %+v", a)
	}
	if a := TimeIn("new york time", now); a == nil || a.Value != "07:00" {
		t.Fatalf("new york = %+v", a)
	}
	if a := TimeIn("bedtime stories", now); a != nil {
		t.Fatalf("bedtime = %+v", a)
	}
}

func TestCurrencyAndWeather(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rates":
			_, _ = w.Write([]byte(`{"result":"success","rates":{"EUR":1,"USD":1.1,"RSD":117}}`))
		case "/geo":
			_, _ = w.Write([]byte(`{"results":[{"name":"Sarajevo","country":"Bosnia and Herzegovina","latitude":43.85,"longitude":18.36}]}`))
		case "/wx":
			_, _ = w.Write([]byte(`{"current":{"temperature_2m":7.4,"apparent_temperature":5,"weather_code":3,"wind_speed_10m":9,"relative_humidity_2m":81},
				"daily":{"time":["2026-11-01","2026-11-02"],"weather_code":[3,61],"temperature_2m_max":[11,9],"temperature_2m_min":[2,4],"precipitation_probability_max":[5,80]}}`))
		}
	}))
	defer srv.Close()
	o := NewOnline("test")
	o.RatesURL, o.GeoURL, o.WeatherURL = srv.URL+"/rates", srv.URL+"/geo", srv.URL+"/wx"
	ctx := context.Background()
	if a := o.Currency(ctx, "100 eur to bam"); a == nil || a.Value != "195.58 BAM" {
		t.Fatalf("eur→bam = %+v", a)
	}
	if a := o.Currency(ctx, "50 $ in km"); a == nil || a.Value != "88.9014 BAM" {
		t.Fatalf("usd→bam = %+v", a)
	}
	if a := o.Currency(ctx, "how to cook rice"); a != nil {
		t.Fatalf("rice = %+v", a)
	}
	a := o.Weather(ctx, "weather sarajevo")
	if a == nil || a.Value != "7 °C · Overcast" || len(a.Rows) != 2 || !strings.Contains(a.Rows[1].Value, "rain 80%") {
		t.Fatalf("weather = %+v", a)
	}
	if a := o.Answer(ctx, "15% of 240", time.Now()); a == nil || a.Kind != "calc" {
		t.Fatalf("answer = %+v", a)
	}
}
