package service

import "testing"

func TestIsValidOrderNumber(t *testing.T) {
	tests := []struct {
		name   string
		number string
		want   bool
	}{
		{"spec upload example", "12345678903", true},
		{"spec orders list example (processed)", "9278923470", true},
		{"spec orders list example (invalid status, valid format)", "346436439", true},
		{"spec withdraw example", "2377225624", true},
		{"classic Luhn test number", "79927398713", true},
		{"fails checksum", "1234567890", false},
		{"fails checksum, one digit off from valid", "12345678904", false},
		{"empty string", "", false},
		{"single non-checksum digit", "1", false},
		{"contains letters", "1234abc5678", false},
		{"contains whitespace", "1234 5678", false},
		{"contains leading zero, valid checksum", "0000000000", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidOrderNumber(tt.number)
			if got != tt.want {
				t.Errorf("IsValidOrderNumber(%q) = %v, want %v", tt.number, got, tt.want)
			}
		})
	}
}
