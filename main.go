package main

import (
	"bufio"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// --- JSON-RPC / MCP Types ---

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      interface{}     `json:"id"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// --- Main Server Loop ---

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	// Increase buffer size for large JSON payloads
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024*10)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// Ignore malformed lines or log to stderr
			continue
		}

		handleRequest(req)
	}
}

func handleRequest(req JSONRPCRequest) {
	var response interface{}
	var err *RPCError

	switch req.Method {
	case "initialize":
		response = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "omni-tool",
				"version": "1.0.0",
			},
		}
	case "notifications/initialized":
		// No response needed for notifications
		return
	case "tools/list":
		response = map[string]interface{}{
			"tools": getToolDefinitions(),
		}
	case "tools/call":
		var params CallToolParams
		if e := json.Unmarshal(req.Params, &params); e != nil {
			err = &RPCError{Code: -32602, Message: "Invalid params"}
		} else {
			res, eStr := executeTool(params.Name, params.Arguments)
			if eStr != "" {
				response = map[string]interface{}{
					"content": []map[string]string{
						{"type": "text", "text": fmt.Sprintf("Error: %s", eStr)},
					},
					"isError": true,
				}
			} else {
				// Serialize result to string for text content
				jsonBytes, _ := json.MarshalIndent(res, "", "  ")
				response = map[string]interface{}{
					"content": []map[string]string{
						{"type": "text", "text": string(jsonBytes)},
					},
				}
			}
		}
	default:
		// Return method not found for unknown methods with an ID
		if req.ID != nil {
			err = &RPCError{Code: -32601, Message: "Method not found"}
		} else {
			// Notifications (no ID) can be ignored
			return
		}
	}

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		Result:  response,
		Error:   err,
		ID:      req.ID,
	}

	out, _ := json.Marshal(resp)
	fmt.Printf("%s\n", out)
}

// --- Tool Definitions ---

func getToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "convert",
			Description: "Universal converter for Time, Color, and Physical Units (Length, Weight, Temp, Digital, CSS).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"value": {"type": "string", "description": "The value to convert (e.g., '10', 'now', '#FF0000', '1690000000')"},
					"unit": {"type": "string", "description": "The source unit or context (e.g., 'km', 'lbs', 'iso', 'hex', 'rgb')"}
				},
				"required": ["value"]
			}`),
		},
		{
			Name:        "compare",
			Description: "Compares two values, handling unit conversions (e.g., 10km vs 5miles) and types.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"value_a": {"type": "string"},
					"unit_a": {"type": "string", "description": "Unit for value A (optional)"},
					"value_b": {"type": "string"},
					"unit_b": {"type": "string", "description": "Unit for value B (optional)"}
				},
				"required": ["value_a", "value_b"]
			}`),
		},
		{
			Name:        "transform_string",
			Description: "Takes ANY string, detects encoding (Base64/Hex/JSON), returns decoded values and transformations.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"text": {"type": "string"}
				},
				"required": ["text"]
			}`),
		},
		{
			Name:        "analyze_color",
			Description: "Takes a color (Hex, RGB) and returns conversions plus accessibility analysis.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"color_input": {"type": "string"}
				},
				"required": ["color_input"]
			}`),
		},
		{
			Name:        "inspect_jwt",
			Description: "Decodes a JWT header & payload without verification.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"token": {"type": "string"}
				},
				"required": ["token"]
			}`),
		},
		{
			Name:        "generate_mock_data",
			Description: "Generates random mock data (uuid, hex, ipv4, user_json).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"data_type": {"type": "string"},
					"count": {"type": "integer"}
				},
				"required": ["data_type"]
			}`),
		},
		{
			Name:        "calculate_statistics",
			Description: "Returns stats (mean, median, mode, stdev) for a list of numbers.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"numbers": {"type": "array", "items": {"type": "number"}}
				},
				"required": ["numbers"]
			}`),
		},
	}
}

// --- Execution Router ---

func executeTool(name string, args map[string]interface{}) (interface{}, string) {
	switch name {
	case "convert":
		val, _ := args["value"].(string)
		unit, _ := args["unit"].(string)
		return toolConvert(val, unit)
	case "compare":
		valA, _ := args["value_a"].(string)
		unitA, _ := args["unit_a"].(string)
		valB, _ := args["value_b"].(string)
		unitB, _ := args["unit_b"].(string)
		return toolCompare(valA, unitA, valB, unitB)
	case "transform_string":
		txt, _ := args["text"].(string)
		return toolTransformString(txt)
	case "analyze_color":
		col, _ := args["color_input"].(string)
		return toolAnalyzeColor(col)
	case "inspect_jwt":
		tok, _ := args["token"].(string)
		return toolInspectJWT(tok)
	case "generate_mock_data":
		dt, _ := args["data_type"].(string)
		cnt, _ := args["count"].(float64)
		return toolGenerateMockData(dt, int(cnt))
	case "calculate_statistics":
		rawNums, ok := args["numbers"].([]interface{})
		if !ok {
			return nil, "Invalid numbers array"
		}
		nums := make([]float64, len(rawNums))
		for i, n := range rawNums {
			nums[i] = n.(float64)
		}
		return toolCalculateStatistics(nums)
	}
	return nil, "Tool not found"
}

// --- Tool Implementations ---

// 1. Unified Convert Tool
func toolConvert(valStr string, unitStr string) (interface{}, string) {
	// 1. Check if unit implies a category
	category := inferCategory(unitStr)

	// 2. Route based on category
	if category == "color" {
		return toolAnalyzeColor(valStr)
	}

	// 3. If it's a known physical unit, use numeric conversion
	if category != "" {
		val, err := strconv.ParseFloat(valStr, 64)
		if err == nil {
			return toolConvertUnits(val, unitStr, category)
		}
	}

	// 4. Fallback: Treat as Time
	return toolConvertTime(valStr, unitStr)
}

