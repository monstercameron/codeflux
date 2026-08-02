package readout

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestFormatExactMoneyForReadingKeepsExactMinorUnits(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		value domain.Money
		want  string
	}{
		{"whole dollars", domain.Money{Currency: "USD", MinorUnits: 5000}, "$50.00"},
		{"sub unit", domain.Money{Currency: "USD", MinorUnits: 7}, "$0.07"},
		{"zero", domain.Money{Currency: "USD", MinorUnits: 0}, "$0.00"},
		{"grouped", domain.Money{Currency: "USD", MinorUnits: 123456789}, "$1,234,567.89"},
		{"negative", domain.Money{Currency: "USD", MinorUnits: -250}, "-$2.50"},
		{"zero digit currency", domain.Money{Currency: "JPY", MinorUnits: 12480}, "¥12,480"},
		{"unsymboled currency", domain.Money{Currency: "CHF", MinorUnits: 1250}, "CHF 12.50"},
		{"unknown currency keeps its code", domain.Money{Currency: "XTS", MinorUnits: 1}, "XTS 0.01"},
		{"absent currency", domain.Money{MinorUnits: 1250}, "12.50"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := FormatExactMoneyForReading(testCase.value); got != testCase.want {
				t.Fatalf("FormatExactMoneyForReading(%v) = %q, want %q",
					testCase.value, got, testCase.want)
			}
		})
	}
}

func TestFormatExactMoneyRangeForReadingCollapsesAPointEstimate(t *testing.T) {
	low := domain.Money{Currency: "USD", MinorUnits: 40}
	high := domain.Money{Currency: "USD", MinorUnits: 120}
	if got := FormatExactMoneyRangeForReading(low, high); got != "$0.40 – $1.20" {
		t.Fatalf("range = %q", got)
	}
	if got := FormatExactMoneyRangeForReading(low, low); got != "$0.40" {
		t.Fatalf("collapsed range = %q", got)
	}
}

func TestFormatExactTokenCountForReadingGroupsThousands(t *testing.T) {
	for count, want := range map[uint64]string{
		0: "0", 7: "7", 999: "999", 1000: "1,000", 12480: "12,480", 1234567: "1,234,567",
	} {
		if got := FormatExactTokenCountForReading(count); got != want {
			t.Fatalf("FormatExactTokenCountForReading(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestFormatMinorUnitHintNamesTheUnitTheFieldTakes(t *testing.T) {
	if got := FormatMinorUnitHint("USD"); got != "Whole cents. 5000 is $50.00." {
		t.Fatalf("USD hint = %q", got)
	}
	if got := FormatMinorUnitHint("JPY"); got != "Whole JPY. 5000 is ¥5,000." {
		t.Fatalf("JPY hint = %q", got)
	}
	if got := FormatMinorUnitHint(""); got != "Whole cents. 5000 is $50.00." {
		t.Fatalf("default hint = %q", got)
	}
}
