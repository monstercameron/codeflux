// Package readout renders exact machine measurements as the short strings a
// person reads in the console.
//
// The domain stores money as an exact signed count of currency minor units and
// tokens as exact counts, because rounding a spend that a hard cap is enforced
// against would be a correctness bug. Those exact values were also what the
// interface displayed, so a fifty dollar cap read "USD 5000 minor units" in the
// application bar, the metric strip and the task detail rail at once. This
// package is the single place that turns an exact value into its readable form
// so no surface invents its own.
//
// Nothing here decides whether a value is known. An unknown measurement must be
// said out loud by the caller that holds the known flag; formatting an absent
// value as zero would report a measurement nobody took.
package readout

import (
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// currencyMinorUnitDigits is the number of minor-unit digits per supported
// currency code. Codes absent from this table are rendered with two, which is
// the ISO 4217 default and the only case the product has shipped.
var currencyMinorUnitDigits = map[string]int{
	"USD": 2, "EUR": 2, "GBP": 2, "CAD": 2, "AUD": 2, "CHF": 2,
	"JPY": 0, "KRW": 0,
}

// currencySymbols carries the symbol for the currencies a reader recognizes
// without its code. Anything else keeps its ISO code, which is clearer than a
// symbol nobody can attribute.
var currencySymbols = map[string]string{
	"USD": "$", "EUR": "€", "GBP": "£", "JPY": "¥",
}

// FormatExactMoneyForReading renders an exact money amount as its currency
// value, for example "$50.00" or "CHF 12.50".
//
// The conversion is exact: minor units are split by the currency's own
// minor-unit digits rather than divided in floating point, so a value can be
// read back without loss.
func FormatExactMoneyForReading(value domain.Money) string {
	code := strings.ToUpper(strings.TrimSpace(string(value.Currency)))
	digits, known := currencyMinorUnitDigits[code]
	if !known {
		digits = 2
	}
	minor := value.MinorUnits
	sign := ""
	if minor < 0 {
		sign = "-"
		minor = -minor
	}
	rendered := strconv.FormatInt(minor, 10)
	if digits > 0 {
		for len(rendered) <= digits {
			rendered = "0" + rendered
		}
		split := len(rendered) - digits
		rendered = groupThousands(rendered[:split]) + "." + rendered[split:]
	} else {
		rendered = groupThousands(rendered)
	}
	if symbol, ok := currencySymbols[code]; ok {
		return sign + symbol + rendered
	}
	if code == "" {
		return sign + rendered
	}
	return sign + code + " " + rendered
}

// FormatExactMoneyRangeForReading renders a forecast band as one phrase, for
// example "$0.40 – $1.20". Identical bounds collapse to a single value, because
// a range that cannot vary is a point estimate wearing a range's punctuation.
func FormatExactMoneyRangeForReading(low, high domain.Money) string {
	first := FormatExactMoneyForReading(low)
	second := FormatExactMoneyForReading(high)
	if first == second {
		return first
	}
	return first + " – " + second
}

// FormatExactTokenCountForReading renders a token count with thousands
// separators, for example "12,480".
func FormatExactTokenCountForReading(count uint64) string {
	return groupThousands(strconv.FormatUint(count, 10))
}

// FormatMinorUnitHint explains what an exact minor-unit input field expects,
// for example "Whole cents. 5000 is $50.00." It exists because the field
// itself must keep taking exact minor units: the hard cap is enforced against
// that integer, and accepting a decimal there would introduce the rounding the
// domain type exists to prevent.
func FormatMinorUnitHint(currency domain.CurrencyCode) string {
	code := strings.ToUpper(strings.TrimSpace(string(currency)))
	if code == "" {
		code = "USD"
	}
	digits, known := currencyMinorUnitDigits[code]
	if !known {
		digits = 2
	}
	if digits == 0 {
		return "Whole " + code + ". 5000 is " +
			FormatExactMoneyForReading(domain.Money{Currency: domain.CurrencyCode(code), MinorUnits: 5000}) + "."
	}
	unit := "minor units"
	if code == "USD" || code == "CAD" || code == "AUD" {
		unit = "cents"
	}
	return "Whole " + unit + ". 5000 is " +
		FormatExactMoneyForReading(domain.Money{Currency: domain.CurrencyCode(code), MinorUnits: 5000}) + "."
}

func groupThousands(digits string) string {
	if len(digits) <= 3 {
		return digits
	}
	var builder strings.Builder
	lead := len(digits) % 3
	if lead > 0 {
		builder.WriteString(digits[:lead])
	}
	for index := lead; index < len(digits); index += 3 {
		if builder.Len() > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(digits[index : index+3])
	}
	return builder.String()
}