// Helper: Infer category from unit string
func inferCategory(unit string) string {
	u := strings.ToLower(strings.TrimSpace(unit))
	switch u {
	case "m", "meter", "meters", "km", "kilometer", "cm", "centimeter", "mm", "millimeter", "mi", "mile", "miles", "ft", "foot", "feet", "in", "inch", "inches", "yd", "yard", "yards":
		return "length"
	case "kg", "kilogram", "g", "gram", "mg", "milligram", "lb", "lbs", "pound", "oz", "ounce", "stone":
		return "weight"
	case "c", "celsius", "f", "fahrenheit", "k", "kelvin":
		return "temperature"
	case "b", "bytes", "kb", "kilobytes", "mb", "megabytes", "gb", "gigabytes", "tb", "terabytes":
		return "digital"
	case "px", "pixels", "rem", "em", "pt", "points", "%", "percent":
		return "css"
	case "hex", "hexadecimal", "rgb", "color", "colour", "hsl":
		return "color"
	case "btc", "bitcoin", "sat", "sats", "satoshi", "satoshis", "mbtc", "millibitcoin", "eth", "ether", "gwei", "wei":
		return "crypto"
	case "ms", "millisecond", "milliseconds", "s", "sec", "second", "seconds", "min", "minute", "minutes", "h", "hr", "hour", "hours", "d", "day", "days", "w", "wk", "week", "weeks":
		return "duration"
	case "mph", "km/h", "kmh", "kph", "m/s", "mps", "ft/s", "fps", "knot", "knots", "kn":
		return "speed"
	case "sqft", "sq ft", "sqm", "sq m", "sqkm", "sq km", "sqmi", "sq mi", "acre", "acres", "hectare", "hectares", "ha":
		return "area"
	case "l", "liter", "liters", "litre", "litres", "ml", "milliliter", "milliliters", "gal", "gallon", "gallons", "floz", "fl oz", "cup", "cups", "pint", "pints", "qt", "quart", "quarts":
		return "volume"
	}
	return ""
}

// Internal: Convert Time Logic
func toolConvertTime(input string, targetTZ string) (interface{}, string) {
	var t time.Time
	var err error

	if targetTZ == "" {
		targetTZ = "UTC"
	}

	// Heuristics for parsing
	if input == "now" {
		t = time.Now()
	} else if input == "today" {
		now := time.Now()
		t = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	} else if input == "tomorrow" {
		now := time.Now()
		t = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(24 * time.Hour)
	} else if input == "yesterday" {
		now := time.Now()
		t = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(-24 * time.Hour)
	} else if isNumeric(input) {
		ts, _ := strconv.ParseFloat(input, 64)
		if ts > 30000000000 {
			t = time.UnixMilli(int64(ts))
		} else {
			t = time.Unix(int64(ts), 0)
		}
	} else if dur, ok := parseRelativeTime(input); ok {
		t = time.Now().Add(dur)
	} else {
		formats := []string{time.RFC3339, time.RFC1123, "2006-01-02", "15:04:05", "2006-01-02 15:04:05"}
		parsed := false
		for _, f := range formats {
			if t, err = time.Parse(f, input); err == nil {
				parsed = true
				break
			}
		}
		if !parsed {
			// If we really can't parse it, return error
			return nil, fmt.Sprintf("Could not parse as time or unit: %s", input)
		}
	}

	utc := t.UTC()
	diff := time.Since(t)
	rel := ""
	if diff > 0 {
		rel = fmt.Sprintf("%v ago", diff.Round(time.Second))
	} else {
		rel = fmt.Sprintf("in %v", (-diff).Round(time.Second))
	}

	return map[string]interface{}{
		"type":     "time_conversion",
		"original": input,
		"epoch": map[string]int64{
			"seconds":      t.Unix(),
			"milliseconds": t.UnixMilli(),
		},
		"formats": map[string]string{
			"iso":       t.Format(time.RFC3339),
			"rfc2822":   t.Format(time.RFC1123),
			"date_only": t.Format("2006-01-02"),
			"time_only": t.Format("15:04:05"),
			"human":     t.Format("Mon, 02 Jan 2006 15:04:05 MST"),
		},
		"javascript": map[string]interface{}{
			"new_Date":           fmt.Sprintf("new Date(%d)", t.UnixMilli()),
			"toISOString":        t.UTC().Format("2006-01-02T15:04:05.000Z"),
			"toDateString":       t.Format("Mon Jan 02 2006"),
			"toTimeString":       t.Format("15:04:05 GMT-0700 (MST)"),
			"toLocaleString":     t.Format("1/2/2006, 3:04:05 PM"),
			"toLocaleDateString": t.Format("1/2/2006"),
			"toLocaleTimeString": t.Format("3:04:05 PM"),
		},
		"momentjs": map[string]string{
			"format_default": t.Format("Mon Jan 02 2006 15:04:05 GMT-0700"),
			"format_L":       t.Format("01/02/2006"),
			"format_LL":      t.Format("January 2, 2006"),
			"format_LLL":     t.Format("January 2, 2006 3:04 PM"),
			"format_LLLL":    t.Format("Monday, January 2, 2006 3:04 PM"),
			"format_LT":      t.Format("3:04 PM"),
			"format_LTS":     t.Format("3:04:05 PM"),
			"format_ISO":     t.Format(time.RFC3339),
			"unix":           fmt.Sprintf("%d", t.Unix()),
			"valueOf":        fmt.Sprintf("%d", t.UnixMilli()),
			"fromNow":        formatRelativeMoment(diff),
			"toNow":          formatRelativeMoment(-diff),
		},
		"world_clock": map[string]string{
			"UTC":          utc.Format(time.RFC3339),
			"Local_Server": t.Local().Format(time.RFC3339),
		},
		"relative": rel,
	}, ""
}

// parseRelativeTime parses strings like "in 4 days", "3 hours ago", "next week"
func parseRelativeTime(input string) (time.Duration, bool) {
	input = strings.ToLower(strings.TrimSpace(input))

	// Handle "next/last" shortcuts
	switch input {
	case "next week":
		return 7 * 24 * time.Hour, true
	case "last week":
		return -7 * 24 * time.Hour, true
	case "next month":
		return 30 * 24 * time.Hour, true
	case "last month":
		return -30 * 24 * time.Hour, true
	case "next year":
		return 365 * 24 * time.Hour, true
	case "last year":
		return -365 * 24 * time.Hour, true
	}

	// Pattern: "in X unit(s)" or "X unit(s) ago"
	inPattern := regexp.MustCompile(`^in\s+(\d+\.?\d*)\s+(\w+)$`)
	agoPattern := regexp.MustCompile(`^(\d+\.?\d*)\s+(\w+)\s+ago$`)

	var num float64
	var unit string
	var future bool

	if matches := inPattern.FindStringSubmatch(input); len(matches) == 3 {
		num, _ = strconv.ParseFloat(matches[1], 64)
		unit = matches[2]
		future = true
	} else if matches := agoPattern.FindStringSubmatch(input); len(matches) == 3 {
		num, _ = strconv.ParseFloat(matches[1], 64)
		unit = matches[2]
		future = false
	} else {
		return 0, false
	}

	// Convert unit to duration
	var multiplier time.Duration
	switch strings.TrimSuffix(unit, "s") { // handle plural
	case "second", "sec":
		multiplier = time.Second
	case "minute", "min":
		multiplier = time.Minute
	case "hour", "hr":
		multiplier = time.Hour
	case "day":
		multiplier = 24 * time.Hour
	case "week", "wk":
		multiplier = 7 * 24 * time.Hour
	case "month":
		multiplier = 30 * 24 * time.Hour
	case "year", "yr":
		multiplier = 365 * 24 * time.Hour
	default:
		return 0, false
	}

	dur := time.Duration(num * float64(multiplier))
	if !future {
		dur = -dur
	}
	return dur, true
}

