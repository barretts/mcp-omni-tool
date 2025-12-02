# Omni Tool MCP Server

A universal conversion and transformation toolkit for AI assistants via the Model Context Protocol (MCP). Made by AI for AI.

## Installation

### Docker (Recommended)

```bash
docker pull sosukecn/omni-tool:latest
```

### From Source

```bash
go build -o omni-tool main.go
```

## Usage with Claude Desktop

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "omni-tool": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "sosukecn/omni-tool:latest"]
    }
  }
}
```

## Tools

| Tool | Description |
|------|-------------|
| `convert` | Universal converter: time, colors, units (length, weight, temp, digital, CSS, crypto, duration, speed, area, volume) |
| `compare` | Compare values with automatic unit conversion |
| `transform_string` | Detect encoding, decode, and transform strings |
| `analyze_color` | Parse any color format and get all conversions + accessibility info |
| `inspect_jwt` | Decode JWT tokens without verification |
| `generate_mock_data` | Generate UUIDs, hex strings, IP addresses |
| `calculate_statistics` | Calculate mean, median, min, max, sum |

## Examples

### Convert Units

```
convert "10" unit:"px"      → 0.625rem, 7.5pt, 62.5%
convert "60" unit:"mph"     → 96.56 km/h, 26.82 m/s
convert "0.034" unit:"btc"  → 3,400,000 satoshi
convert "2" unit:"cups"     → 473ml, 0.47L
```

### Convert Time

```
convert "now"           → timestamps, ISO, JS Date(), moment.js formats
convert "in 4 days"     → future date calculations
convert "3 hours ago"   → past date calculations
convert "tomorrow"      → next day at midnight
convert "yesterday"     → previous day
convert "next week"     → 7 days from now
convert "last month"    → 30 days ago
convert "1733000000"    → parse Unix timestamp
```

**JavaScript output:**
- `new Date(1764706087806)`
- `toISOString()`, `toLocaleString()`, etc.

**moment.js output:**
- `format("L")`, `format("LLLL")`
- `fromNow` ("in 4 days")

### Analyze Colors

```
analyze_color "#FF5733"
analyze_color "oklch(0.6804 0.2100 33.69)"
analyze_color "rgb(255, 87, 51)"
analyze_color "#79589F99"  # with alpha
```

**Supported formats:** Hex (3/4/6/8 digit), RGB, RGBA, HSL, HWB, LAB, LCH, Oklab, Oklch

**Output includes:** All format conversions, CMYK, HSV, ANSI256, WCAG contrast ratios, recommended text color

### Transform Strings

```
transform_string "SGVsbG8gV29ybGQ="  → detects Base64, decodes
transform_string "{'key': 'value'}"  → detects JSON, parses
```

**Output:** Base64/Hex/URL encode/decode, MD5, SHA256, upper/lower

## Unit Categories

| Category | Units |
|----------|-------|
| **Length** | m, km, cm, mm, mi, ft, in, yd |
| **Weight** | kg, g, mg, lb, oz, stone |
| **Temperature** | C, F, K |
| **Digital** | B, KB, MB, GB, TB |
| **CSS** | px, rem, em, pt, % |
| **Crypto** | BTC, satoshi, mBTC, ETH, gwei, wei |
| **Duration** | ms, sec, min, hr, day, week |
| **Speed** | mph, km/h, m/s, ft/s, knots |
| **Area** | sq ft, sq m, sq km, acres, hectares |
| **Volume** | ml, L, gal, fl oz, cups, pints, quarts |

## License

MIT
