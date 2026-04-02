package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ─── ANSI ─────────────────────────────────────────────────────────────────────

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiBlue   = "\033[34m"
	ansiCyan   = "\033[36m"
)

func bold(s string) string   { return ansiBold + s + ansiReset }
func dim(s string) string    { return ansiDim + s + ansiReset }
func colorRed(s string) string    { return ansiRed + s + ansiReset }
func colorBlue(s string) string   { return ansiBlue + s + ansiReset }
func colorCyan(s string) string   { return ansiCyan + s + ansiReset }
func colorYellow(s string) string { return ansiYellow + s + ansiReset }

// ─── Location ─────────────────────────────────────────────────────────────────

type location struct {
	lat, lon float64
	city     string
}

func resolveLocation(city string) (location, error) {
	if city != "" {
		return geocodeCity(city)
	}
	return geolocateIP()
}

func geolocateIP() (location, error) {
	resp, err := http.Get("http://ip-api.com/json/?fields=status,city,regionName,lat,lon")
	if err != nil {
		return location{}, err
	}
	defer resp.Body.Close()
	var d struct {
		Status     string  `json:"status"`
		City       string  `json:"city"`
		RegionName string  `json:"regionName"`
		Lat        float64 `json:"lat"`
		Lon        float64 `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return location{}, err
	}
	if d.Status != "success" {
		return location{}, fmt.Errorf("ip-api: %s", d.Status)
	}
	return location{lat: d.Lat, lon: d.Lon, city: d.City + ", " + d.RegionName}, nil
}

func geocodeCity(name string) (location, error) {
	u := "https://geocoding-api.open-meteo.com/v1/search?name=" + url.QueryEscape(name) + "&count=1&language=en&format=json"
	resp, err := http.Get(u)
	if err != nil {
		return location{}, err
	}
	defer resp.Body.Close()
	var d struct {
		Results []struct {
			Lat    float64 `json:"latitude"`
			Lon    float64 `json:"longitude"`
			Name   string  `json:"name"`
			Admin1 string  `json:"admin1"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return location{}, err
	}
	if len(d.Results) == 0 {
		return location{}, fmt.Errorf("city not found: %s", name)
	}
	r := d.Results[0]
	label := r.Name
	if r.Admin1 != "" {
		label += ", " + r.Admin1
	}
	return location{lat: r.Lat, lon: r.Lon, city: label}, nil
}

// ─── Forecast ─────────────────────────────────────────────────────────────────

type forecast struct {
	Current struct {
		Temp      float64 `json:"temperature_2m"`
		FeelsLike float64 `json:"apparent_temperature"`
		Code      int     `json:"weather_code"`
		Humidity  int     `json:"relative_humidity_2m"`
		WindSpeed float64 `json:"wind_speed_10m"`
		WindDir   float64 `json:"wind_direction_10m"`
		Rain      float64 `json:"rain"`
		Snowfall  float64 `json:"snowfall"`
	} `json:"current"`
	Hourly struct {
		Time       []string  `json:"time"`
		Temp       []float64 `json:"temperature_2m"`
		RainChance []int     `json:"precipitation_probability"`
		Code       []int     `json:"weather_code"`
	} `json:"hourly"`
	Daily struct {
		Time       []string  `json:"time"`
		TempMax    []float64 `json:"temperature_2m_max"`
		TempMin    []float64 `json:"temperature_2m_min"`
		Code       []int     `json:"weather_code"`
		RainChance []int     `json:"precipitation_probability_max"`
	} `json:"daily"`
}