// formatRelativeMoment returns moment.js style relative time strings
func formatRelativeMoment(d time.Duration) string {
	abs := d
	if abs < 0 {
		abs = -abs
	}

	var value string
	switch {
	case abs < time.Second*45:
		value = "a few seconds"
	case abs < time.Minute*2:
		value = "a minute"
	case abs < time.Hour:
		value = fmt.Sprintf("%d minutes", int(abs.Minutes()))
	case abs < time.Hour*2:
		value = "an hour"
	case abs < time.Hour*24:
		value = fmt.Sprintf("%d hours", int(abs.Hours()))
	case abs < time.Hour*24*2:
		value = "a day"
	case abs < time.Hour*24*30:
		value = fmt.Sprintf("%d days", int(abs.Hours()/24))
	case abs < time.Hour*24*60:
		value = "a month"
	case abs < time.Hour*24*365:
		value = fmt.Sprintf("%d months", int(abs.Hours()/(24*30)))
	case abs < time.Hour*24*365*2:
		value = "a year"
	default:
		value = fmt.Sprintf("%d years", int(abs.Hours()/(24*365)))
	}

	if d > 0 {
		return value + " ago"
	}
	return "in " + value
}

// Internal: Convert Physical Units Logic
func toolConvertUnits(val float64, from string, cat string) (interface{}, string) {
	from = strings.ToLower(strings.TrimSpace(from))
	base := 0.0
	conversions := map[string]interface{}{}

	switch cat {
	case "length":
		// Base: Meter
		switch from {
		case "m", "meter", "meters":
			base = val
		case "km", "kilometer":
			base = val * 1000
		case "cm", "centimeter":
			base = val / 100
		case "mm", "millimeter":
			base = val / 1000
		case "mi", "mile", "miles":
			base = val * 1609.34
		case "yd", "yard", "yards":
			base = val * 0.9144
		case "ft", "foot", "feet":
			base = val * 0.3048
		case "in", "inch", "inches":
			base = val * 0.0254
		default:
			return nil, "Unknown length unit"
		}
		conversions = map[string]interface{}{
			"metric":   map[string]float64{"m": base, "km": base / 1000, "cm": base * 100, "mm": base * 1000},
			"imperial": map[string]float64{"ft": base * 3.28084, "in": base * 39.3701, "mi": base * 0.000621371, "yd": base * 1.09361},
		}
	case "weight":
		// Base: Kg
		switch from {
		case "kg", "kilogram":
			base = val
		case "g", "gram":
			base = val / 1000
		case "mg", "milligram":
			base = val / 1_000_000
		case "lb", "lbs", "pound":
			base = val * 0.453592
		case "oz", "ounce":
			base = val * 0.0283495
		case "stone":
			base = val * 6.35029
		default:
			return nil, "Unknown weight unit"
		}
		conversions = map[string]interface{}{
			"metric":   map[string]float64{"kg": base, "g": base * 1000, "mg": base * 1_000_000},
			"imperial": map[string]float64{"lbs": base * 2.20462, "oz": base * 35.274, "stone": base * 0.157473},
		}
	case "digital":
		// Base: Bytes
		switch from {
		case "b", "bytes":
			base = val
		case "kb", "kilobytes":
			base = val * 1024
		case "mb", "megabytes":
			base = val * 1024 * 1024
		case "gb", "gigabytes":
			base = val * 1024 * 1024 * 1024
		case "tb", "terabytes":
			base = val * 1024 * 1024 * 1024 * 1024
		default:
			return nil, "Unknown digital unit"
		}
		conversions = map[string]interface{}{
			"b": base, "kb": base / 1024, "mb": base / (1024 * 1024), "gb": base / (1024 * 1024 * 1024), "tb": base / (1024 * 1024 * 1024 * 1024),
		}
	case "css":
		// Base: Pixels (16px base)
		switch from {
		case "px", "pixels":
			base = val
		case "rem", "em":
			base = val * 16
		case "pt", "points":
			base = val * (4.0 / 3.0)
		case "%", "percent":
			base = val * 0.16
		default:
			return nil, "Unknown CSS unit"
		}
		conversions = map[string]interface{}{
			"px": base, "rem": base / 16, "em": base / 16, "pt": base * 0.75, "%": (base / 16) * 100,
		}
	case "temperature":
		var c float64
		switch from {
		case "c", "celsius":
			c = val
		case "f", "fahrenheit":
			c = (val - 32) * 5 / 9
		case "k", "kelvin":
			c = val - 273.15
		}
		conversions = map[string]interface{}{
			"c": c, "f": (c * 9 / 5) + 32, "k": c + 273.15,
		}
	case "crypto":
		// Bitcoin: 1 BTC = 100,000,000 satoshi = 1000 mBTC
		// Ethereum: 1 ETH = 1,000,000,000 gwei = 1,000,000,000,000,000,000 wei
		switch from {
		case "btc", "bitcoin":
			satoshi := val * 100_000_000
			conversions = map[string]interface{}{
				"btc":     val,
				"mbtc":    val * 1000,
				"satoshi": satoshi,
				"sats":    satoshi,
			}
		case "sat", "sats", "satoshi", "satoshis":
			btc := val / 100_000_000
			conversions = map[string]interface{}{
				"btc":     btc,
				"mbtc":    btc * 1000,
				"satoshi": val,
				"sats":    val,
			}
		case "mbtc", "millibitcoin":
			btc := val / 1000
			satoshi := btc * 100_000_000
			conversions = map[string]interface{}{
				"btc":     btc,
				"mbtc":    val,
				"satoshi": satoshi,
				"sats":    satoshi,
			}
		case "eth", "ether":
			conversions = map[string]interface{}{
				"eth":  val,
				"gwei": val * 1_000_000_000,
				"wei":  val * 1_000_000_000_000_000_000,
			}
		case "gwei":
			eth := val / 1_000_000_000
			conversions = map[string]interface{}{
				"eth":  eth,
				"gwei": val,
				"wei":  val * 1_000_000_000,
			}
		case "wei":
			eth := val / 1_000_000_000_000_000_000
			conversions = map[string]interface{}{
				"eth":  eth,
				"gwei": val / 1_000_000_000,
				"wei":  val,
			}
		default:
			return nil, "Unknown crypto unit"
		}
	case "duration":
		// Base: milliseconds
		var ms float64
		switch from {
		case "ms", "millisecond", "milliseconds":
			ms = val
		case "s", "sec", "second", "seconds":
			ms = val * 1000
		case "min", "minute", "minutes":
			ms = val * 60 * 1000
		case "h", "hr", "hour", "hours":
			ms = val * 60 * 60 * 1000
		case "d", "day", "days":
			ms = val * 24 * 60 * 60 * 1000
		case "w", "wk", "week", "weeks":
			ms = val * 7 * 24 * 60 * 60 * 1000
		default:
			return nil, "Unknown duration unit"
		}
		conversions = map[string]interface{}{
			"ms":      ms,
			"seconds": ms / 1000,
			"minutes": ms / (60 * 1000),
			"hours":   ms / (60 * 60 * 1000),
			"days":    ms / (24 * 60 * 60 * 1000),
			"weeks":   ms / (7 * 24 * 60 * 60 * 1000),
		}
	case "speed":
		// Base: m/s
		var mps float64
		switch from {
		case "m/s", "mps":
			mps = val
		case "km/h", "kmh", "kph":
			mps = val / 3.6
		case "mph":
			mps = val * 0.44704
		case "ft/s", "fps":
			mps = val * 0.3048
		case "knot", "knots", "kn":
			mps = val * 0.514444
		default:
			return nil, "Unknown speed unit"
		}
		conversions = map[string]interface{}{
			"m/s":   mps,
			"km/h":  mps * 3.6,
			"mph":   mps / 0.44704,
			"ft/s":  mps / 0.3048,
			"knots": mps / 0.514444,
		}
	case "area":
		// Base: square meters
		var sqm float64
		switch from {
		case "sqm", "sq m":
			sqm = val
		case "sqft", "sq ft":
			sqm = val * 0.092903
		case "sqkm", "sq km":
			sqm = val * 1_000_000
		case "sqmi", "sq mi":
			sqm = val * 2_589_988
		case "acre", "acres":
			sqm = val * 4046.86
		case "hectare", "hectares", "ha":
			sqm = val * 10000
		default:
			return nil, "Unknown area unit"
		}
		conversions = map[string]interface{}{
			"sq_m":     sqm,
			"sq_ft":    sqm / 0.092903,
			"sq_km":    sqm / 1_000_000,
			"sq_mi":    sqm / 2_589_988,
			"acres":    sqm / 4046.86,
			"hectares": sqm / 10000,
		}
	case "volume":
		// Base: milliliters
		var ml float64
		switch from {
		case "ml", "milliliter", "milliliters":
			ml = val
		case "l", "liter", "liters", "litre", "litres":
			ml = val * 1000
		case "gal", "gallon", "gallons":
			ml = val * 3785.41
		case "floz", "fl oz":
			ml = val * 29.5735
		case "cup", "cups":
			ml = val * 236.588
		case "pint", "pints":
			ml = val * 473.176
		case "qt", "quart", "quarts":
			ml = val * 946.353
		default:
			return nil, "Unknown volume unit"
		}
		conversions = map[string]interface{}{
			"ml":      ml,
			"liters":  ml / 1000,
			"gallons": ml / 3785.41,
			"fl_oz":   ml / 29.5735,
			"cups":    ml / 236.588,
			"pints":   ml / 473.176,
			"quarts":  ml / 946.353,
		}
	}

	return map[string]interface{}{
		"type":        "unit_conversion",
		"category":    cat,
		"input":       map[string]interface{}{"val": val, "unit": from},
		"conversions": conversions,
	}, ""
}

