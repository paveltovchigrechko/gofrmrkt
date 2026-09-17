package service

// IsValidOrderNumber reports whether number consists solely of digits and
// passes the Luhn checksum, as required for order numbers by the spec.
func IsValidOrderNumber(number string) bool {
	if number == "" {
		return false
	}

	sum := 0
	double := false
	for i := len(number) - 1; i >= 0; i-- {
		c := number[i]
		if c < '0' || c > '9' {
			return false
		}
		digit := int(c - '0')

		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
		double = !double
	}

	return sum%10 == 0
}