func fetchForecast(lat, lon float64) (*forecast, error) {
	p := url.Values{
		"latitude":         {fmt.Sprintf("%f", lat)},
		"longitude":        {fmt.Sprintf("%f", lon)},
		"current":          {"temperature_2m,apparent_temperature,weather_code,relative_humidity_2m,wind_speed_10m,wind_direction_10m,rain,snowfall"},
		"hourly":           {"temperature_2m,precipitation_probability,weather_code"},
		"daily":            {"temperature_2m_max,temperature_2m_min,weather_code,precipitation_probability_max"},
		"temperature_unit": {"fahrenheit"},
		"wind_speed_unit":  {"mph"},
		"forecast_days":    {"5"},
		"timezone":         {"auto"},
	}
	resp, err := http.Get("https://api.open-meteo.com/v1/forecast?" + p.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var f forecast
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

// ─── Conditions ───────────────────────────────────────────────────────────────

type conditions struct {
	City       string `json:"city"`
	TempF      int    `json:"temp_f"`
	FeelsLike  int    `json:"feels_like_f"`
	Condition  string `json:"condition"`
	Code       int    `json:"code"`
	IsHot      bool   `json:"is_hot"`
	IsCold     bool   `json:"is_cold"`
	IsRainy    bool   `json:"is_rainy"`
	IsSnowy    bool   `json:"is_snowy"`
	IsStormy   bool   `json:"is_stormy"`
	RainChance int    `json:"rain_chance_pct"`
	WindMph    int    `json:"wind_mph"`
	Humidity   int    `json:"humidity_pct"`
}

func buildConditions(loc location, f *forecast) conditions {
	cur := f.Current
	temp := int(math.Round(cur.Temp))

	rainChance := 0
	now := time.Now()
	for i, t := range f.Hourly.Time {
		if parsed, err := time.Parse("2006-01-02T15:04", t); err == nil && !parsed.Before(now) {
			if i < len(f.Hourly.RainChance) {
				rainChance = f.Hourly.RainChance[i]
			}
			break
		}
	}

	return conditions{
		City:       loc.city,
		TempF:      temp,
		FeelsLike:  int(math.Round(cur.FeelsLike)),
		Condition:  codeDesc(cur.Code),
		Code:       cur.Code,
		IsHot:      temp >= 85,
		IsCold:     temp <= 40,
		IsRainy:    cur.Rain > 0 || rainChance >= 50,
		IsSnowy:    cur.Snowfall > 0,
		IsStormy:   cur.Code >= 95,
		RainChance: rainChance,
		WindMph:    int(math.Round(cur.WindSpeed)),
		Humidity:   cur.Humidity,
	}
}

// ─── BUSD ─────────────────────────────────────────────────────────────────────

// busdPublish fires a weather.conditions event on the gl1tch bus so signal
// handlers (e.g. "companion") can react to the current conditions.
// Silently no-ops if the bus socket is unavailable.
func busdPublish(c conditions) {
	sock, err := busdSock()
	if err != nil {
		return
	}
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()

	reg, _ := json.Marshal(map[string]any{"name": "gl1tch-weather", "subscribe": []string{}})
	fmt.Fprintf(conn, "%s\n", reg)

	payload, _ := json.Marshal(c)
	frame, _ := json.Marshal(map[string]any{
		"action":  "publish",
		"event":   "weather.conditions",
		"payload": json.RawMessage(payload),
	})
	fmt.Fprintf(conn, "%s\n", frame)
}

func busdSock() (string, error) {
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		return filepath.Join(xdg, "glitch", "bus.sock"), nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "glitch", "bus.sock"), nil
}

// ─── Render ───────────────────────────────────────────────────────────────────

func render(loc location, f *forecast) {
	cur := f.Current
	temp := int(math.Round(cur.Temp))
	feels := int(math.Round(cur.FeelsLike))

	fmt.Println()
	fmt.Printf("%s %s\n\n",
		bold("▸ "+loc.city),
		dim(fmt.Sprintf("%.4f, %.4f", loc.lat, loc.lon)),
	)

	tempStr := fmt.Sprintf("%d°F", temp)
	switch {
	case temp >= 85:
		tempStr = colorRed(tempStr)
	case temp <= 40:
		tempStr = colorBlue(tempStr)
	default:
		tempStr = bold(tempStr)
	}

	extra := ""
	if cur.Rain > 0 {
		extra += "  " + colorCyan(fmt.Sprintf("rain %.1fmm", cur.Rain))
	}
	if cur.Snowfall > 0 {
		extra += "  " + colorBlue(fmt.Sprintf("snow %.1fcm", cur.Snowfall))
	}

	fmt.Printf("  %s %s  feels %d°F  %s  %d%% humidity%s\n",
		codeIcon(cur.Code), tempStr, feels,
		dim(codeDesc(cur.Code)),
		cur.Humidity, extra,
	)
	fmt.Printf("  %s %d mph %s\n\n",
		dim("wind"),
		int(math.Round(cur.WindSpeed)),
		windDir(cur.WindDir),
	)

	// 12h strip
	now := time.Now()
	start := -1
	for i, t := range f.Hourly.Time {
		if parsed, err := time.Parse("2006-01-02T15:04", t); err == nil && !parsed.Before(now) {
			start = i
			break
		}
	}
	if start >= 0 {
		fmt.Printf("  %s\n", bold("next 12h"))
		end := start + 12
		if end > len(f.Hourly.Time) {
			end = len(f.Hourly.Time)
		}
		for i := start; i < end; i++ {
			t, _ := time.Parse("2006-01-02T15:04", f.Hourly.Time[i])
			rain := f.Hourly.RainChance[i]
			rStr := dim("  —")
			if rain >= 50 {
				rStr = colorCyan(fmt.Sprintf("%3d%%", rain))
			} else if rain >= 20 {
				rStr = colorYellow(fmt.Sprintf("%3d%%", rain))
			}
			fmt.Printf("  %5s  %3d°F  %s  %s\n",
				t.Format("3PM"),
				int(math.Round(f.Hourly.Temp[i])),
				codeIcon(f.Hourly.Code[i]),
				rStr,
			)
		}
		fmt.Println()
	}

	// 5-day
	fmt.Printf("  %s\n", bold("5-day"))
	for i, d := range f.Daily.Time {
		date, _ := time.Parse("2006-01-02", d)
		rain := f.Daily.RainChance[i]
		rStr := dim("  — ")
		if rain >= 50 {
			rStr = colorCyan(fmt.Sprintf(" %2d%%", rain))
		} else if rain >= 20 {
			rStr = colorYellow(fmt.Sprintf(" %2d%%", rain))
		}
		fmt.Printf("  %s  %3d/%3d°F  %s %s\n",
			date.Format("Mon"),
			int(math.Round(f.Daily.TempMin[i])),
			int(math.Round(f.Daily.TempMax[i])),
			codeIcon(f.Daily.Code[i]),
			rStr,
		)
	}
	fmt.Println()
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func codeIcon(code int) string {
	switch {
	case code == 0:
		return "☀ "
	case code <= 3:
		return "⛅"
	case code <= 49:
		return "☁ "
	case code <= 67:
		return "🌧"
	case code <= 79:
		return "❄ "
	case code <= 82:
		return "🌦"
	case code <= 99:
		return "⛈ "
	default:
		return "? "
	}
}

func codeDesc(code int) string {
	switch {
	case code == 0:
		return "clear"
	case code == 1:
		return "mostly clear"
	case code == 2:
		return "partly cloudy"
	case code == 3:
		return "overcast"
	case code <= 49:
		return "fog"
	case code <= 55:
		return "drizzle"
	case code <= 57:
		return "freezing drizzle"
	case code <= 65:
		return "rain"
	case code <= 67:
		return "freezing rain"
	case code <= 75:
		return "snow"
	case code == 77:
		return "snow grains"
	case code <= 82:
		return "rain showers"
	case code <= 86:
		return "snow showers"
	case code == 95:
		return "thunderstorm"
	case code <= 99:
		return "thunderstorm + hail"
	default:
		return "unknown"
	}
}

func windDir(deg float64) string {
	dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	return dirs[int(math.Round(deg/22.5))%16]
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	city := strings.Join(os.Args[1:], " ")

	loc, err := resolveLocation(city)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	f, err := fetchForecast(loc.lat, loc.lon)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	render(loc, f)
	busdPublish(buildConditions(loc, f))
}