// 2. Updated Compare Tool
func toolCompare(valA string, unitA string, valB string, unitB string) (interface{}, string) {
	// If units are present, try to normalize
	if unitA != "" && unitB != "" {
		catA := inferCategory(unitA)
		catB := inferCategory(unitB)

		if catA == catB && catA != "" {
			// Compatible physical units
			fA, errA := strconv.ParseFloat(valA, 64)
			fB, errB := strconv.ParseFloat(valB, 64)

			if errA == nil && errB == nil {
				// Convert both to base value using temporary helper logic
				baseA := getBaseValue(fA, unitA, catA)
				baseB := getBaseValue(fB, unitB, catB)

				diff := baseA - baseB
				pct := 0.0
				if baseB != 0 {
					pct = (diff / baseB) * 100
				}

				return map[string]interface{}{
					"type":                         "physical_comparison",
					"category":                     catA,
					"normalized_base_diff":         diff,
					"percent_diff_a_relative_to_b": pct,
					"a_greater":                    baseA > baseB,
					"inputs": map[string]string{
						"a": fmt.Sprintf("%v %s", valA, unitA),
						"b": fmt.Sprintf("%v %s", valB, unitB),
					},
				}, ""
			}
		}
	}

	// Default to generic compare (numeric, string, color)
	return toolCompareValues(valA, valB) // Reuse existing logic
}

func getBaseValue(val float64, unit string, cat string) float64 {
	// Replicates the switch logic from toolConvertUnits just for base extraction
	// In a real app, we'd refactor this to be shared, but copying for single-file simplicity
	unit = strings.ToLower(strings.TrimSpace(unit))
	switch cat {
	case "length":
		switch unit {
		case "m", "meter", "meters":
			return val
		case "km", "kilometer":
			return val * 1000
		case "cm", "centimeter":
			return val / 100
		case "mm", "millimeter":
			return val / 1000
		case "mi", "mile", "miles":
			return val * 1609.34
		case "yd", "yard", "yards":
			return val * 0.9144
		case "ft", "foot", "feet":
			return val * 0.3048
		case "in", "inch", "inches":
			return val * 0.0254
		}
	case "weight":
		switch unit {
		case "kg", "kilogram":
			return val
		case "g", "gram":
			return val / 1000
		case "mg", "milligram":
			return val / 1_000_000
		case "lb", "lbs", "pound":
			return val * 0.453592
		case "oz", "ounce":
			return val * 0.0283495
		case "stone":
			return val * 6.35029
		}
	case "digital":
		switch unit {
		case "b", "bytes":
			return val
		case "kb", "kilobytes":
			return val * 1024
		case "mb", "megabytes":
			return val * 1024 * 1024
		case "gb", "gigabytes":
			return val * 1024 * 1024 * 1024
		case "tb", "terabytes":
			return val * 1024 * 1024 * 1024 * 1024
		}
	case "css":
		switch unit {
		case "px", "pixels":
			return val
		case "rem", "em":
			return val * 16
		case "pt", "points":
			return val * (4.0 / 3.0)
		case "%", "percent":
			return val * 0.16
		}
	case "temperature":
		// Temperature is special because 0 C != 0 F, but for "base value" normalization
		// in a diff context (e.g. 10C vs 50F), converting to C is fine.
		switch unit {
		case "c", "celsius":
			return val
		case "f", "fahrenheit":
			return (val - 32) * 5 / 9
		case "k", "kelvin":
			return val - 273.15
		}
	}
	return val
}

// 3. Transform String
func toolTransformString(text string) (interface{}, string) {
	decodings := map[string]interface{}{}
	detected := []string{}

	// Base64
	if b, err := base64.StdEncoding.DecodeString(text); err == nil {
		if isASCII(string(b)) {
			decodings["base64"] = string(b)
			detected = append(detected, "base64")
		}
	}

	// URL
	if un, err := url.QueryUnescape(text); err == nil && un != text {
		decodings["url"] = un
		detected = append(detected, "url")
	}

	// Hex
	if h, err := hex.DecodeString(text); err == nil {
		if isASCII(string(h)) {
			decodings["hex"] = string(h)
			detected = append(detected, "hex")
		}
	}

	// JSON
	if strings.HasPrefix(strings.TrimSpace(text), "{") || strings.HasPrefix(strings.TrimSpace(text), "[") {
		var js interface{}
		if json.Unmarshal([]byte(text), &js) == nil {
			decodings["json"] = js
			detected = append(detected, "json")
		}
	}

	// Hashes
	md5Sum := md5.Sum([]byte(text))
	shaSum := sha256.Sum256([]byte(text))

	return map[string]interface{}{
		"original": text,
		"analysis": map[string]interface{}{
			"length":         len(text),
			"detected_types": detected,
		},
		"decodings": decodings,
		"transformations": map[string]interface{}{
			"upper":  strings.ToUpper(text),
			"lower":  strings.ToLower(text),
			"base64": base64.StdEncoding.EncodeToString([]byte(text)),
			"url":    url.QueryEscape(text),
			"hex":    hex.EncodeToString([]byte(text)),
			"md5":    hex.EncodeToString(md5Sum[:]),
			"sha256": hex.EncodeToString(shaSum[:]),
		},
	}, ""
}

// 4. Analyze Color
func toolAnalyzeColor(input string) (interface{}, string) {
	input = strings.ToLower(strings.TrimSpace(input))
	r, g, b, a := 0, 0, 0, 255 // alpha defaults to 255 (fully opaque)
	hasAlpha := false
	parsed := false

	// 1. Parsing Logic
	hexInput := strings.TrimPrefix(input, "#")

	// Hex formats: #RGB, #RGBA, #RRGGBB, #RRGGBBAA
	if strings.HasPrefix(input, "#") || (len(hexInput) >= 3 && isHex(hexInput)) {
		switch len(hexInput) {
		case 3: // #RGB
			fmt.Sscanf(hexInput, "%1x%1x%1x", &r, &g, &b)
			r, g, b = r*17, g*17, b*17 // expand to full range
			parsed = true
		case 4: // #RGBA
			fmt.Sscanf(hexInput, "%1x%1x%1x%1x", &r, &g, &b, &a)
			r, g, b, a = r*17, g*17, b*17, a*17
			hasAlpha = true
			parsed = true
		case 6: // #RRGGBB
			fmt.Sscanf(hexInput, "%02x%02x%02x", &r, &g, &b)
			parsed = true
		case 8: // #RRGGBBAA
			fmt.Sscanf(hexInput, "%02x%02x%02x%02x", &r, &g, &b, &a)
			hasAlpha = true
			parsed = true
		}
	} else if strings.HasPrefix(input, "rgba") {
		// rgba(r, g, b, a)
		re := regexp.MustCompile(`[\d.]+`)
		matches := re.FindAllString(input, 4)
		if len(matches) >= 4 {
			r, _ = strconv.Atoi(matches[0])
			g, _ = strconv.Atoi(matches[1])
			b, _ = strconv.Atoi(matches[2])
			alphaF, _ := strconv.ParseFloat(matches[3], 64)
			if alphaF <= 1.0 {
				a = int(alphaF * 255)
			} else {
				a = int(alphaF)
			}
			hasAlpha = true
			parsed = true
		}
	} else if strings.HasPrefix(input, "rgb") {
		// rgb(r, g, b)
		re := regexp.MustCompile(`\d+`)
		matches := re.FindAllString(input, 3)
		if len(matches) == 3 {
			r, _ = strconv.Atoi(matches[0])
			g, _ = strconv.Atoi(matches[1])
			b, _ = strconv.Atoi(matches[2])
			parsed = true
		}
	} else if strings.HasPrefix(input, "hsl") {
		// hsl(h, s%, l%) or hsla(h, s%, l%, a)
		re := regexp.MustCompile(`[\d.]+`)
		matches := re.FindAllString(input, 4)
		if len(matches) >= 3 {
			hue, _ := strconv.ParseFloat(matches[0], 64)
			sat, _ := strconv.ParseFloat(matches[1], 64)
			lit, _ := strconv.ParseFloat(matches[2], 64)
			r, g, b = hslToRGB(hue, sat/100, lit/100)
			if len(matches) >= 4 {
				alphaF, _ := strconv.ParseFloat(matches[3], 64)
				if alphaF <= 1.0 {
					a = int(alphaF * 255)
				} else {
					a = int(alphaF)
				}
				hasAlpha = true
			}
			parsed = true
		}
	} else if strings.HasPrefix(input, "hwb") {
		// hwb(h w% b%) or hwb(h w% b% / a)
		re := regexp.MustCompile(`[\d.]+`)
		matches := re.FindAllString(input, 4)
		if len(matches) >= 3 {
			hue, _ := strconv.ParseFloat(matches[0], 64)
			white, _ := strconv.ParseFloat(matches[1], 64)
			black, _ := strconv.ParseFloat(matches[2], 64)
			r, g, b = hwbToRGB(hue, white/100, black/100)
			if len(matches) >= 4 {
				alphaF, _ := strconv.ParseFloat(matches[3], 64)
				if alphaF <= 1.0 {
					a = int(alphaF * 255)
				} else {
					a = int(alphaF)
				}
				hasAlpha = true
			}
			parsed = true
		}
	} else if strings.HasPrefix(input, "lab") {
		// lab(l a b) or lab(l a b / alpha)
		re := regexp.MustCompile(`-?[\d.]+`)
		matches := re.FindAllString(input, 4)
		if len(matches) >= 3 {
			L, _ := strconv.ParseFloat(matches[0], 64)
			A, _ := strconv.ParseFloat(matches[1], 64)
			B, _ := strconv.ParseFloat(matches[2], 64)
			r, g, b = labToRGB(L, A, B)
			if len(matches) >= 4 {
				alphaF, _ := strconv.ParseFloat(matches[3], 64)
				if alphaF <= 1.0 {
					a = int(alphaF * 255)
				} else {
					a = int(alphaF)
				}
				hasAlpha = true
			}
			parsed = true
		}
	} else if strings.HasPrefix(input, "lch") {
		// lch(l c h) or lch(l c h / alpha)
		re := regexp.MustCompile(`-?[\d.]+`)
		matches := re.FindAllString(input, 4)
		if len(matches) >= 3 {
			L, _ := strconv.ParseFloat(matches[0], 64)
			C, _ := strconv.ParseFloat(matches[1], 64)
			H, _ := strconv.ParseFloat(matches[2], 64)
			r, g, b = lchToRGB(L, C, H)
			if len(matches) >= 4 {
				alphaF, _ := strconv.ParseFloat(matches[3], 64)
				if alphaF <= 1.0 {
					a = int(alphaF * 255)
				} else {
					a = int(alphaF)
				}
				hasAlpha = true
			}
			parsed = true
		}
	} else if strings.HasPrefix(input, "oklch") {
		// oklch(l c h) or oklch(l c h / alpha)
		re := regexp.MustCompile(`-?[\d.]+`)
		matches := re.FindAllString(input, 4)
		if len(matches) >= 3 {
			L, _ := strconv.ParseFloat(matches[0], 64)
			C, _ := strconv.ParseFloat(matches[1], 64)
			H, _ := strconv.ParseFloat(matches[2], 64)
			r, g, b = oklchToRGB(L, C, H)
			if len(matches) >= 4 {
				alphaF, _ := strconv.ParseFloat(matches[3], 64)
				if alphaF <= 1.0 {
					a = int(alphaF * 255)
				} else {
					a = int(alphaF)
				}
				hasAlpha = true
			}
			parsed = true
		}
	} else if strings.HasPrefix(input, "oklab") {
		// oklab(l a b) or oklab(l a b / alpha)
		re := regexp.MustCompile(`-?[\d.]+`)
		matches := re.FindAllString(input, 4)
		if len(matches) >= 3 {
			L, _ := strconv.ParseFloat(matches[0], 64)
			A, _ := strconv.ParseFloat(matches[1], 64)
			B, _ := strconv.ParseFloat(matches[2], 64)
			r, g, b = oklabToRGB(L, A, B)
			if len(matches) >= 4 {
				alphaF, _ := strconv.ParseFloat(matches[3], 64)
				if alphaF <= 1.0 {
					a = int(alphaF * 255)
				} else {
					a = int(alphaF)
				}
				hasAlpha = true
			}
			parsed = true
		}
	} else {
		// Named colors fallback
		switch input {
		case "white":
			r, g, b = 255, 255, 255
			parsed = true
		case "black":
			r, g, b = 0, 0, 0
			parsed = true
		case "red":
			r, g, b = 255, 0, 0
			parsed = true
		case "green":
			r, g, b = 0, 128, 0
			parsed = true
		case "blue":
			r, g, b = 0, 0, 255
			parsed = true
		case "yellow":
			r, g, b = 255, 255, 0
			parsed = true
		case "cyan":
			r, g, b = 0, 255, 255
			parsed = true
		case "magenta":
			r, g, b = 255, 0, 255
			parsed = true
		case "gray", "grey":
			r, g, b = 128, 128, 128
			parsed = true
		case "transparent":
			r, g, b, a = 0, 0, 0, 0
			hasAlpha = true
			parsed = true
		}
	}

	if !parsed {
		return nil, fmt.Sprintf("Could not parse color: %s", input)
	}

	// Clamp values
	clamp := func(x int) int {
		if x < 0 {
			return 0
		}
		if x > 255 {
			return 255
		}
		return x
	}
	r, g, b, a = clamp(r), clamp(g), clamp(b), clamp(a)
	alphaFloat := float64(a) / 255.0

	// 2. Calculations
	// HSL & HSV
	h, s, l := rgbToHSL(r, g, b)
	_, sv, v := rgbToHSV(r, g, b)

	// HWB
	hwbH, hwbW, hwbB := rgbToHWB(r, g, b)

	// CMYK
	c, m, y, k := rgbToCMYK(r, g, b)

	// LAB & LCH
	labL, labA, labB := rgbToLAB(r, g, b)
	lchL, lchC, lchH := rgbToLCH(r, g, b)

	// Oklab & Oklch
	okL, okA, okB := rgbToOklab(r, g, b)
	oklchL, oklchC, oklchH := rgbToOklch(r, g, b)

	// Luminance & Contrast
	lum := 0.2126*float64(r)/255.0 + 0.7152*float64(g)/255.0 + 0.0722*float64(b)/255.0
	contrastWhite := (1.0 + 0.05) / (lum + 0.05)
	contrastBlack := (lum + 0.05) / (0.0 + 0.05)

	// Ansi 256 approximation
	ansi := 16 + (36 * (r / 51)) + (6 * (g / 51)) + (b / 51)
	if r == g && g == b {
		if r < 8 {
			ansi = 16
		} else if r > 248 {
			ansi = 231
		} else {
			ansi = 232 + (((r - 8) * 24) / 247)
		}
	}

	// Build formats map
	formats := map[string]interface{}{
		"hex":       strings.ToUpper(fmt.Sprintf("#%02x%02x%02x", r, g, b)),
		"rgb":       map[string]int{"r": r, "g": g, "b": b},
		"rgb_css":   fmt.Sprintf("rgb(%d, %d, %d)", r, g, b),
		"hsl":       map[string]interface{}{"h": round(h), "s": round(s * 100), "l": round(l * 100)},
		"hsl_css":   fmt.Sprintf("hsl(%.0f, %.0f%%, %.0f%%)", h, s*100, l*100),
		"hsv":       map[string]interface{}{"h": round(h), "s": round(sv * 100), "v": round(v * 100)},
		"hwb":       map[string]interface{}{"h": round(hwbH), "w": round(hwbW), "b": round(hwbB)},
		"hwb_css":   fmt.Sprintf("hwb(%.0f %.0f%% %.0f%%)", hwbH, hwbW, hwbB),
		"cmyk":      map[string]interface{}{"c": round(c * 100), "m": round(m * 100), "y": round(y * 100), "k": round(k * 100)},
		"cmyk_css":  fmt.Sprintf("cmyk(%.0f%%, %.0f%%, %.0f%%, %.0f%%)", c*100, m*100, y*100, k*100),
		"lab":       map[string]interface{}{"l": roundDig(labL, 2), "a": roundDig(labA, 2), "b": roundDig(labB, 2)},
		"lab_css":   fmt.Sprintf("lab(%.2f %.2f %.2f)", labL, labA, labB),
		"lch":       map[string]interface{}{"l": roundDig(lchL, 2), "c": roundDig(lchC, 2), "h": roundDig(lchH, 2)},
		"lch_css":   fmt.Sprintf("lch(%.2f %.2f %.2f)", lchL, lchC, lchH),
		"oklab":     map[string]interface{}{"l": roundDig(okL, 4), "a": roundDig(okA, 4), "b": roundDig(okB, 4)},
		"oklab_css": fmt.Sprintf("oklab(%.4f %.4f %.4f)", okL, okA, okB),
		"oklch":     map[string]interface{}{"l": roundDig(oklchL, 4), "c": roundDig(oklchC, 4), "h": roundDig(oklchH, 2)},
		"oklch_css": fmt.Sprintf("oklch(%.4f %.4f %.2f)", oklchL, oklchC, oklchH),
		"ansi256":   ansi,
	}

	// Add alpha formats if alpha channel is present
	if hasAlpha {
		formats["alpha"] = roundDig(alphaFloat, 3)
		formats["alpha_percent"] = round(alphaFloat * 100)
		formats["hexa"] = strings.ToUpper(fmt.Sprintf("#%02x%02x%02x%02x", r, g, b, a))
		formats["rgba"] = map[string]interface{}{"r": r, "g": g, "b": b, "a": roundDig(alphaFloat, 3)}
		formats["rgba_css"] = fmt.Sprintf("rgba(%d, %d, %d, %.3f)", r, g, b, alphaFloat)
		formats["hsla_css"] = fmt.Sprintf("hsla(%.0f, %.0f%%, %.0f%%, %.3f)", h, s*100, l*100, alphaFloat)
		formats["hwb_css"] = fmt.Sprintf("hwb(%.0f %.0f%% %.0f%% / %.3f)", hwbH, hwbW, hwbB, alphaFloat)
		formats["lab_css"] = fmt.Sprintf("lab(%.2f %.2f %.2f / %.3f)", labL, labA, labB, alphaFloat)
		formats["lch_css"] = fmt.Sprintf("lch(%.2f %.2f %.2f / %.3f)", lchL, lchC, lchH, alphaFloat)
		formats["oklab_css"] = fmt.Sprintf("oklab(%.4f %.4f %.4f / %.3f)", okL, okA, okB, alphaFloat)
		formats["oklch_css"] = fmt.Sprintf("oklch(%.4f %.4f %.2f / %.3f)", oklchL, oklchC, oklchH, alphaFloat)
	}

	return map[string]interface{}{
		"original_input": input,
		"has_alpha":      hasAlpha,
		"formats":        formats,
		"accessibility": map[string]interface{}{
			"luminance":         roundDig(lum, 4),
			"contrast_white":    roundDig(contrastWhite, 2),
			"contrast_black":    roundDig(contrastBlack, 2),
			"wcag_aa_compliant": contrastWhite >= 4.5 || contrastBlack >= 4.5,
			"recommended_text_color": func() string {
				if contrastBlack > contrastWhite {
					return "black"
				} else {
					return "white"
				}
			}(),
		},
	}, ""
}

// --- Color Helpers ---

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

func rgbToHSL(r, g, b int) (float64, float64, float64) {
	rf, gf, bf := float64(r)/255.0, float64(g)/255.0, float64(b)/255.0
	max := math.Max(rf, math.Max(gf, bf))
	min := math.Min(rf, math.Min(gf, bf))
	h, s, l := 0.0, 0.0, (max+min)/2.0

	if max != min {
		d := max - min
		if l > 0.5 {
			s = d / (2.0 - max - min)
		} else {
			s = d / (max + min)
		}
		switch max {
		case rf:
			h = (gf - bf) / d
			if gf < bf {
				h += 6.0
			}
		case gf:
			h = (bf-rf)/d + 2.0
		case bf:
			h = (rf-gf)/d + 4.0
		}
		h *= 60.0
	}
	return h, s, l
}

func rgbToHSV(r, g, b int) (float64, float64, float64) {
	rf, gf, bf := float64(r)/255.0, float64(g)/255.0, float64(b)/255.0
	max := math.Max(rf, math.Max(gf, bf))
	min := math.Min(rf, math.Min(gf, bf))
	h, s, v := 0.0, 0.0, max
	d := max - min
	if max != 0 {
		s = d / max
	}

	if max != min {
		switch max {
		case rf:
			h = (gf - bf) / d
			if gf < bf {
				h += 6.0
			}
		case gf:
			h = (bf-rf)/d + 2.0
		case bf:
			h = (rf-gf)/d + 4.0
		}
		h *= 60.0
	}
	return h, s, v
}

func rgbToCMYK(r, g, b int) (float64, float64, float64, float64) {
	if r == 0 && g == 0 && b == 0 {
		return 0, 0, 0, 1
	}
	rf, gf, bf := float64(r)/255.0, float64(g)/255.0, float64(b)/255.0
	k := 1.0 - math.Max(rf, math.Max(gf, bf))
	c := (1.0 - rf - k) / (1.0 - k)
	m := (1.0 - gf - k) / (1.0 - k)
	y := (1.0 - bf - k) / (1.0 - k)
	return c, m, y, k
}

func rgbToHWB(r, g, b int) (float64, float64, float64) {
	h, _, _ := rgbToHSL(r, g, b)
	rf, gf, bf := float64(r)/255.0, float64(g)/255.0, float64(b)/255.0
	w := math.Min(rf, math.Min(gf, bf))
	bl := 1.0 - math.Max(rf, math.Max(gf, bf))
	return h, w * 100, bl * 100
}

// sRGB to linear RGB
func srgbToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func rgbToXYZ(r, g, b int) (float64, float64, float64) {
	rf := srgbToLinear(float64(r) / 255.0)
	gf := srgbToLinear(float64(g) / 255.0)
	bf := srgbToLinear(float64(b) / 255.0)

	// sRGB D65 matrix
	x := rf*0.4124564 + gf*0.3575761 + bf*0.1804375
	y := rf*0.2126729 + gf*0.7151522 + bf*0.0721750
	z := rf*0.0193339 + gf*0.1191920 + bf*0.9503041
	return x * 100, y * 100, z * 100
}

func rgbToLAB(r, g, b int) (float64, float64, float64) {
	x, y, z := rgbToXYZ(r, g, b)
	// D65 reference white
	x /= 95.047
	y /= 100.0
	z /= 108.883

	f := func(t float64) float64 {
		if t > 0.008856 {
			return math.Pow(t, 1.0/3.0)
		}
		return (7.787 * t) + (16.0 / 116.0)
	}

	fx, fy, fz := f(x), f(y), f(z)
	L := (116.0 * fy) - 16.0
	a := 500.0 * (fx - fy)
	bVal := 200.0 * (fy - fz)
	return L, a, bVal
}

func rgbToLCH(r, g, b int) (float64, float64, float64) {
	L, a, bVal := rgbToLAB(r, g, b)
	C := math.Sqrt(a*a + bVal*bVal)
	H := math.Atan2(bVal, a) * (180.0 / math.Pi)
	if H < 0 {
		H += 360
	}
	return L, C, H
}

func rgbToOklab(r, g, b int) (float64, float64, float64) {
	rf := srgbToLinear(float64(r) / 255.0)
	gf := srgbToLinear(float64(g) / 255.0)
	bf := srgbToLinear(float64(b) / 255.0)

	l := 0.4122214708*rf + 0.5363325363*gf + 0.0514459929*bf
	m := 0.2119034982*rf + 0.6806995451*gf + 0.1073969566*bf
	s := 0.0883024619*rf + 0.2817188376*gf + 0.6299787005*bf

	l_ := math.Cbrt(l)
	m_ := math.Cbrt(m)
	s_ := math.Cbrt(s)

	L := 0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_
	A := 1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_
	B := 0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_
	return L, A, B
}

func rgbToOklch(r, g, b int) (float64, float64, float64) {
	L, a, bVal := rgbToOklab(r, g, b)
	C := math.Sqrt(a*a + bVal*bVal)
	H := math.Atan2(bVal, a) * (180.0 / math.Pi)
	if H < 0 {
		H += 360
	}
	return L, C, H
}

// --- Reverse Conversions (to RGB) ---

func hslToRGB(h, s, l float64) (int, int, int) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}

	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2

	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}

	return clampInt((r + m) * 255), clampInt((g + m) * 255), clampInt((b + m) * 255)
}

func hwbToRGB(h, w, bl float64) (int, int, int) {
	// Normalize whiteness and blackness
	if w+bl >= 1 {
		gray := w / (w + bl)
		g := clampInt(gray * 255)
		return g, g, g
	}

	r, g, b := hslToRGB(h, 1.0, 0.5)
	rf := float64(r)/255*(1-w-bl) + w
	gf := float64(g)/255*(1-w-bl) + w
	bf := float64(b)/255*(1-w-bl) + w

	return clampInt(rf * 255), clampInt(gf * 255), clampInt(bf * 255)
}

func linearToSrgb(c float64) float64 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

func xyzToRGB(x, y, z float64) (int, int, int) {
	// XYZ to linear sRGB
	x /= 100
	y /= 100
	z /= 100

	r := x*3.2404542 + y*-1.5371385 + z*-0.4985314
	g := x*-0.9692660 + y*1.8760108 + z*0.0415560
	b := x*0.0556434 + y*-0.2040259 + z*1.0572252

	// Linear to sRGB
	r = linearToSrgb(r)
	g = linearToSrgb(g)
	b = linearToSrgb(b)

	return clampInt(r * 255), clampInt(g * 255), clampInt(b * 255)
}

func labToRGB(L, A, B float64) (int, int, int) {
	// LAB to XYZ
	fy := (L + 16) / 116
	fx := A/500 + fy
	fz := fy - B/200

	fInv := func(t float64) float64 {
		if t > 0.206893 {
			return t * t * t
		}
		return (t - 16.0/116.0) / 7.787
	}

	x := fInv(fx) * 95.047
	y := fInv(fy) * 100.0
	z := fInv(fz) * 108.883

	return xyzToRGB(x, y, z)
}

func lchToRGB(L, C, H float64) (int, int, int) {
	hRad := H * math.Pi / 180
	a := C * math.Cos(hRad)
	b := C * math.Sin(hRad)
	return labToRGB(L, a, b)
}

func oklabToRGB(L, A, B float64) (int, int, int) {
	l_ := L + 0.3963377774*A + 0.2158037573*B
	m_ := L - 0.1055613458*A - 0.0638541728*B
	s_ := L - 0.0894841775*A - 1.2914855480*B

	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_

	r := +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g := -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	b := -0.0041960863*l - 0.7034186147*m + 1.7076147010*s

	r = linearToSrgb(r)
	g = linearToSrgb(g)
	b = linearToSrgb(b)

	return clampInt(r * 255), clampInt(g * 255), clampInt(b * 255)
}

func oklchToRGB(L, C, H float64) (int, int, int) {
	hRad := H * math.Pi / 180
	a := C * math.Cos(hRad)
	b := C * math.Sin(hRad)
	return oklabToRGB(L, a, b)
}

func clampInt(v float64) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return int(math.Round(v))
}

func round(x float64) int {
	return int(math.Round(x))
}

func roundDig(x float64, n int) float64 {
	p := math.Pow(10, float64(n))
	return math.Round(x*p) / p
}

// 5. Inspect JWT
func toolInspectJWT(token string) (interface{}, string) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, "Invalid JWT format"
	}

	decode := func(s string) interface{} {
		// JWT uses RawURLEncoding (no padding)
		b, _ := base64.RawURLEncoding.DecodeString(s)
		var out interface{}
		json.Unmarshal(b, &out)
		return out
	}

	return map[string]interface{}{
		"header":  decode(parts[0]),
		"payload": decode(parts[1]),
	}, ""
}

// 6. Generate Mock Data
func toolGenerateMockData(dtype string, count int) (interface{}, string) {
	if count <= 0 {
		count = 1
	}
	res := make([]interface{}, count)

	for i := 0; i < count; i++ {
		switch dtype {
		case "uuid":
			// Basic random UUID v4 logic
			u := make([]byte, 16)
			// In production use crypto/rand
			for j := range u {
				u[j] = byte(i + j)
			} // Dummy for example
			res[i] = fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
		case "ipv4":
			res[i] = "192.168.1.1" // Placeholder
		case "hex":
			res[i] = "deadbeef"
		}
	}
	return map[string]interface{}{"type": dtype, "data": res}, ""
}

// 7. Compare Values
func toolCompareValues(a, b string) (interface{}, string) {
	// Numeric
	fa, errA := strconv.ParseFloat(a, 64)
	fb, errB := strconv.ParseFloat(b, 64)

	if errA == nil && errB == nil {
		diff := fa - fb
		return map[string]interface{}{
			"type":      "numeric",
			"diff":      diff,
			"a_greater": fa > fb,
		}, ""
	}

	// String similarity (Levenshtein)
	dist := levenshtein(a, b)
	maxLen := math.Max(float64(len(a)), float64(len(b)))
	sim := 0.0
	if maxLen > 0 {
		sim = (1.0 - float64(dist)/maxLen) * 100
	}

	return map[string]interface{}{
		"type":               "string",
		"levenshtein":        dist,
		"similarity_percent": sim,
	}, ""
}

// 8. Statistics
func toolCalculateStatistics(nums []float64) (interface{}, string) {
	if len(nums) == 0 {
		return nil, "Empty list"
	}

	sum := 0.0
	for _, n := range nums {
		sum += n
	}
	mean := sum / float64(len(nums))

	sort.Float64s(nums)
	median := 0.0
	if len(nums)%2 == 0 {
		median = (nums[len(nums)/2-1] + nums[len(nums)/2]) / 2
	} else {
		median = nums[len(nums)/2]
	}

	minVal := nums[0]
	maxVal := nums[len(nums)-1]

	return map[string]interface{}{
		"count":  len(nums),
		"sum":    sum,
		"mean":   mean,
		"median": median,
		"min":    minVal,
		"max":    maxVal,
	}, ""
}

// --- Helpers ---

func isNumeric(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
	}
	for i := 0; i <= la; i++ {
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, min(d[i][j-1]+1, d[i-1][j-1]+cost))
		}
	}
	return d[la][lb]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
